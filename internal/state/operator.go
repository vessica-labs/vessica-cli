package state

import (
	"context"
	"fmt"
	"time"
)

const briefingSlotGrace = 30 * time.Minute

type expectedBriefingSlot struct {
	Key   string
	Date  string
	DueAt time.Time
}

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
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM action_ledger WHERE workspace_id=? AND transport_source='mcp' AND (result_json LIKE '%invalid_token%' OR result_json LIKE '%missing_token%' OR result_json LIKE '%access_denied%')`, workspace.ID).Scan(&out.OAuthFailures); err != nil {
		return out, err
	}
	if err = db.QueryRow(ctx, `SELECT COUNT(*),COALESCE(SUM(latency_ms),0),COALESCE(SUM(CASE WHEN policy_decision='denied' OR execution_state='failed' THEN 1 ELSE 0 END),0) FROM action_ledger WHERE workspace_id=? AND transport_source='mcp'`, workspace.ID).Scan(&out.MCPCalls, &out.MCPLatencyMS, &out.MCPErrors); err != nil {
		return out, err
	}
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM action_ledger WHERE workspace_id=? AND policy_decision='denied'`, workspace.ID).Scan(&out.DeniedActions); err != nil {
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

	expected, err := expectedBriefingSlots(now)
	if err != nil {
		return out, err
	}
	for _, slot := range expected {
		var present int
		if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM canonical_knowledge_artifacts WHERE workspace_id=? AND canonical_key=? AND updated_at>=?`, workspace.ID, slot.Key, FormatTime(slot.DueAt.UTC())).Scan(&present); err != nil {
			return out, err
		}
		if present == 0 {
			out.MissingBriefings = append(out.MissingBriefings, slot.Key+":"+slot.Date)
		}
	}
	var newsletterPresent int
	if err = db.QueryRow(ctx, `SELECT COUNT(*) FROM canonical_knowledge_artifacts WHERE workspace_id=? AND canonical_key='newsletter:daily'`, workspace.ID).Scan(&newsletterPresent); err != nil {
		return out, err
	}
	if newsletterPresent == 0 {
		out.MissingBriefings = append(out.MissingBriefings, "newsletter:daily")
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

	rows, err = db.Query(ctx, `SELECT id,workspace_id,actor_id,agent_id,agent_run_id,tool,policy_decision,transport_source,redacted_arguments_json,result_json,latency_ms,idempotency_key,external_ids_json,created_at,execution_state,claim_token_hash,lease_until,updated_at,arguments_hash FROM action_ledger WHERE workspace_id=? ORDER BY created_at DESC LIMIT 100`, workspace.ID)
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
	if err = rows.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func expectedBriefingSlots(now time.Time) ([]expectedBriefingSlot, error) {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return nil, fmt.Errorf("load briefing timezone: %w", err)
	}
	localNow := now.In(location)
	return []expectedBriefingSlot{
		mostRecentBriefingSlot(localNow, "cos-briefing:morning", 6, 30),
		mostRecentBriefingSlot(localNow, "cos-briefing:afternoon", 16, 30),
	}, nil
}

func mostRecentBriefingSlot(localNow time.Time, key string, hour, minute int) expectedBriefingSlot {
	date := localNow
	due := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, localNow.Location())
	if localNow.Before(due.Add(briefingSlotGrace)) {
		date = date.AddDate(0, 0, -1)
	}
	for date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
		date = date.AddDate(0, 0, -1)
	}
	due = time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, localNow.Location())
	return expectedBriefingSlot{Key: key, Date: date.Format("2006-01-02"), DueAt: due}
}
