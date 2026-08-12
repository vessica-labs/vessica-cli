package state

import (
	"context"
	stderrors "errors"
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
	mcpActionKey := unique + "-mcp-action"
	mcpClaim, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "postgres-user", Tool: "conversation_send", IdempotencyKey: mcpActionKey}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	conversation, message, _, err := db.SendConversationMessageIdempotent(ctx, mcpActionKey, "postgres-user", "", "Postgres", ConversationMessageInput{Role: "user", ContentJSON: `{"text":"once"}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE action_ledger SET lease_until=? WHERE id=?`, FormatTime(time.Now().Add(-time.Minute)), mcpClaim.Ledger.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "postgres-user", Tool: "conversation_send", IdempotencyKey: mcpActionKey}, time.Minute)
	if err != nil || !reclaimed.Acquired {
		t.Fatalf("Postgres action reclaim=%#v err=%v", reclaimed, err)
	}
	conversationReplay, messageReplay, replay, err := db.SendConversationMessageIdempotent(ctx, mcpActionKey, "postgres-user", "", "Postgres", ConversationMessageInput{Role: "user", ContentJSON: `{"text":"once"}`})
	if err != nil || !replay || conversationReplay.ID != conversation.ID || messageReplay.ID != message.ID {
		t.Fatalf("Postgres domain replay conversation=%#v message=%#v replay=%v err=%v", conversationReplay, messageReplay, replay, err)
	}

	boundAgent, err := db.CreateAgent(ctx, unique+"-BOUND", "Postgres bound agent", testDefinition, `{}`, 1_000_000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	agentActionKey, agentArgs := unique+"-agent-action", `{"agent":"`+boundAgent.ID+`","mode":"one"}`
	agentClaim, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "postgres-user", Tool: "agent_run", IdempotencyKey: agentActionKey, RedactedArgumentsJSON: agentArgs}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	firstAgentTrigger, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: boundAgent.ID, IdempotencyKey: agentActionKey, Trigger: "mcp", InputJSON: `{"mode":"one"}`})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteActionExecution(ctx, agentClaim.Ledger.ID, "bad-fence", `{}`, 0); err == nil {
		t.Fatal("Postgres simulated agent finalize failure succeeded")
	}
	if _, err = db.Exec(ctx, `UPDATE action_ledger SET lease_until=? WHERE id=?`, FormatTime(time.Now().Add(-time.Minute)), agentClaim.Ledger.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "postgres-user", Tool: "agent_run", IdempotencyKey: agentActionKey, RedactedArgumentsJSON: `{"agent":"` + boundAgent.ID + `","mode":"changed"}`}, time.Minute); !stderrors.Is(err, ErrActionIdempotencyConflict) {
		t.Fatalf("Postgres changed agent args err=%v", err)
	}
	if reclaimed, reclaimErr := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "postgres-user", Tool: "agent_run", IdempotencyKey: agentActionKey, RedactedArgumentsJSON: agentArgs}, time.Minute); reclaimErr != nil || !reclaimed.Acquired {
		t.Fatalf("Postgres agent reclaim=%#v err=%v", reclaimed, reclaimErr)
	}
	secondAgentTrigger, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: boundAgent.ID, IdempotencyKey: agentActionKey, Trigger: "mcp", InputJSON: `{"mode":"one"}`})
	if err != nil || secondAgentTrigger.AgentRunID != firstAgentTrigger.AgentRunID {
		t.Fatalf("Postgres agent replay=%#v err=%v", secondAgentTrigger, err)
	}

	subscriptionActionKey, subscriptionArgs := unique+"-subscription-action", `{"source_key":"postgres-source","source_url":"https://one.test"}`
	subscriptionClaim, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "postgres-user", Tool: "subscription_upsert", IdempotencyKey: subscriptionActionKey, RedactedArgumentsJSON: subscriptionArgs}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	firstSubscription, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: unique + "-source", SourceURL: "https://one.test", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteActionExecution(ctx, subscriptionClaim.Ledger.ID, "bad-fence", `{}`, 0); err == nil {
		t.Fatal("Postgres simulated subscription finalize failure succeeded")
	}
	if _, err = db.Exec(ctx, `UPDATE action_ledger SET lease_until=? WHERE id=?`, FormatTime(time.Now().Add(-time.Minute)), subscriptionClaim.Ledger.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "postgres-user", Tool: "subscription_upsert", IdempotencyKey: subscriptionActionKey, RedactedArgumentsJSON: `{"source_key":"postgres-source","source_url":"https://changed.test"}`}, time.Minute); !stderrors.Is(err, ErrActionIdempotencyConflict) {
		t.Fatalf("Postgres changed subscription args err=%v", err)
	}
	if reclaimed, reclaimErr := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "postgres-user", Tool: "subscription_upsert", IdempotencyKey: subscriptionActionKey, RedactedArgumentsJSON: subscriptionArgs}, time.Minute); reclaimErr != nil || !reclaimed.Acquired {
		t.Fatalf("Postgres subscription reclaim=%#v err=%v", reclaimed, reclaimErr)
	}
	secondSubscription, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: unique + "-source", SourceURL: "https://one.test", Status: "active"})
	if err != nil || secondSubscription.ID != firstSubscription.ID || secondSubscription.SourceURL != "https://one.test" {
		t.Fatalf("Postgres subscription replay=%#v err=%v", secondSubscription, err)
	}

	emailExpected, calendarExpected := "", ""
	if checkpoint, checkpointErr := db.GetSourceCheckpoint(ctx, "outlook_email", "outlook"); checkpointErr == nil {
		emailExpected = checkpoint.CheckpointValue
	}
	if checkpoint, checkpointErr := db.GetSourceCheckpoint(ctx, "outlook_calendar", "outlook"); checkpointErr == nil {
		calendarExpected = checkpoint.CheckpointValue
	}
	candidate := FormatTime(time.Now().Add(time.Hour))
	reservationInput := OutlookIngestionBatchInput{IdempotencyKey: unique + "-outlook", SubmittedBy: "postgres-user"}
	reserved, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, reservationInput, emailExpected, candidate, calendarExpected, candidate)
	if err != nil || reserved.ReservationToken == "" {
		t.Fatalf("Postgres reserve=%#v err=%v", reserved, err)
	}
	if _, _, _, err = db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: reserved.ID, SourceID: unique + "-first", NormalizedJSON: `{}`}, unique+"-processing"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: reserved.ID, SourceID: unique + "-rollback", NormalizedJSON: `{}`}, unique+"-processing"); err == nil {
		t.Fatal("Postgres atomic outbox conflict succeeded")
	}
	var rolledBack int
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_items WHERE source_id=?`, unique+"-rollback").Scan(&rolledBack); err != nil || rolledBack != 0 {
		t.Fatalf("Postgres rollback residue=%d err=%v", rolledBack, err)
	}
	if _, err = db.Exec(ctx, `UPDATE source_checkpoint_reservations SET lease_until=? WHERE batch_id=?`, FormatTime(time.Now().Add(-time.Minute)), reserved.ID); err != nil {
		t.Fatal(err)
	}
	reclaimedReservation, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, reservationInput, emailExpected, candidate, calendarExpected, candidate)
	if err != nil || reclaimedReservation.ReservationToken == "" || reclaimedReservation.ReservationToken == reserved.ReservationToken {
		t.Fatalf("Postgres reservation reclaim=%#v err=%v", reclaimedReservation, err)
	}
	if err = db.ReleaseOutlookCheckpointReservation(ctx, reserved.ID, reserved.ReservationToken); err == nil {
		t.Fatal("Postgres stale reservation fence released")
	}
	if err = db.ReleaseOutlookCheckpointReservation(ctx, reserved.ID, reclaimedReservation.ReservationToken); err != nil {
		t.Fatal(err)
	}
}
