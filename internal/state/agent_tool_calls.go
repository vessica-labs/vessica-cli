package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) ValidateAgentFence(ctx context.Context, runID, fence string) error {
	var n int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM agent_run_attempts WHERE run_id=? AND fence_token=? AND status='running' AND lease_until>?`, runID, fence, Now()).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrAgentFenceLost
	}
	return nil
}

func (db *DB) ReplayAgentToolCall(ctx context.Context, runID, fence string, ordinal int64, toolID, argumentHash string) (string, bool, error) {
	if err := db.ValidateAgentFence(ctx, runID, fence); err != nil {
		return "", false, err
	}
	var storedTool, storedHash, status string
	var result sql.NullString
	err := db.QueryRow(ctx, `SELECT c.tool_id,c.argument_hash,c.status,c.result_json FROM agent_tool_calls c JOIN agent_run_attempts a ON a.id=c.attempt_id WHERE a.run_id=? AND c.logical_ordinal=? ORDER BY a.attempt_number DESC LIMIT 1`, runID, ordinal).Scan(&storedTool, &storedHash, &status, &result)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if storedTool != toolID || storedHash != argumentHash {
		return "", false, fmt.Errorf("tool replay mismatch")
	}
	if status != "completed" {
		return "", false, fmt.Errorf("prior tool outcome is %s; refusing unsafe replay", status)
	}
	return result.String, true, nil
}

func (db *DB) BeginAgentToolCall(ctx context.Context, runID, fence string, ordinal int64, toolID, argumentHash string) error {
	if err := db.ValidateAgentFence(ctx, runID, fence); err != nil {
		return err
	}
	var attemptID string
	if err := db.QueryRow(ctx, `SELECT id FROM agent_run_attempts WHERE run_id=? AND fence_token=?`, runID, fence).Scan(&attemptID); err != nil {
		return err
	}
	now := Now()
	result, err := db.Exec(ctx, `INSERT INTO agent_tool_calls(id,attempt_id,logical_ordinal,tool_id,argument_hash,status,created_at,updated_at) VALUES(?,?,?,?,?,'started',?,?) ON CONFLICT(attempt_id,logical_ordinal) DO NOTHING`, id.New("atool"), attemptID, ordinal, toolID, argumentHash, now, now)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("tool call already started")
	}
	return nil
}

func (db *DB) FailAgentToolCall(ctx context.Context, runID, fence string, ordinal int64, errorText string) error {
	if err := db.ValidateAgentFence(ctx, runID, fence); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `UPDATE agent_tool_calls SET status='failed',error=?,updated_at=? WHERE attempt_id=(SELECT id FROM agent_run_attempts WHERE run_id=? AND fence_token=?) AND logical_ordinal=?`, errorText, Now(), runID, fence, ordinal)
	return err
}

func (db *DB) CompleteAgentToolCall(ctx context.Context, runID, fence string, ordinal int64, toolID, argumentHash, resultJSON string) error {
	if err := db.ValidateAgentFence(ctx, runID, fence); err != nil {
		return err
	}
	var attemptID string
	if err := db.QueryRow(ctx, `SELECT id FROM agent_run_attempts WHERE run_id=? AND fence_token=?`, runID, fence).Scan(&attemptID); err != nil {
		return err
	}
	now := Now()
	result, err := db.Exec(ctx, `UPDATE agent_tool_calls SET status='completed',result_json=?,updated_at=? WHERE attempt_id=? AND logical_ordinal=? AND tool_id=? AND argument_hash=? AND status='started'`, resultJSON, now, attemptID, ordinal, toolID, argumentHash)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("tool call completion mismatch")
	}
	return nil
}
