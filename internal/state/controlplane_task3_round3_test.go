package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

func expireActionClaim(t *testing.T, db *DB, ledgerID string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE action_ledger SET lease_until=? WHERE id=?`, FormatTime(time.Now().Add(-time.Minute)), ledgerID); err != nil {
		t.Fatal(err)
	}
}

func TestAgentRunActionBindsArgumentsAcrossFinalizeFailureAndReacquire(t *testing.T) {
	db, ctx := agentTestDB(t), context.Background()
	agent, err := db.CreateAgent(ctx, "BOUND", "bound", `{}`, `{}`, 1, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	key, arguments := "agent-action", `{"agent_id":"`+agent.ID+`","input":{"mode":"a"}}`
	claim, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "agent_run", IdempotencyKey: key, RedactedArgumentsJSON: arguments}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: key, Trigger: "mcp", InputJSON: `{"mode":"a"}`})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteActionExecution(ctx, claim.Ledger.ID, "wrong-token", `{}`, 0); err == nil {
		t.Fatal("simulated finalize failure succeeded")
	}
	expireActionClaim(t, db, claim.Ledger.ID)
	if _, err = db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "agent_run", IdempotencyKey: key, RedactedArgumentsJSON: `{"agent_id":"` + agent.ID + `","input":{"mode":"changed"}}`}, time.Minute); !errors.Is(err, ErrActionIdempotencyConflict) {
		t.Fatalf("changed arguments err=%v", err)
	}
	reclaimed, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "agent_run", IdempotencyKey: key, RedactedArgumentsJSON: arguments}, time.Minute)
	if err != nil || !reclaimed.Acquired {
		t.Fatalf("reclaim=%#v err=%v", reclaimed, err)
	}
	replay, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: key, Trigger: "mcp", InputJSON: `{"mode":"a"}`})
	if err != nil || replay.AgentRunID != first.AgentRunID {
		t.Fatalf("replay=%#v err=%v", replay, err)
	}
	var runs int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runs WHERE agent_id=?`, agent.ID).Scan(&runs)
	if runs != 1 {
		t.Fatalf("agent runs=%d", runs)
	}
}

func TestSubscriptionActionsBindArgumentsAcrossFinalizeFailureAndReacquire(t *testing.T) {
	db, ctx := agentTestDB(t), context.Background()
	upsertKey, args := "subscription-upsert", `{"source_key":"one","source_url":"https://one.test"}`
	claim, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "subscription_upsert", IdempotencyKey: upsertKey, RedactedArgumentsJSON: args}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	one, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "one", SourceURL: "https://one.test", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.CompleteActionExecution(ctx, claim.Ledger.ID, "wrong-token", `{}`, 0); err == nil {
		t.Fatal("simulated finalize failure succeeded")
	}
	expireActionClaim(t, db, claim.Ledger.ID)
	if _, err = db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "subscription_upsert", IdempotencyKey: upsertKey, RedactedArgumentsJSON: `{"source_key":"one","source_url":"https://changed.test"}`}, time.Minute); !errors.Is(err, ErrActionIdempotencyConflict) {
		t.Fatalf("changed upsert err=%v", err)
	}
	if reclaimed, reclaimErr := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "subscription_upsert", IdempotencyKey: upsertKey, RedactedArgumentsJSON: args}, time.Minute); reclaimErr != nil || !reclaimed.Acquired {
		t.Fatalf("upsert reclaim=%#v err=%v", reclaimed, reclaimErr)
	}
	replayed, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "one", SourceURL: "https://one.test", Status: "active"})
	if err != nil || replayed.ID != one.ID || replayed.SourceURL != "https://one.test" {
		t.Fatalf("upsert replay=%#v err=%v", replayed, err)
	}

	two, _ := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "two", SourceURL: "https://two.test", Status: "active"})
	disableKey, disableArgs := "subscription-disable", `{"subscription_id":"`+one.ID+`"}`
	disableClaim, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "subscription_disable", IdempotencyKey: disableKey, RedactedArgumentsJSON: disableArgs}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DisableNewsletterSubscription(ctx, one.ID); err != nil {
		t.Fatal(err)
	}
	expireActionClaim(t, db, disableClaim.Ledger.ID)
	if _, err = db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "subscription_disable", IdempotencyKey: disableKey, RedactedArgumentsJSON: `{"subscription_id":"` + two.ID + `"}`}, time.Minute); !errors.Is(err, ErrActionIdempotencyConflict) {
		t.Fatalf("changed disable err=%v", err)
	}
	if reclaimed, reclaimErr := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "subscription_disable", IdempotencyKey: disableKey, RedactedArgumentsJSON: disableArgs}, time.Minute); reclaimErr != nil || !reclaimed.Acquired {
		t.Fatalf("disable reclaim=%#v err=%v", reclaimed, reclaimErr)
	}
	if disabled, disableErr := db.DisableNewsletterSubscription(ctx, one.ID); disableErr != nil || disabled.Status != "disabled" {
		t.Fatalf("disable replay=%#v err=%v", disabled, disableErr)
	}
	if refreshed, getErr := db.ListNewsletterSubscriptions(ctx); getErr != nil || len(refreshed) != 2 || refreshed[1].Status != "active" {
		t.Fatalf("subscriptions=%#v err=%v", refreshed, getErr)
	}
}

