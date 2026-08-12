package state

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func reviewDBPair(t *testing.T) (*DB, *DB) {
	t.Helper()
	root := t.TempDir()
	first, err := Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if _, err = first.EnsureWorkspace(context.Background(), "workspace-one", "hosted"); err != nil {
		t.Fatal(err)
	}
	second, err := Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err = second.EnsureWorkspace(context.Background(), "workspace-two", "hosted"); err != nil {
		t.Fatal(err)
	}
	return first, second
}

// Break caught: any child record can be attached to a parent owned by a
// different workspace, leaking durable input across installations.
func TestControlPlaneChildrenRejectForeignWorkspaceParents(t *testing.T) {
	first, second := reviewDBPair(t)
	ctx := context.Background()
	subscription, err := first.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "one", SourceURL: "https://one.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.UpsertNewsletterItem(ctx, NewsletterItemInput{SubscriptionID: subscription.ID, SourceItemID: "foreign", NormalizedJSON: `{}`}); err == nil {
		t.Fatal("foreign newsletter subscription accepted")
	}
	conversation, err := first.CreateConversation(ctx, ConversationInput{ActorID: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.AppendConversationMessage(ctx, conversation.ID, ConversationMessageInput{Role: "user", ContentJSON: `{}`}); err == nil {
		t.Fatal("foreign conversation accepted")
	}
	batch, err := first.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "batch", SubmittedBy: "connector"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = second.UpsertOutlookIngestionItem(ctx, OutlookIngestionItemInput{BatchID: batch.ID, SourceID: "foreign", NormalizedJSON: `{}`}); err == nil {
		t.Fatal("foreign outlook batch accepted")
	}
	agent, err := first.CreateAgent(ctx, "FOREIGN", "test", testDefinition, "{}", 5_000_000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: "foreign", Trigger: "mcp"}); err == nil {
		t.Fatal("foreign agent accepted")
	}
}

// Break caught: simultaneous exchange of one authorization code mints two
// token grants instead of one atomic consumption.
func TestOAuthAuthorizationCodeCanBeConsumedOnceConcurrently(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	client, err := db.UpsertOAuthClient(ctx, OAuthClientInput{ClientID: "public-client", Name: "Client"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.CreateOAuthAuthorizationCode(ctx, OAuthAuthorizationCodeInput{ClientID: client.ClientID, ActorID: "user", CodeHash: "code-hash", ExpiresAt: FormatTime(time.Now().Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := db.ConsumeOAuthAuthorizationCode(ctx, "code-hash")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful consumptions=%d", successes)
	}
}

// Break caught: a process crash after durable run creation but before trigger
// linking leaves a retry unable to recover the one run it already created.
func TestAgentRunTriggerRecoversCreatedButUnlinkedRun(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	agent, err := db.CreateAgent(ctx, "RECOVER", "test", testDefinition, "{}", 5_000_000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := db.GetWorkspace(ctx)
	now := Now()
	trigger := &AgentRunTrigger{ID: id.New("atrigger"), WorkspaceID: ws.ID, AgentID: agent.ID, IdempotencyKey: "recover-key", Trigger: "mcp", InputJSON: `{}`}
	if _, err = db.Exec(ctx, `INSERT INTO agent_run_triggers(id,workspace_id,agent_id,idempotency_key,trigger,input_json,state,created_at,updated_at) VALUES(?,?,?,?,?,'{}','creating',?,?)`, trigger.ID, ws.ID, agent.ID, trigger.IdempotencyKey, trigger.Trigger, now, now); err != nil {
		t.Fatal(err)
	}
	run, err := db.CreateAgentRunForTrigger(ctx, agent.ID, "mcp", `{}`, "", "", nil, trigger.ID)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: "recover-key", Trigger: "mcp", InputJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.AgentRunID != run.ID {
		t.Fatalf("recovered=%#v run=%#v", recovered, run)
	}
}

// Break caught: a retry after a pre-run crash changes the original intent or
// rate snapshot instead of resuming the accepted durable request.
func TestAgentRunTriggerRecoveryUsesOriginalDurableRequest(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	agent, err := db.CreateAgent(ctx, "ORIGINAL", "test", testDefinition, "{}", 5_000_000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := db.GetWorkspace(ctx)
	now := Now()
	if _, err = db.Exec(ctx, `INSERT INTO agent_run_triggers(id,workspace_id,agent_id,idempotency_key,trigger,input_json,rate_snapshot_json,state,lease_until,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'creating',?,?,?)`, "atrigger_original", ws.ID, agent.ID, "original-key", "mcp", `{"prompt":"original"}`, `{"rate":"original"}`, FormatTime(time.Now().Add(-time.Minute)), now, now); err != nil {
		t.Fatal(err)
	}
	trigger, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: "original-key", Trigger: "web", InputJSON: `{"prompt":"changed"}`, RateSnapshot: map[string]string{"rate": "changed"}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.GetAgentRun(ctx, trigger.AgentRunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Trigger != "mcp" || run.InputJSON != `{"prompt":"original"}` || run.RateSnapshotJSON != `{"rate":"original"}` {
		t.Fatalf("run=%#v", run)
	}
}

// Break caught: a trigger without explicit rates persists an empty snapshot,
// unlike direct agent-run creation, and later replays with different pricing.
func TestAgentRunTriggerDefaultsRateSnapshot(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	agent, err := db.CreateAgent(ctx, "DEFAULT-RATES", "test", testDefinition, "{}", 5_000_000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	trigger, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: "default-rates", Trigger: "mcp", InputJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.GetAgentRun(ctx, trigger.AgentRunID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(DefaultAgentRateSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if run.RateSnapshotJSON != string(want) {
		t.Fatalf("rate snapshot=%s want=%s", run.RateSnapshotJSON, want)
	}
}

// Break caught: a crash between completing an outbox marker and writing its
// receipt makes the externally complete item permanently unreconstructible.
func TestCompleteOutlookOutboxAtomicallyWritesReceiptAndLifecycle(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	batch, _ := db.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "atomic", SubmittedBy: "connector"})
	item, _, _ := db.UpsertOutlookIngestionItem(ctx, OutlookIngestionItemInput{BatchID: batch.ID, SourceID: "atomic-source", NormalizedJSON: `{}`})
	outbox, _ := db.EnqueueOutlookOutbox(ctx, batch.ID, item.ID, "atomic-source")
	claimed, err := db.ClaimOutlookOutbox(ctx, "worker", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim=%#v err=%v", claimed, err)
	}
	if err = db.CompleteOutlookOutboxAtomically(ctx, outbox.ID, "worker", "accepted", `{"ok":true}`); err != nil {
		t.Fatal(err)
	}
	var completed, receipt, itemState, batchState int
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_outbox WHERE id=? AND state='completed'`, outbox.ID).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_receipts WHERE workspace_id=? AND batch_id=? AND item_id=? AND state='accepted'`, batch.WorkspaceID, batch.ID, item.ID).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_items WHERE id=? AND state='completed'`, item.ID).Scan(&itemState); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_batches WHERE id=? AND state='completed'`, batch.ID).Scan(&batchState); err != nil {
		t.Fatal(err)
	}
	if completed != 1 || receipt != 1 || itemState != 1 || batchState != 1 {
		t.Fatalf("outbox=%d receipt=%d item=%d batch=%d", completed, receipt, itemState, batchState)
	}
}
