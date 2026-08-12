package app

import (
	"context"
	"encoding/json"

	generalagent "github.com/vessica-labs/vessica-cli/internal/agent"
)

func (s *Service) AgentsForWorkspace(ctx context.Context, workspaceID string) ([]AgentSummary, error) {
	agents, err := s.DB.ListAgentsForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]AgentSummary, 0, len(agents))
	for _, agent := range agents {
		summary := AgentSummary{Agent: agent}
		if version, getErr := s.DB.GetAgentVersion(ctx, agent.ID, agent.CurrentVersion); getErr == nil {
			var definition generalagent.Definition
			if json.Unmarshal([]byte(version.DefinitionJSON), &definition) == nil {
				summary.Model, summary.ReasoningEffort = definition.Model.ID, definition.Model.ReasoningEffort
			}
		}
		if schedule, _ := s.DB.GetAgentSchedule(ctx, agent.ID); schedule != nil {
			summary.HeartbeatEnabled, summary.NextRunAt = schedule.Enabled, schedule.NextDueAt
		}
		summary.BudgetLimitMicroUSD, _, summary.BudgetSpentMicroUSD, _, _, _, _ = s.DB.AgentBudget(ctx, agent.ID)
		if runs, _ := s.DB.ListAgentRunsForWorkspace(ctx, workspaceID, agent.ID); len(runs) > 0 {
			summary.LastRun = &runs[0]
		}
		_ = s.DB.QueryRow(ctx, `SELECT COALESCE(mean_score,0) FROM agent_eval_stats WHERE agent_id=?`, agent.ID).Scan(&summary.EvaluationScore)
		out = append(out, summary)
	}
	return out, nil
}

func (s *Service) AgentForWorkspace(ctx context.Context, workspaceID, ref string) (*AgentDetail, error) {
	agent, err := s.DB.GetAgentForWorkspace(ctx, workspaceID, ref)
	if err != nil {
		return nil, err
	}
	version, err := s.DB.GetAgentVersion(ctx, agent.ID, agent.CurrentVersion)
	if err != nil {
		return nil, err
	}
	var definition generalagent.Definition
	if err = json.Unmarshal([]byte(version.DefinitionJSON), &definition); err != nil {
		return nil, err
	}
	schedule, _ := s.DB.GetAgentSchedule(ctx, agent.ID)
	limit, reserved, spent, timezone, start, end, err := s.DB.AgentBudget(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	runs, _ := s.DB.ListAgentRunsForWorkspace(ctx, workspaceID, agent.ID)
	versions, _ := s.DB.ListAgentVersions(ctx, agent.ID)
	evaluations, _ := s.DB.ListAgentEvaluations(ctx, agent.ID)
	return &AgentDetail{Agent: agent, Version: version, Definition: definition, Schedule: schedule,
		Budget: map[string]any{"daily_limit_microusd": limit, "reserved_microusd": reserved, "spent_microusd": spent, "timezone": timezone, "period_start": start, "period_end": end},
		Runs:   runs, Versions: versions, Evaluations: evaluations}, nil
}
