package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	generalagent "github.com/vessica-labs/vessica-cli/internal/agent"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

const heartbeatTestDefinition = `{"kind":"vessica.agent/v1","name":"HEARTBEAT","purpose":"test","system_prompt":"help","model":{"id":"gpt-5.6-terra","reasoning_effort":"medium"},"tools":[],"knowledge":[],"budget":{"daily_usd":"5.00","timezone":"America/Los_Angeles"}}`

func TestHeartbeatCreatesFreshRunAndCoalescesOverlap(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err = db.EnsureWorkspace(ctx, root, "hosted"); err != nil {
		t.Fatal(err)
	}
	agent, err := db.CreateAgent(ctx, "HEARTBEAT", "test", heartbeatTestDefinition, "{}", 5_000_000, "America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 1, 10, 30, 0, 0, time.UTC)
	due := now.Add(-time.Minute).Format(time.RFC3339Nano)
	if err = db.SetAgentSchedule(ctx, agent.ID, "0 9 * * *", "America/Los_Angeles", due, true); err != nil {
		t.Fatal(err)
	}
	service := New(db, root, config.Defaults())
	if err = service.TickAgentSchedules(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err = db.SetAgentSchedule(ctx, agent.ID, "0 9 * * *", "America/Los_Angeles", due, true); err != nil {
		t.Fatal(err)
	}
	if err = service.TickAgentSchedules(ctx, now); err != nil {
		t.Fatal(err)
	}
	runs, err := db.ListAgentRuns(ctx, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Trigger != "heartbeat" {
		t.Fatalf("heartbeat runs=%v", runs)
	}
	if runs[0].ParentRunID != "" {
		t.Fatalf("heartbeat should start a fresh root session: %+v", runs[0])
	}
	schedule, err := db.GetAgentSchedule(ctx, agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	next, err := time.Parse(time.RFC3339Nano, schedule.NextDueAt)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation("America/Los_Angeles")
	if local := next.In(location); local.Hour() != 9 || local.Day() != 1 {
		t.Fatalf("DST-aware next heartbeat=%s", local)
	}
	if err = db.CancelAgentRun(ctx, runs[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = db.SetAgentState(ctx, agent.ID, "paused"); err != nil {
		t.Fatal(err)
	}
	due = now.Add(-time.Minute).Format(time.RFC3339Nano)
	if err = db.SetAgentSchedule(ctx, agent.ID, "0 9 * * *", "America/Los_Angeles", due, true); err != nil {
		t.Fatal(err)
	}
	if err = service.TickAgentSchedules(ctx, now); err != nil {
		t.Fatal(err)
	}
	runs, _ = db.ListAgentRuns(ctx, agent.ID)
	if len(runs) != 1 {
		t.Fatalf("paused agent received heartbeat: %v", runs)
	}
}

func TestOperationalAgentUpdateDoesNotCreateVersion(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	_, _ = db.EnsureWorkspace(ctx, root, "hosted")
	service := New(db, root, config.Defaults())
	definition := generalagent.Definition{Kind: generalagent.DefinitionKind, Name: "OPS", Purpose: "test", SystemPrompt: "help", Model: generalagent.Model{ID: generalagent.DefaultModel, ReasoningEffort: "medium"}, Budget: &generalagent.Budget{DailyUSD: "5.00", Timezone: "UTC"}}
	agent, err := service.CreateStructuredAgent(ctx, definition, map[string]any{"source": "test"})
	if err != nil {
		t.Fatal(err)
	}
	definition.Budget.DailyUSD = "10.00"
	version, err := service.UpdateStructuredAgent(ctx, agent.ID, definition)
	if err != nil {
		t.Fatal(err)
	}
	if version.Version != 1 {
		t.Fatalf("operational update created version %d", version.Version)
	}
	limit, _, _, _, _, _, err := db.AgentBudget(ctx, agent.ID)
	if err != nil || limit != 10_000_000 {
		t.Fatalf("budget=%d err=%v", limit, err)
	}
}
