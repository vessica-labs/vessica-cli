package state

import (
	"context"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestClaimLeaseAndWaves(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "E", "body")
	if err != nil {
		t.Fatal(err)
	}
	t1, err := db.CreateTicket(ctx, epic.ID, "feature", "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := db.CreateTicket(ctx, epic.ID, "feature", "B", "", []string{t1.ID})
	if err != nil {
		t.Fatal(err)
	}
	waves, err := db.ComputeWaves(ctx, epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(waves) < 2 {
		t.Fatalf("expected >=2 waves, got %d", len(waves))
	}
	ready, err := db.ReadyTickets(ctx, epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].ID != t1.ID {
		t.Fatalf("ready=%v", ready)
	}
	claim, _, err := db.ClaimTicket(ctx, t1.ID, "agent_1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.AgentID != "agent_1" {
		t.Fatal(claim)
	}
	_, _, err = db.ClaimTicket(ctx, t1.ID, "agent_2", time.Minute)
	if err == nil {
		t.Fatal("expected claim conflict")
	}
	run, err := db.CreateRun(ctx, epic.ID, "", "codex", "gpt-5.6-terra", "high", "docker", 1, false, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}
	rcpt, err := db.CreateReceipt(ctx, run.ID, epic.ID, "ok", map[string]any{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CloseTicket(ctx, t1.ID, "agent_1", rcpt.ID); err != nil {
		t.Fatal(err)
	}
	ready, err = db.ReadyTickets(ctx, epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].ID != t2.ID {
		t.Fatalf("expected t2 ready, got %#v", ready)
	}
}

func TestValidationFailureTicketIsIdempotentAndResolvedByPassingRetry(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "validation.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, _ := db.CreateEpic(ctx, "Validation", "body")
	first, err := db.CreateTicketWithMeta(ctx, epic.ID, "bug", "Failed", "body", nil, "run-1", "step_1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateTicketWithMeta(ctx, epic.ID, "bug", "Failed again", "body", nil, "run-1", "step_1")
	if err != nil || second.ID != first.ID {
		t.Fatalf("first=%s second=%#v err=%v", first.ID, second, err)
	}
	if count, err := db.ResolveValidationFailure(ctx, "run-1", "step_1"); err != nil || count != 1 {
		t.Fatalf("resolved=%d err=%v", count, err)
	}
	resolved, _ := db.GetTicket(ctx, first.ID)
	if resolved.Status != "closed" {
		t.Fatalf("status=%s", resolved.Status)
	}
}

func TestRunScopedWavesDoNotIncludePriorRunTickets(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "E", "body")
	if err != nil {
		t.Fatal(err)
	}
	run1, err := db.CreateRun(ctx, epic.ID, "", "codex", "gpt-5.6-terra", "high", "docker", 1, false, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}
	oldTicket, err := db.CreateTicketForRun(ctx, epic.ID, run1.ID, "feature", "Add scheduler", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.ClaimTicket(ctx, oldTicket.ID, "agent_old", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := db.HeartbeatClaim(ctx, oldTicket.ID, "agent_old", time.Hour); err != nil {
		t.Fatal(err)
	}

	run2, err := db.CreateRun(ctx, epic.ID, "", "codex", "gpt-5.6-terra", "high", "docker", 1, false, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}
	newTicket, err := db.CreateTicketForRun(ctx, epic.ID, run2.ID, "feature", "Add scheduler", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	waves, err := db.ComputeWavesForRun(ctx, epic.ID, run2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(waves) != 1 || waves[0].SourceRunID != run2.ID {
		t.Fatalf("waves=%#v", waves)
	}
	run2Tickets, err := db.ListTicketsForRun(ctx, epic.ID, run2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(run2Tickets) != 1 || run2Tickets[0].ID != newTicket.ID || run2Tickets[0].WaveID != waves[0].ID {
		t.Fatalf("run2 tickets=%#v", run2Tickets)
	}
	old, err := db.GetTicket(ctx, oldTicket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != "in_progress" || old.SourceRunID != run1.ID || old.WaveID != "" {
		t.Fatalf("old ticket crossed run boundary: %#v", old)
	}
}

func TestIdempotencyAndCascade(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "E", "body")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, epic.ID, "feature", "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutIdempotency(ctx, "k1", ticket); err != nil {
		t.Fatal(err)
	}
	if raw, ok, err := db.GetIdempotency(ctx, "k1"); err != nil || !ok || len(raw) == 0 {
		t.Fatalf("idempotency raw=%s ok=%v err=%v", string(raw), ok, err)
	}

	if err := db.DeleteEpic(ctx, epic.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetTicket(ctx, ticket.ID); err == nil {
		t.Fatal("expected ticket to cascade delete with epic")
	}
}

func TestAppendEventSequences(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "E", "body")
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.CreateRun(ctx, epic.ID, "", "codex", "gpt-5.6-terra", "high", "docker", 1, false, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		ev, err := db.AppendEvent(ctx, run.ID, "", "test", map[string]any{"i": i})
		if err != nil {
			t.Fatal(err)
		}
		if ev.Seq != int64(i) {
			t.Fatalf("seq=%d want %d", ev.Seq, i)
		}
	}
}

func TestAppendEventSequencesAreAtomic(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "E", "body")
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.CreateRun(ctx, epic.ID, "", "codex", "gpt-5.6-terra", "high", "docker", 1, false, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}

	const count = 24
	sequences := make(chan int64, count)
	errors := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			event, err := db.AppendEvent(ctx, run.ID, "", "concurrent", map[string]any{"i": i})
			if err != nil {
				errors <- err
				return
			}
			sequences <- event.Seq
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("append concurrent event: %v", err)
	}
	close(sequences)
	var got []int
	for sequence := range sequences {
		got = append(got, int(sequence))
	}
	sort.Ints(got)
	for i, sequence := range got {
		if sequence != i+1 {
			t.Fatalf("sequences=%v", got)
		}
	}
}

func TestConcurrentTicketClaimHasOneWinner(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "E", "body")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := db.CreateTicket(ctx, epic.ID, "feature", "A", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, agent := range []string{"agent_1", "agent_2"} {
		wg.Add(1)
		go func(agent string) {
			defer wg.Done()
			<-start
			_, _, err := db.ClaimTicket(ctx, ticket.ID, agent, time.Minute)
			results <- err
		}(agent)
	}
	close(start)
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners=%d, want 1", winners)
	}
}
