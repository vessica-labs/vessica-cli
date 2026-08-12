package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

const outlookReservationLease = 5 * time.Minute

// CreateOutlookIngestionBatchWithCheckpoints atomically validates and leases
// both source checkpoints. The returned raw token is ephemeral; only its hash
// is stored and it fences finalize/release.
func (db *DB) CreateOutlookIngestionBatchWithCheckpoints(ctx context.Context, in OutlookIngestionBatchInput, emailExpected, emailCandidate, calendarExpected, calendarCandidate string, leaseOverride ...time.Duration) (*OutlookIngestionBatch, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.IdempotencyKey == "" || in.SubmittedBy == "" || emailCandidate == "" || calendarCandidate == "" {
		return nil, fmt.Errorf("outlook batch identity, submitter, and checkpoint candidates are required")
	}
	if in.CheckpointJSON == "" {
		in.CheckpointJSON = "{}"
	}
	if in.WarningsJSON == "" {
		in.WarningsJSON = "[]"
	}
	lease := outlookReservationLease
	if len(leaseOverride) > 0 && leaseOverride[0] > 0 {
		lease = leaseOverride[0]
	}
	nowTime := time.Now().UTC()
	now, leaseUntil := FormatTime(nowTime), FormatTime(nowTime.Add(lease))
	claimToken := id.New("outlookclaim")
	claimHash := actionClaimHash(claimToken)
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existingID, existingState string
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id,state FROM outlook_ingestion_batches WHERE workspace_id=? AND idempotency_key=?`), ws.ID, in.IdempotencyKey).Scan(&existingID, &existingState)
	if err == nil {
		if existingState == "queued" || existingState == "completed" {
			return db.getOutlookBatch(ctx, in.IdempotencyKey)
		}
		var reservations, matching int
		if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM source_checkpoint_reservations WHERE workspace_id=? AND batch_id=?`), ws.ID, existingID).Scan(&reservations); err != nil {
			return nil, err
		}
		if reservations > 0 {
			if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM source_checkpoint_reservations WHERE workspace_id=? AND batch_id=? AND ((source_type='outlook_email' AND expected_value=? AND candidate_value=?) OR (source_type='outlook_calendar' AND expected_value=? AND candidate_value=?))`), ws.ID, existingID, emailExpected, emailCandidate, calendarExpected, calendarCandidate).Scan(&matching); err != nil || matching != 2 {
				return nil, fmt.Errorf("outlook batch checkpoint reservation is incomplete or changed")
			}
			result, updateErr := tx.ExecContext(ctx, db.Rebind(`UPDATE source_checkpoint_reservations SET claim_token_hash=?,lease_until=?,updated_at=? WHERE workspace_id=? AND batch_id=?`), claimHash, leaseUntil, now, ws.ID, existingID)
			if updateErr != nil {
				return nil, updateErr
			}
			changed, _ := result.RowsAffected()
			if changed != 2 {
				return nil, fmt.Errorf("outlook reservation reclaim lost")
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			batch, getErr := db.getOutlookBatch(ctx, in.IdempotencyKey)
			if batch != nil {
				batch.ReservationToken = claimToken
			}
			return batch, getErr
		}
		// A fenced release leaves the received batch reusable if its checkpoint
		// precondition still holds.
		if err = db.validateOutlookCheckpointsTx(ctx, tx, ws.ID, emailExpected, calendarExpected); err != nil {
			return nil, err
		}
		for _, checkpoint := range []struct{ typ, expected, candidate string }{{"outlook_email", emailExpected, emailCandidate}, {"outlook_calendar", calendarExpected, calendarCandidate}} {
			if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO source_checkpoint_reservations(workspace_id,source_type,source_id,batch_id,expected_value,candidate_value,created_at,claim_token_hash,lease_until,updated_at) VALUES(?,?,'outlook',?,?,?,?,?,?,?)`), ws.ID, checkpoint.typ, existingID, checkpoint.expected, checkpoint.candidate, now, claimHash, leaseUntil, now); err != nil {
				return nil, err
			}
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		batch, getErr := db.getOutlookBatch(ctx, in.IdempotencyKey)
		if batch != nil {
			batch.ReservationToken = claimToken
		}
		return batch, getErr
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if err = db.validateOutlookCheckpointsTx(ctx, tx, ws.ID, emailExpected, calendarExpected); err != nil {
		return nil, err
	}
	var reservations int
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM source_checkpoint_reservations WHERE workspace_id=? AND source_id='outlook'`), ws.ID).Scan(&reservations); err != nil {
		return nil, err
	}
	if reservations != 0 {
		return nil, fmt.Errorf("outlook checkpoints are reserved by another batch")
	}
	batchID := id.New("obatch")
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO outlook_ingestion_batches(id,workspace_id,idempotency_key,submitted_by,state,checkpoint_json,warnings_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`), batchID, ws.ID, in.IdempotencyKey, in.SubmittedBy, "received", in.CheckpointJSON, in.WarningsJSON, now, now); err != nil {
		return nil, err
	}
	for _, checkpoint := range []struct{ typ, expected, candidate string }{{"outlook_email", emailExpected, emailCandidate}, {"outlook_calendar", calendarExpected, calendarCandidate}} {
		if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO source_checkpoint_reservations(workspace_id,source_type,source_id,batch_id,expected_value,candidate_value,created_at,claim_token_hash,lease_until,updated_at) VALUES(?,?,'outlook',?,?,?,?,?,?,?)`), ws.ID, checkpoint.typ, batchID, checkpoint.expected, checkpoint.candidate, now, claimHash, leaseUntil, now); err != nil {
			return nil, fmt.Errorf("reserve %s checkpoint: %w", checkpoint.typ, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	batch, err := db.getOutlookBatch(ctx, in.IdempotencyKey)
	if batch != nil {
		batch.ReservationToken = claimToken
	}
	return batch, err
}

func (db *DB) validateOutlookCheckpointsTx(ctx context.Context, tx *sql.Tx, workspaceID, emailExpected, calendarExpected string) error {
	for _, checkpoint := range []struct{ typ, expected string }{{"outlook_email", emailExpected}, {"outlook_calendar", calendarExpected}} {
		var current string
		err := tx.QueryRowContext(ctx, db.Rebind(`SELECT checkpoint_value FROM source_checkpoints WHERE workspace_id=? AND source_type=? AND source_id='outlook'`), workspaceID, checkpoint.typ).Scan(&current)
		if err == sql.ErrNoRows {
			current = ""
		} else if err != nil {
			return err
		}
		if current != checkpoint.expected {
			return fmt.Errorf("stale %s checkpoint: expected %q, current %q", checkpoint.typ, checkpoint.expected, current)
		}
	}
	return nil
}

func (db *DB) ReleaseOutlookCheckpointReservation(ctx context.Context, batchID, claimToken string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if batchID == "" || claimToken == "" {
		return fmt.Errorf("outlook reservation release requires its fence")
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, db.Rebind(`DELETE FROM source_checkpoint_reservations WHERE workspace_id=? AND batch_id=? AND claim_token_hash=?`), ws.ID, batchID, actionClaimHash(claimToken))
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 2 {
		return fmt.Errorf("outlook reservation fence lost")
	}
	return tx.Commit()
}
