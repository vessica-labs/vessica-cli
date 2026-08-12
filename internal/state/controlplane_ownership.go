package state

import (
	"context"
	"fmt"
)

// requireWorkspaceRecord is the control-plane parent-child authority gate.
// Parent workspace IDs are immutable, so the successful scoped lookup remains
// valid through the immediately following write on both supported databases.
func (db *DB) requireWorkspaceRecord(ctx context.Context, table, recordID string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	allowed := map[string]bool{"newsletter_subscriptions": true, "outlook_ingestion_batches": true, "outlook_ingestion_items": true, "conversations": true, "agents": true, "agent_runs": true, "agent_run_triggers": true}
	if !allowed[table] {
		return fmt.Errorf("unsupported ownership table")
	}
	var n int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id=? AND workspace_id=?`, recordID, ws.ID).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%s not found in workspace", table)
	}
	return nil
}

func (db *DB) requireAgentRunForAgent(ctx context.Context, agentID, runID string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	var n int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runs WHERE id=? AND agent_id=? AND workspace_id=?`, runID, agentID, ws.ID).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("agent run not found in workspace")
	}
	return nil
}

func (db *DB) requireOutlookItemForBatch(ctx context.Context, batchID, itemID string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	var n int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_items WHERE id=? AND batch_id=? AND workspace_id=?`, itemID, batchID, ws.ID).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("outlook item not found in batch workspace")
	}
	return nil
}
