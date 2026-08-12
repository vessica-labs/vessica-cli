package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func cloudAgentService(t *testing.T) (*Service, *state.DB) {
	t.Helper()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.EnsureWorkspace(context.Background(), root, "hosted"); err != nil {
		t.Fatal(err)
	}
	return New(db, root, config.Defaults()), db
}

func TestCloudAgentServiceOAuthLifecycle(t *testing.T) {
	s, _ := cloudAgentService(t)
	ctx := context.Background()
	client, err := s.RegisterOAuthClient(ctx, state.OAuthClientInput{ClientID: "public", Name: "Public"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.IssueOAuthRefreshToken(ctx, state.OAuthRefreshTokenInput{ClientID: client.ClientID, ActorID: "user", MaterialHash: "refresh", FamilyID: "family", ExpiresAt: state.FormatTime(time.Now().Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ValidateOAuthRefreshToken(ctx, "refresh"); err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeOAuthRefreshToken(ctx, "refresh"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ValidateOAuthRefreshToken(ctx, "refresh"); err == nil {
		t.Fatal("revoked refresh token validated")
	}
}

func TestCloudAgentServiceOutlookFailureReschedulesWork(t *testing.T) {
	s, _ := cloudAgentService(t)
	ctx := context.Background()
	batch, err := s.SubmitOutlookIngestion(ctx, state.OutlookIngestionBatchInput{IdempotencyKey: "app", SubmittedBy: "connector"})
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := s.UpsertOutlookIngestionItem(ctx, state.OutlookIngestionItemInput{BatchID: batch.ID, SourceID: "app-source", NormalizedJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := s.EnqueueOutlookOutbox(ctx, batch.ID, item.ID, "app-source")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimOutlookOutbox(ctx, "worker", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim=%#v err=%v", claimed, err)
	}
	if err = s.FailOutlookOutbox(ctx, outbox.ID, "worker", "temporary", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var stateText, lastError string
	if err = s.DB.QueryRow(ctx, `SELECT state,last_error FROM outlook_outbox WHERE id=?`, outbox.ID).Scan(&stateText, &lastError); err != nil {
		t.Fatal(err)
	}
	if stateText != "pending" || lastError != "temporary" {
		t.Fatalf("state=%s error=%s", stateText, lastError)
	}
}

func TestCloudAgentServiceSourcesAndCheckpoints(t *testing.T) {
	s, _ := cloudAgentService(t)
	ctx := context.Background()
	sub, err := s.UpsertNewsletterSubscription(ctx, state.NewsletterSubscriptionInput{SourceKey: "app", SourceURL: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpsertNewsletterItem(ctx, state.NewsletterItemInput{SubscriptionID: sub.ID, SourceItemID: "one", NormalizedJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertSourceCheckpoint(ctx, "newsletter", sub.ID, `{"cursor":"one"}`); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := s.SourceCheckpoint(ctx, "newsletter", sub.ID)
	if err != nil || checkpoint.CheckpointJSON != `{"cursor":"one"}` {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
}
