package state

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (db *DB) GetAgentForWorkspace(ctx context.Context, workspaceID, ref string) (*Agent, error) {
	var agent Agent
	err := db.QueryRow(ctx, `SELECT id,workspace_id,name,purpose,state,current_version,created_at,updated_at FROM agents WHERE workspace_id=? AND (id=? OR name_key=?)`, workspaceID, ref, strings.ToLower(ref)).Scan(&agent.ID, &agent.WorkspaceID, &agent.Name, &agent.Purpose, &agent.State, &agent.CurrentVersion, &agent.CreatedAt, &agent.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found: %s", ref)
	}
	return &agent, err
}

func (db *DB) ListAgentsForWorkspace(ctx context.Context, workspaceID string) ([]Agent, error) {
	rows, err := db.Query(ctx, `SELECT id,workspace_id,name,purpose,state,current_version,created_at,updated_at FROM agents WHERE workspace_id=? ORDER BY name_key`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []Agent
	for rows.Next() {
		var agent Agent
		if err = rows.Scan(&agent.ID, &agent.WorkspaceID, &agent.Name, &agent.Purpose, &agent.State, &agent.CurrentVersion, &agent.CreatedAt, &agent.UpdatedAt); err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func (db *DB) GetAgentRunForWorkspace(ctx context.Context, workspaceID, runID string) (*AgentRun, error) {
	return scanAgentRun(db.QueryRow(ctx, `SELECT id,workspace_id,agent_id,definition_version,trigger,input_json,originating_repository_id,parent_run_id,trigger_id,root_run_id,nesting_depth,status,budget_period_start,reservation_microusd,rate_snapshot_json,resolved_knowledge_json,output_json,terminal_error,cancel_requested_at,created_at,updated_at,started_at,finished_at FROM agent_runs WHERE workspace_id=? AND id=?`, workspaceID, runID), runID)
}

func (db *DB) ListAgentRunsForWorkspace(ctx context.Context, workspaceID, agentID string) ([]AgentRun, error) {
	query := `SELECT id FROM agent_runs WHERE workspace_id=?`
	args := []any{workspaceID}
	if agentID != "" {
		query += ` AND agent_id=?`
		args = append(args, agentID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []AgentRun
	for rows.Next() {
		var runID string
		if err = rows.Scan(&runID); err != nil {
			return nil, err
		}
		run, getErr := db.GetAgentRunForWorkspace(ctx, workspaceID, runID)
		if getErr != nil {
			return nil, getErr
		}
		runs = append(runs, *run)
	}
	return runs, rows.Err()
}

func scanAgentRun(row interface{ Scan(...any) error }, runID string) (*AgentRun, error) {
	var run AgentRun
	var repo, parent, triggerID, out, terminal, cancel, started, finished sql.NullString
	err := row.Scan(&run.ID, &run.WorkspaceID, &run.AgentID, &run.DefinitionVersion, &run.Trigger, &run.InputJSON, &repo, &parent, &triggerID, &run.RootRunID, &run.NestingDepth, &run.Status, &run.BudgetPeriodStart, &run.ReservationMicroUSD, &run.RateSnapshotJSON, &run.ResolvedKnowledgeJSON, &out, &terminal, &cancel, &run.CreatedAt, &run.UpdatedAt, &started, &finished)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent run not found: %s", runID)
	}
	run.OriginatingRepositoryID, run.ParentRunID, run.TriggerID = repo.String, parent.String, triggerID.String
	run.OutputJSON, run.TerminalError, run.CancelRequestedAt = out.String, terminal.String, cancel.String
	run.StartedAt, run.FinishedAt = started.String, finished.String
	return &run, err
}
