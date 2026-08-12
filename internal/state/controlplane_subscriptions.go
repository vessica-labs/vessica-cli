package state

import (
	"context"
	"fmt"
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
func (db *DB) FinalizeOutlookIngestionBatch(ctx context.Context, batchID, emailCheckpointJSON, calendarCheckpointJSON string) error {
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
	for _, checkpoint := range []struct{ sourceType, value string }{
		{"outlook_email", emailCheckpointJSON}, {"outlook_calendar", calendarCheckpointJSON},
	} {
		if checkpoint.value == "" {
			checkpoint.value = "{}"
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO source_checkpoints(workspace_id,source_type,source_id,checkpoint_json,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(workspace_id,source_type,source_id) DO UPDATE SET checkpoint_json=excluded.checkpoint_json,updated_at=excluded.updated_at`), ws.ID, checkpoint.sourceType, "outlook", checkpoint.value, now); err != nil {
			return err
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
