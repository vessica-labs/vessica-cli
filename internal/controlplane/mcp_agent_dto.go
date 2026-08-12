package controlplane

import (
	generalagent "github.com/vessica-labs/vessica-cli/internal/agent"
	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

// mcpAgentRun is the explicit public representation. Runtime authority,
// reservations, rates, inputs, resolved knowledge, claims and leases never
// cross the MCP boundary.
type mcpAgentRun struct {
	ID                      string `json:"id"`
	AgentID                 string `json:"agent_id"`
	DefinitionVersion       int    `json:"definition_version"`
	Trigger                 string `json:"trigger"`
	OriginatingRepositoryID string `json:"originating_repository_id,omitempty"`
	ParentRunID             string `json:"parent_run_id,omitempty"`
	RootRunID               string `json:"root_run_id"`
	NestingDepth            int    `json:"nesting_depth"`
	Status                  string `json:"status"`
	OutputJSON              string `json:"output_json,omitempty"`
	TerminalError           string `json:"terminal_error,omitempty"`
	CancelRequestedAt       string `json:"cancel_requested_at,omitempty"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`
	StartedAt               string `json:"started_at,omitempty"`
	FinishedAt              string `json:"finished_at,omitempty"`
}

type mcpAgentRunTrigger struct {
	RunID   string `json:"run_id"`
	AgentID string `json:"agent_id"`
	State   string `json:"state"`
}

type mcpAgent struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Purpose        string `json:"purpose"`
	State          string `json:"state"`
	CurrentVersion int    `json:"current_version"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type mcpAgentSummary struct {
	mcpAgent
	Model               string       `json:"model"`
	ReasoningEffort     string       `json:"reasoning_effort"`
	HeartbeatEnabled    bool         `json:"heartbeat_enabled"`
	NextRunAt           string       `json:"next_run_at,omitempty"`
	BudgetLimitMicroUSD int64        `json:"budget_limit_microusd"`
	BudgetSpentMicroUSD int64        `json:"budget_spent_microusd"`
	LastRun             *mcpAgentRun `json:"last_run,omitempty"`
	EvaluationScore     float64      `json:"evaluation_score"`
}

type mcpAgentDetail struct {
	Agent      mcpAgent                `json:"agent"`
	Definition generalagent.Definition `json:"definition"`
	Schedule   *state.AgentSchedule    `json:"schedule,omitempty"`
	Budget     map[string]any          `json:"budget"`
	Runs       []mcpAgentRun           `json:"runs"`
}

func publicAgent(agent state.Agent) mcpAgent {
	return mcpAgent{ID: agent.ID, Name: agent.Name, Purpose: agent.Purpose, State: agent.State, CurrentVersion: agent.CurrentVersion, CreatedAt: agent.CreatedAt, UpdatedAt: agent.UpdatedAt}
}

func publicAgentSummaries(summaries []appservice.AgentSummary) []mcpAgentSummary {
	out := make([]mcpAgentSummary, 0, len(summaries))
	for _, summary := range summaries {
		public := mcpAgentSummary{mcpAgent: publicAgent(summary.Agent), Model: summary.Model, ReasoningEffort: summary.ReasoningEffort,
			HeartbeatEnabled: summary.HeartbeatEnabled, NextRunAt: summary.NextRunAt, BudgetLimitMicroUSD: summary.BudgetLimitMicroUSD,
			BudgetSpentMicroUSD: summary.BudgetSpentMicroUSD, EvaluationScore: summary.EvaluationScore}
		if summary.LastRun != nil {
			run := publicAgentRun(*summary.LastRun)
			public.LastRun = &run
		}
		out = append(out, public)
	}
	return out
}

func publicAgentDetail(detail *appservice.AgentDetail) *mcpAgentDetail {
	if detail == nil || detail.Agent == nil {
		return nil
	}
	budget := map[string]any{}
	for _, field := range []string{"daily_limit_microusd", "spent_microusd", "timezone", "period_start", "period_end"} {
		if value, present := detail.Budget[field]; present {
			budget[field] = value
		}
	}
	return &mcpAgentDetail{Agent: publicAgent(*detail.Agent), Definition: detail.Definition, Schedule: detail.Schedule, Budget: budget, Runs: publicAgentRuns(detail.Runs)}
}

func publicAgentRun(run state.AgentRun) mcpAgentRun {
	return mcpAgentRun{ID: run.ID, AgentID: run.AgentID, DefinitionVersion: run.DefinitionVersion, Trigger: run.Trigger,
		OriginatingRepositoryID: run.OriginatingRepositoryID, ParentRunID: run.ParentRunID, RootRunID: run.RootRunID,
		NestingDepth: run.NestingDepth, Status: run.Status, OutputJSON: run.OutputJSON, TerminalError: run.TerminalError,
		CancelRequestedAt: run.CancelRequestedAt, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt}
}

func publicAgentRuns(runs []state.AgentRun) []mcpAgentRun {
	out := make([]mcpAgentRun, 0, len(runs))
	for _, run := range runs {
		out = append(out, publicAgentRun(run))
	}
	return out
}
