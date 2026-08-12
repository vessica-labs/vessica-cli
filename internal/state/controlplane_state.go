package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
)

func (db *DB) AppendActionLedger(ctx context.Context, in ActionLedgerInput) (*ActionLedger, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.ActorID == "" || in.Tool == "" || in.PolicyDecision == "" || in.IdempotencyKey == "" {
		return nil, fmt.Errorf("action ledger actor, tool, decision, and idempotency key are required")
	}
	if in.RedactedArgumentsJSON == "" {
		in.RedactedArgumentsJSON = "{}"
	}
	if in.ResultJSON == "" {
		in.ResultJSON = "{}"
	}
	if in.ExternalIDsJSON == "" {
		in.ExternalIDsJSON = "[]"
	}
	v := &ActionLedger{ID: id.New("act"), WorkspaceID: ws.ID, ActorID: in.ActorID, AgentID: in.AgentID, AgentRunID: in.AgentRunID, Tool: in.Tool, PolicyDecision: in.PolicyDecision, RedactedArgumentsJSON: redaction.Redact(in.RedactedArgumentsJSON), ResultJSON: redaction.Redact(in.ResultJSON), LatencyMS: in.LatencyMS, IdempotencyKey: in.IdempotencyKey, ExternalIDsJSON: in.ExternalIDsJSON, CreatedAt: Now()}
	_, err = db.Exec(ctx, `INSERT INTO action_ledger(id,workspace_id,actor_id,agent_id,agent_run_id,tool,policy_decision,redacted_arguments_json,result_json,latency_ms,idempotency_key,external_ids_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,idempotency_key) DO NOTHING`, v.ID, v.WorkspaceID, v.ActorID, nullStr(v.AgentID), nullStr(v.AgentRunID), v.Tool, v.PolicyDecision, v.RedactedArgumentsJSON, v.ResultJSON, v.LatencyMS, v.IdempotencyKey, v.ExternalIDsJSON, v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return db.GetActionLedgerByKey(ctx, in.IdempotencyKey)
}
func (db *DB) GetActionLedgerByKey(ctx context.Context, key string) (*ActionLedger, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	var v ActionLedger
	var agent, run sql.NullString
	e = db.QueryRow(ctx, `SELECT id,workspace_id,actor_id,agent_id,agent_run_id,tool,policy_decision,redacted_arguments_json,result_json,latency_ms,idempotency_key,external_ids_json,created_at FROM action_ledger WHERE workspace_id=? AND idempotency_key=?`, ws.ID, key).Scan(&v.ID, &v.WorkspaceID, &v.ActorID, &agent, &run, &v.Tool, &v.PolicyDecision, &v.RedactedArgumentsJSON, &v.ResultJSON, &v.LatencyMS, &v.IdempotencyKey, &v.ExternalIDsJSON, &v.CreatedAt)
	if e == sql.ErrNoRows {
		return nil, fmt.Errorf("action ledger entry not found")
	}
	v.AgentID = agent.String
	v.AgentRunID = run.String
	return &v, e
}

func (db *DB) CreateConversation(ctx context.Context, in ConversationInput) (*Conversation, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	if in.ActorID == "" {
		return nil, fmt.Errorf("conversation actor is required")
	}
	now := Now()
	v := &Conversation{ID: id.New("conv"), WorkspaceID: ws.ID, ActorID: in.ActorID, Title: in.Title, Status: "active", CreatedAt: now, UpdatedAt: now}
	_, e = db.Exec(ctx, `INSERT INTO conversations(id,workspace_id,actor_id,title,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.ActorID, v.Title, v.Status, now, now)
	return v, e
}
func (db *DB) AppendConversationMessage(ctx context.Context, conversationID string, in ConversationMessageInput) (*ConversationMessage, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	if in.Role == "" || in.ContentJSON == "" {
		return nil, fmt.Errorf("conversation message role and content are required")
	}
	if in.MetadataJSON == "" {
		in.MetadataJSON = "{}"
	}
	tx, e := db.SQL.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	res, e := tx.ExecContext(ctx, db.Rebind(`UPDATE conversations SET next_message_seq=next_message_seq+1,updated_at=? WHERE id=? AND workspace_id=? AND status='active'`), Now(), conversationID, ws.ID)
	if e != nil {
		return nil, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, fmt.Errorf("active conversation not found")
	}
	var seq int64
	if e = tx.QueryRowContext(ctx, db.Rebind(`SELECT next_message_seq FROM conversations WHERE id=? AND workspace_id=?`), conversationID, ws.ID).Scan(&seq); e != nil {
		return nil, e
	}
	v := &ConversationMessage{ID: id.New("msg"), ConversationID: conversationID, WorkspaceID: ws.ID, Sequence: seq, Role: in.Role, ContentJSON: in.ContentJSON, MetadataJSON: in.MetadataJSON, CreatedAt: Now()}
	_, e = tx.ExecContext(ctx, db.Rebind(`INSERT INTO conversation_messages(id,conversation_id,workspace_id,sequence,role,content_json,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?)`), v.ID, v.ConversationID, v.WorkspaceID, v.Sequence, v.Role, v.ContentJSON, v.MetadataJSON, v.CreatedAt)
	if e != nil {
		return nil, e
	}
	return v, tx.Commit()
}
func (db *DB) ListConversationMessages(ctx context.Context, conversationID string, after int64) ([]ConversationMessage, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := db.Query(ctx, `SELECT id,conversation_id,workspace_id,sequence,role,content_json,metadata_json,created_at FROM conversation_messages WHERE workspace_id=? AND conversation_id=? AND sequence>? ORDER BY sequence`, ws.ID, conversationID, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []ConversationMessage
	for rows.Next() {
		var v ConversationMessage
		if e = rows.Scan(&v.ID, &v.ConversationID, &v.WorkspaceID, &v.Sequence, &v.Role, &v.ContentJSON, &v.MetadataJSON, &v.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (db *DB) UpsertSourceCheckpoint(ctx context.Context, typ, sourceID, checkpointJSON string) error {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return e
	}
	if typ == "" || sourceID == "" {
		return fmt.Errorf("source checkpoint type and id are required")
	}
	if checkpointJSON == "" {
		checkpointJSON = "{}"
	}
	_, e = db.Exec(ctx, `INSERT INTO source_checkpoints(workspace_id,source_type,source_id,checkpoint_json,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(workspace_id,source_type,source_id) DO UPDATE SET checkpoint_json=excluded.checkpoint_json,updated_at=excluded.updated_at`, ws.ID, typ, sourceID, checkpointJSON, Now())
	return e
}
func (db *DB) GetSourceCheckpoint(ctx context.Context, typ, sourceID string) (*SourceCheckpoint, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	var v SourceCheckpoint
	e = db.QueryRow(ctx, `SELECT workspace_id,source_type,source_id,checkpoint_json,updated_at FROM source_checkpoints WHERE workspace_id=? AND source_type=? AND source_id=?`, ws.ID, typ, sourceID).Scan(&v.WorkspaceID, &v.SourceType, &v.SourceID, &v.CheckpointJSON, &v.UpdatedAt)
	if e == sql.ErrNoRows {
		return nil, fmt.Errorf("source checkpoint not found")
	}
	return &v, e
}
func (db *DB) UpsertNewsletterSubscription(ctx context.Context, in NewsletterSubscriptionInput) (*NewsletterSubscription, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	if in.SourceKey == "" || in.SourceURL == "" {
		return nil, fmt.Errorf("newsletter source key and url are required")
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
	_, e = db.Exec(ctx, `INSERT INTO newsletter_subscriptions(id,workspace_id,source_key,source_url,title,status,retention_days,metadata_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,source_key) DO UPDATE SET source_url=excluded.source_url,title=excluded.title,status=excluded.status,retention_days=excluded.retention_days,metadata_json=excluded.metadata_json,updated_at=excluded.updated_at`, id.New("nsource"), ws.ID, in.SourceKey, in.SourceURL, in.Title, in.Status, in.RetentionDays, in.MetadataJSON, now, now)
	if e != nil {
		return nil, e
	}
	var v NewsletterSubscription
	e = db.QueryRow(ctx, `SELECT id,workspace_id,source_key,source_url,title,status,retention_days,metadata_json,created_at,updated_at FROM newsletter_subscriptions WHERE workspace_id=? AND source_key=?`, ws.ID, in.SourceKey).Scan(&v.ID, &v.WorkspaceID, &v.SourceKey, &v.SourceURL, &v.Title, &v.Status, &v.RetentionDays, &v.MetadataJSON, &v.CreatedAt, &v.UpdatedAt)
	return &v, e
}
func (db *DB) UpsertNewsletterItem(ctx context.Context, in NewsletterItemInput) (*NewsletterItem, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	if in.SubscriptionID == "" || in.SourceItemID == "" || in.NormalizedJSON == "" {
		return nil, fmt.Errorf("newsletter item subscription, source id, and normalized content are required")
	}
	if e = db.requireWorkspaceRecord(ctx, "newsletter_subscriptions", in.SubscriptionID); e != nil {
		return nil, e
	}
	now := Now()
	_, e = db.Exec(ctx, `INSERT INTO newsletter_items(id,workspace_id,subscription_id,source_item_id,normalized_json,published_at,retain_until,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(subscription_id,source_item_id) DO UPDATE SET normalized_json=excluded.normalized_json,published_at=excluded.published_at,retain_until=excluded.retain_until,updated_at=excluded.updated_at`, id.New("nitem"), ws.ID, in.SubscriptionID, in.SourceItemID, in.NormalizedJSON, nullStr(in.PublishedAt), nullStr(in.RetainUntil), now, now)
	if e != nil {
		return nil, e
	}
	var v NewsletterItem
	var published, retained sql.NullString
	e = db.QueryRow(ctx, `SELECT id,workspace_id,subscription_id,source_item_id,normalized_json,published_at,retain_until,created_at,updated_at FROM newsletter_items WHERE workspace_id=? AND subscription_id=? AND source_item_id=?`, ws.ID, in.SubscriptionID, in.SourceItemID).Scan(&v.ID, &v.WorkspaceID, &v.SubscriptionID, &v.SourceItemID, &v.NormalizedJSON, &published, &retained, &v.CreatedAt, &v.UpdatedAt)
	v.PublishedAt = published.String
	v.RetainUntil = retained.String
	return &v, e
}

func (db *DB) CreateOutlookIngestionBatch(ctx context.Context, in OutlookIngestionBatchInput) (*OutlookIngestionBatch, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	if in.IdempotencyKey == "" || in.SubmittedBy == "" {
		return nil, fmt.Errorf("outlook batch idempotency key and submitter are required")
	}
	if in.CheckpointJSON == "" {
		in.CheckpointJSON = "{}"
	}
	if in.WarningsJSON == "" {
		in.WarningsJSON = "[]"
	}
	now := Now()
	_, e = db.Exec(ctx, `INSERT INTO outlook_ingestion_batches(id,workspace_id,idempotency_key,submitted_by,state,checkpoint_json,warnings_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,idempotency_key) DO NOTHING`, id.New("obatch"), ws.ID, in.IdempotencyKey, in.SubmittedBy, "received", in.CheckpointJSON, in.WarningsJSON, now, now)
	if e != nil {
		return nil, e
	}
	return db.getOutlookBatch(ctx, in.IdempotencyKey)
}
func (db *DB) getOutlookBatch(ctx context.Context, key string) (*OutlookIngestionBatch, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	var v OutlookIngestionBatch
	var errText, completed sql.NullString
	e = db.QueryRow(ctx, `SELECT id,workspace_id,idempotency_key,submitted_by,state,checkpoint_json,warnings_json,error,created_at,updated_at,completed_at FROM outlook_ingestion_batches WHERE workspace_id=? AND idempotency_key=?`, ws.ID, key).Scan(&v.ID, &v.WorkspaceID, &v.IdempotencyKey, &v.SubmittedBy, &v.State, &v.CheckpointJSON, &v.WarningsJSON, &errText, &v.CreatedAt, &v.UpdatedAt, &completed)
	if e == sql.ErrNoRows {
		return nil, fmt.Errorf("outlook batch not found")
	}
	v.Error = errText.String
	v.CompletedAt = completed.String
	return &v, e
}
func (db *DB) UpsertOutlookIngestionItem(ctx context.Context, in OutlookIngestionItemInput) (*OutlookIngestionItem, bool, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, false, e
	}
	if in.BatchID == "" || in.SourceID == "" || in.NormalizedJSON == "" {
		return nil, false, fmt.Errorf("outlook item batch, source id, and normalized content are required")
	}
	if e = db.requireWorkspaceRecord(ctx, "outlook_ingestion_batches", in.BatchID); e != nil {
		return nil, false, e
	}
	now := Now()
	res, e := db.Exec(ctx, `INSERT INTO outlook_ingestion_items(id,workspace_id,batch_id,source_id,internet_message_id,conversation_id,message_at,normalized_json,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,source_id) DO NOTHING`, id.New("oitem"), ws.ID, in.BatchID, in.SourceID, nullStr(in.InternetMessageID), nullStr(in.ConversationID), nullStr(in.MessageAt), in.NormalizedJSON, "received", now, now)
	if e != nil {
		return nil, false, e
	}
	n, _ := res.RowsAffected()
	var v OutlookIngestionItem
	var internet, conversation, messageAt, errText sql.NullString
	e = db.QueryRow(ctx, `SELECT id,workspace_id,batch_id,source_id,internet_message_id,conversation_id,message_at,normalized_json,state,error,created_at,updated_at FROM outlook_ingestion_items WHERE workspace_id=? AND source_id=?`, ws.ID, in.SourceID).Scan(&v.ID, &v.WorkspaceID, &v.BatchID, &v.SourceID, &internet, &conversation, &messageAt, &v.NormalizedJSON, &v.State, &errText, &v.CreatedAt, &v.UpdatedAt)
	v.InternetMessageID = internet.String
	v.ConversationID = conversation.String
	v.MessageAt = messageAt.String
	v.Error = errText.String
	return &v, n == 0, e
}
func (db *DB) EnqueueOutlookOutbox(ctx context.Context, batchID, itemID, key string) (*OutlookOutbox, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	if batchID == "" || itemID == "" || key == "" {
		return nil, fmt.Errorf("outlook outbox batch, item, and processing key are required")
	}
	if e = db.requireWorkspaceRecord(ctx, "outlook_ingestion_batches", batchID); e != nil {
		return nil, e
	}
	if e = db.requireOutlookItemForBatch(ctx, batchID, itemID); e != nil {
		return nil, e
	}
	now := Now()
	_, e = db.Exec(ctx, `INSERT INTO outlook_outbox(id,workspace_id,batch_id,item_id,processing_key,state,created_at,updated_at) VALUES(?,?,?,?,?,'pending',?,?) ON CONFLICT(workspace_id,processing_key) DO NOTHING`, id.New("outbox"), ws.ID, batchID, itemID, key, now, now)
	if e != nil {
		return nil, e
	}
	var v OutlookOutbox
	var owner, until, processed, last sql.NullString
	e = db.QueryRow(ctx, `SELECT id,workspace_id,batch_id,item_id,processing_key,state,attempts,lease_owner,lease_until,processed_at,last_error,created_at,updated_at FROM outlook_outbox WHERE workspace_id=? AND processing_key=?`, ws.ID, key).Scan(&v.ID, &v.WorkspaceID, &v.BatchID, &v.ItemID, &v.ProcessingKey, &v.State, &v.Attempts, &owner, &until, &processed, &last, &v.CreatedAt, &v.UpdatedAt)
	v.LeaseOwner = owner.String
	v.LeaseUntil = until.String
	v.ProcessedAt = processed.String
	v.LastError = last.String
	return &v, e
}

func (db *DB) ClaimOutlookOutbox(ctx context.Context, owner string, lease time.Duration) (*OutlookOutbox, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if owner == "" {
		return nil, fmt.Errorf("outlook outbox worker is required")
	}
	if lease <= 0 {
		lease = time.Minute
	}
	now := time.Now().UTC()
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var outboxID string
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id FROM outlook_outbox WHERE workspace_id=? AND ((state='pending' AND available_at<=?) OR (state='processing' AND lease_until<?)) ORDER BY created_at LIMIT 1`), ws.ID, FormatTime(now), FormatTime(now)).Scan(&outboxID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leaseUntil := FormatTime(now.Add(lease))
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE outlook_outbox SET state='processing',attempts=attempts+1,lease_owner=?,lease_until=?,updated_at=? WHERE id=? AND workspace_id=? AND ((state='pending' AND available_at<=?) OR (state='processing' AND lease_until<?))`), owner, leaseUntil, Now(), outboxID, ws.ID, FormatTime(now), FormatTime(now))
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, nil
	}
	var v OutlookOutbox
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,batch_id,item_id,processing_key,state,attempts,COALESCE(lease_owner,''),COALESCE(lease_until,''),available_at,COALESCE(processed_at,''),COALESCE(last_error,''),created_at,updated_at FROM outlook_outbox WHERE id=?`), outboxID).Scan(&v.ID, &v.WorkspaceID, &v.BatchID, &v.ItemID, &v.ProcessingKey, &v.State, &v.Attempts, &v.LeaseOwner, &v.LeaseUntil, &v.AvailableAt, &v.ProcessedAt, &v.LastError, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &v, tx.Commit()
}

func (db *DB) FailOutlookOutbox(ctx context.Context, outboxID, owner, errorText string, retryAt time.Time) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if outboxID == "" || owner == "" || errorText == "" {
		return fmt.Errorf("outlook failure fields are required")
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var batchID, itemID string
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT batch_id,item_id FROM outlook_outbox WHERE id=? AND workspace_id=? AND state='processing' AND lease_owner=?`), outboxID, ws.ID, owner).Scan(&batchID, &itemID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("outlook outbox fence lost")
	}
	if err != nil {
		return err
	}
	now := Now()
	available := FormatTime(retryAt)
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE outlook_outbox SET state='pending',available_at=?,lease_owner=NULL,lease_until=NULL,last_error=?,updated_at=? WHERE id=? AND workspace_id=? AND state='processing' AND lease_owner=?`), available, errorText, now, outboxID, ws.ID, owner)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("outlook outbox fence lost")
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE outlook_ingestion_items SET state='retrying',error=?,updated_at=? WHERE id=? AND batch_id=? AND workspace_id=?`), errorText, now, itemID, batchID, ws.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE outlook_ingestion_batches SET state='processing',error=?,updated_at=? WHERE id=? AND workspace_id=?`), errorText, now, batchID, ws.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) CompleteOutlookOutbox(ctx context.Context, outboxID, owner, resultJSON string) error {
	return db.CompleteOutlookOutboxAtomically(ctx, outboxID, owner, "accepted", resultJSON)
}

