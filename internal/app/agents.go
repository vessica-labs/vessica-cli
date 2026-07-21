package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	generalagent "github.com/vessica-labs/vessica-cli/internal/agent"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type AgentDetail struct {
	Agent       *state.Agent            `json:"agent"`
	Version     *state.AgentVersion     `json:"version"`
	Definition  generalagent.Definition `json:"definition"`
	Schedule    *state.AgentSchedule    `json:"schedule,omitempty"`
	Budget      map[string]any          `json:"budget"`
	Runs        []state.AgentRun        `json:"runs"`
	Versions    []state.AgentVersion    `json:"versions"`
	Evaluations []state.AgentEvaluation `json:"evaluations"`
}

type AgentSummary struct {
	state.Agent
	Model               string          `json:"model"`
	ReasoningEffort     string          `json:"reasoning_effort"`
	HeartbeatEnabled    bool            `json:"heartbeat_enabled"`
	NextRunAt           string          `json:"next_run_at,omitempty"`
	BudgetLimitMicroUSD int64           `json:"budget_limit_microusd"`
	BudgetSpentMicroUSD int64           `json:"budget_spent_microusd"`
	LastRun             *state.AgentRun `json:"last_run,omitempty"`
	EvaluationScore     float64         `json:"evaluation_score"`
}

func (s *Service) Agents(ctx context.Context) ([]AgentSummary, error) {
	agents, err := s.DB.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AgentSummary, 0, len(agents))
	for _, a := range agents {
		summary := AgentSummary{Agent: a}
		if v, e := s.DB.GetAgentVersion(ctx, a.ID, a.CurrentVersion); e == nil {
			var d generalagent.Definition
			if json.Unmarshal([]byte(v.DefinitionJSON), &d) == nil {
				summary.Model, summary.ReasoningEffort = d.Model.ID, d.Model.ReasoningEffort
			}
		}
		if schedule, _ := s.DB.GetAgentSchedule(ctx, a.ID); schedule != nil {
			summary.HeartbeatEnabled, summary.NextRunAt = schedule.Enabled, schedule.NextDueAt
		}
		summary.BudgetLimitMicroUSD, _, summary.BudgetSpentMicroUSD, _, _, _, _ = s.DB.AgentBudget(ctx, a.ID)
		if runs, _ := s.DB.ListAgentRuns(ctx, a.ID); len(runs) > 0 {
			summary.LastRun = &runs[0]
		}
		_ = s.DB.QueryRow(ctx, `SELECT COALESCE(mean_score,0) FROM agent_eval_stats WHERE agent_id=?`, a.ID).Scan(&summary.EvaluationScore)
		out = append(out, summary)
	}
	return out, nil
}

