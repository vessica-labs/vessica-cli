package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

// SendConversationMessageIdempotent commits the domain mutation and its MCP
// action key in one transaction. Reclaiming an audit lease therefore replays
// the exact conversation/message pair rather than repeating the mutation.
func (db *DB) SendConversationMessageIdempotent(ctx context.Context, actionKey, actorID, conversationID, title string, in ConversationMessageInput) (*Conversation, *ConversationMessage, bool, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	if actionKey == "" || actorID == "" || in.Role == "" || in.ContentJSON == "" {
		return nil, nil, false, fmt.Errorf("conversation action key, actor, role and content are required")
	}
	if in.MetadataJSON == "" {
		in.MetadataJSON = "{}"
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, err
	}
	defer tx.Rollback()
	var existingConversationID, existingMessageID string
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT conversation_id,message_id FROM mcp_conversation_actions WHERE workspace_id=? AND action_key=?`), ws.ID, actionKey).Scan(&existingConversationID, &existingMessageID)
	if err == nil {
		conversation, message, getErr := getConversationActionRecords(ctx, db, tx, ws.ID, existingConversationID, existingMessageID)
		return conversation, message, true, getErr
	}
	if err != sql.ErrNoRows {
		return nil, nil, false, err
	}
	now := Now()
	conversation := &Conversation{}
	if conversationID == "" {
		*conversation = Conversation{ID: id.New("conv"), WorkspaceID: ws.ID, ActorID: actorID, Title: title, Status: "active", CreatedAt: now, UpdatedAt: now}
		if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO conversations(id,workspace_id,actor_id,agent_id,title,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`), conversation.ID, conversation.WorkspaceID, conversation.ActorID, "", conversation.Title, conversation.Status, now, now); err != nil {
			return nil, nil, false, err
		}
		conversationID = conversation.ID
	} else {
		if err = scanConversation(tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,actor_id,agent_id,title,status,created_at,updated_at FROM conversations WHERE id=? AND workspace_id=? AND status='active'`), conversationID, ws.ID), conversation); err != nil {
			return nil, nil, false, fmt.Errorf("active conversation not found")
		}
	}
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE conversations SET next_message_seq=next_message_seq+1,updated_at=? WHERE id=? AND workspace_id=? AND status='active'`), now, conversationID, ws.ID)
	if err != nil {
		return nil, nil, false, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, nil, false, fmt.Errorf("active conversation not found")
	}
	var sequence int64
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT next_message_seq FROM conversations WHERE id=? AND workspace_id=?`), conversationID, ws.ID).Scan(&sequence); err != nil {
		return nil, nil, false, err
	}
	message := &ConversationMessage{ID: id.New("msg"), ConversationID: conversationID, WorkspaceID: ws.ID, Sequence: sequence, Role: in.Role, ContentJSON: in.ContentJSON, MetadataJSON: in.MetadataJSON, CreatedAt: now}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO conversation_messages(id,conversation_id,workspace_id,sequence,role,content_json,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?)`), message.ID, message.ConversationID, message.WorkspaceID, message.Sequence, message.Role, message.ContentJSON, message.MetadataJSON, message.CreatedAt); err != nil {
		return nil, nil, false, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO mcp_conversation_actions(workspace_id,action_key,actor_id,conversation_id,message_id,created_at) VALUES(?,?,?,?,?,?)`), ws.ID, actionKey, actorID, conversationID, message.ID, now); err != nil {
		return nil, nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	return conversation, message, false, nil
}

func getConversationActionRecords(ctx context.Context, db *DB, tx *sql.Tx, workspaceID, conversationID, messageID string) (*Conversation, *ConversationMessage, error) {
	conversation := &Conversation{}
	if err := scanConversation(tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,actor_id,agent_id,title,status,created_at,updated_at FROM conversations WHERE id=? AND workspace_id=?`), conversationID, workspaceID), conversation); err != nil {
		return nil, nil, err
	}
	message := &ConversationMessage{}
	err := tx.QueryRowContext(ctx, db.Rebind(`SELECT id,conversation_id,workspace_id,sequence,role,content_json,metadata_json,created_at FROM conversation_messages WHERE id=? AND workspace_id=?`), messageID, workspaceID).Scan(&message.ID, &message.ConversationID, &message.WorkspaceID, &message.Sequence, &message.Role, &message.ContentJSON, &message.MetadataJSON, &message.CreatedAt)
	return conversation, message, err
}

func scanConversation(row interface{ Scan(...any) error }, conversation *Conversation) error {
	return row.Scan(&conversation.ID, &conversation.WorkspaceID, &conversation.ActorID, &conversation.AgentID, &conversation.Title, &conversation.Status, &conversation.CreatedAt, &conversation.UpdatedAt)
}
