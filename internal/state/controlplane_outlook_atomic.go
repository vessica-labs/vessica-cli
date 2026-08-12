package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

// UpsertOutlookIngestionItemAndEnqueue makes source deduplication and durable
// delivery inseparable: a newly accepted item is never visible without outbox
// work in the same committed transaction.
func (db *DB) UpsertOutlookIngestionItemAndEnqueue(ctx context.Context, in OutlookIngestionItemInput, processingKey string) (*OutlookIngestionItem, *OutlookOutbox, bool, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	if in.BatchID == "" || in.SourceID == "" || in.NormalizedJSON == "" || processingKey == "" {
		return nil, nil, false, fmt.Errorf("outlook item and processing key are required")
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, err
	}
	defer tx.Rollback()
	var batchExists int
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM outlook_ingestion_batches WHERE id=? AND workspace_id=?`), in.BatchID, ws.ID).Scan(&batchExists); err != nil || batchExists != 1 {
		return nil, nil, false, fmt.Errorf("outlook batch not found")
	}
	now := Now()
	itemID := id.New("oitem")
	result, err := tx.ExecContext(ctx, db.Rebind(`INSERT INTO outlook_ingestion_items(id,workspace_id,batch_id,source_id,internet_message_id,conversation_id,message_at,normalized_json,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,source_id) DO NOTHING`), itemID, ws.ID, in.BatchID, in.SourceID, nullStr(in.InternetMessageID), nullStr(in.ConversationID), nullStr(in.MessageAt), in.NormalizedJSON, "received", now, now)
	if err != nil {
		return nil, nil, false, err
	}
	inserted, _ := result.RowsAffected()
	item, err := scanOutlookItem(tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,batch_id,source_id,internet_message_id,conversation_id,message_at,normalized_json,state,error,created_at,updated_at FROM outlook_ingestion_items WHERE workspace_id=? AND source_id=?`), ws.ID, in.SourceID))
	if err != nil {
		return nil, nil, false, err
	}
	duplicate := inserted == 0
	if duplicate {
		outbox, outboxErr := scanOutlookOutbox(tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,batch_id,item_id,processing_key,state,attempts,COALESCE(lease_owner,''),COALESCE(lease_until,''),available_at,COALESCE(processed_at,''),COALESCE(last_error,''),created_at,updated_at FROM outlook_outbox WHERE workspace_id=? AND item_id=? ORDER BY created_at LIMIT 1`), ws.ID, item.ID))
		if outboxErr != nil {
			return nil, nil, false, fmt.Errorf("deduplicated outlook item has no durable outbox: %w", outboxErr)
		}
		if err = tx.Commit(); err != nil {
			return nil, nil, false, err
		}
		return item, outbox, true, nil
	}
	outboxID := id.New("outbox")
	result, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO outlook_outbox(id,workspace_id,batch_id,item_id,processing_key,state,created_at,updated_at) VALUES(?,?,?,?,?,'pending',?,?) ON CONFLICT(workspace_id,processing_key) DO NOTHING`), outboxID, ws.ID, in.BatchID, item.ID, processingKey, now, now)
	if err != nil {
		return nil, nil, false, err
	}
	inserted, _ = result.RowsAffected()
	if inserted != 1 {
		return nil, nil, false, fmt.Errorf("outlook processing key already belongs to another item")
	}
	outbox, err := scanOutlookOutbox(tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,batch_id,item_id,processing_key,state,attempts,COALESCE(lease_owner,''),COALESCE(lease_until,''),available_at,COALESCE(processed_at,''),COALESCE(last_error,''),created_at,updated_at FROM outlook_outbox WHERE id=? AND workspace_id=?`), outboxID, ws.ID))
	if err != nil {
		return nil, nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	return item, outbox, false, nil
}

func scanOutlookItem(row interface{ Scan(...any) error }) (*OutlookIngestionItem, error) {
	var v OutlookIngestionItem
	var internet, conversation, messageAt, errText sql.NullString
	err := row.Scan(&v.ID, &v.WorkspaceID, &v.BatchID, &v.SourceID, &internet, &conversation, &messageAt, &v.NormalizedJSON, &v.State, &errText, &v.CreatedAt, &v.UpdatedAt)
	v.InternetMessageID, v.ConversationID, v.MessageAt, v.Error = internet.String, conversation.String, messageAt.String, errText.String
	return &v, err
}

func scanOutlookOutbox(row interface{ Scan(...any) error }) (*OutlookOutbox, error) {
	var v OutlookOutbox
	err := row.Scan(&v.ID, &v.WorkspaceID, &v.BatchID, &v.ItemID, &v.ProcessingKey, &v.State, &v.Attempts, &v.LeaseOwner, &v.LeaseUntil, &v.AvailableAt, &v.ProcessedAt, &v.LastError, &v.CreatedAt, &v.UpdatedAt)
	return &v, err
}