func TestOutlookItemAndOutboxCommitOrRollbackTogether(t *testing.T) {
	db, ctx := agentTestDB(t), context.Background()
	batch, _ := db.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "atomic-v3", SubmittedBy: "user"})
	first, _, duplicate, err := db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: batch.ID, SourceID: "first", NormalizedJSON: `{}`}, "processing-key")
	if err != nil || duplicate || first == nil {
		t.Fatalf("first=%#v duplicate=%v err=%v", first, duplicate, err)
	}
	if _, _, _, err = db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: batch.ID, SourceID: "must-rollback", NormalizedJSON: `{}`}, "processing-key"); err == nil {
		t.Fatal("outbox conflict succeeded")
	}
	var orphan, itemCount, outboxCount int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_items WHERE source_id='must-rollback'`).Scan(&orphan)
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_items`).Scan(&itemCount)
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_outbox`).Scan(&outboxCount)
	if orphan != 0 || itemCount != outboxCount {
		t.Fatalf("orphan=%d items=%d outbox=%d", orphan, itemCount, outboxCount)
	}
}

func TestOutlookReservationCrashReclaimIsLeasedAndFenced(t *testing.T) {
	db, ctx := agentTestDB(t), context.Background()
	candidate := "2026-08-11T08:00:00-07:00"
	in := OutlookIngestionBatchInput{IdempotencyKey: "crash-reclaim", SubmittedBy: "user"}
	first, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, in, "", candidate, "", candidate, time.Millisecond)
	if err != nil || first.ReservationToken == "" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	if _, err = db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "blocked", SubmittedBy: "user"}, "", candidate, "", candidate); err == nil {
		t.Fatal("active reservation did not block competing batch")
	}
	if _, _, _, err = db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: first.ID, SourceID: "message", NormalizedJSON: `{}`}, "message-key"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE source_checkpoint_reservations SET lease_until=? WHERE batch_id=?`, FormatTime(time.Now().Add(-time.Minute)), first.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, in, "", candidate, "", candidate)
	if err != nil || reclaimed.ReservationToken == "" || reclaimed.ReservationToken == first.ReservationToken {
		t.Fatalf("reclaimed=%#v err=%v", reclaimed, err)
	}
	if _, _, duplicate, err := db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: first.ID, SourceID: "message", NormalizedJSON: `{}`}, "ignored-on-dedupe"); err != nil || !duplicate {
		t.Fatalf("duplicate=%v err=%v", duplicate, err)
	}
	if err = db.FinalizeOutlookIngestionBatch(ctx, first.ID, "", candidate, `{}`, "", candidate, `{}`, first.ReservationToken); err == nil {
		t.Fatal("expired reservation token finalized")
	}
	if err = db.ReleaseOutlookCheckpointReservation(ctx, first.ID, first.ReservationToken); err == nil {
		t.Fatal("expired token released reclaimed reservation")
	}
	if err = db.FinalizeOutlookIngestionBatch(ctx, first.ID, "", candidate, `{}`, "", candidate, `{}`, reclaimed.ReservationToken); err != nil {
		t.Fatal(err)
	}
	var items, outbox int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_items`).Scan(&items)
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_outbox`).Scan(&outbox)
	if items != 1 || outbox != 1 {
		t.Fatalf("items=%d outbox=%d", items, outbox)
	}
}
