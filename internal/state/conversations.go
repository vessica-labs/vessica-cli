package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) CreateConversation(ctx context.Context, in ConversationInput) (*Conversation, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.ActorID == "" {
		return nil, fmt.Errorf("conversation actor is required")
	}
	if in.AgentID != "" {
		if err = db.requireWorkspaceRecord(ctx, "agents", in.AgentID); err != nil {
			return nil, fmt.Errorf("conversation agent is not available in this workspace")
		}
	}
	now := Now()
	value := &Conversation{ID: id.New("conv"), WorkspaceID: ws.ID, ActorID: in.ActorID, AgentID: in.AgentID, Title: in.Title, Status: "active", CreatedAt: now, UpdatedAt: now}
	_, err = db.Exec(ctx, `INSERT INTO conversations(id,workspace_id,actor_id,agent_id,title,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, value.ID, value.WorkspaceID, value.ActorID, value.AgentID, value.Title, value.Status, now, now)
	return value, err
}

func (db *DB) GetConversation(ctx context.Context, conversationID string) (*Conversation, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	value := &Conversation{}
	if err = scanConversation(db.QueryRow(ctx, `SELECT id,workspace_id,actor_id,agent_id,title,status,created_at,updated_at FROM conversations WHERE id=? AND workspace_id=?`, conversationID, ws.ID), value); err == sql.ErrNoRows {
		return nil, fmt.Errorf("conversation not found")
	}
	return value, err
}

func (db *DB) ListConversations(ctx context.Context) ([]Conversation, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT id,workspace_id,actor_id,agent_id,title,status,created_at,updated_at FROM conversations WHERE workspace_id=? ORDER BY updated_at DESC,id DESC`, ws.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var value Conversation
		if err = scanConversation(rows, &value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (db *DB) SetConversationAgent(ctx context.Context, conversationID, agentID string) (*Conversation, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if err = db.requireWorkspaceRecord(ctx, "agents", agentID); err != nil {
		return nil, fmt.Errorf("conversation agent is not available in this workspace")
	}
	result, err := db.Exec(ctx, `UPDATE conversations SET agent_id=?,updated_at=? WHERE id=? AND workspace_id=? AND status='active'`, agentID, Now(), conversationID, ws.ID)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, fmt.Errorf("active conversation not found")
	}
	return db.GetConversation(ctx, conversationID)
}

func (db *DB) AppendConversationMessage(ctx context.Context, conversationID string, in ConversationMessageInput) (*ConversationMessage, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.Role == "" || in.ContentJSON == "" {
		return nil, fmt.Errorf("conversation message role and content are required")
	}
	if in.MetadataJSON == "" {
		in.MetadataJSON = "{}"
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE conversations SET next_message_seq=next_message_seq+1,updated_at=? WHERE id=? AND workspace_id=? AND status='active'`), Now(), conversationID, ws.ID)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, fmt.Errorf("active conversation not found")
	}
	var sequence int64
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT next_message_seq FROM conversations WHERE id=? AND workspace_id=?`), conversationID, ws.ID).Scan(&sequence); err != nil {
		return nil, err
	}
	value := &ConversationMessage{ID: id.New("msg"), ConversationID: conversationID, WorkspaceID: ws.ID, Sequence: sequence, Role: in.Role, ContentJSON: in.ContentJSON, MetadataJSON: in.MetadataJSON, CreatedAt: Now()}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO conversation_messages(id,conversation_id,workspace_id,sequence,role,content_json,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?)`), value.ID, value.ConversationID, value.WorkspaceID, value.Sequence, value.Role, value.ContentJSON, value.MetadataJSON, value.CreatedAt); err != nil {
		return nil, err
	}
	return value, tx.Commit()
}

func (db *DB) ListConversationMessages(ctx context.Context, conversationID string, after int64) ([]ConversationMessage, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT id,conversation_id,workspace_id,sequence,role,content_json,metadata_json,created_at FROM conversation_messages WHERE workspace_id=? AND conversation_id=? AND sequence>? ORDER BY sequence`, ws.ID, conversationID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConversationMessage{}
	for rows.Next() {
		var value ConversationMessage
		if err = rows.Scan(&value.ID, &value.ConversationID, &value.WorkspaceID, &value.Sequence, &value.Role, &value.ContentJSON, &value.MetadataJSON, &value.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
