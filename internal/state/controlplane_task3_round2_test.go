package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceScopedAgentReadsRejectForeignIDsAndEnumeration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "shared.db")
	ctx := context.Background()
	primary, err := Open("sqlite", path, root)
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	one, _ := primary.EnsureWorkspace(ctx, "one", "hosted")
	oneAgent, _ := primary.CreateAgent(ctx, "ONE", "one", `{}`, `{}`, 1, "UTC")
	oneTrigger, _ := primary.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: oneAgent.ID, IdempotencyKey: "one-run", Trigger: "mcp"})

	foreign, err := Open("sqlite", path, root)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	two, _ := foreign.EnsureWorkspace(ctx, "two", "hosted")
	twoAgent, _ := foreign.CreateAgent(ctx, "TWO", "two", `{}`, `{}`, 1, "UTC")
	twoTrigger, _ := foreign.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: twoAgent.ID, IdempotencyKey: "two-run", Trigger: "mcp"})

	agents, err := primary.ListAgentsForWorkspace(ctx, one.ID)
	if err != nil || len(agents) != 1 || agents[0].ID != oneAgent.ID {
		t.Fatalf("scoped agents=%#v err=%v", agents, err)
	}
	if _, err = primary.GetAgentForWorkspace(ctx, one.ID, twoAgent.ID); err == nil {
		t.Fatal("foreign agent direct ID was readable")
	}
	runs, err := primary.ListAgentRunsForWorkspace(ctx, one.ID, "")
	if err != nil || len(runs) != 1 || runs[0].ID != oneTrigger.AgentRunID {
		t.Fatalf("scoped runs=%#v err=%v", runs, err)
	}
	if _, err = primary.GetAgentRunForWorkspace(ctx, one.ID, twoTrigger.AgentRunID); err == nil {
		t.Fatalf("foreign run from workspace %s was readable in %s", two.ID, one.ID)
	}
}

func TestConversationActionIdempotencySurvivesAuditFinalizeFailureAndLeaseExpiry(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	actionKey := "mcp_action_hash"
	claim, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "conversation_send", IdempotencyKey: actionKey}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	conversation, message, replay, err := db.SendConversationMessageIdempotent(ctx, actionKey, "user", "", "Title", ConversationMessageInput{Role: "user", ContentJSON: `{"text":"hello"}`})
	if err != nil || replay {
		t.Fatalf("first send conversation=%#v message=%#v replay=%v err=%v", conversation, message, replay, err)
	}
	// Simulate a durable domain commit followed by an unavailable audit finalize.
	if err = db.CompleteActionExecution(ctx, claim.Ledger.ID, "wrong-finalize-token", `{"ok":true}`, 1); err == nil {
		t.Fatal("simulated audit finalize failure unexpectedly succeeded")
	}
	if _, err = db.Exec(ctx, `UPDATE action_ledger SET lease_until=? WHERE id=?`, FormatTime(time.Now().Add(-time.Minute)), claim.Ledger.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := db.ClaimActionExecution(ctx, ActionLedgerInput{ActorID: "user", Tool: "conversation_send", IdempotencyKey: actionKey}, time.Minute)
	if err != nil || !reclaimed.Acquired {
		t.Fatalf("reclaim=%#v err=%v", reclaimed, err)
	}
	conversation2, message2, replay, err := db.SendConversationMessageIdempotent(ctx, actionKey, "user", "", "Title", ConversationMessageInput{Role: "user", ContentJSON: `{"text":"hello"}`})
	if err != nil || !replay || conversation2.ID != conversation.ID || message2.ID != message.ID {
		t.Fatalf("domain replay conversation=%#v message=%#v replay=%v err=%v", conversation2, message2, replay, err)
	}
	var conversations, messages int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM conversations`).Scan(&conversations)
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM conversation_messages`).Scan(&messages)
	if conversations != 1 || messages != 1 {
		t.Fatalf("duplicate side effects conversations=%d messages=%d", conversations, messages)
	}
}

