package state

import (
	"context"
	"fmt"
	"time"
)

func (db *DB) ListNewsletterSubscriptions(ctx context.Context) ([]NewsletterSubscription, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT id,workspace_id,source_key,source_url,title,status,retention_days,metadata_json,created_at,updated_at FROM newsletter_subscriptions WHERE workspace_id=? ORDER BY source_key`, ws.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NewsletterSubscription
	for rows.Next() {
		var v NewsletterSubscription
		if err = rows.Scan(&v.ID, &v.WorkspaceID, &v.SourceKey, &v.SourceURL, &v.Title, &v.Status, &v.RetentionDays, &v.MetadataJSON, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (db *DB) DisableNewsletterSubscription(ctx context.Context, ref string) (*NewsletterSubscription, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	result, err := db.Exec(ctx, `UPDATE newsletter_subscriptions SET status='disabled',updated_at=? WHERE workspace_id=? AND (id=? OR source_key=?)`, Now(), ws.ID, ref, ref)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, fmt.Errorf("newsletter subscription not found")
	}
	var v NewsletterSubscription
	err = db.QueryRow(ctx, `SELECT id,workspace_id,source_key,source_url,title,status,retention_days,metadata_json,created_at,updated_at FROM newsletter_subscriptions WHERE workspace_id=? AND (id=? OR source_key=?)`, ws.ID, ref, ref).Scan(&v.ID, &v.WorkspaceID, &v.SourceKey, &v.SourceURL, &v.Title, &v.Status, &v.RetentionDays, &v.MetadataJSON, &v.CreatedAt, &v.UpdatedAt)
	return &v, err
}

// FinalizeOutlookIngestionBatch advances both independent source checkpoints
// and the batch lifecycle in one transaction after every item is durable.
func (db *DB) FinalizeOutlookIngestionBatch(ctx context.Context, batchID, emailExpected, emailCandidate, emailCheckpointJSON, calendarExpected, calendarCandidate, calendarCheckpointJSON string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := Now()
	for _, checkpoint := range []struct{ sourceType, expected, candidate, value string }{
		{"outlook_email", emailExpected, emailCandidate, emailCheckpointJSON},
		{"outlook_calendar", calendarExpected, calendarCandidate, calendarCheckpointJSON},
	} {
		candidateTime, parseErr := time.Parse(time.RFC3339, checkpoint.candidate)
		if parseErr != nil {
			return fmt.Errorf("%s candidate checkpoint must be RFC 3339", checkpoint.sourceType)
		}
		if checkpoint.expected != "" {
			expectedTime, expectedErr := time.Parse(time.RFC3339, checkpoint.expected)
			if expectedErr != nil || expectedTime.After(candidateTime) {
				return fmt.Errorf("%s checkpoint must be monotonic", checkpoint.sourceType)
			}
		}
		if checkpoint.value == "" {
			checkpoint.value = "{}"
		}
		result, updateErr := tx.ExecContext(ctx, db.Rebind(`UPDATE source_checkpoints SET checkpoint_json=?,checkpoint_value=?,updated_at=? WHERE workspace_id=? AND source_type=? AND source_id='outlook' AND checkpoint_value=?`), checkpoint.value, checkpoint.candidate, now, ws.ID, checkpoint.sourceType, checkpoint.expected)
		if updateErr != nil {
			return updateErr
		}
		changed, _ := result.RowsAffected()
		if changed == 1 {
			continue
		}
		if checkpoint.expected != "" {
			return fmt.Errorf("stale %s checkpoint: expected %q", checkpoint.sourceType, checkpoint.expected)
		}
		result, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO source_checkpoints(workspace_id,source_type,source_id,checkpoint_json,checkpoint_value,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(workspace_id,source_type,source_id) DO NOTHING`), ws.ID, checkpoint.sourceType, "outlook", checkpoint.value, checkpoint.candidate, now)
		if err != nil {
			return err
		}
		changed, _ = result.RowsAffected()
		if changed != 1 {
			return fmt.Errorf("stale %s checkpoint: expected %q", checkpoint.sourceType, checkpoint.expected)
		}
	}
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE outlook_ingestion_batches SET state='queued',error=NULL,updated_at=? WHERE id=? AND workspace_id=?`), now, batchID, ws.ID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("outlook batch not found")
	}
	return tx.Commit()
}
