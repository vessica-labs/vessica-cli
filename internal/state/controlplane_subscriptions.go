package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

var newsletterCredentialRefPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,127}$`)

func (db *DB) UpsertNewsletterSubscription(ctx context.Context, in NewsletterSubscriptionInput) (*NewsletterSubscription, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.SourceKey == "" || in.SourceURL == "" {
		return nil, fmt.Errorf("newsletter source key and url are required")
	}
	if err = validateNewsletterSubscriptionInput(in.SourceURL, in.MetadataJSON); err != nil {
		return nil, err
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.RetentionDays <= 0 {
		in.RetentionDays = 30
	}
	if in.MetadataJSON == "" {
		in.MetadataJSON = "{}"
	}
	now := Now()
	_, err = db.Exec(ctx, `INSERT INTO newsletter_subscriptions(id,workspace_id,source_key,source_url,title,status,retention_days,metadata_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,source_key) DO UPDATE SET source_url=excluded.source_url,title=excluded.title,status=excluded.status,retention_days=excluded.retention_days,metadata_json=excluded.metadata_json,updated_at=excluded.updated_at`, id.New("nsource"), ws.ID, in.SourceKey, in.SourceURL, in.Title, in.Status, in.RetentionDays, in.MetadataJSON, now, now)
	if err != nil {
		return nil, err
	}
	var subscription NewsletterSubscription
	err = db.QueryRow(ctx, `SELECT id,workspace_id,source_key,source_url,title,status,retention_days,metadata_json,created_at,updated_at FROM newsletter_subscriptions WHERE workspace_id=? AND source_key=?`, ws.ID, in.SourceKey).Scan(&subscription.ID, &subscription.WorkspaceID, &subscription.SourceKey, &subscription.SourceURL, &subscription.Title, &subscription.Status, &subscription.RetentionDays, &subscription.MetadataJSON, &subscription.CreatedAt, &subscription.UpdatedAt)
	return &subscription, err
}

func validateNewsletterSubscriptionInput(sourceURL, metadataJSON string) error {
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("newsletter source URL is invalid or contains credentials")
	}
	if strings.TrimSpace(metadataJSON) == "" {
		return nil
	}
	var metadata any
	if err = json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return fmt.Errorf("newsletter metadata must be valid JSON")
	}
	return validateNewsletterCredentialMetadata(metadata)
}

func validateNewsletterCredentialMetadata(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if normalized == "credential_env" {
				ref, ok := child.(string)
				if !ok || !newsletterCredentialRefPattern.MatchString(ref) {
					return fmt.Errorf("newsletter credential_env must be an environment variable reference")
				}
				continue
			}
			if strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") || strings.Contains(normalized, "api_key") || strings.Contains(normalized, "apikey") || strings.Contains(normalized, "credential") {
				return fmt.Errorf("newsletter metadata may store credential references only")
			}
			if err := validateNewsletterCredentialMetadata(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateNewsletterCredentialMetadata(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func (db *DB) ListNewsletterItemsSince(ctx context.Context, since string) ([]NewsletterItem, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT id,workspace_id,subscription_id,source_item_id,normalized_json,COALESCE(published_at,''),COALESCE(retain_until,''),created_at,updated_at FROM newsletter_items WHERE workspace_id=? AND ((published_at IS NOT NULL AND published_at!='' AND published_at>=?) OR ((published_at IS NULL OR published_at='') AND created_at>=?)) ORDER BY COALESCE(published_at,created_at),source_item_id`, workspace.ID, since, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []NewsletterItem
	for rows.Next() {
		var item NewsletterItem
		if err = rows.Scan(&item.ID, &item.WorkspaceID, &item.SubscriptionID, &item.SourceItemID, &item.NormalizedJSON, &item.PublishedAt, &item.RetainUntil, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) DeleteExpiredNewsletterItems(ctx context.Context, before string) (int64, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return 0, err
	}
	result, err := db.Exec(ctx, `DELETE FROM newsletter_items WHERE workspace_id=? AND retain_until IS NOT NULL AND retain_until!='' AND retain_until<?`, workspace.ID, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

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
func (db *DB) FinalizeOutlookIngestionBatch(ctx context.Context, batchID, emailExpected, emailCandidate, emailCheckpointJSON, calendarExpected, calendarCandidate, calendarCheckpointJSON string, reservationToken ...string) error {
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
	reservationHash := ""
	if len(reservationToken) > 0 && reservationToken[0] != "" {
		reservationHash = actionClaimHash(reservationToken[0])
	}
	var batchState string
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT state FROM outlook_ingestion_batches WHERE id=? AND workspace_id=?`), batchID, ws.ID).Scan(&batchState); err != nil {
		return fmt.Errorf("outlook batch not found")
	}
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
		var reservedExpected, reservedCandidate, reservedHash, leaseUntil string
		reservationErr := tx.QueryRowContext(ctx, db.Rebind(`SELECT expected_value,candidate_value,claim_token_hash,lease_until FROM source_checkpoint_reservations WHERE workspace_id=? AND source_type=? AND source_id='outlook' AND batch_id=?`), ws.ID, checkpoint.sourceType, batchID).Scan(&reservedExpected, &reservedCandidate, &reservedHash, &leaseUntil)
		if reservationErr == nil && (reservedExpected != checkpoint.expected || reservedCandidate != checkpoint.candidate) {
			return fmt.Errorf("%s checkpoint does not match its reservation", checkpoint.sourceType)
		}
		if reservationErr == nil && (reservationHash == "" || reservedHash != reservationHash || leaseUntil < now) {
			return fmt.Errorf("%s checkpoint reservation fence is stale or invalid", checkpoint.sourceType)
		}
		if reservationErr != nil && reservationErr != sql.ErrNoRows {
			return reservationErr
		}
		if reservationErr == sql.ErrNoRows {
			var otherReservations int
			if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM source_checkpoint_reservations WHERE workspace_id=? AND source_type=? AND source_id='outlook'`), ws.ID, checkpoint.sourceType).Scan(&otherReservations); err != nil {
				return err
			}
			if otherReservations != 0 {
				return fmt.Errorf("%s checkpoint is reserved by another batch", checkpoint.sourceType)
			}
			if batchState == "queued" || batchState == "completed" {
				var current string
				if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT checkpoint_value FROM source_checkpoints WHERE workspace_id=? AND source_type=? AND source_id='outlook'`), ws.ID, checkpoint.sourceType).Scan(&current); err == nil && current == checkpoint.candidate {
					continue
				}
			}
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
	if reservationHash != "" {
		result, err = tx.ExecContext(ctx, db.Rebind(`DELETE FROM source_checkpoint_reservations WHERE workspace_id=? AND batch_id=? AND claim_token_hash=?`), ws.ID, batchID, reservationHash)
		if err != nil {
			return err
		}
		deleted, _ := result.RowsAffected()
		if deleted != 2 {
			return fmt.Errorf("outlook reservation fence lost")
		}
	} else {
		var reservations int
		if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM source_checkpoint_reservations WHERE workspace_id=? AND batch_id=?`), ws.ID, batchID).Scan(&reservations); err != nil {
			return err
		}
		if reservations != 0 {
			return fmt.Errorf("outlook reservation fence is required")
		}
	}
	return tx.Commit()
}
