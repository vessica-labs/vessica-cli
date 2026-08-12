package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

// CreateOutlookIngestionBatchWithCheckpoints validates and reserves both
// expected checkpoints in the same transaction that creates the batch. No item
// or outbox record can be written for a stale/unreserved submission.
func (db *DB) CreateOutlookIngestionBatchWithCheckpoints(ctx context.Context, in OutlookIngestionBatchInput, emailExpected, emailCandidate, calendarExpected, calendarCandidate string) (*OutlookIngestionBatch, error) {
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
		var reservations int
		if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM source_checkpoint_reservations WHERE workspace_id=? AND batch_id=? AND ((source_type='outlook_email' AND expected_value=? AND candidate_value=?) OR (source_type='outlook_calendar' AND expected_value=? AND candidate_value=?))`), ws.ID, existingID, emailExpected, emailCandidate, calendarExpected, calendarCandidate).Scan(&reservations); err != nil || reservations != 2 {
			return nil, fmt.Errorf("outlook batch checkpoint reservation is incomplete")
		}
		return db.getOutlookBatch(ctx, in.IdempotencyKey)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	batchID := id.New("obatch")
	for _, checkpoint := range []struct{ sourceType, expected, candidate string }{
		{"outlook_email", emailExpected, emailCandidate}, {"outlook_calendar", calendarExpected, calendarCandidate},
	} {
		var current string
		checkpointErr := tx.QueryRowContext(ctx, db.Rebind(`SELECT checkpoint_value FROM source_checkpoints WHERE workspace_id=? AND source_type=? AND source_id='outlook'`), ws.ID, checkpoint.sourceType).Scan(&current)
		if checkpointErr == sql.ErrNoRows {
			current = ""
		} else if checkpointErr != nil {
			return nil, checkpointErr
		}
		if current != checkpoint.expected {
			return nil, fmt.Errorf("stale %s checkpoint: expected %q, current %q", checkpoint.sourceType, checkpoint.expected, current)
		}
	}
	now := Now()
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO outlook_ingestion_batches(id,workspace_id,idempotency_key,submitted_by,state,checkpoint_json,warnings_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`), batchID, ws.ID, in.IdempotencyKey, in.SubmittedBy, "received", in.CheckpointJSON, in.WarningsJSON, now, now); err != nil {
		return nil, err
	}
	for _, checkpoint := range []struct{ sourceType, expected, candidate string }{
		{"outlook_email", emailExpected, emailCandidate}, {"outlook_calendar", calendarExpected, calendarCandidate},
	} {
		if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO source_checkpoint_reservations(workspace_id,source_type,source_id,batch_id,expected_value,candidate_value,created_at) VALUES(?,?, 'outlook',?,?,?,?)`), ws.ID, checkpoint.sourceType, batchID, checkpoint.expected, checkpoint.candidate, now); err != nil {
			return nil, fmt.Errorf("reserve %s checkpoint: %w", checkpoint.sourceType, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return db.getOutlookBatch(ctx, in.IdempotencyKey)
}
