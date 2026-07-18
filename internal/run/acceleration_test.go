package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestParallelBuildGatesOverlapIndependentLanesAndPreserveBuildBeforeTest(t *testing.T) {
	root, db, runRecord, _ := promptSandboxFixture(t)
	defer db.Close()
	engine := &Engine{DB: db, Root: root}
	commands := []namedBuildCommand{
		{name: "lint", cmd: "sleep 0.20"},
		{name: "lint-arch", cmd: "sleep 0.20"},
		{name: "build", cmd: "sleep 0.20 && printf build > build.marker"},
		{name: "test", cmd: "test -f build.marker && sleep 0.20"},
	}
	started := time.Now()
	if err := engine.runParallelBuildGates(context.Background(), runRecord, root, commands); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 650*time.Millisecond {
		t.Fatalf("independent gates did not overlap: %s", elapsed)
	}
}

func TestParsePlanningBundleAcceptsValidatedXSTicket(t *testing.T) {
	raw := `{
  "complexity":"xs",
  "complexity_rationale":"localized docs change",
  "prd_markdown":"# PRD",
  "adr_markdown":"# ADR",
  "design_spec_markdown":"# Design",
  "test_scenarios_markdown":"# Tests",
  "ticket":{
    "type":"docs",
    "title":"Clarify the guide",
    "body":"Update one guide section.",
    "acceptance_criteria":["The guide states the supported behavior."],
    "depends_on_titles":[],
    "estimated_complexity":"xs",
    "split_justification":""
  }
}`
	bundle, err := parsePlanningBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Ticket == nil || bundle.Ticket.Title != "Clarify the guide" {
		t.Fatalf("ticket=%#v", bundle.Ticket)
	}
}

func TestXSTicketPlanReusesPlanningEvidence(t *testing.T) {
	root, db, runRecord, _ := promptSandboxFixture(t)
	defer db.Close()
	epic, err := db.GetEpic(context.Background(), runRecord.EpicID)
	if err != nil {
		t.Fatal(err)
	}
	ticket := plannedTicket{Type: "docs", Title: "One call", Body: "Reuse planning output.", AcceptanceCriteria: []string{"Only one ticket is created."}, EstimatedComplexity: "xs"}
	if _, err = db.CreateRunEvidence(context.Background(), runRecord.ID, "plan", "planning_bundle", "", "passed", planningEvidence{Complexity: "xs", Rationale: "localized", Ticket: &ticket}); err != nil {
		t.Fatal(err)
	}
	engine := &Engine{DB: db, Root: root}
	plan, fast, err := engine.xsTicketPlan(context.Background(), runRecord, epic, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !fast || len(plan) != 1 || plan[0].Title != ticket.Title {
		t.Fatalf("fast=%v plan=%#v", fast, plan)
	}
}

func TestFocusedValidationAndOfflineInstallContracts(t *testing.T) {
	guidance := focusedValidationGuidance(t.TempDir(), "Update the documentation guide")
	for _, required := range []string{"git diff --check", "Do not run repository-wide", "documentation ticket"} {
		if !strings.Contains(guidance, required) {
			t.Fatalf("guidance missing %q: %s", required, guidance)
		}
	}
	install := "corepack prepare pnpm@11 --activate && pnpm install --frozen-lockfile"
	if got := offlineInstallCommand(install); !strings.Contains(got, "pnpm install --offline --frozen-lockfile") {
		t.Fatalf("offline command=%q", got)
	}
}

func TestContextPacketIsBounded(t *testing.T) {
	packet := boundContextPacket(strings.Repeat("x", maxContextPacketBytes+100))
	if len(packet) != maxContextPacketBytes || !strings.HasSuffix(packet, "...") {
		t.Fatalf("bounded packet has length %d", len(packet))
	}
}

func TestDeterministicXSTicketUsesTestScenarios(t *testing.T) {
	epic := &state.Epic{Title: "Fix docs", Body: "Clarify the reference."}
	artifact := &state.Artifact{Type: "test-scenarios", Body: "# Tests\n1. Reference names the field\n2. Existing examples remain valid"}
	ticket := deterministicXSTicket(epic, []*state.Artifact{artifact})
	if ticket.Type != "docs" || len(ticket.AcceptanceCriteria) != 2 {
		t.Fatalf("ticket=%#v", ticket)
	}
}

func TestTerminalEpicStatusReflectsReviewState(t *testing.T) {
	if got := terminalEpicStatus(&state.Run{PRURL: "https://example.test/pr/1", PRMode: "draft"}); got != state.EpicStatusInReview {
		t.Fatalf("draft PR status=%q", got)
	}
	if got := terminalEpicStatus(&state.Run{PRURL: "https://example.test/pr/1", PRMode: "merged"}); got != state.EpicStatusCompleted {
		t.Fatalf("merged PR status=%q", got)
	}
	if got := terminalEpicStatus(&state.Run{}); got != state.EpicStatusCompleted {
		t.Fatalf("terminal status=%q", got)
	}
}
