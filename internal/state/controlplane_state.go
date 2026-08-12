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
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id FROM outlook_outbox WHERE workspace_id=? AND (state='pending' OR (state='processing' AND lease_until<?)) ORDER BY created_at LIMIT 1`), ws.ID, FormatTime(now)).Scan(&outboxID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leaseUntil := FormatTime(now.Add(lease))
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE outlook_outbox SET state='processing',attempts=attempts+1,lease_owner=?,lease_until=?,updated_at=? WHERE id=? AND workspace_id=? AND (state='pending' OR (state='processing' AND lease_until<?))`), owner, leaseUntil, Now(), outboxID, ws.ID, FormatTime(now))
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, nil
	}
	var v OutlookOutbox
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,batch_id,item_id,processing_key,state,attempts,COALESCE(lease_owner,''),COALESCE(lease_until,''),COALESCE(processed_at,''),COALESCE(last_error,''),created_at,updated_at FROM outlook_outbox WHERE id=?`), outboxID).Scan(&v.ID, &v.WorkspaceID, &v.BatchID, &v.ItemID, &v.ProcessingKey, &v.State, &v.Attempts, &v.LeaseOwner, &v.LeaseUntil, &v.ProcessedAt, &v.LastError, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &v, tx.Commit()
}

func (db *DB) CompleteOutlookOutbox(ctx context.Context, outboxID, owner, resultJSON string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if outboxID == "" || owner == "" {
		return fmt.Errorf("outlook outbox id and worker are required")
	}
	if resultJSON == "" {
		resultJSON = "{}"
	}
	result, err := db.Exec(ctx, `UPDATE outlook_outbox SET state='completed',processed_at=?,lease_owner=NULL,lease_until=NULL,last_error=NULL,updated_at=? WHERE id=? AND workspace_id=? AND state='processing' AND lease_owner=?`, Now(), Now(), outboxID, ws.ID, owner)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("outlook outbox fence lost")
	}
	return nil
}

func (db *DB) RecordOutlookIngestionReceipt(ctx context.Context, batchID, itemID, receiptState, resultJSON, errorText string) (*OutlookIngestionReceipt, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if batchID == "" || itemID == "" || receiptState == "" {
		return nil, fmt.Errorf("outlook receipt batch, item, and state are required")
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
	err = db.QueryRow(ctx, `SELECT id,workspace_id,batch_id,item_id,state,result_json,error,created_at FROM outlook_ingestion_receipts WHERE batch_id=? AND item_id=?`, batchID, itemID).Scan(&v.ID, &v.WorkspaceID, &v.BatchID, &v.ItemID, &v.State, &v.ResultJSON, &storedError, &v.CreatedAt)
	v.Error = storedError.String
	return &v, err
}

func (db *DB) UpsertAgentTaskCheckpoint(ctx context.Context, in AgentTaskCheckpointInput) (*AgentTaskCheckpoint, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	if in.AgentID == "" || in.AgentRunID == "" || in.CheckpointKey == "" || in.StateJSON == "" {
		return nil, fmt.Errorf("agent checkpoint fields are required")
	}
	if in.Status == "" {
		in.Status = "saved"
	}
	now := Now()
	_, e = db.Exec(ctx, `INSERT INTO agent_task_checkpoints(id,workspace_id,agent_id,agent_run_id,checkpoint_key,state_json,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,agent_run_id,checkpoint_key) DO UPDATE SET state_json=excluded.state_json,status=excluded.status,updated_at=excluded.updated_at`, id.New("atcp"), ws.ID, in.AgentID, in.AgentRunID, in.CheckpointKey, in.StateJSON, in.Status, now, now)
	if e != nil {
		return nil, e
	}
	var v AgentTaskCheckpoint
	e = db.QueryRow(ctx, `SELECT id,workspace_id,agent_id,agent_run_id,checkpoint_key,state_json,status,created_at,updated_at FROM agent_task_checkpoints WHERE workspace_id=? AND agent_run_id=? AND checkpoint_key=?`, ws.ID, in.AgentRunID, in.CheckpointKey).Scan(&v.ID, &v.WorkspaceID, &v.AgentID, &v.AgentRunID, &v.CheckpointKey, &v.StateJSON, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return &v, e
}

// TriggerAgentRun durably reserves the caller's idempotency key before it
// creates a run. A retry can therefore only observe the original trigger,
// never create a second run for the same external request.
func (db *DB) TriggerAgentRun(ctx context.Context, in AgentRunTriggerInput) (*AgentRunTrigger, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.AgentID == "" || in.IdempotencyKey == "" || in.Trigger == "" {
		return nil, fmt.Errorf("agent trigger id, trigger, and idempotency key are required")
	}
	if in.InputJSON == "" {
		in.InputJSON = "{}"
	}
	now := Now()
	trigger := &AgentRunTrigger{ID: id.New("atrigger"), WorkspaceID: ws.ID, AgentID: in.AgentID, IdempotencyKey: in.IdempotencyKey, Trigger: in.Trigger, InputJSON: in.InputJSON, RepositoryID: in.RepositoryID, ParentRunID: in.ParentRunID, State: "accepted", CreatedAt: now, UpdatedAt: now}
	_, err = db.Exec(ctx, `INSERT INTO agent_run_triggers(id,workspace_id,agent_id,idempotency_key,trigger,input_json,repository_id,parent_run_id,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,agent_id,idempotency_key) DO NOTHING`, trigger.ID, trigger.WorkspaceID, trigger.AgentID, trigger.IdempotencyKey, trigger.Trigger, trigger.InputJSON, nullStr(trigger.RepositoryID), nullStr(trigger.ParentRunID), trigger.State, now, now)
	if err != nil {
		return nil, err
	}
	existing, err := db.getAgentRunTrigger(ctx, in.AgentID, in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing.AgentRunID != "" {
		return existing, nil
	}
	claim, err := db.Exec(ctx, `UPDATE agent_run_triggers SET state='creating',updated_at=? WHERE id=? AND state='accepted' AND agent_run_id IS NULL`, Now(), existing.ID)
	if err != nil {
		return nil, err
	}
	claimed, _ := claim.RowsAffected()
	if claimed == 0 {
		return db.getAgentRunTrigger(ctx, in.AgentID, in.IdempotencyKey)
	}
	runRecord, err := db.CreateAgentRun(ctx, in.AgentID, in.Trigger, in.InputJSON, in.RepositoryID, in.ParentRunID, in.RateSnapshot)
	if err != nil {
		_, _ = db.Exec(ctx, `UPDATE agent_run_triggers SET state='accepted',updated_at=? WHERE id=? AND state='creating'`, Now(), existing.ID)
		return nil, err
	}
	result, err := db.Exec(ctx, `UPDATE agent_run_triggers SET agent_run_id=?,state='created',updated_at=? WHERE id=? AND state='creating' AND agent_run_id IS NULL`, runRecord.ID, Now(), existing.ID)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return db.getAgentRunTrigger(ctx, in.AgentID, in.IdempotencyKey)
	}
	return db.getAgentRunTrigger(ctx, in.AgentID, in.IdempotencyKey)
}

func (db *DB) getAgentRunTrigger(ctx context.Context, agentID, key string) (*AgentRunTrigger, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var v AgentRunTrigger
	var repo, parent, run sql.NullString
	err = db.QueryRow(ctx, `SELECT id,workspace_id,agent_id,idempotency_key,trigger,input_json,repository_id,parent_run_id,agent_run_id,state,created_at,updated_at FROM agent_run_triggers WHERE workspace_id=? AND agent_id=? AND idempotency_key=?`, ws.ID, agentID, key).Scan(&v.ID, &v.WorkspaceID, &v.AgentID, &v.IdempotencyKey, &v.Trigger, &v.InputJSON, &repo, &parent, &run, &v.State, &v.CreatedAt, &v.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent trigger not found")
	}
	v.RepositoryID = repo.String
	v.ParentRunID = parent.String
	v.AgentRunID = run.String
	return &v, err
}
