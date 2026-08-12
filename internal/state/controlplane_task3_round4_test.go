package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

func reservationFingerprint(t *testing.T, db *DB, batchID string) (string, string) {
	t.Helper()
	var claimHash, leaseUntil string
	if err := db.QueryRow(context.Background(), `SELECT MIN(claim_token_hash),MIN(lease_until) FROM source_checkpoint_reservations WHERE batch_id=?`, batchID).Scan(&claimHash, &leaseUntil); err != nil {
		t.Fatal(err)
	}
	return claimHash, leaseUntil
}

func expireOutlookReservation(t *testing.T, db *DB, batchID string) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE source_checkpoint_reservations SET lease_until=? WHERE batch_id=?`, FormatTime(time.Now().Add(-time.Minute)), batchID); err != nil {
		t.Fatal(err)
	}
}

func TestOutlookReservationFourCaseOwnershipSemantics(t *testing.T) {
	const candidate = "2026-08-12T08:00:00-07:00"

	t.Run("same batch active preserves live fence", func(t *testing.T) {
		db, ctx := agentTestDB(t), context.Background()
		in := OutlookIngestionBatchInput{IdempotencyKey: "same-active", SubmittedBy: "user"}
		first, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, in, "", candidate, "", candidate)
		if err != nil {
			t.Fatal(err)
		}
		beforeHash, beforeLease := reservationFingerprint(t, db, first.ID)
		second, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, in, "", candidate, "", candidate)
		if second != nil || !errors.Is(err, ErrOutlookReservationAlreadyClaimed) {
			t.Fatalf("second=%#v err=%v", second, err)
		}
		afterHash, afterLease := reservationFingerprint(t, db, first.ID)
		if beforeHash != afterHash || beforeLease != afterLease {
			t.Fatalf("active fence rotated hash=%v lease=%v", beforeHash != afterHash, beforeLease != afterLease)
		}
		if err = db.FinalizeOutlookIngestionBatch(ctx, first.ID, "", candidate, `{}`, "", candidate, `{}`, first.ReservationToken); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("same batch expired rotates fence", func(t *testing.T) {
		db, ctx := agentTestDB(t), context.Background()
		in := OutlookIngestionBatchInput{IdempotencyKey: "same-expired", SubmittedBy: "user"}
		first, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, in, "", candidate, "", candidate)
		if err != nil {
			t.Fatal(err)
		}
		expireOutlookReservation(t, db, first.ID)
		reclaimed, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, in, "", candidate, "", candidate)
		if err != nil || reclaimed.ReservationToken == "" || reclaimed.ReservationToken == first.ReservationToken {
			t.Fatalf("reclaimed=%#v err=%v", reclaimed, err)
		}
		if err = db.FinalizeOutlookIngestionBatch(ctx, first.ID, "", candidate, `{}`, "", candidate, `{}`, first.ReservationToken); err == nil {
			t.Fatal("stale fence finalized after same-batch reclaim")
		}
		if err = db.ReleaseOutlookCheckpointReservation(ctx, first.ID, first.ReservationToken); err == nil {
			t.Fatal("stale fence released after same-batch reclaim")
		}
		if err = db.FinalizeOutlookIngestionBatch(ctx, first.ID, "", candidate, `{}`, "", candidate, `{}`, reclaimed.ReservationToken); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("different batch active rejects", func(t *testing.T) {
		db, ctx := agentTestDB(t), context.Background()
		first, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "different-active-owner", SubmittedBy: "user"}, "", candidate, "", candidate)
		if err != nil {
			t.Fatal(err)
		}
		beforeHash, beforeLease := reservationFingerprint(t, db, first.ID)
		second, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "different-active-contender", SubmittedBy: "user"}, "", candidate, "", candidate)
		if second != nil || !errors.Is(err, ErrOutlookReservationHeld) {
			t.Fatalf("second=%#v err=%v", second, err)
		}
		afterHash, afterLease := reservationFingerprint(t, db, first.ID)
		if beforeHash != afterHash || beforeLease != afterLease {
			t.Fatal("competing batch changed active ownership")
		}
	})

	t.Run("different batch expired reclaims from committed checkpoint", func(t *testing.T) {
		db, ctx := agentTestDB(t), context.Background()
		committed := "2026-08-12T07:00:00-07:00"
		seed, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "expired-seed", SubmittedBy: "user"}, "", committed, "", committed)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.FinalizeOutlookIngestionBatch(ctx, seed.ID, "", committed, `{}`, "", committed, `{}`, seed.ReservationToken); err != nil {
			t.Fatal(err)
		}
		abandonedCandidate := "2026-08-12T08:00:00-07:00"
		abandoned, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "expired-owner", SubmittedBy: "user"}, committed, abandonedCandidate, committed, abandonedCandidate)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err = db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: abandoned.ID, SourceID: "same-message", NormalizedJSON: `{}`}, "same-message-key"); err != nil {
			t.Fatal(err)
		}
		expireOutlookReservation(t, db, abandoned.ID)
		newCandidate := "2026-08-12T09:00:00-07:00"
		replacement, err := db.CreateOutlookIngestionBatchWithCheckpoints(ctx, OutlookIngestionBatchInput{IdempotencyKey: "expired-replacement", SubmittedBy: "user"}, committed, newCandidate, committed, newCandidate)
		if err != nil || replacement.ReservationToken == "" {
			t.Fatalf("replacement=%#v err=%v", replacement, err)
		}
		if _, _, duplicate, acceptErr := db.UpsertOutlookIngestionItemAndEnqueue(ctx, OutlookIngestionItemInput{BatchID: replacement.ID, SourceID: "same-message", NormalizedJSON: `{}`}, "replacement-message-key"); acceptErr != nil || !duplicate {
			t.Fatalf("duplicate=%v err=%v", duplicate, acceptErr)
		}
		if err = db.FinalizeOutlookIngestionBatch(ctx, abandoned.ID, committed, abandonedCandidate, `{}`, committed, abandonedCandidate, `{}`, abandoned.ReservationToken); err == nil {
			t.Fatal("abandoned fence finalized after different-batch reclaim")
		}
		if err = db.FinalizeOutlookIngestionBatch(ctx, replacement.ID, committed, newCandidate, `{}`, committed, newCandidate, `{}`, replacement.ReservationToken); err != nil {
			t.Fatal(err)
		}
		for _, typ := range []string{"outlook_email", "outlook_calendar"} {
			checkpoint, getErr := db.GetSourceCheckpoint(ctx, typ, "outlook")
			if getErr != nil || checkpoint.CheckpointValue != newCandidate {
				t.Fatalf("%s checkpoint=%#v err=%v", typ, checkpoint, getErr)
			}
		}
		var items, outbox int
		_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_items WHERE source_id='same-message'`).Scan(&items)
		_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_outbox WHERE processing_key='same-message-key'`).Scan(&outbox)
		if items != 1 || outbox != 1 {
			t.Fatalf("items=%d outbox=%d", items, outbox)
		}
	})
}