func (s *Service) Agent(ctx context.Context, ref string) (*AgentDetail, error) {
	a, err := s.DB.GetAgent(ctx, ref)
	if err != nil {
		return nil, err
	}
	v, err := s.DB.GetAgentVersion(ctx, a.ID, a.CurrentVersion)
	if err != nil {
		return nil, err
	}
	var d generalagent.Definition
	if err = json.Unmarshal([]byte(v.DefinitionJSON), &d); err != nil {
		return nil, err
	}
	schedule, _ := s.DB.GetAgentSchedule(ctx, a.ID)
	limit, reserved, spent, tz, start, end, err := s.DB.AgentBudget(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	runs, _ := s.DB.ListAgentRuns(ctx, a.ID)
	versions, _ := s.DB.ListAgentVersions(ctx, a.ID)
	evaluations, _ := s.DB.ListAgentEvaluations(ctx, a.ID)
	return &AgentDetail{Agent: a, Version: v, Definition: d, Schedule: schedule, Budget: map[string]any{"daily_limit_microusd": limit, "reserved_microusd": reserved, "spent_microusd": spent, "timezone": tz, "period_start": start, "period_end": end}, Runs: runs, Versions: versions, Evaluations: evaluations}, nil
}

func (s *Service) CreateAgentBuild(ctx context.Context, description, agentID, createdBy string, review bool, timezone string) (*state.AgentBuildOperation, error) {
	if strings.TrimSpace(description) == "" {
		return nil, fmt.Errorf("description is required")
	}
	kind := "create"
	if agentID != "" {
		kind = "update"
		a, err := s.DB.GetAgent(ctx, agentID)
		if err != nil {
			return nil, err
		}
		agentID = a.ID
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		timezone = "UTC"
	}
	return s.DB.CreateAgentBuild(ctx, kind, description, agentID, createdBy, review, timezone)
}

func (s *Service) CreateStructuredAgent(ctx context.Context, d generalagent.Definition, provenance map[string]any) (*state.Agent, error) {
	d.Defaults("UTC")
	if err := d.Validate(func(model string) bool { return model == generalagent.DefaultModel }); err != nil {
		return nil, err
	}
	if err := ValidateAgentOperations(d); err != nil {
		return nil, err
	}
	if err := s.ValidateAgentCritic(ctx, "", d.EvalCriticAgentID); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(d)
	limit, err := usdMicro(d.Budget.DailyUSD)
	if err != nil {
		return nil, err
	}
	prov, _ := json.Marshal(provenance)
	a, err := s.DB.CreateAgent(ctx, d.Name, d.Purpose, string(raw), string(prov), limit, d.Budget.Timezone)
	if err != nil {
		return nil, err
	}
	if d.Heartbeat != nil && d.Heartbeat.Enabled {
		if err = s.SetAgentHeartbeat(ctx, a.ID, *d.Heartbeat); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (s *Service) ActivateAgentDraft(ctx context.Context, draftID string) (*state.Agent, error) {
	draft, err := s.DB.GetAgentDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if draft.Status != "pending" {
		return nil, fmt.Errorf("draft is %s", draft.Status)
	}
	var d generalagent.Definition
	if err = json.Unmarshal([]byte(draft.DefinitionJSON), &d); err != nil {
		return nil, fmt.Errorf("invalid generated definition: %w", err)
	}
	d.Defaults("UTC")
	if err = d.Validate(func(model string) bool { return model == generalagent.DefaultModel }); err != nil {
		return nil, err
	}
	if err = ValidateAgentOperations(d); err != nil {
		return nil, err
	}
	if err = s.ValidateAgentCritic(ctx, draft.AgentID, d.EvalCriticAgentID); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(d)
	var a *state.Agent
	if draft.AgentID == "" {
		limit, e := usdMicro(d.Budget.DailyUSD)
		if e != nil {
			return nil, e
		}
		a, err = s.DB.CreateAgent(ctx, d.Name, d.Purpose, string(raw), `{"source":"builder"}`, limit, d.Budget.Timezone)
	} else {
		a, err = s.DB.GetAgent(ctx, draft.AgentID)
		if err == nil {
			current, currentErr := s.DB.GetAgentVersion(ctx, a.ID, a.CurrentVersion)
			var currentDefinition generalagent.Definition
			if currentErr != nil || json.Unmarshal([]byte(current.DefinitionJSON), &currentDefinition) != nil || !sameAgentCore(currentDefinition, d) {
				_, err = s.DB.AddAgentVersion(ctx, a.ID, d.Purpose, string(raw), `{"source":"builder"}`)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if draft.AgentID != "" {
		if err = s.applyAgentOperationalSettings(ctx, a.ID, d); err != nil {
			return nil, err
		}
	} else if d.Heartbeat != nil && d.Heartbeat.Enabled {
		if err = s.SetAgentHeartbeat(ctx, a.ID, *d.Heartbeat); err != nil {
			return nil, err
		}
	}
	if err = s.DB.MarkDraftActivated(ctx, draftID, a.ID); err != nil {
		return nil, err
	}
	return s.DB.GetAgent(ctx, a.ID)
}

func (s *Service) UpdateStructuredAgent(ctx context.Context, ref string, d generalagent.Definition) (*state.AgentVersion, error) {
	a, err := s.DB.GetAgent(ctx, ref)
	if err != nil {
		return nil, err
	}
	budgetProvided, heartbeatProvided := d.Budget != nil, d.Heartbeat != nil
	current, _ := s.DB.GetAgentVersion(ctx, a.ID, a.CurrentVersion)
	var currentDefinition generalagent.Definition
	if current != nil {
		if json.Unmarshal([]byte(current.DefinitionJSON), &currentDefinition) == nil {
			if !budgetProvided {
				d.Budget = currentDefinition.Budget
			}
			if !heartbeatProvided {
				d.Heartbeat = currentDefinition.Heartbeat
			}
		}
	}
	d.Defaults("UTC")
	if !strings.EqualFold(a.Name, d.Name) {
		return nil, fmt.Errorf("agent name cannot be changed")
	}
	if err = d.Validate(func(model string) bool { return model == generalagent.DefaultModel }); err != nil {
		return nil, err
	}
	if err = ValidateAgentOperations(d); err != nil {
		return nil, err
	}
	if err = s.ValidateAgentCritic(ctx, a.ID, d.EvalCriticAgentID); err != nil {
		return nil, err
	}
	operational := d
	if !budgetProvided {
		operational.Budget = nil
	}
	if !heartbeatProvided {
		operational.Heartbeat = nil
	}
	if err = s.applyAgentOperationalSettings(ctx, a.ID, operational); err != nil {
		return nil, err
	}
	if current != nil && sameAgentCore(currentDefinition, d) {
		return current, nil
	}
	raw, _ := json.Marshal(d)
	return s.DB.AddAgentVersion(ctx, a.ID, d.Purpose, string(raw), `{"source":"structured"}`)
}

func sameAgentCore(left, right generalagent.Definition) bool {
	left.Budget, left.Heartbeat = nil, nil
	right.Budget, right.Heartbeat = nil, nil
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

func (s *Service) applyAgentOperationalSettings(ctx context.Context, agentID string, definition generalagent.Definition) error {
	if definition.Budget != nil {
		limit, err := usdMicro(definition.Budget.DailyUSD)
		if err != nil {
			return err
		}
		if err = s.DB.SetAgentBudget(ctx, agentID, limit, definition.Budget.Timezone); err != nil {
			return err
		}
		if _, err = s.DB.ReleaseBudgetBlockedRuns(ctx, agentID); err != nil {
			return err
		}
	}
	if definition.Heartbeat == nil {
		return nil
	}
	if !definition.Heartbeat.Enabled {
		return s.DB.DisableAgentSchedule(ctx, agentID)
	}
	return s.SetAgentHeartbeat(ctx, agentID, *definition.Heartbeat)
}

func (s *Service) ValidateAgentCritic(ctx context.Context, selfID, criticRef string) error {
	if strings.TrimSpace(criticRef) == "" {
		return nil
	}
	critic, err := s.DB.GetAgent(ctx, criticRef)
	if err != nil {
		return fmt.Errorf("eval critic: %w", err)
	}
	if critic.ID == selfID {
		return fmt.Errorf("an agent cannot evaluate itself")
	}
	if critic.ID != criticRef {
		return fmt.Errorf("eval critic must use agent ID %s", critic.ID)
	}
	if critic.State != "active" {
		return fmt.Errorf("eval critic is %s", critic.State)
	}
	return nil
}

func ValidateAgentOperations(definition generalagent.Definition) error {
	if definition.Budget != nil {
		if _, err := usdMicro(definition.Budget.DailyUSD); err != nil {
			return err
		}
		if _, err := time.LoadLocation(definition.Budget.Timezone); err != nil {
			return fmt.Errorf("invalid budget timezone: %w", err)
		}
	}
	if definition.Heartbeat == nil || !definition.Heartbeat.Enabled {
		return nil
	}
	if _, err := time.LoadLocation(definition.Heartbeat.Timezone); err != nil {
		return fmt.Errorf("invalid heartbeat timezone: %w", err)
	}
	_, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(definition.Heartbeat.Cron)
	if err != nil {
		return fmt.Errorf("invalid five-field cron: %w", err)
	}
	return nil
}

func (s *Service) SetAgentBudget(ctx context.Context, ref, dailyUSD, timezone string) (int, error) {
	a, err := s.DB.GetAgent(ctx, ref)
	if err != nil {
		return 0, err
	}
	limit, err := usdMicro(dailyUSD)
	if err != nil {
		return 0, err
	}
	if err = s.DB.SetAgentBudget(ctx, a.ID, limit, timezone); err != nil {
		return 0, err
	}
	return s.DB.ReleaseBudgetBlockedRuns(ctx, a.ID)
}

func (s *Service) SetAgentHeartbeat(ctx context.Context, ref string, h generalagent.Heartbeat) error {
	a, err := s.DB.GetAgent(ctx, ref)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(h.Timezone)
	if err != nil {
		return err
	}
	schedule, err := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(h.Cron)
	if err != nil {
		return fmt.Errorf("invalid five-field cron: %w", err)
	}
	next := state.FormatTime(schedule.Next(time.Now().In(loc)))
	return s.DB.SetAgentSchedule(ctx, a.ID, h.Cron, h.Timezone, next, h.Enabled)
}

func (s *Service) StartAgentRun(ctx context.Context, ref, prompt, trigger, repositoryID, parentRunID string) (*state.AgentRun, error) {
	a, err := s.DB.GetAgent(ctx, ref)
	if err != nil {
		return nil, err
	}
	if trigger == "" {
		trigger = "manual"
	}
	input, _ := json.Marshal(map[string]string{"prompt": prompt})
	run, err := s.DB.CreateAgentRun(ctx, a.ID, trigger, string(input), repositoryID, parentRunID, state.DefaultAgentRateSnapshot())
	if err != nil {
		return nil, err
	}
	version, err := s.DB.GetAgentVersion(ctx, a.ID, run.DefinitionVersion)
	if err != nil {
		_ = s.DB.CancelAgentRun(ctx, run.ID)
		return nil, err
	}
	var definition generalagent.Definition
	if err = json.Unmarshal([]byte(version.DefinitionJSON), &definition); err != nil {
		_ = s.DB.CancelAgentRun(ctx, run.ID)
		return nil, err
	}
	resolved := make([]map[string]any, 0, len(definition.Knowledge))
	for _, ref := range definition.Knowledge {
		artifact, e := s.Artifact(ctx, ref.ArtifactID)
		if e != nil {
			_ = s.DB.CancelAgentRun(ctx, run.ID)
			return nil, fmt.Errorf("resolve knowledge %s: %w", ref.ArtifactID, e)
		}
		if ref.Version != "" && ref.Version != "latest" && strconv.Itoa(artifact.Version) != ref.Version {
			versions, e := s.ArtifactVersions(ctx, ref.ArtifactID, "", 200)
			if e != nil {
				_ = s.DB.CancelAgentRun(ctx, run.ID)
				return nil, e
			}
			found := false
			for _, candidate := range versions.Items {
				if strconv.Itoa(candidate.Version) == ref.Version {
					artifact = candidate
					found = true
					break
				}
			}
			if !found {
				_ = s.DB.CancelAgentRun(ctx, run.ID)
				return nil, fmt.Errorf("artifact version not found: %s@%s", ref.ArtifactID, ref.Version)
			}
		}
		resolved = append(resolved, map[string]any{"artifact_id": artifact.ID, "version_id": fmt.Sprintf("%s@%d", artifact.ID, artifact.Version), "version": artifact.Version, "description": ref.Description})
	}
	raw, _ := json.Marshal(resolved)
	if err = s.DB.SetAgentRunResolvedKnowledge(ctx, run.ID, string(raw)); err != nil {
		_ = s.DB.CancelAgentRun(ctx, run.ID)
		return nil, err
	}
	run.ResolvedKnowledgeJSON = string(raw)
	return run, nil
}

func (s *Service) TickAgentSchedules(ctx context.Context, now time.Time) error {
	agents, err := s.DB.ListAgents(ctx)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		if agent.State == "active" {
			_, _ = s.DB.ReleaseBudgetBlockedRuns(ctx, agent.ID)
		}
	}
	due, err := s.DB.DueAgentSchedules(ctx, state.FormatTime(now))
	if err != nil {
		return err
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	for _, item := range due {
		schedule, e := parser.Parse(item.Cron)
		if e != nil {
			continue
		}
		loc, e := time.LoadLocation(item.Timezone)
		if e != nil {
			continue
		}
		active, e := s.DB.HasActiveAgentRun(ctx, item.AgentID, "heartbeat")
		if e == nil && !active {
			_, _ = s.StartAgentRun(ctx, item.AgentID, "Execute your primary objective for this heartbeat.", "heartbeat", "", "")
		}
		next := state.FormatTime(schedule.Next(now.In(loc)))
		_ = s.DB.AdvanceAgentSchedule(ctx, item.AgentID, item.NextDueAt, next)
	}
	return nil
}

func usdMicro(value string) (int64, error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) > 2 || len(parts) == 0 {
		return 0, fmt.Errorf("invalid USD amount")
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, fmt.Errorf("invalid USD amount")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 6 {
		return 0, fmt.Errorf("USD amount has more than six decimal places")
	}
	fraction += strings.Repeat("0", 6-len(fraction))
	micro := int64(0)
	if fraction != "" {
		micro, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid USD amount")
		}
	}
	total := whole*1_000_000 + micro
	if total <= 0 {
		return 0, fmt.Errorf("budget must be positive")
	}
	return total, nil
}
