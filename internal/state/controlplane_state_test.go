package state

import (
	"context"
	"testing"
)

// Break caught: action audit records being overwritten on retry would erase an
// immutable decision record.
func TestActionLedgerIsAppendOnlyAndIdempotent(t *testing.T) {
	db := agentTestDB(t)
	first, err := db.AppendActionLedger(context.Background(), ActionLedgerInput{ActorID: "user_1", Tool: "newsletter.sync", PolicyDecision: "allow", RedactedArgumentsJSON: `{"authorization":"Bearer unsafe-token","query":"AI"}`, ResultJSON: `{"token":"unsafe-result-token","accepted":true}`, LatencyMS: 42, IdempotencyKey: "newsletter-1", ExternalIDsJSON: `["feed-1"]`})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.AppendActionLedger(context.Background(), ActionLedgerInput{ActorID: "user_1", Tool: "newsletter.sync", PolicyDecision: "allow", IdempotencyKey: "newsletter-1"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.ResultJSON == "" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if first.RedactedArgumentsJSON == `{"authorization":"Bearer unsafe-token","query":"AI"}` {
		t.Fatalf("ledger retained an unredacted argument: %s", first.RedactedArgumentsJSON)
	}
	if first.ResultJSON == `{"token":"unsafe-result-token","accepted":true}` {
		t.Fatalf("ledger retained an unredacted result: %s", first.ResultJSON)
	}
}

// Break caught: MCP and web reading a conversation out of order would show a
// different history from the one durably written by either adapter.
func TestConversationMessagesHaveWorkspaceScopedOrder(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	conversation, err := db.CreateConversation(ctx, ConversationInput{ActorID: "user_1", Title: "Shared conversation"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AppendConversationMessage(ctx, conversation.ID, ConversationMessageInput{Role: "user", ContentJSON: `{"text":"first"}`}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AppendConversationMessage(ctx, conversation.ID, ConversationMessageInput{Role: "assistant", ContentJSON: `{"text":"second"}`}); err != nil {
		t.Fatal(err)
	}
	messages, err := db.ListConversationMessages(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Sequence != 1 || messages[1].Sequence != 2 {
		t.Fatalf("messages=%#v", messages)
	}
}

// Break caught: a repeated source item creates duplicate newsletter records or
// loses the source checkpoint used by the next poll.
func TestNewsletterItemAndCheckpointAreIdempotent(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	subscription, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "vessica-blog", SourceURL: "https://example.test/feed", RetentionDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.UpsertNewsletterItem(ctx, NewsletterItemInput{SubscriptionID: subscription.ID, SourceItemID: "post-1", NormalizedJSON: `{"title":"One"}`, RetainUntil: "2026-08-20T00:00:00.000000000Z"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.UpsertNewsletterItem(ctx, NewsletterItemInput{SubscriptionID: subscription.ID, SourceItemID: "post-1", NormalizedJSON: `{"title":"Changed"}`, RetainUntil: "2026-08-20T00:00:00.000000000Z"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.NormalizedJSON != `{"title":"Changed"}` {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if err = db.UpsertSourceCheckpoint(ctx, "newsletter", subscription.ID, `{"cursor":"post-1"}`); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := db.GetSourceCheckpoint(ctx, "newsletter", subscription.ID)
	if err != nil || checkpoint.CheckpointJSON != `{"cursor":"post-1"}` {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
}

func TestNewsletterSubscriptionStoresOnlyCredentialReferences(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	if _, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "reddit", SourceURL: "https://oauth.reddit.com/r/golang/new", MetadataJSON: `{"type":"reddit","credential_env":"REDDIT_ACCESS_TOKEN"}`}); err != nil {
		t.Fatal(err)
	}
	for _, metadata := range []string{
		`{"type":"reddit","access_token":"raw-secret"}`,
		`{"type":"x","credential_env":"raw-secret"}`,
		`{"type":"x","nested":{"api_key":"raw-secret"}}`,
	} {
		if _, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "unsafe-" + metadata, SourceURL: "https://example.test", MetadataJSON: metadata}); err == nil {
			t.Fatalf("unsafe metadata accepted: %s", metadata)
		}
	}
	if _, err := db.UpsertNewsletterSubscription(ctx, NewsletterSubscriptionInput{SourceKey: "userinfo", SourceURL: "https://user:secret@example.test", MetadataJSON: `{}`}); err == nil {
		t.Fatal("credential-bearing source URL accepted")
	}
}

// Break caught: an Outlook retry duplicates a message or loses the durable
// processing marker that protects downstream effects.
func TestOutlookBatchDeduplicatesSourceItemsAndOutbox(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	batch, err := db.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "batch-1", SubmittedBy: "connector"})
	if err != nil {
		t.Fatal(err)
	}
	item, duplicate, err := db.UpsertOutlookIngestionItem(ctx, OutlookIngestionItemInput{BatchID: batch.ID, SourceID: "immutable-outlook-id", NormalizedJSON: `{"summary":"follow up"}`})
	if err != nil || duplicate {
		t.Fatalf("item=%#v duplicate=%v err=%v", item, duplicate, err)
	}
	duplicateItem, duplicate, err := db.UpsertOutlookIngestionItem(ctx, OutlookIngestionItemInput{BatchID: batch.ID, SourceID: "immutable-outlook-id", NormalizedJSON: `{"summary":"new"}`})
	if err != nil || !duplicate || duplicateItem.ID != item.ID {
		t.Fatalf("item=%#v duplicate=%v err=%v", duplicateItem, duplicate, err)
	}
	first, err := db.EnqueueOutlookOutbox(ctx, batch.ID, item.ID, "knowledge:immutable-outlook-id")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.EnqueueOutlookOutbox(ctx, batch.ID, item.ID, "knowledge:immutable-outlook-id")
	if err != nil || second.ID != first.ID {
		t.Fatalf("first=%#v second=%#v err=%v", first, second, err)
	}
	claimed, err := db.ClaimOutlookOutbox(ctx, "worker-1", 0)
	if err != nil || claimed == nil || claimed.State != "processing" {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	if err = db.CompleteOutlookOutbox(ctx, claimed.ID, "worker-1", `{"stored":true}`); err != nil {
		t.Fatal(err)
	}
	receipt, err := db.RecordOutlookIngestionReceipt(ctx, batch.ID, item.ID, "accepted", `{"stored":true}`, "")
	if err != nil || receipt.State != "accepted" {
		t.Fatalf("receipt=%#v err=%v", receipt, err)
	}
}

// Break caught: a crash between independent checkpoint writes can advance one
// Outlook source even though the batch lifecycle was not durably finalized.
func TestFinalizeOutlookBatchCommitsCheckpointsAndLifecycleTogether(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	batch, err := db.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "finalize", SubmittedBy: "connector"})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := "2026-08-11T08:00:00-07:00"
	if err = db.FinalizeOutlookIngestionBatch(ctx, batch.ID, "", checkpoint, `{"candidate":"email"}`, "", checkpoint, `{"candidate":"calendar"}`); err != nil {
		t.Fatal(err)
	}
	email, err := db.GetSourceCheckpoint(ctx, "outlook_email", "outlook")
	if err != nil || email.CheckpointJSON != `{"candidate":"email"}` {
		t.Fatalf("email checkpoint=%#v err=%v", email, err)
	}
	calendar, err := db.GetSourceCheckpoint(ctx, "outlook_calendar", "outlook")
	if err != nil || calendar.CheckpointJSON != `{"candidate":"calendar"}` {
		t.Fatalf("calendar checkpoint=%#v err=%v", calendar, err)
	}
	finalized, err := db.getOutlookBatch(ctx, "finalize")
	if err != nil || finalized.State != "completed" {
		t.Fatalf("finalized batch=%#v err=%v", finalized, err)
	}
}
