package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestHostedInboxJobsOutboxAndMappingsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(ctx, root, "hosted"); err != nil {
		t.Fatal(err)
	}
	integration, err := db.UpsertTrackerIntegration(ctx, "linear", "connected", map[string]string{"team": "team-1"}, "", "secret")
	if err != nil {
		t.Fatal(err)
	}
	first, job, duplicate, err := db.ReceiveWebhook(ctx, integration, "delivery-1", "Issue", []byte(`{"type":"Issue"}`))
	if err != nil || duplicate || job == nil {
		t.Fatalf("first delivery=%#v job=%#v duplicate=%v err=%v", first, job, duplicate, err)
	}
	_, secondJob, duplicate, err := db.ReceiveWebhook(ctx, integration, "delivery-1", "Issue", []byte(`{"type":"Issue"}`))
	if err != nil || !duplicate || secondJob != nil {
		t.Fatalf("duplicate job=%#v duplicate=%v err=%v", secondJob, duplicate, err)
	}
	jobs, err := db.ListJobs(ctx, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%#v err=%v", jobs, err)
	}
	claimed, err := db.ClaimJob(ctx, "worker-1", time.Minute)
	if err != nil || claimed == nil || claimed.ID != job.ID {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
	if err := db.CompleteJob(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	recoverable, err := db.EnqueueJob(ctx, "sync_run", map[string]string{"run_id": "one"}, "")
	if err != nil {
		t.Fatal(err)
	}
	running, err := db.ClaimJob(ctx, "crashed-worker", time.Minute)
	if err != nil || running == nil || running.ID != recoverable.ID {
		t.Fatalf("running=%#v err=%v", running, err)
	}
	if _, err := db.Exec(ctx, `UPDATE jobs SET lease_until=? WHERE id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), running.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := db.ClaimJob(ctx, "replacement-worker", time.Minute)
	if err != nil || reclaimed == nil || reclaimed.ID != recoverable.ID {
		t.Fatalf("reclaimed=%#v err=%v", reclaimed, err)
	}
	if err := db.CompleteJob(ctx, reclaimed.ID); err != nil {
		t.Fatal(err)
	}

	message, err := db.EnqueueOutbox(ctx, integration.ID, "linear.comment", "comment:1", map[string]string{"body": "one"})
	if err != nil {
		t.Fatal(err)
	}
	claimedMessage, err := db.ClaimOutbox(ctx)
	if err != nil || claimedMessage == nil || claimedMessage.ID != message.ID {
		t.Fatalf("claimed outbox=%#v err=%v", claimedMessage, err)
	}
	if err := db.CompleteOutbox(ctx, claimedMessage.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnqueueOutbox(ctx, integration.ID, "linear.comment", "comment:1", map[string]string{"body": "two"}); err != nil {
		t.Fatal(err)
	}
	if again, err := db.ClaimOutbox(ctx); err != nil || again != nil {
		t.Fatalf("completed idempotency key was replayed: %#v err=%v", again, err)
	}
	stuck, err := db.EnqueueOutbox(ctx, integration.ID, "linear.comment", "comment:stuck", map[string]string{"body": "stuck"})
	if err != nil {
		t.Fatal(err)
	}
	runningMessage, err := db.ClaimOutbox(ctx)
	if err != nil || runningMessage == nil || runningMessage.ID != stuck.ID {
		t.Fatalf("running message=%#v err=%v", runningMessage, err)
	}
	if _, err := db.Exec(ctx, `UPDATE outbox_messages SET updated_at=? WHERE id=?`, time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339Nano), runningMessage.ID); err != nil {
		t.Fatal(err)
	}
	reclaimedMessage, err := db.ClaimOutbox(ctx)
	if err != nil || reclaimedMessage == nil || reclaimedMessage.ID != stuck.ID {
		t.Fatalf("reclaimed message=%#v err=%v", reclaimedMessage, err)
	}

	mapping, err := db.UpsertExternalMapping(ctx, "linear", "epic", "epic-1", "LIN-1", map[string]string{"url": "u"}, "synced")
	if err != nil || mapping.ExternalID != "LIN-1" {
		t.Fatalf("mapping=%#v err=%v", mapping, err)
	}
	updated, err := db.UpsertExternalMapping(ctx, "linear", "epic", "epic-1", "LIN-2", nil, "synced")
	if err != nil || updated.ExternalID != "LIN-2" {
		t.Fatalf("updated mapping=%#v err=%v", updated, err)
	}
}
