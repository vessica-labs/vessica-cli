package state

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func agentTestDB(t *testing.T) *DB {
	t.Helper()
	root := t.TempDir()
	db, err := Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.EnsureWorkspace(context.Background(), root, "hosted"); err != nil {
		t.Fatal(err)
	}
	return db
}

const testDefinition = `{"kind":"vessica.agent/v1","name":"TEST","purpose":"test","system_prompt":"help","model":{"id":"gpt-5.6-terra","reasoning_effort":"medium"},"tools":[],"knowledge":[],"budget":{"daily_usd":"1.00","timezone":"UTC"}}`

func TestAgentBudgetAdmissionIsAtomic(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	a, err := db.CreateAgent(ctx, "TEST", "test", testDefinition, "{}", 1_000_000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	statuses := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := db.CreateAgentRun(ctx, a.ID, "manual", `{"prompt":"x"}`, "", "", nil)
			if e != nil {
				statuses <- "error"
				return
			}
			statuses <- r.Status
		}()
	}
	wg.Wait()
	close(statuses)
	counts := map[string]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts["queued"] != 1 || counts["budget_blocked"] != 1 {
		t.Fatalf("statuses=%v", counts)
	}
	limit, reserved, spent, _, _, _, err := db.AgentBudget(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if limit != 1_000_000 || reserved != 1_000_000 || spent != 0 {
		t.Fatalf("budget limit=%d reserved=%d spent=%d", limit, reserved, spent)
	}
}

func TestAgentAttemptFenceRejectsExpiredWorker(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	a, _ := db.CreateAgent(ctx, "TEST", "test", testDefinition, "{}", 5_000_000, "UTC")
	run, _ := db.CreateAgentRun(ctx, a.ID, "manual", `{"prompt":"x"}`, "", "", nil)
	task, _, err := db.ClaimAgentRuntimeTask(ctx, "worker-1", time.Minute)
	if err != nil || task == nil {
		t.Fatalf("claim=%v err=%v", task, err)
	}
	_, _ = db.Exec(ctx, `UPDATE agent_runtime_tasks SET lease_until=? WHERE id=?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), task.ID)
	_, _ = db.Exec(ctx, `UPDATE agent_run_attempts SET lease_until=? WHERE run_id=?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), run.ID)
	task2, _, err := db.ClaimAgentRuntimeTask(ctx, "worker-2", time.Minute)
	if err != nil || task2 == nil {
		t.Fatalf("reclaim=%v err=%v", task2, err)
	}
	err = db.AppendAgentRunEvents(ctx, run.ID, task.FenceToken, []AgentRunEvent{{AttemptOrdinal: 1, Type: "agent.message.delta", PayloadJSON: `{"text":"stale"}`}})
	if err != ErrAgentFenceLost {
		t.Fatalf("old fence error=%v", err)
	}
	if err = db.AppendAgentRunEvents(ctx, run.ID, task2.FenceToken, []AgentRunEvent{{AttemptOrdinal: 1, Type: "agent.message.delta", PayloadJSON: `{"text":"fresh"}`}}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentRunPinsDefinitionVersion(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	a, _ := db.CreateAgent(ctx, "TEST", "test", testDefinition, "{}", 5_000_000, "UTC")
	run, err := db.CreateAgentRun(ctx, a.ID, "manual", `{"prompt":"x"}`, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddAgentVersion(ctx, a.ID, "updated", testDefinition, `{"source":"test"}`); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetAgentRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefinitionVersion != 1 {
		t.Fatalf("pinned version=%d", got.DefinitionVersion)
	}
	if got.RateSnapshotJSON == "{}" {
		t.Fatal("run did not pin the default model rate snapshot")
	}
}

func TestAgentFailureRetriesWithoutReleasingReservation(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	agent, _ := db.CreateAgent(ctx, "TEST", "test", testDefinition, "{}", 5_000_000, "UTC")
	run, _ := db.CreateAgentRun(ctx, agent.ID, "manual", `{"prompt":"x"}`, "", "", nil)
	task, _, err := db.ClaimAgentRuntimeTask(ctx, "worker-1", time.Minute)
	if err != nil || task == nil {
		t.Fatalf("claim=%v err=%v", task, err)
	}
	if err = db.FailAgentRun(ctx, run.ID, task.FenceToken, "temporary", `{"total_tokens":10}`, 125_000); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetAgentRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "queued" || got.ReservationMicroUSD != run.ReservationMicroUSD {
		t.Fatalf("run status=%s reservation=%d", got.Status, got.ReservationMicroUSD)
	}
	_, reserved, spent, _, _, _, err := db.AgentBudget(ctx, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reserved != run.ReservationMicroUSD || spent != 125_000 {
		t.Fatalf("reserved=%d spent=%d", reserved, spent)
	}
	var status string
	var attempts int
	if err = db.QueryRow(ctx, `SELECT status,attempts FROM agent_runtime_tasks WHERE subject_id=?`, run.ID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || attempts != 1 {
		t.Fatalf("task status=%s attempts=%d", status, attempts)
	}
}

func TestAgentToolResultReplaysAcrossAttempts(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	agent, _ := db.CreateAgent(ctx, "TEST", "test", testDefinition, "{}", 5_000_000, "UTC")
	run, _ := db.CreateAgentRun(ctx, agent.ID, "manual", `{"prompt":"x"}`, "", "", nil)
	first, _, _ := db.ClaimAgentRuntimeTask(ctx, "worker-1", time.Minute)
	if err := db.BeginAgentToolCall(ctx, run.ID, first.FenceToken, 1, "artifact.create", "hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteAgentToolCall(ctx, run.ID, first.FenceToken, 1, "artifact.create", "hash", `{"id":"art_1"}`); err != nil {
		t.Fatal(err)
	}
	if err := db.FailAgentRun(ctx, run.ID, first.FenceToken, "retry", `{}`, 0); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(ctx, `UPDATE agent_runtime_tasks SET available_at=? WHERE subject_id=?`, Now(), run.ID)
	second, _, err := db.ClaimAgentRuntimeTask(ctx, "worker-2", time.Minute)
	if err != nil || second == nil {
		t.Fatalf("second claim=%v err=%v", second, err)
	}
	result, replayed, err := db.ReplayAgentToolCall(ctx, run.ID, second.FenceToken, 1, "artifact.create", "hash")
	if err != nil || !replayed || result != `{"id":"art_1"}` {
		t.Fatalf("result=%q replayed=%v err=%v", result, replayed, err)
	}
	if _, _, err = db.ReplayAgentToolCall(ctx, run.ID, second.FenceToken, 1, "artifact.create", "different"); err == nil {
		t.Fatal("expected replay mismatch")
	}
}

func TestAgentToolReplayRefusesUnknownOutcome(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	agent, _ := db.CreateAgent(ctx, "TEST", "test", testDefinition, "{}", 5_000_000, "UTC")
	run, _ := db.CreateAgentRun(ctx, agent.ID, "manual", `{"prompt":"x"}`, "", "", nil)
	first, _, _ := db.ClaimAgentRuntimeTask(ctx, "worker-1", time.Minute)
	if err := db.BeginAgentToolCall(ctx, run.ID, first.FenceToken, 1, "artifact.create", "hash"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.ReplayAgentToolCall(ctx, run.ID, first.FenceToken, 1, "artifact.create", "hash"); err == nil {
		t.Fatal("expected unsafe replay to be rejected")
	}
}
