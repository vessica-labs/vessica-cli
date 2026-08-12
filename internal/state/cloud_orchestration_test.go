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
