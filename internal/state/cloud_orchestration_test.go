package state

import (
	"context"
	"testing"
	"time"
)

func TestOutlookCompletionEnqueuesExactlyOneCOSOrchestration(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	batch, _ := db.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "cos-batch", SubmittedBy: "connector"})
	for _, sourceID := range []string{"one", "two"} {
		item, outbox, _, err := db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: batch.ID, SourceID: sourceID, NormalizedJSON: `{"summary":"safe data"}`}, "knowledge:"+sourceID)
		if err != nil || item == nil || outbox == nil {
			t.Fatalf("item=%#v outbox=%#v err=%v", item, outbox, err)
		}
	}
	for i := 0; i < 2; i++ {
		claimed, err := db.ClaimOutlookOutbox(ctx, "worker", time.Minute)
		if err != nil || claimed == nil {
			t.Fatalf("claim=%#v err=%v", claimed, err)
		}
		if err = db.CompleteOutlookOutboxAtomically(ctx, claimed.ID, "worker", "accepted", `{"memory_id":"mem"}`); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM cloud_orchestration_tasks WHERE kind='cos_briefing' AND subject_id=?`, batch.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("COS tasks=%d", count)
	}
	claim, err := db.ClaimCloudOrchestrationTask(ctx, "cos-worker", time.Minute)
	if err != nil || claim == nil || claim.SubjectID != batch.ID {
		t.Fatalf("claim=%#v err=%v", claim, err)
	}
	if err = db.RescheduleCloudOrchestrationTask(ctx, claim.ID, "cos-worker", "run pending", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func TestOutlookZeroItemAndAllDeduplicatedBatchesCompleteAndEnqueueCOS(t *testing.T) {
	db, ctx := agentTestDB(t), context.Background()
	checkpoint1 := "2026-08-12T08:00:00Z"
	empty, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "empty", SubmittedBy: "connector"}, "", checkpoint1, "", checkpoint1)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinalizeOutlookIngestionBatch(ctx, empty.ID, "", checkpoint1, `{}`, "", checkpoint1, `{}`, empty.ReservationToken); err != nil {
		t.Fatal(err)
	}
	replayed, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "empty", SubmittedBy: "connector"}, "", checkpoint1, "", checkpoint1)
	if err != nil || replayed.State != "completed" {
		t.Fatalf("empty replay=%#v err=%v", replayed, err)
	}
	if err = db.FinalizeOutlookIngestionBatch(ctx, replayed.ID, "", checkpoint1, `{}`, "", checkpoint1, `{}`); err != nil {
		t.Fatal(err)
	}
	checkpoint2 := "2026-08-13T08:00:00Z"
	dedup, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "dedup", SubmittedBy: "connector"}, checkpoint1, checkpoint2, checkpoint1, checkpoint2)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := db.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "seed", SubmittedBy: "connector"})
	if err != nil {
		t.Fatal(err)
	}
	item, _, _, err := db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: seed.ID, SourceID: "existing", NormalizedJSON: `{}`}, "existing")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, duplicate, err := db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: dedup.ID, SourceID: item.SourceID, NormalizedJSON: `{}`}, "ignored"); err != nil || !duplicate {
		t.Fatalf("duplicate=%v err=%v", duplicate, err)
	}
	if err = db.FinalizeOutlookIngestionBatch(ctx, dedup.ID, checkpoint1, checkpoint2, `{}`, checkpoint1, checkpoint2, `{}`, dedup.ReservationToken); err != nil {
		t.Fatal(err)
	}
	var completed, tasks int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_batches WHERE id IN (?,?) AND state='completed'`, empty.ID, dedup.ID).Scan(&completed)
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM cloud_orchestration_tasks WHERE kind='cos_briefing' AND subject_id IN (?,?)`, empty.ID, dedup.ID).Scan(&tasks)
	if completed != 2 || tasks != 2 {
		t.Fatalf("completed=%d tasks=%d", completed, tasks)
	}
}