func TestOutlookReservationRejectsStaleBatchBeforeAnyItems(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	current, next := "2026-08-11T08:00:00-07:00", "2026-08-11T09:00:00-07:00"
	seed, _ := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "seed", SubmittedBy: "user"}, "", current, "", current)
	if err := db.FinalizeOutlookIngestionBatch(ctx, seed.ID, "", current, `{}`, "", current, `{}`, seed.ReservationToken); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "stale", SubmittedBy: "user"}, "", next, current, next); err == nil {
		t.Fatal("stale checkpoint reservation succeeded")
	}
	var batches, items, outbox int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_batches WHERE idempotency_key='stale'`).Scan(&batches)
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_items`).Scan(&items)
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_outbox`).Scan(&outbox)
	if batches != 0 || items != 0 || outbox != 0 {
		t.Fatalf("stale residue batches=%d items=%d outbox=%d", batches, items, outbox)
	}
}

func TestOtherMCPWritePrimitivesAreDurablyIdempotent(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	agent, _ := db.CreateAgent(ctx, "IDEMPOTENT", "test", `{}`, `{}`, 1, "UTC")
	firstTrigger, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: "hashed-agent-key", Trigger: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	secondTrigger, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: "hashed-agent-key", Trigger: "mcp"})
	if err != nil || secondTrigger.AgentRunID != firstTrigger.AgentRunID {
		t.Fatalf("agent trigger replay=%#v err=%v", secondTrigger, err)
	}
	subscription, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "source", SourceURL: "https://example.test", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	replayedSubscription, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "source", SourceURL: "https://example.test", Status: "active"})
	if err != nil || replayedSubscription.ID != subscription.ID {
		t.Fatalf("subscription replay=%#v err=%v", replayedSubscription, err)
	}
	if _, err = db.DisableNewsletterSubscription(ctx, subscription.ID); err != nil {
		t.Fatal(err)
	}
	if disabled, disableErr := db.DisableNewsletterSubscription(ctx, subscription.ID); disableErr != nil || disabled.Status != "disabled" {
		t.Fatalf("disable replay=%#v err=%v", disabled, disableErr)
	}
	batchInput := OutlookIngestionBatchInput{IdempotencyKey: "hashed-batch-key", SubmittedBy: "user"}
	batch, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, batchInput, "", "2026-08-11T08:00:00-07:00", "", "2026-08-11T08:00:00-07:00")
	if err != nil {
		t.Fatal(err)
	}
	if replayedBatch, replayErr := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, batchInput, "", "2026-08-11T08:00:00-07:00", "", "2026-08-11T08:00:00-07:00"); !errors.Is(replayErr, ErrOutlookReservationAlreadyClaimed) || replayedBatch != nil {
		t.Fatalf("active batch replay=%#v err=%v", replayedBatch, replayErr)
	}
	itemInput := OutlookIngestionItemInput{BatchID: batch.ID, SourceID: "message", NormalizedJSON: `{}`}
	item, duplicate, err := db.UpsertOutlookIngestionItem(ctx, itemInput)
	if err != nil || duplicate {
		t.Fatalf("first item=%#v duplicate=%v err=%v", item, duplicate, err)
	}
	replayedItem, duplicate, err := db.UpsertOutlookIngestionItem(ctx, itemInput)
	if err != nil || !duplicate || replayedItem.ID != item.ID {
		t.Fatalf("item replay=%#v duplicate=%v err=%v", replayedItem, duplicate, err)
	}
	outbox, err := db.EnqueueOutlookOutbox(ctx, batch.ID, item.ID, "batch:message")
	if err != nil {
		t.Fatal(err)
	}
	replayedOutbox, err := db.EnqueueOutlookOutbox(ctx, batch.ID, item.ID, "batch:message")
	if err != nil || replayedOutbox.ID != outbox.ID {
		t.Fatalf("outbox replay=%#v err=%v", replayedOutbox, err)
	}
	checkpoint := "2026-08-11T08:00:00-07:00"
	if err = db.FinalizeOutlookIngestionBatch(ctx, batch.ID, "", checkpoint, `{}`, "", checkpoint, `{}`, batch.ReservationToken); err != nil {
		t.Fatal(err)
	}
	recoveredBatch, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, batchInput, "", checkpoint, "", checkpoint)
	if err != nil || recoveredBatch.ID != batch.ID {
		t.Fatalf("finalized batch recovery=%#v err=%v", recoveredBatch, err)
	}
	if err = db.FinalizeOutlookIngestionBatch(ctx, recoveredBatch.ID, "", checkpoint, `{}`, "", checkpoint, `{}`); err != nil {
		t.Fatalf("finalized batch was not idempotent: %v", err)
	}
}
