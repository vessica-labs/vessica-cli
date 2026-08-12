package state

import (
	"context"
	"database/sql"
	"time"
)

type StaleSourceCheckpoint struct {
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	UpdatedAt  string `json:"updated_at"`
}

type AgentBudgetSignal struct {
	AgentID          string `json:"agent_id"`
	AgentName        string `json:"agent_name"`
	LimitMicroUSD    int64  `json:"limit_microusd"`
	ReservedMicroUSD int64  `json:"reserved_microusd"`
	SpentMicroUSD    int64  `json:"spent_microusd"`
}

type OperatorSnapshot struct {
	OAuthFailures         int64                   `json:"oauth_failures"`
	MCPCalls              int64                   `json:"mcp_calls"`
	MCPErrors             int64                   `json:"mcp_errors"`
	MCPLatencyMS          int64                   `json:"mcp_latency_ms"`
	DeniedActions         int64                   `json:"denied_actions"`
	RejectedRecords       int64                   `json:"rejected_records"`
	StaleIngestionBatches int64                   `json:"stale_ingestion_batches"`
	FailedAgents          []AgentRun              `json:"failed_agents"`
	StaleCheckpoints      []StaleSourceCheckpoint `json:"stale_checkpoints"`
	MissingBriefings      []string                `json:"missing_briefings"`
	Budgets               []AgentBudgetSignal     `json:"budgets"`
	RecentActions         []ActionLedger          `json:"recent_actions"`
	CheckpointStaleBefore string                  `json:"checkpoint_stale_before"`
}

func (db *DB) OperatorSnapshot(ctx context.Context, now time.Time) (OperatorSnapshot, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return OperatorSnapshot{}, err
	}
	staleBefore := FormatTime(now.UTC().Add(-24 * time.Hour))
	out := OperatorSnapshot{CheckpointStaleBefore: staleBefore, FailedAgents: []AgentRun{}, StaleCheckpoints: []StaleSourceCheckpoint{}, MissingBriefings: []string{}, Budgets: []AgentBudgetSignal{}, RecentActions: []ActionLedger{}}
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM action_ledger WHERE workspace_id=? AND (result_json LIKE '%invalid_token%' OR result_json LIKE '%missing_token%' OR result_json LIKE '%access_denied%')`, workspace.ID).Scan(&out.OAuthFailures); err != nil {
		return out, err
	}
	if err = db.QueryRow(ctx, `SELECT COUNT(*),COALESCE(SUM(latency_ms),0),COALESCE(SUM(CASE WHEN policy_decision='denied' OR execution_state='failed' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN policy_decision='denied' THEN 1 ELSE 0 END),0) FROM action_ledger WHERE workspace_id=?`, workspace.ID).Scan(&out.MCPCalls, &out.MCPLatencyMS, &out.MCPErrors, &out.DeniedActions); err != nil {
		return out, err
	}
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_receipts WHERE workspace_id=? AND state='rejected'`, workspace.ID).Scan(&out.RejectedRecords); err != nil {
		return out, err
	}
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM outlook_ingestion_batches WHERE workspace_id=? AND state NOT IN ('completed','failed') AND updated_at<?`, workspace.ID, staleBefore).Scan(&out.StaleIngestionBatches); err != nil {
		return out, err
	}

	rows, err := db.Query(ctx, `SELECT source_type,source_id,updated_at FROM source_checkpoints WHERE workspace_id=? AND updated_at<? ORDER BY updated_at`, workspace.ID, staleBefore)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var value StaleSourceCheckpoint
		if err = rows.Scan(&value.SourceType, &value.SourceID, &value.UpdatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.StaleCheckpoints = append(out.StaleCheckpoints, value)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}

	rows, err = db.Query(ctx, `SELECT id FROM agent_runs WHERE workspace_id=? AND status='failed' ORDER BY updated_at DESC LIMIT 50`, workspace.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var runID string
		if err = rows.Scan(&runID); err != nil {
			rows.Close()
			return out, err
		}
		run, getErr := db.GetAgentRunForWorkspace(ctx, workspace.ID, runID)
		if getErr != nil {
			rows.Close()
			return out, getErr
		}
		out.FailedAgents = append(out.FailedAgents, *run)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}

	for _, key := range []string{"cos-briefing:morning", "cos-briefing:afternoon", "newsletter:daily"} {
		var present int
		if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM canonical_knowledge_artifacts WHERE workspace_id=? AND canonical_key=?`, workspace.ID, key).Scan(&present); err != nil {
			return out, err
		}
		if present == 0 {
			out.MissingBriefings = append(out.MissingBriefings, key)
		}
	}

	rows, err = db.Query(ctx, `SELECT a.id,a.name,p.limit_microusd,p.reserved_microusd,p.spent_microusd FROM agent_budget_periods p JOIN agents a ON a.id=p.agent_id WHERE a.workspace_id=? ORDER BY a.name`, workspace.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var value AgentBudgetSignal
		if err = rows.Scan(&value.AgentID, &value.AgentName, &value.LimitMicroUSD, &value.ReservedMicroUSD, &value.SpentMicroUSD); err != nil {
			rows.Close()
			return out, err
		}
		out.Budgets = append(out.Budgets, value)
	}
	if err = rows.Close(); err != nil {
		return out, err
	}

	rows, err = db.Query(ctx, `SELECT id,workspace_id,actor_id,agent_id,agent_run_id,tool,policy_decision,redacted_arguments_json,result_json,latency_ms,idempotency_key,external_ids_json,created_at,execution_state,claim_token_hash,lease_until,updated_at,arguments_hash FROM action_ledger WHERE workspace_id=? ORDER BY created_at DESC LIMIT 100`, workspace.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		value, scanErr := scanActionLedger(rows)
		if scanErr != nil {
			return out, scanErr
		}
		out.RecentActions = append(out.RecentActions, *value)
	}
	if err = rows.Err(); err != nil && err != sql.ErrNoRows {
		return out, err
	}
	return out, nil
}