// CompleteOutlookOutboxAtomically makes the external processing outcome,
// receipt, item state, and batch terminal state one durable transition.
func (db *DB) CompleteOutlookOutboxAtomically(ctx context.Context, outboxID, owner, receiptState, resultJSON string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if outboxID == "" || owner == "" || receiptState == "" {
		return fmt.Errorf("outlook completion fields are required")
	}
	if resultJSON == "" {
		resultJSON = "{}"
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var batchID, itemID string
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT batch_id,item_id FROM outlook_outbox WHERE id=? AND workspace_id=? AND state='processing' AND lease_owner=?`), outboxID, ws.ID, owner).Scan(&batchID, &itemID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("outlook outbox fence lost")
	}
	if err != nil {
		return err
	}
	now := Now()
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO outlook_ingestion_receipts(id,workspace_id,batch_id,item_id,state,result_json,error,created_at) VALUES(?,?,?,?,?,?,NULL,?) ON CONFLICT(batch_id,item_id) DO UPDATE SET state=excluded.state,result_json=excluded.result_json,error=NULL`), id.New("oreceipt"), ws.ID, batchID, itemID, receiptState, redaction.Redact(resultJSON), now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE outlook_ingestion_items SET state='completed',error=NULL,updated_at=? WHERE id=? AND batch_id=? AND workspace_id=?`), now, itemID, batchID, ws.ID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE outlook_outbox SET state='completed',processed_at=?,lease_owner=NULL,lease_until=NULL,last_error=NULL,updated_at=? WHERE id=? AND workspace_id=? AND state='processing' AND lease_owner=?`), now, now, outboxID, ws.ID, owner)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("outlook outbox fence lost")
	}
	var remaining int
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM outlook_outbox WHERE batch_id=? AND workspace_id=? AND state!='completed'`), batchID, ws.ID).Scan(&remaining); err != nil {
		return err
	}
	batchState, completedAt := "processing", ""
	if remaining == 0 {
		batchState, completedAt = "completed", now
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE outlook_ingestion_batches SET state=?,completed_at=?,error=NULL,updated_at=? WHERE id=? AND workspace_id=?`), batchState, nullStr(completedAt), now, batchID, ws.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) RecordOutlookIngestionReceipt(ctx context.Context, batchID, itemID, receiptState, resultJSON, errorText string) (*OutlookIngestionReceipt, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if batchID == "" || itemID == "" || receiptState == "" {
		return nil, fmt.Errorf("outlook receipt batch, item, and state are required")
	}
	if err = db.requireWorkspaceRecord(ctx, "outlook_ingestion_batches", batchID); err != nil {
		return nil, err
	}
	if err = db.requireOutlookItemForBatch(ctx, batchID, itemID); err != nil {
		return nil, err
	}
	if resultJSON == "" {
		resultJSON = "{}"
	}
	now := Now()
	_, err = db.Exec(ctx, `INSERT INTO outlook_ingestion_receipts(id,workspace_id,batch_id,item_id,state,result_json,error,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(batch_id,item_id) DO UPDATE SET state=excluded.state,result_json=excluded.result_json,error=excluded.error`, id.New("oreceipt"), ws.ID, batchID, itemID, receiptState, redaction.Redact(resultJSON), nullStr(errorText), now)
	if err != nil {
		return nil, err
	}
	var v OutlookIngestionReceipt
	var storedError sql.NullString
	err = db.QueryRow(ctx, `SELECT id,workspace_id,batch_id,item_id,state,result_json,error,created_at FROM outlook_ingestion_receipts WHERE workspace_id=? AND batch_id=? AND item_id=?`, ws.ID, batchID, itemID).Scan(&v.ID, &v.WorkspaceID, &v.BatchID, &v.ItemID, &v.State, &v.ResultJSON, &storedError, &v.CreatedAt)
	v.Error = storedError.String
	return &v, err
}

func (db *DB) SetOutlookIngestionItemState(ctx context.Context, itemID, itemState, errorText string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if itemState == "" {
		return fmt.Errorf("outlook item state is required")
	}
	result, err := db.Exec(ctx, `UPDATE outlook_ingestion_items SET state=?,error=?,updated_at=? WHERE id=? AND workspace_id=?`, itemState, nullStr(errorText), Now(), itemID, ws.ID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("outlook item not found in workspace")
	}
	return nil
}

func (db *DB) SetOutlookIngestionBatchState(ctx context.Context, batchID, batchState, errorText string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if batchState == "" {
		return fmt.Errorf("outlook batch state is required")
	}
	completedAt := ""
	if batchState == "completed" {
		completedAt = Now()
	}
	result, err := db.Exec(ctx, `UPDATE outlook_ingestion_batches SET state=?,error=?,completed_at=?,updated_at=? WHERE id=? AND workspace_id=?`, batchState, nullStr(errorText), nullStr(completedAt), Now(), batchID, ws.ID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("outlook batch not found in workspace")
	}
	return nil
}
