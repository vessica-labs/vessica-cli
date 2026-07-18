package run

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/runner"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestParseTicketPlan(t *testing.T) {
	raw := `{
  "complexity": "m",
  "complexity_rationale": "Two dependent implementation slices are useful.",
  "tickets": [
    {
      "type": "feature",
      "title": "A",
      "body": "Do A",
      "acceptance_criteria": ["A works"],
      "estimated_complexity": "s",
      "split_justification": "Dependency sequence requires A before the later integration verification work can start."
    },
    {
      "type": "feature",
      "title": "B",
      "body": "Do B",
      "acceptance_criteria": ["B works"],
      "depends_on_titles": ["A"],
      "estimated_complexity": "s",
      "split_justification": "Dependency sequence keeps B separate because it integrates behavior after A exists."
    }
  ]
}`
	specs, err := parseTicketPlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 || specs[1].DependsOnTitles[0] != "A" || specs[0].AcceptanceCriteria[0] != "A works" {
		t.Fatalf("unexpected specs: %#v", specs)
	}
}

func TestParseTicketPlanRejectsBadDependency(t *testing.T) {
	_, err := parseTicketPlan(`{"complexity":"s","tickets":[{"type":"feature","title":"A","body":"Do A","acceptance_criteria":["A works"],"depends_on_titles":["missing"]}]}`)
	if err == nil {
		t.Fatal("expected bad dependency error")
	}
}

func TestParseTicketPlanRejectsTooManySimpleTickets(t *testing.T) {
	_, err := parseTicketPlan(`{
	  "complexity": "s",
	  "tickets": [
	    {"type":"feature","title":"A","body":"Do A","acceptance_criteria":["A works"],"split_justification":"Independent work could be separate but simple work should stay together."},
	    {"type":"feature","title":"B","body":"Do B","acceptance_criteria":["B works"],"split_justification":"Independent work could be separate but simple work should stay together."}
	  ]
	}`)
	if err == nil {
		t.Fatal("expected simple ticket cap error")
	}
}

func TestParseTicketPlanRejectsWeakSplitJustification(t *testing.T) {
	_, err := parseTicketPlan(`{
	  "complexity": "m",
	  "tickets": [
	    {"type":"feature","title":"A","body":"Do A","acceptance_criteria":["A works"],"split_justification":"Do first."},
	    {"type":"feature","title":"B","body":"Do B","acceptance_criteria":["B works"],"split_justification":"Do second."}
	  ]
	}`)
	if err == nil {
		t.Fatal("expected weak split justification error")
	}
}

func TestParsePlanningBundle(t *testing.T) {
	bundle, err := parsePlanningBundle(`{
	  "complexity": "XS",
	  "complexity_rationale": "One localized change.",
	  "prd_markdown": "# PRD\n\nDo the thing.",
	  "adr_markdown": "# ADR\n\nKeep existing architecture.",
	  "design_spec_markdown": "# Design\n\nOne component changes.",
	  "test_scenarios_markdown": "# Tests\n\n1. Happy path works"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Complexity != "xs" {
		t.Fatalf("complexity=%q", bundle.Complexity)
	}
}

func TestParsePlanningBundleRejectsMissingMarkdown(t *testing.T) {
	_, err := parsePlanningBundle(`{
	  "complexity": "s",
	  "prd_markdown": "PRD without heading",
	  "adr_markdown": "# ADR",
	  "design_spec_markdown": "# Design",
	  "test_scenarios_markdown": "# Tests"
	}`)
	if err == nil {
		t.Fatal("expected markdown heading error")
	}
}

func TestRunnerResultErrorPreservesUnderlyingFailure(t *testing.T) {
	err := runnerResultError("plan", runner.Result{Status: "failed", Output: "progress\nmodel requires a newer Codex CLI"})
	if err == nil || !strings.Contains(err.Error(), "model requires a newer Codex CLI") {
		t.Fatalf("error=%v", err)
	}
}

func TestRunnerEventCarriesExecutingTicketID(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.EnsureWorkspace(ctx, root, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "Ticket events", "body")
	if err != nil {
		t.Fatal(err)
	}
	runRecord, err := db.CreateRun(ctx, epic.ID, "", "codex", "model", "high", "local", 1, false, "draft", "", "")
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{DB: db, Root: root}
	ticketCtx := context.WithValue(ctx, runnerTicketIDKey, "tkt_123")
	engine.emitRunner(ticketCtx, runRecord, "code", "coder", runner.Event{Type: "agent.output", Message: "summary"})
	events, err := db.ListEvents(ctx, runRecord.ID, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(events[0].PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ticket_id"] != "tkt_123" || payload["phase"] != "code" || payload["role"] != "coder" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestCoderSystemPromptAddsEngineManagedOverlay(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vessica", "agents", "coder")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "AGENTS.md"), []byte("old guidance: ves ticket close"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := (&Engine{Root: root}).agentSystemPrompt("coder", root, "code")
	if !strings.Contains(prompt, "old guidance") {
		t.Fatalf("expected workspace prompt to be preserved: %s", prompt)
	}
	if !strings.Contains(prompt, "Do not run Vessica lifecycle commands") {
		t.Fatalf("expected engine-managed overlay: %s", prompt)
	}
	if !strings.Contains(prompt, "ves ticket close") {
		t.Fatalf("expected close command to be explicitly prohibited: %s", prompt)
	}
	if !strings.Contains(prompt, "use pnpm exclusively") {
		t.Fatalf("expected pnpm requirement: %s", prompt)
	}
}

func TestCoderDirectPromptKeepsGlobalPackageManagerRule(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vessica", "agents", "coder")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "AGENTS.md"), []byte("workspace coder guidance"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt := (&Engine{Root: root}).agentSystemPrompt("coder", root, "prompt")
	if !strings.Contains(prompt, "workspace coder guidance") || !strings.Contains(prompt, "use pnpm exclusively") {
		t.Fatalf("prompt=%s", prompt)
	}
}

func TestScenarioSteps(t *testing.T) {
	steps := scenarioSteps(`# Test Scenarios

1. Happy path loads
2. Validation errors handled
- Regression suite green
`)
	if len(steps) != 3 {
		t.Fatalf("steps=%#v", steps)
	}
}
