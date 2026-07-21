package state

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestPostgresHostedSchema(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	unique := fmt.Sprintf("postgres-%d", time.Now().UnixNano())
	db, err := Open("postgres-url", url, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(ctx, "postgres-integration", "hosted"); err != nil {
		t.Fatal(err)
	}
	integration, err := db.UpsertTrackerIntegration(ctx, "linear", "connected", map[string]string{"team": "one"}, "", "secret")
	if err != nil {
		t.Fatal(err)
	}
	_, job, duplicate, err := db.ReceiveWebhook(ctx, integration, unique+"-delivery", "Issue", []byte(`{"type":"Issue"}`))
	if err != nil || duplicate || job == nil {
		t.Fatalf("job=%#v duplicate=%v err=%v", job, duplicate, err)
	}
	claimed, err := db.ClaimJob(ctx, "postgres-test", 0)
	if err != nil || claimed == nil {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	user, err := db.UpsertDashboardUser(ctx, "12345", "VessicaMember", "Vessica Member", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.UpsertMembership(ctx, user.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err = db.CreateDashboardSession(ctx, user.ID, "owner", unique+"-session", unique+"-csrf", expires); err != nil {
		t.Fatal(err)
	}
	invitation, err := db.CreateInvitation(ctx, "CaseSensitiveLogin", "member", unique+"-invitation", expires, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := db.UpsertDashboardUser(ctx, "67890", "casesensitivelogin", "Member", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AcceptInvitation(ctx, invitation.TokenHash, "CASESENSITIVELOGIN", member.ID); err != nil {
		t.Fatal(err)
	}
	operation, err := db.CreateHostingOperation(ctx, "railway_promotion", unique+"-operation", user.ID, map[string]any{"preview_origin": "https://preview.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AppendHostingOperationEvent(ctx, operation.ID, "verify", "running", "verifying", nil); err != nil {
		t.Fatal(err)
	}

	epic, err := db.CreateEpic(ctx, "Postgres concurrency", "atomic state transitions")
	if err != nil {
		t.Fatal(err)
	}
	runRecord, err := db.CreateRun(ctx, epic.ID, "", "codex", "test", "high", "railway", 1, false, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}
	const eventCount = 32
	sequences := make(chan int64, eventCount)
	errors := make(chan error, eventCount)
	var wg sync.WaitGroup
	for i := 0; i < eventCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			event, appendErr := db.AppendEvent(ctx, runRecord.ID, "", "postgres.concurrent", map[string]any{"i": i})
			if appendErr != nil {
				errors <- appendErr
				return
			}
			sequences <- event.Seq
		}(i)
	}
	wg.Wait()
	close(errors)
	for appendErr := range errors {
		t.Fatalf("append concurrent Postgres event: %v", appendErr)
	}
	close(sequences)
	var got []int
	for sequence := range sequences {
		got = append(got, int(sequence))
	}
	sort.Ints(got)
	for i, sequence := range got {
		if sequence != i+1 {
			t.Fatalf("Postgres sequences=%v", got)
		}
	}

	ticket, err := db.CreateTicket(ctx, epic.ID, "feature", "Atomic claim", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimStart := make(chan struct{})
	claimResults := make(chan error, 2)
	for _, agent := range []string{"postgres-agent-1", "postgres-agent-2"} {
		wg.Add(1)
		go func(agent string) {
			defer wg.Done()
			<-claimStart
			_, _, claimErr := db.ClaimTicket(ctx, ticket.ID, agent, time.Minute)
			claimResults <- claimErr
		}(agent)
	}
	close(claimStart)
	wg.Wait()
	close(claimResults)
	winners := 0
	for claimErr := range claimResults {
		if claimErr == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("Postgres claim winners=%d, want 1", winners)
	}

	agentName := fmt.Sprintf("PGAGENT-%d", time.Now().UnixNano())
	agent, err := db.CreateAgent(ctx, agentName, "Postgres agent concurrency", testDefinition, `{"source":"postgres-test"}`, 1_000_000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	admissionStart := make(chan struct{})
	admissionStatuses := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-admissionStart
			run, runErr := db.CreateAgentRun(ctx, agent.ID, "manual", `{"prompt":"postgres"}`, "", "", nil)
			if runErr != nil {
				admissionStatuses <- "error"
				return
			}
			admissionStatuses <- run.Status
		}()
	}
	close(admissionStart)
	wg.Wait()
	close(admissionStatuses)
	admissions := map[string]int{}
	for status := range admissionStatuses {
		admissions[status]++
	}
	if admissions["queued"] != 1 || admissions["budget_blocked"] != 1 {
		t.Fatalf("Postgres agent admissions=%v", admissions)
	}
	runs, err := db.ListAgentRuns(ctx, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var queued *AgentRun
	for i := range runs {
		if runs[i].Status == "queued" {
			queued = &runs[i]
			break
		}
	}
	if queued == nil {
		t.Fatal("missing admitted Postgres agent run")
	}
	task, _, err := db.ClaimAgentRuntimeTaskForRun(ctx, queued.ID, "postgres-runtime", time.Minute)
	if err != nil || task == nil {
		t.Fatalf("claim Postgres agent task=%#v err=%v", task, err)
	}
	if err = db.AppendAgentRunEvents(ctx, queued.ID, task.FenceToken, []AgentRunEvent{{AttemptOrdinal: 1, Type: "agent.run.started", PayloadJSON: `{}`}}); err != nil {
		t.Fatal(err)
	}
	if err = db.BeginAgentToolCall(ctx, queued.ID, task.FenceToken, 1, "artifact.create", "postgres-hash"); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteAgentToolCall(ctx, queued.ID, task.FenceToken, 1, "artifact.create", "postgres-hash", `{"id":"art_pg"}`); err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteAgentRun(ctx, queued.ID, task.FenceToken, `{"ok":true}`, `{"total_tokens":20}`, 200_000); err != nil {
		t.Fatal(err)
	}
	if released, releaseErr := db.ReleaseBudgetBlockedRuns(ctx, agent.ID); releaseErr != nil || released != 1 {
		t.Fatalf("release Postgres budget-blocked runs=%d err=%v", released, releaseErr)
	}
	if _, err = db.AddAgentVersion(ctx, agent.ID, "updated", testDefinition, `{"source":"postgres-test"}`); err != nil {
		t.Fatal(err)
	}
	pinned, err := db.GetAgentRun(ctx, queued.ID)
	if err != nil || pinned.DefinitionVersion != 1 {
		t.Fatalf("Postgres pinned version=%d err=%v", pinned.DefinitionVersion, err)
	}
}
