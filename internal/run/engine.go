package run

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/pack"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/runner"
	"github.com/vessica-labs/vessica-cli/internal/sandbox"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

type Engine struct {
	DB         *state.DB
	Root       string
	Config     config.Config
	Stream     bool
	EventsOnly bool
	StreamMode streaming.Mode
	Renderer   *streaming.Renderer
	EventSink  func(*state.Event)
	Local      bool // prefer local sandbox when docker unavailable
	rawMu      sync.Mutex
}

type Options struct {
	EpicID          string
	TicketID        string
	Runner          string
	Model           string
	ReasoningEffort string
	Sandbox         string
	Concurrency     int
	Preview         bool
	PRMode          string
	StartAt         string
	StopAfter       string
	ReuseArtifacts  string
	Stream          bool
	EventsOnly      bool
	StreamMode      streaming.Mode
	ResumeFrom      string
	RunID           string
}

type plannedTicket struct {
	Type                string   `json:"type"`
	Title               string   `json:"title"`
	Body                string   `json:"body"`
	AcceptanceCriteria  []string `json:"acceptance_criteria"`
	DependsOnTitles     []string `json:"depends_on_titles"`
	SplitJustification  string   `json:"split_justification"`
	EstimatedComplexity string   `json:"estimated_complexity"`
}

type ticketPlan struct {
	Complexity string          `json:"complexity"`
	Rationale  string          `json:"complexity_rationale"`
	Tickets    []plannedTicket `json:"tickets"`
}

type planningBundle struct {
	PRDMarkdown           string `json:"prd_markdown"`
	ADRMarkdown           string `json:"adr_markdown"`
	DesignSpecMarkdown    string `json:"design_spec_markdown"`
	TestScenariosMarkdown string `json:"test_scenarios_markdown"`
	Complexity            string `json:"complexity"`
	Rationale             string `json:"complexity_rationale"`
}

func (e *Engine) emit(ctx context.Context, runID, typ string, payload any) {
	if m, ok := payload.(map[string]any); ok {
		if msg, ok := m["message"].(string); ok {
			m["message"] = redaction.Redact(msg)
			payload = m
		}
	}
	ev, err := e.DB.AppendEvent(ctx, runID, "", typ, payload)
	if err != nil {
		return
	}
	if e.EventSink != nil {
		e.EventSink(ev)
	}
	if e.Stream {
		mode := e.StreamMode
		if mode == "" {
			if e.EventsOnly {
				mode = streaming.ModeEvents
			} else {
				mode = streaming.ModePretty
			}
		}
		msg := ""
		if m, ok := payload.(map[string]any); ok {
			if s, ok := m["message"].(string); ok {
				msg = s
			}
		}
		switch mode {
		case streaming.ModePretty:
			if e.Renderer == nil {
				e.Renderer = streaming.NewRenderer(os.Stdout, os.Stderr)
			}
			e.Renderer.Render(ev)
		case streaming.ModeEvents:
			summary := streaming.EventSummary(ev)
			if summary == "" {
				summary = typ
			}
			fmt.Fprintf(os.Stderr, "[%d] %s %s\n", ev.Seq, typ, summary)
		case streaming.ModeJSONL:
			_ = streaming.WriteEvent(os.Stdout, ev)
		case streaming.ModeRaw:
			if !strings.HasPrefix(typ, "agent.") {
				fmt.Fprintf(os.Stderr, "[%d] %s %s\n", ev.Seq, typ, msg)
			}
		}
	}
}

func (e *Engine) emitRunner(ctx context.Context, r *state.Run, phase, role string, runnerEvent runner.Event) {
	payload := map[string]any{}
	for key, value := range runnerEvent.Data {
		payload[key] = value
	}
	payload["message"] = redaction.Redact(runnerEvent.Message)
	payload["role"] = role
	payload["phase"] = phase
	if raw := strings.TrimSpace(redaction.Redact(runnerEvent.Raw)); raw != "" {
		raw = normalizeRawLine(raw)
		if ref, err := e.appendRawLog(r.ID, raw); err == nil {
			for key, value := range ref {
				payload[key] = value
			}
		}
		if e.Stream && e.StreamMode == streaming.ModeRaw {
			fmt.Fprintln(os.Stdout, raw)
		}
	}
	e.emit(ctx, r.ID, runnerEvent.Type, payload)
}

func normalizeRawLine(line string) string {
	if json.Valid([]byte(line)) {
		return line
	}
	b, _ := json.Marshal(map[string]any{"type": "vessica.raw", "text": line})
	return string(b)
}

func (e *Engine) appendRawLog(runID, line string) (map[string]any, error) {
	e.rawMu.Lock()
	defer e.rawMu.Unlock()
	dir := filepath.Join(e.Root, ".vessica", "runs", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "agent.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	offset, err := f.Seek(0, 2)
	if err != nil {
		return nil, err
	}
	data := []byte(line + "\n")
	if _, err := f.Write(data); err != nil {
		return nil, err
	}
	return map[string]any{
		"raw_log_path":   filepath.ToSlash(filepath.Join(".vessica", "runs", runID, "agent.jsonl")),
		"raw_log_offset": offset,
		"raw_log_length": len(data),
	}, nil
}

func (e *Engine) RunEpic(ctx context.Context, opts Options) (*state.Run, error) {
	if opts.Runner == "" {
		opts.Runner = e.Config.Runner.Default
	}
	if opts.Model == "" {
		opts.Model = e.Config.Runner.Model
	}
	if opts.ReasoningEffort == "" {
		opts.ReasoningEffort = e.Config.Runner.ReasoningEffort
	}
	if opts.Sandbox == "" {
		opts.Sandbox = e.Config.Sandbox.Backend
	}
	r, err := e.DB.CreateRun(ctx, opts.EpicID, opts.TicketID, opts.Runner, opts.Model, opts.ReasoningEffort, opts.Sandbox, opts.Concurrency, opts.Preview, opts.PRMode, opts.StartAt, opts.StopAfter)
	if err != nil {
		return nil, err
	}
	return e.execute(ctx, r, opts)
}

func simulationMode() bool {
	return os.Getenv("VES_RUNNER_MODE") == "stub" || os.Getenv("VES_SIMULATION") == "1"
}

func defaultTicketPlan() []plannedTicket {
	return []plannedTicket{
		{
			Type:                "feature",
			Title:               "Implement epic end-to-end",
			Body:                "Implement the requested epic as one coherent change. Include relevant tests, build updates, validation hooks, and documentation in the same ticket unless a true dependency requires a split.",
			AcceptanceCriteria:  []string{"Requested behavior is implemented", "Relevant tests or smoke checks pass", "Build and preview remain healthy"},
			EstimatedComplexity: "s",
		},
	}
}

func (e *Engine) Resume(ctx context.Context, runID, fromPhase string) (*state.Run, error) {
	r, err := e.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	// Runs created before model selection was persisted inherit the workspace default.
	if r.Model == "" {
		r.Model = e.Config.Runner.Model
	}
	if r.ReasoningEffort == "" {
		r.ReasoningEffort = e.Config.Runner.ReasoningEffort
	}
	opts := Options{
		EpicID:          r.EpicID,
		TicketID:        r.TicketID,
		Runner:          r.Runner,
		Model:           r.Model,
		ReasoningEffort: r.ReasoningEffort,
		Sandbox:         r.SandboxBackend,
		Concurrency:     r.Concurrency,
		Preview:         r.Preview,
		PRMode:          r.PRMode,
		StartAt:         fromPhase,
		// Resume continues through remaining phases; do not inherit a prior --stop-after.
		StopAfter:  "",
		Stream:     e.Stream,
		EventsOnly: e.EventsOnly,
		StreamMode: e.StreamMode,
		RunID:      runID,
	}
	if opts.StartAt == "" {
		opts.StartAt = r.CurrentPhase
	}
	// If resuming the phase that already completed/stopped, advance to the next phase.
	if opts.StartAt != "" {
		if phases, err := e.DB.ListPhases(ctx, runID); err == nil {
			for _, p := range phases {
				if p.Phase == opts.StartAt && (p.Status == "completed" || p.Status == "skipped") {
					if idx := phaseIndex(opts.StartAt); idx >= 0 && idx+1 < len(state.SoftwareEpicPhases) {
						opts.StartAt = state.SoftwareEpicPhases[idx+1]
					}
					break
				}
			}
		}
	}
	r.Status = "running"
	r.Error = ""
	r.StopAfter = ""
	r.FinishedAt = ""
	_ = e.DB.UpdateRun(ctx, r)
	return e.execute(ctx, r, opts)
}

func (e *Engine) execute(ctx context.Context, r *state.Run, opts Options) (*state.Run, error) {
	e.Stream = opts.Stream
	e.EventsOnly = opts.EventsOnly
	e.StreamMode = opts.StreamMode
	r.Status = "running"
	r.StartedAt = state.Now()
	_ = e.DB.UpdateRun(ctx, r)
	e.emit(ctx, r.ID, "run.started", map[string]any{"epic_id": r.EpicID})

	phases := state.SoftwareEpicPhases
	startIdx := 0
	if opts.StartAt != "" {
		startIdx = phaseIndex(opts.StartAt)
		if startIdx < 0 {
			return r, fmt.Errorf("unknown start phase: %s", opts.StartAt)
		}
	}
	stopIdx := len(phases) - 1
	if opts.StopAfter != "" {
		stopIdx = phaseIndex(opts.StopAfter)
		if stopIdx < 0 {
			return r, fmt.Errorf("unknown stop phase: %s", opts.StopAfter)
		}
	}

	// Skip phases before start
	phaseStatus := map[string]string{}
	if existing, err := e.DB.ListPhases(ctx, r.ID); err == nil {
		for _, p := range existing {
			phaseStatus[p.Phase] = p.Status
		}
	}
	for i := 0; i < startIdx; i++ {
		if phaseStatus[phases[i]] == "completed" {
			continue
		}
		_ = e.DB.SetPhaseStatus(ctx, r.ID, phases[i], "skipped", "")
	}

	var runErr error
	for i := startIdx; i <= stopIdx; i++ {
		phase := phases[i]
		r.CurrentPhase = phase
		_ = e.DB.UpdateRun(ctx, r)
		_ = e.DB.SetPhaseStatus(ctx, r.ID, phase, "running", "")
		e.emit(ctx, r.ID, "run.phase.started", map[string]any{"phase": phase})

		if err := e.runPhase(ctx, r, opts, phase); err != nil {
			_ = e.DB.SetPhaseStatus(ctx, r.ID, phase, "failed", err.Error())
			e.emit(ctx, r.ID, "error", map[string]any{"phase": phase, "message": redaction.Redact(err.Error())})
			r.Status = "failed"
			r.Error = err.Error()
			r.FinishedAt = state.Now()
			_ = e.DB.UpdateRun(ctx, r)
			if sbRec, sbErr := e.DB.GetSandboxForRun(ctx, r.ID); sbErr == nil {
				_ = retention.MarkFailed(ctx, e.DB, sbRec)
			}
			runErr = err
			e.recordWorkflowKnowledge(ctx, r, "run.failed", "Run failed during "+phase+": "+redaction.Redact(err.Error()), "run:"+r.ID+":failed")
			break
		}
		_ = e.DB.SetPhaseStatus(ctx, r.ID, phase, "completed", "")
		e.emit(ctx, r.ID, "run.phase.completed", map[string]any{"phase": phase})
		if phase == "ticketize" {
			e.recordWorkflowKnowledge(ctx, r, "epic.planned", "Epic planning and ticket graph completed", "epic:"+r.EpicID+":planned")
		}
	}

	if runErr == nil {
		if stopIdx < len(phases)-1 {
			r.Status = "stopped"
		} else if r.Status == "running" || r.Status == "" {
			r.Status = "completed"
		}
		if r.FinishedAt == "" {
			r.FinishedAt = state.Now()
		}
		_ = e.DB.UpdateRun(ctx, r)
		e.emit(ctx, r.ID, "run.completed", map[string]any{"status": r.Status})
		if r.Status == "completed" {
			e.recordWorkflowKnowledge(ctx, r, "run.completed", "Run completed successfully", "run:"+r.ID+":completed")
		}
	}
	result, err := e.DB.GetRun(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if sbRec, sbErr := e.DB.GetSandboxForRun(ctx, r.ID); sbErr == nil {
		result.SandboxID = sbRec.ID
		if expires := retention.EffectiveExpiry(sbRec); !expires.IsZero() {
			result.SandboxExpiresAt = expires.Format(time.RFC3339Nano)
		}
	}
	return result, runErr
}

func phaseIndex(name string) int {
	for i, p := range state.SoftwareEpicPhases {
		if p == name {
			return i
		}
	}
	return -1
}

func (e *Engine) runPhase(ctx context.Context, r *state.Run, opts Options, phase string) error {
	switch phase {
	case "preflight":
		return e.phasePreflight(ctx, r)
	case "harness":
		return e.phaseHarness(ctx, r)
	case "plan":
		return e.phasePlan(ctx, r)
	case "design":
		return e.phaseDesign(ctx, r)
	case "ticketize":
		return e.phaseTicketize(ctx, r)
	case "code":
		return e.phaseCode(ctx, r, opts.Concurrency)
	case "build":
		return e.phaseBuild(ctx, r)
	case "validate":
		return e.phaseValidate(ctx, r)
	case "preview":
		if !opts.Preview && !r.Preview {
			e.emit(ctx, r.ID, "warning", map[string]any{"message": "preview skipped"})
			return nil
		}
		return e.phasePreview(ctx, r)
	case "pr":
		if opts.PRMode == "none" || r.PRMode == "none" {
			e.emit(ctx, r.ID, "warning", map[string]any{"message": "pr skipped"})
			return nil
		}
		return e.phasePR(ctx, r)
	case "receipt":
		return e.phaseReceipt(ctx, r)
	default:
		return fmt.Errorf("unknown phase %s", phase)
	}
}

func (e *Engine) phasePreflight(ctx context.Context, r *state.Run) error {
	checks := []string{}
	if err := e.DB.Ping(ctx); err != nil {
		return fmt.Errorf("state backend: %w", err)
	}
	checks = append(checks, "state:ok")
	if e.Config.Repo.Remote == "" && !simulationMode() {
		return fmt.Errorf("repo.remote is required for default sandbox clone")
	}
	if e.Config.Repo.Remote != "" {
		checks = append(checks, "repo_remote:ok")
	}
	if _, err := pack.ReadLock(e.Root); err != nil {
		return fmt.Errorf("pack not installed: %w", err)
	}
	checks = append(checks, "pack:ok")
	needsDocker := r.SandboxBackend == "docker" && !e.Local
	if _, err := exec.LookPath("docker"); err != nil && needsDocker {
		if !simulationMode() {
			return fmt.Errorf("docker not found in PATH")
		}
		e.Local = true
		checks = append(checks, "docker:missing_using_local")
	} else if needsDocker {
		checks = append(checks, "docker:ok")
	} else {
		checks = append(checks, "sandbox:"+r.SandboxBackend)
	}
	if r.Runner == "" {
		return fmt.Errorf("runner not configured")
	}
	if r.Runner == "codex" {
		if _, err := exec.LookPath("codex"); err != nil && !simulationMode() {
			return fmt.Errorf("codex runner not found in PATH")
		}
	}
	if _, err := os.Stat(filepath.Join(e.Root, "package.json")); err == nil {
		if _, err := exec.LookPath("pnpm"); err != nil && !simulationMode() {
			return fmt.Errorf("pnpm not found in PATH")
		}
		checks = append(checks, "pnpm:ok")
	}
	e.emit(ctx, r.ID, "agent.progress", map[string]any{"message": "preflight", "checks": checks})
	return nil
}

func (e *Engine) phaseHarness(ctx context.Context, r *state.Run) error {
	audit, err := harness.Audit(e.Root)
	if err != nil {
		return err
	}
	if audit.Drift != "ok" {
		if _, err := harness.Sync(e.Root); err != nil {
			return err
		}
		e.emit(ctx, r.ID, "agent.progress", map[string]any{"message": "harness synced"})
	}
	return nil
}

func (e *Engine) phasePlan(ctx context.Context, r *state.Run) error {
	epic, err := e.DB.GetEpic(ctx, r.EpicID)
	if err != nil {
		return err
	}
	bundle, err := e.generatePlanningBundle(ctx, r, epic)
	if err != nil {
		return err
	}
	prd, err := e.DB.CreateArtifact(ctx, "prd", epic.Title+" PRD", bundle.PRDMarkdown, epic.ID, r.ID)
	if err != nil {
		return err
	}
	adr, err := e.DB.CreateArtifact(ctx, "adr", "ADR: "+epic.Title, bundle.ADRMarkdown, epic.ID, r.ID)
	if err != nil {
		return err
	}
	ts, err := e.DB.CreateArtifact(ctx, "test-scenarios", "Tests: "+epic.Title, bundle.TestScenariosMarkdown, epic.ID, r.ID)
	if err != nil {
		return err
	}
	design, err := e.DB.CreateArtifact(ctx, "design-spec", "Design: "+epic.Title, bundle.DesignSpecMarkdown, epic.ID, r.ID)
	if err != nil {
		return err
	}
	for _, artifact := range []*state.Artifact{prd, adr, ts, design} {
		e.mirrorArtifactKnowledge(ctx, r, artifact)
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "plan", "planning_bundle", "", "passed", map[string]any{"complexity": bundle.Complexity, "rationale": bundle.Rationale})
	e.emit(ctx, r.ID, "agent.progress", map[string]any{"message": "plan artifacts created", "artifacts": []string{prd.ID, adr.ID, ts.ID, design.ID}, "complexity": bundle.Complexity})
	return nil
}

func (e *Engine) phaseDesign(ctx context.Context, r *state.Run) error {
	epic, err := e.DB.GetEpic(ctx, r.EpicID)
	if err != nil {
		return err
	}
	if arts, err := e.DB.ListArtifacts(ctx, epic.ID, "design-spec"); err == nil {
		for _, a := range arts {
			if a.SourceRunID == r.ID && strings.TrimSpace(a.Body) != "" {
				e.emit(ctx, r.ID, "agent.progress", map[string]any{"message": "design reused from planning bundle", "artifact_id": a.ID})
				return nil
			}
		}
	}
	body, err := e.generateArtifactBody(ctx, r, "design", "design", fmt.Sprintf("Write a lightweight DesignSpec in markdown for this epic.\nKeep it under 600 words. Capture only components, interfaces, state/data, risks, and validation notes that materially affect implementation.\nTitle: %s\nBody:\n%s", epic.Title, epic.Body), func() string {
		return fmt.Sprintf("# Design Spec: %s\n\n## Overview\n\n%s\n\n## Components\n\n- API\n- UI\n- Persistence\n", epic.Title, epic.Body)
	})
	if err != nil {
		return err
	}
	a, err := e.DB.CreateArtifact(ctx, "design-spec", "Design: "+epic.Title, body, epic.ID, r.ID)
	if err != nil {
		return err
	}
	e.emit(ctx, r.ID, "agent.progress", map[string]any{"message": "design created", "artifact_id": a.ID})
	return nil
}

func (e *Engine) phaseTicketize(ctx context.Context, r *state.Run) error {
	epic, err := e.DB.GetEpic(ctx, r.EpicID)
	if err != nil {
		return err
	}
	arts, err := e.DB.ListArtifacts(ctx, epic.ID, "")
	if err != nil {
		return err
	}
	arts = artifactsForRun(arts, r.ID)
	var ids []string
	for _, a := range arts {
		ids = append(ids, a.ID)
	}
	set, err := e.DB.CreateArtifactSet(ctx, epic.ID, r.ID, ids)
	if err != nil {
		return err
	}
	_ = e.DB.ApproveArtifactSet(ctx, set.ID)
	r.ArtifactSetID = set.ID
	_ = e.DB.UpdateRun(ctx, r)

	specs, err := e.planTickets(ctx, r, epic, arts)
	if err != nil {
		return err
	}
	created, err := e.createPlannedTickets(ctx, epic.ID, r.ID, specs)
	if err != nil {
		return err
	}
	waves, err := e.DB.ComputeWavesForRun(ctx, epic.ID, r.ID)
	if err != nil {
		return err
	}
	_, _ = e.DB.UpdateEpic(ctx, epic.ID, "", "", "planned")
	var ticketIDs []string
	for _, t := range created {
		ticketIDs = append(ticketIDs, t.ID)
	}
	e.emit(ctx, r.ID, "agent.progress", map[string]any{
		"message":      "ticketized",
		"artifact_set": set.ID,
		"tickets":      ticketIDs,
		"waves":        len(waves),
	})
	return nil
}

func artifactsForRun(arts []state.Artifact, runID string) []state.Artifact {
	var current []state.Artifact
	seen := map[string]bool{}
	for _, a := range arts {
		if a.SourceRunID != runID || seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		current = append(current, a)
	}
	if len(current) > 0 {
		return current
	}
	latestByType := map[string]state.Artifact{}
	for _, a := range arts {
		if seen[a.ID] {
			continue
		}
		if _, ok := latestByType[a.Type]; !ok {
			latestByType[a.Type] = a
		}
	}
	var fallback []state.Artifact
	for _, typ := range []string{"prd", "adr", "design-spec", "test-scenarios"} {
		if a, ok := latestByType[typ]; ok {
			fallback = append(fallback, a)
		}
	}
	return fallback
}

func (e *Engine) phaseCode(ctx context.Context, r *state.Run, concurrency int) error {
	if concurrency <= 0 {
		concurrency = 3
	}
	branch := fmt.Sprintf("vessica/%s/%s", r.EpicID, r.ID)
	var sbRec *state.Sandbox
	if r.SandboxBackend == "railway" {
		sbRec, _ = e.DB.GetSandboxForRun(ctx, r.ID)
	}
	if sbRec == nil {
		var err error
		sbRec, err = e.DB.CreateSandbox(ctx, r.ID, r.SandboxBackend, branch)
		if err != nil {
			return err
		}
	}
	if err := retention.Initialize(ctx, e.DB, sbRec); err != nil {
		return err
	}
	sb, err := e.openSandbox(ctx, sbRec)
	if err != nil {
		return err
	}
	if old, err := retention.DestroySuperseded(ctx, e.DB, e.Root, r.ID, sbRec.ID); err != nil {
		return err
	} else if len(old) > 0 {
		e.emit(ctx, r.ID, "sandbox.superseded", map[string]any{"sandbox_ids": old, "replacement_id": sbRec.ID})
	}
	workdir := sb.Workdir()
	if workdir == "" {
		workdir = e.Root
	}
	e.emit(ctx, r.ID, "sandbox.ready", map[string]any{"sandbox_id": sb.ID(), "workdir": workdir})
	if r.Preview && !simulationMode() {
		if err := e.startPreviewInSandbox(ctx, r, sbRec, sb, workdir, "code"); err != nil {
			e.emit(ctx, r.ID, "preview.deferred", map[string]any{"phase": "code", "message": redaction.Redact(err.Error())})
		}
	}

	if r.TicketID != "" {
		ticket, err := e.DB.GetTicket(ctx, r.TicketID)
		if err != nil {
			return err
		}
		if ticket.EpicID != r.EpicID {
			return fmt.Errorf("ticket %s belongs to epic %s, not run epic %s", ticket.ID, ticket.EpicID, r.EpicID)
		}
		results, err := e.processWaveTickets(ctx, r, workdir, "", []state.Ticket{*ticket}, 1)
		if err != nil {
			return err
		}
		return e.mergeAndCloseTicketResults(ctx, r, workdir, results)
	}

	waves, err := e.DB.ListWavesForRun(ctx, r.EpicID, r.ID)
	if err != nil {
		return err
	}
	if len(waves) == 0 {
		waves, err = e.DB.ComputeWavesForRun(ctx, r.EpicID, r.ID)
		if err != nil {
			return err
		}
	}
	for _, wave := range waves {
		tickets, err := e.DB.ListTicketsForRun(ctx, r.EpicID, r.ID)
		if err != nil {
			return err
		}
		var ready []state.Ticket
		for _, ticket := range tickets {
			if ticket.WaveID == wave.ID && ticket.Status != "closed" {
				ready = append(ready, ticket)
			}
		}
		results, err := e.processWaveTickets(ctx, r, workdir, wave.ID, ready, concurrency)
		if err != nil {
			return err
		}
		if err := e.mergeAndCloseTicketResults(ctx, r, workdir, results); err != nil {
			return err
		}
		e.emit(ctx, r.ID, "wave.completed", map[string]any{"wave_id": wave.ID, "index": wave.Index})
	}
	return nil
}

func (e *Engine) mergeAndCloseTicketResults(ctx context.Context, r *state.Run, workdir string, results []ticketWorkResult) error {
	for _, result := range results {
		if err := e.mergeTicketBranch(ctx, workdir, result.branch, r.ID, result.ticket.ID); err != nil {
			return err
		}
		if r.PreviewURL != "" {
			e.emit(ctx, r.ID, "preview.updated", map[string]any{"url": r.PreviewURL, "ticket_id": result.ticket.ID, "files": result.files})
		}
		ev, _ := e.DB.CreateRunEvidence(ctx, r.ID, "code", "ticket", result.ticket.ID, "passed", map[string]any{
			"ticket_id": result.ticket.ID,
			"agent_id":  result.agentID,
			"branch":    result.branch,
			"commit":    result.commit,
			"files":     result.files,
			"runner":    result.runner.Model,
			"output":    truncate(result.runner.Output, 2000),
		})
		rcpt, err := e.DB.CreateReceipt(ctx, r.ID, r.EpicID, "ticket_closed", map[string]any{"ticket_id": result.ticket.ID, "evidence_id": ev.ID, "branch": result.branch, "commit": result.commit, "files": result.files})
		if err != nil {
			return err
		}
		if _, err := e.DB.CloseTicket(ctx, result.ticket.ID, result.agentID, rcpt.ID); err != nil {
			return err
		}
		e.emit(ctx, r.ID, "ticket.closed", map[string]any{"ticket_id": result.ticket.ID, "evidence": rcpt.ID, "branch": result.branch, "commit": result.commit})
		e.recordWorkflowKnowledge(ctx, r, "ticket.completed", "Completed ticket: "+result.ticket.Title, "ticket:"+result.ticket.ID+":completed",
			knowledge.ExternalRef{System: "vessica.ticket", ID: result.ticket.ID},
			knowledge.ExternalRef{System: "vessica.receipt", ID: rcpt.ID},
			knowledge.ExternalRef{System: "git.commit", ID: result.commit})
	}
	return nil
}

type ticketWorkResult struct {
	ticket  state.Ticket
	agentID string
	branch  string
	commit  string
	files   []string
	runner  runner.Result
}

func (e *Engine) processWaveTickets(ctx context.Context, r *state.Run, integrationWorkdir, waveID string, tickets []state.Ticket, concurrency int) ([]ticketWorkResult, error) {
	if len(tickets) == 0 {
		return nil, nil
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(tickets) {
		concurrency = len(tickets)
	}
	results := make([]ticketWorkResult, len(tickets))
	errs := make([]error, len(tickets))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var worktreeMu sync.Mutex
	for i, ticket := range tickets {
		i, ticket := i, ticket
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			agentID := id.New(id.Agent)
			claimed := false
			succeeded := false
			defer func() {
				if claimed && !succeeded {
					_ = e.DB.ReleaseClaim(context.Background(), ticket.ID, agentID, "run ticket failed")
				}
			}()
			if _, _, err := e.DB.ClaimTicket(ctx, ticket.ID, agentID, 45*time.Minute); err != nil {
				errs[i] = err
				return
			}
			claimed = true
			e.emit(ctx, r.ID, "ticket.claimed", map[string]any{"ticket_id": ticket.ID, "agent_id": agentID, "wave_id": waveID})
			_, _ = e.DB.HeartbeatClaim(ctx, ticket.ID, agentID, 45*time.Minute)
			worktreeMu.Lock()
			ticketWorkdir, ticketBranch, err := e.prepareTicketWorktree(ctx, integrationWorkdir, r, &ticket)
			worktreeMu.Unlock()
			if err != nil {
				errs[i] = err
				return
			}
			prompt := fmt.Sprintf(`Implement ticket %s: %s

%s

Engine-managed lifecycle:
- Vessica has already claimed this ticket for this run.
- Do not run ves ticket claim, close, heartbeat, release, or memory commands.
- Do not attempt to discover or use the internal Vessica agent id.
- Make the code changes in this worktree, run relevant local checks, and return a concise evidence summary.

Use TDD where helpful. Return changed files and commands run.`, ticket.ID, ticket.Title, ticket.Body)
			res, err := e.invokeRunner(ctx, r, "code", prompt, "coder", ticketWorkdir)
			if err != nil {
				errs[i] = err
				return
			}
			if res.Status != "ok" {
				errs[i] = fmt.Errorf("runner failed for ticket %s: %s", ticket.ID, truncate(res.Output, 500))
				return
			}
			if strings.Contains(res.Model, "stub") && !simulationMode() {
				errs[i] = fmt.Errorf("refusing to close ticket %s with stub runner evidence outside simulation mode", ticket.ID)
				return
			}
			commit, files, err := e.commitTicketWork(ctx, ticketWorkdir, ticket.ID, ticket.Title)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = ticketWorkResult{ticket: ticket, agentID: agentID, branch: ticketBranch, commit: commit, files: files, runner: res}
			succeeded = true
		}()
	}
	wg.Wait()
	var firstErr error
	for i, err := range errs {
		if err != nil {
			e.emit(ctx, r.ID, "ticket.failed", map[string]any{"ticket_id": tickets[i].ID, "wave_id": waveID, "error": err.Error()})
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func (e *Engine) phaseBuild(ctx context.Context, r *state.Run) error {
	workdir := e.runWorkdir(ctx, r)
	hy := e.loadRunHarness(workdir)
	lintArch := strings.TrimSpace(hy.Lint.Arch)
	if lintArch == "" {
		lintArch = ".vessica/lint-arch.sh"
	}
	if !filepath.IsAbs(lintArch) {
		if _, err := os.Stat(filepath.Join(workdir, lintArch)); err == nil {
			lintArch = filepath.Join(workdir, lintArch)
		} else if _, err := os.Stat(filepath.Join(e.Root, lintArch)); err == nil {
			lintArch = filepath.Join(e.Root, lintArch)
		}
	}
	cmds := []struct{ name, cmd string }{
		{"lint", hy.Lint.Command},
		{"lint-arch", "bash " + shellQuote(lintArch)},
		{"test", hy.Test.Command},
		{"build", hy.Build.Command},
	}
	for _, c := range cmds {
		cmd := strings.TrimSpace(harness.ResolveNodeCommand(workdir, c.cmd))
		if cmd == "" || strings.Contains(cmd, "configure ") {
			_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "build", c.name, "", "skipped", map[string]any{"command": cmd})
			e.emit(ctx, r.ID, "build.output", map[string]any{"step": c.name, "status": "skipped"})
			continue
		}
		e.emit(ctx, r.ID, "build.output", map[string]any{"step": c.name, "command": cmd})
		out, err := exec.CommandContext(ctx, "bash", "-lc", "cd "+shellQuote(workdir)+" && "+cmd).CombinedOutput()
		msg := redaction.Redact(string(out))
		e.emit(ctx, r.ID, "build.output", map[string]any{"step": c.name, "output": truncate(msg, 4000)})
		if err != nil {
			if _, fixErr := e.invokeRunner(ctx, r, "build", "Fix build failure: "+c.name+"\n"+msg, "build", workdir); fixErr != nil && !simulationMode() {
				return fixErr
			}
			out2, err2 := exec.CommandContext(ctx, "bash", "-lc", "cd "+shellQuote(workdir)+" && "+cmd).CombinedOutput()
			e.emit(ctx, r.ID, "build.output", map[string]any{"step": c.name + ":retry", "output": truncate(redaction.Redact(string(out2)), 4000)})
			if err2 != nil {
				if simulationMode() {
					_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "build", c.name, "", "skipped", map[string]any{"command": cmd, "output": truncate(redaction.Redact(string(out2)), 4000), "simulation": true})
					e.emit(ctx, r.ID, "warning", map[string]any{"message": c.name + " failed in simulation; continuing"})
					continue
				}
				// Soft-fail lint; hard-fail test/build
				if c.name == "lint" || c.name == "lint-arch" {
					e.emit(ctx, r.ID, "warning", map[string]any{"message": c.name + " failed; continuing"})
					continue
				}
				_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "build", c.name, "", "failed", map[string]any{"command": cmd, "output": truncate(redaction.Redact(string(out2)), 4000), "error": err2.Error()})
				return fmt.Errorf("%s failed: %w", c.name, err2)
			}
			_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "build", c.name, "", "passed", map[string]any{"command": cmd, "output": truncate(redaction.Redact(string(out2)), 4000), "retried": true})
			continue
		}
		_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "build", c.name, "", "passed", map[string]any{"command": cmd, "output": truncate(msg, 4000)})
	}
	return nil
}

func (e *Engine) phaseValidate(ctx context.Context, r *state.Run) error {
	e.emit(ctx, r.ID, "validation.step", map[string]any{"step": "load_test_scenarios"})
	arts, _ := e.DB.ListArtifacts(ctx, r.EpicID, "test-scenarios")
	if len(arts) == 0 {
		return fmt.Errorf("no test-scenarios artifact; cannot validate")
	}
	var steps []string
	for _, a := range arts {
		steps = append(steps, scenarioSteps(a.Body)...)
	}
	if len(steps) == 0 {
		steps = []string{"Preview page loads without server errors"}
	}
	if simulationMode() {
		for i, step := range steps {
			_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "validate", "validation_step", "", "passed", map[string]any{"step_id": fmt.Sprintf("step_%d", i+1), "step": step, "simulation": true})
			e.emit(ctx, r.ID, "validation.step", map[string]any{"step": step, "status": "passed_simulation"})
		}
		return nil
	}
	if r.PreviewURL == "" {
		rr, err := e.EnsurePreview(ctx, r.ID)
		if err != nil {
			return err
		}
		r.PreviewURL = rr.PreviewURL
	}
	if err := ensurePlaywright(ctx, e.runWorkdir(ctx, r)); err != nil {
		return err
	}
	var failures []string
	for i, step := range steps {
		stepID := fmt.Sprintf("step_%d", i+1)
		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			e.emit(ctx, r.ID, "validation.step", map[string]any{"step_id": stepID, "step": step, "attempt": attempt})
			lastErr = runPlaywrightStep(ctx, e.runWorkdir(ctx, r), r.PreviewURL, step)
			if lastErr == nil {
				_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "validate", "validation_step", "", "passed", map[string]any{"step_id": stepID, "step": step, "attempt": attempt})
				e.emit(ctx, r.ID, "validation.step", map[string]any{"step_id": stepID, "step": step, "status": "passed"})
				break
			}
			_, _ = e.invokeRunner(ctx, r, "validate", "Fix validation failure for "+step+"\n"+lastErr.Error(), "validator", e.runWorkdir(ctx, r))
		}
		if lastErr != nil {
			failures = append(failures, stepID)
			_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "validate", "validation_step", "", "failed", map[string]any{"step_id": stepID, "step": step, "error": lastErr.Error()})
			bug, _ := e.DB.CreateTicketWithMeta(ctx, r.EpicID, "bug", "Validation failed: "+step, lastErr.Error(), nil, r.ID, stepID)
			if bug != nil {
				e.emit(ctx, r.ID, "ticket.created", map[string]any{"ticket_id": bug.ID, "type": "bug", "test_step": stepID})
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("validation failed for %d step(s)", len(failures))
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "validate", "validation", "", "passed", map[string]any{"steps": len(steps), "preview_url": r.PreviewURL})
	return nil
}

func (e *Engine) phasePreview(ctx context.Context, r *state.Run) error {
	if r.PreviewURL != "" {
		if sbRec, err := e.DB.GetSandboxForRun(ctx, r.ID); err == nil {
			available := sbRec.Status != "destroyed" && sbRec.ContainerID != "" && previewURLHealthy(ctx, r.PreviewURL)
			if available && retention.Touch(ctx, e.DB, sbRec) == nil {
				e.emit(ctx, r.ID, "preview.ready", map[string]any{"url": r.PreviewURL, "existing": true, "expires_at": retention.EffectiveExpiry(sbRec).Format(time.RFC3339Nano)})
				return nil
			}
			sbRec.PreviewURL = ""
			_ = e.DB.UpdateSandbox(ctx, sbRec)
			if sbRec.Status == "destroyed" || sbRec.ContainerID == "" {
				r.PreviewURL = ""
				_ = e.DB.UpdateRun(ctx, r)
				return fmt.Errorf("preview sandbox is no longer available; rerun the epic with --preview")
			}
		}
		r.PreviewURL = ""
		_ = e.DB.UpdateRun(ctx, r)
	}
	workdir := e.runWorkdir(ctx, r)
	hy := e.loadRunHarness(workdir)
	port := hy.Preview.Port
	if port == 0 {
		port = e.Config.Preview.Port
	}
	if simulationMode() {
		url := fmt.Sprintf("http://127.0.0.1:%d", port)
		r.PreviewURL = url
		_ = e.DB.UpdateRun(ctx, r)
		if sbRec, err := e.DB.GetSandboxForRun(ctx, r.ID); err == nil {
			sbRec.PreviewPort = port
			sbRec.PreviewURL = url
			_ = e.DB.UpdateSandbox(ctx, sbRec)
		}
		_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "preview", "preview", "", "passed", map[string]any{"url": url, "simulation": true})
		e.emit(ctx, r.ID, "preview.ready", map[string]any{"url": url, "simulation": true})
		return nil
	}
	sbRec, err := e.DB.GetSandboxForRun(ctx, r.ID)
	if err != nil {
		return err
	}
	var sb sandbox.Sandbox = sandbox.NewLocalDev(sbRec.ID, workdir)
	if sbRec.Backend == "docker" && sbRec.ContainerID != "" && sbRec.ContainerID != "local" {
		ds := sandbox.NewDocker(sbRec.ID)
		// DockerSandbox needs the container ID for preview commands; store it through a lightweight recreate path.
		// The helper below avoids exposing a public mutator on the interface.
		ds.SetContainerID(sbRec.ContainerID, workdir, port)
		sb = ds
	}
	return e.startPreviewInSandbox(ctx, r, sbRec, sb, workdir, "preview")
}

func previewURLHealthy(ctx context.Context, url string) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func (e *Engine) startPreviewInSandbox(ctx context.Context, r *state.Run, sbRec *state.Sandbox, sb sandbox.Sandbox, workdir, phase string) error {
	hy := e.loadRunHarness(workdir)
	port := hy.Preview.Port
	if port == 0 {
		port = e.Config.Preview.Port
	}
	command := harness.ResolvePreviewCommand(workdir, hy.Preview.Command, port)
	if command == "" || strings.Contains(command, "configure preview") {
		return fmt.Errorf("preview.command is not configured")
	}
	if strings.Contains(command, "pnpm") && !strings.Contains(command, "corepack enable") {
		command = harness.PnpmBootstrapCommand() + " && " + command
	}
	if install := harness.PreviewInstallCommand(workdir); install != "" {
		if _, err := os.Stat(filepath.Join(workdir, "node_modules")); os.IsNotExist(err) {
			e.emit(ctx, r.ID, "preview.dependencies", map[string]any{"phase": phase, "command": install, "status": "started"})
			installCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			var output bytes.Buffer
			_, installErr := sb.Exec(installCtx, []string{"bash", "-lc", install}, &output, &output)
			cancel()
			if installErr != nil {
				return fmt.Errorf("preview dependency install failed: %w: %s", installErr, redaction.Redact(truncate(strings.TrimSpace(output.String()), 2000)))
			}
			e.emit(ctx, r.ID, "preview.dependencies", map[string]any{"phase": phase, "command": install, "status": "completed"})
		}
	}
	e.emit(ctx, r.ID, "preview.starting", map[string]any{"phase": phase, "command": command, "port": port, "healthcheck": hy.Preview.Healthcheck})
	url, err := sb.StartPreview(ctx, command, port, hy.Preview.Healthcheck)
	if err != nil {
		_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "preview", "preview", "", "failed", map[string]any{"phase": phase, "command": command, "port": port, "error": err.Error()})
		return err
	}
	r.PreviewURL = url
	_ = e.DB.UpdateRun(ctx, r)
	sbRec.PreviewPort = port
	sbRec.PreviewURL = url
	_ = e.DB.UpdateSandbox(ctx, sbRec)
	_ = retention.Touch(ctx, e.DB, sbRec)
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "preview", "preview", "", "passed", map[string]any{"phase": phase, "command": command, "port": port, "url": url, "healthcheck": hy.Preview.Healthcheck})
	e.emit(ctx, r.ID, "preview.ready", map[string]any{"phase": phase, "url": url, "healthcheck": hy.Preview.Healthcheck})
	return nil
}

func (e *Engine) loadRunHarness(workdir string) *harness.HarnessYAML {
	resolved := harness.DetectConfig(workdir)
	for _, root := range []string{workdir, e.Root} {
		configured, err := harness.Load(root)
		if err != nil {
			continue
		}
		if configured.Preview.Command != "" {
			resolved.Preview.Command = configured.Preview.Command
		}
		if configured.Preview.Port > 0 {
			resolved.Preview.Port = configured.Preview.Port
		}
		if configured.Preview.Healthcheck != "" {
			resolved.Preview.Healthcheck = configured.Preview.Healthcheck
		}
		if configured.Build.Command != "" {
			resolved.Build.Command = configured.Build.Command
		}
		if configured.Test.Command != "" {
			resolved.Test.Command = configured.Test.Command
		}
		if configured.Lint.Command != "" {
			resolved.Lint.Command = configured.Lint.Command
		}
		if configured.Lint.Arch != "" {
			resolved.Lint.Arch = configured.Lint.Arch
		}
		break
	}
	return &resolved
}

func (e *Engine) EnsurePreview(ctx context.Context, runID string) (*state.Run, error) {
	r, err := e.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if err := e.phasePreview(ctx, r); err != nil {
		return nil, err
	}
	return e.DB.GetRun(ctx, runID)
}

func (e *Engine) phasePR(ctx context.Context, r *state.Run) error {
	remote := e.Config.Repo.Remote
	epic, _ := e.DB.GetEpic(ctx, r.EpicID)
	title := "ves: epic"
	if epic != nil {
		title = fmt.Sprintf("ves: %s (%s)", epic.Title, r.ID)
	}
	body := receipt.PRBody(ctx, e.DB, r)
	branch := fmt.Sprintf("vessica/%s/%s", r.EpicID, r.ID)

	stubPR := func(reason string) error {
		url := fmt.Sprintf("https://github.com/local/draft/pull/%s", r.ID)
		r.PRURL = url
		_ = e.DB.UpdateRun(ctx, r)
		e.emit(ctx, r.ID, "repo.pr.created", map[string]any{"url": url, "draft": true, "stub": true, "reason": reason})
		return nil
	}

	if remote == "" {
		if !simulationMode() {
			return fmt.Errorf("repo.remote is required for PR creation")
		}
		return stubPR("no repo.remote configured")
	}

	workdir := e.runWorkdir(ctx, r)
	if workdir == e.Root && !simulationMode() {
		return fmt.Errorf("refusing to create PR from workspace root; run checkout was not isolated")
	}
	if out, err := repo.GitCommandContext(ctx, "-C", workdir, "checkout", "-B", branch).CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout branch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	// Preview processes must never leak runtime output into the proposed source change.
	_ = os.Remove(filepath.Join(workdir, ".vessica-preview.log"))
	statusOut, err := repo.GitCommandContext(ctx, "-C", workdir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git status: %w: %s", err, strings.TrimSpace(string(statusOut)))
	}
	if strings.TrimSpace(string(statusOut)) != "" || simulationMode() {
		if out, err := repo.GitCommandContext(ctx, "-C", workdir, "diff", "--check").CombinedOutput(); err != nil {
			return fmt.Errorf("git diff check: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if out, err := repo.GitCommandContext(ctx, "-C", workdir, "add", "-A").CombinedOutput(); err != nil {
			return fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(string(out)))
		}
		commitArgs := []string{"-C", workdir, "commit", "-m", title}
		if simulationMode() {
			commitArgs = append(commitArgs, "--allow-empty")
		}
		if out, err := repo.GitCommandContext(ctx, commitArgs...).CombinedOutput(); err != nil {
			return fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	if err := repo.PushBranch(ctx, workdir, remote, branch); err != nil {
		if !simulationMode() {
			return err
		}
		e.emit(ctx, r.ID, "warning", map[string]any{"message": "push failed: " + redaction.Redact(err.Error())})
	}
	base := repo.DefaultBranch(ctx, workdir)
	pr, err := repo.CreateDraftPR(ctx, remote, branch, base, title, body)
	if err != nil {
		if !simulationMode() {
			return err
		}
		return stubPR(redaction.Redact(err.Error()))
	}
	r.PRURL = pr.HTMLURL
	_ = e.DB.UpdateRun(ctx, r)
	e.emit(ctx, r.ID, "repo.pr.created", map[string]any{"url": pr.HTMLURL, "draft": true})
	return nil
}

func (e *Engine) phaseReceipt(ctx context.Context, r *state.Run) error {
	// Mark terminal status before finalizing so the receipt captures it.
	if r.Status == "running" || r.Status == "" {
		r.Status = "completed"
	}
	r.FinishedAt = state.Now()
	_ = e.DB.UpdateRun(ctx, r)
	var finalSandbox *state.Sandbox
	if sbRec, sbErr := e.DB.GetSandboxForRun(ctx, r.ID); sbErr == nil {
		finalSandbox = sbRec
		if r.Preview && r.PreviewURL != "" {
			_ = retention.Touch(ctx, e.DB, sbRec)
		}
	}
	rcpt, err := receipt.Finalize(ctx, e.DB, r)
	if err != nil {
		return err
	}
	r.ReceiptID = rcpt.ID
	_ = e.DB.UpdateRun(ctx, r)
	if r.PRURL != "" && e.Config.Repo.Remote != "" && !simulationMode() {
		if number, parseErr := repo.ParsePRNumber(r.PRURL); parseErr != nil {
			e.emit(ctx, r.ID, "warning", map[string]any{"message": "could not parse PR URL for receipt update: " + parseErr.Error()})
		} else if updateErr := repo.UpdatePRBody(ctx, e.Config.Repo.Remote, number, receipt.PRBody(ctx, e.DB, r)); updateErr != nil {
			e.emit(ctx, r.ID, "warning", map[string]any{"message": "could not update PR with receipt: " + redaction.Redact(updateErr.Error())})
		} else {
			e.emit(ctx, r.ID, "repo.pr.updated", map[string]any{"url": r.PRURL, "receipt_id": rcpt.ID})
		}
	}
	e.emit(ctx, r.ID, "agent.progress", map[string]any{"message": "receipt finalized", "receipt_id": rcpt.ID})
	if finalSandbox != nil && (!r.Preview || r.PreviewURL == "") {
		if err := retention.Destroy(ctx, e.DB, e.Root, finalSandbox, "no_preview"); err != nil {
			e.emit(ctx, r.ID, "warning", map[string]any{"message": "could not clean up non-preview sandbox: " + err.Error()})
		} else {
			e.emit(ctx, r.ID, "sandbox.destroyed", map[string]any{"sandbox_id": finalSandbox.ID, "reason": "no_preview"})
		}
	}
	return nil
}

func (e *Engine) generatePlanningBundle(ctx context.Context, r *state.Run, epic *state.Epic) (planningBundle, error) {
	if simulationMode() {
		return planningBundle{
			PRDMarkdown:           fmt.Sprintf("# %s\n\n## Problem\n\n%s\n\n## Requirements\n\n- Implement the requested behavior.\n- Preserve build, preview, and validation health.\n", epic.Title, epic.Body),
			ADRMarkdown:           fmt.Sprintf("# ADR: %s\n\n## Decision\n\nImplement the request as the smallest coherent repo change, keeping existing architecture unless a change is explicitly required.\n", epic.Title),
			DesignSpecMarkdown:    fmt.Sprintf("# Design Spec: %s\n\n## Implementation Shape\n\nMake the smallest cohesive change across the affected files. Keep tests and validation close to the changed behavior.\n", epic.Title),
			TestScenariosMarkdown: fmt.Sprintf("# Test Scenarios: %s\n\n1. Happy path works\n2. Required validation or error state works\n3. Existing build and preview remain green\n", epic.Title),
			Complexity:            "s",
			Rationale:             "simulation fallback",
		}, nil
	}
	prompt := fmt.Sprintf(`Create a lean planning bundle for this software epic.
Return only JSON matching this shape:
{
  "complexity": "xs|s|m|l|xl",
  "complexity_rationale": "one concise sentence",
  "prd_markdown": "# ...",
  "adr_markdown": "# ...",
  "design_spec_markdown": "# ...",
  "test_scenarios_markdown": "# ..."
}

Planning policy:
- These artifacts are for human inspection and durable documentation, not ceremony.
- Keep PRD under 600 words.
- Keep ADR under 400 words.
- Keep DesignSpec under 600 words.
- Keep TestScenarios to at most 5 numbered scenarios.
- For trivial/localized work, be very brief and concrete.
- Do not include implementation tickets here.
- Each markdown field must start with a level-one heading.

Complexity rubric:
- xs: copy/config/one localized UI or code change, normally one ticket.
- s: small localized feature or bug fix, normally one ticket.
- m: multi-file feature with some risk, normally 2-3 tickets.
- l: cross-module/system feature, normally 3-6 tickets.
- xl: migration/multiple services/high-risk work, may need more.

Epic title: %s
Epic body:
%s`, epic.Title, epic.Body)
	res, err := e.invokeRunner(ctx, r, "plan", prompt, "planner", "")
	if err != nil {
		return planningBundle{}, err
	}
	bundle, err := parsePlanningBundle(res.Output)
	if err != nil {
		return planningBundle{}, err
	}
	return bundle, nil
}

func parsePlanningBundle(raw string) (planningBundle, error) {
	raw = extractJSON(raw)
	var bundle planningBundle
	if err := json.Unmarshal([]byte(raw), &bundle); err != nil {
		return planningBundle{}, fmt.Errorf("parse planning bundle JSON: %w", err)
	}
	bundle.Complexity = normalizeComplexity(bundle.Complexity)
	if bundle.Complexity == "" {
		return planningBundle{}, fmt.Errorf("planning bundle missing complexity")
	}
	checks := map[string]string{
		"prd_markdown":            bundle.PRDMarkdown,
		"adr_markdown":            bundle.ADRMarkdown,
		"design_spec_markdown":    bundle.DesignSpecMarkdown,
		"test_scenarios_markdown": bundle.TestScenariosMarkdown,
	}
	for name, body := range checks {
		if strings.TrimSpace(body) == "" || !strings.HasPrefix(strings.TrimSpace(body), "#") {
			return planningBundle{}, fmt.Errorf("planning bundle field %s must be non-empty markdown starting with #", name)
		}
	}
	return bundle, nil
}

func (e *Engine) generateArtifactBody(ctx context.Context, r *state.Run, phase, role, prompt string, fallback func() string) (string, error) {
	if simulationMode() {
		return fallback(), nil
	}
	res, err := e.invokeRunner(ctx, r, phase, prompt+"\n\nReturn only markdown. Start with a level-one heading.", role, "")
	if err != nil {
		return "", err
	}
	body := extractMarkdown(res.Output)
	if strings.TrimSpace(body) == "" || !strings.HasPrefix(strings.TrimSpace(body), "#") {
		return "", fmt.Errorf("%s runner returned empty or invalid markdown", role)
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, phase, "artifact", "", "passed", map[string]any{"role": role, "model": res.Model})
	return body, nil
}

func extractMarkdown(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "```") {
		parts := strings.Split(s, "```")
		for i := 1; i < len(parts); i += 2 {
			block := strings.TrimSpace(parts[i])
			block = strings.TrimPrefix(block, "markdown")
			block = strings.TrimPrefix(block, "md")
			block = strings.TrimSpace(block)
			if strings.HasPrefix(block, "#") {
				return block
			}
		}
	}
	if idx := strings.Index(s, "# "); idx >= 0 {
		return strings.TrimSpace(s[idx:])
	}
	return s
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "```") {
		parts := strings.Split(s, "```")
		for i := 1; i < len(parts); i += 2 {
			block := strings.TrimSpace(parts[i])
			block = strings.TrimPrefix(block, "json")
			block = strings.TrimSpace(block)
			if strings.HasPrefix(block, "{") || strings.HasPrefix(block, "[") {
				return block
			}
		}
	}
	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")
	start := -1
	end := -1
	if startObj >= 0 && (startArr < 0 || startObj < startArr) {
		start = startObj
		end = strings.LastIndex(s, "}")
	} else if startArr >= 0 {
		start = startArr
		end = strings.LastIndex(s, "]")
	}
	if start >= 0 && end >= start {
		return strings.TrimSpace(s[start : end+1])
	}
	return s
}

func (e *Engine) planTickets(ctx context.Context, r *state.Run, epic *state.Epic, arts []state.Artifact) ([]plannedTicket, error) {
	if simulationMode() {
		plan := ticketPlan{Complexity: "s", Rationale: "simulation fallback", Tickets: defaultTicketPlan()}
		_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "ticketize", "ticket_plan", "", "passed", map[string]any{"tickets": len(plan.Tickets), "complexity": plan.Complexity, "rationale": plan.Rationale, "model": "stub"})
		return plan.Tickets, nil
	}
	var artifactSummary []string
	for _, a := range arts {
		artifactSummary = append(artifactSummary, fmt.Sprintf("%s: %s\n%s", a.Type, a.Title, truncate(a.Body, 1200)))
	}
	prompt := fmt.Sprintf(`Create an efficient dependency-aware ticket plan for this epic.
Return only JSON matching:
{
  "complexity": "xs|s|m|l|xl",
  "complexity_rationale": "one concise sentence",
  "tickets": [
    {
      "type": "feature|test|docs|bug",
      "title": "...",
      "body": "...",
      "acceptance_criteria": ["..."],
      "depends_on_titles": ["..."],
      "estimated_complexity": "xs|s|m|l|xl",
      "split_justification": ""
    }
  ]
}

Ticket policy:
- Bias hard toward larger and fewer tickets because the coding runner is capable.
- Default to exactly one ticket for xs and s work.
- Tests, docs, accessibility, preview checks, and validation are usually acceptance criteria inside the implementation ticket, not separate tickets.
- Split only for true dependency ordering, real parallelism, high-risk migrations, or independently reviewable cross-module work.
- If you split, every split ticket must include a concrete split_justification.
- Ticket count caps by complexity: xs=1, s=1, m=3, l=6, xl=12.
- A simple static-page or localized UI change should be one ticket.

Epic title: %s
Epic body:
%s

Artifacts:
%s`, epic.Title, epic.Body, strings.Join(artifactSummary, "\n\n---\n\n"))
	res, err := e.invokeRunner(ctx, r, "ticketize", prompt, "planner", e.runWorkdir(ctx, r))
	if err != nil {
		return nil, err
	}
	plan, err := parseTicketPlanEnvelope(res.Output)
	if err != nil {
		return nil, err
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "ticketize", "ticket_plan", "", "passed", map[string]any{"tickets": len(plan.Tickets), "complexity": plan.Complexity, "rationale": plan.Rationale, "model": res.Model})
	return plan.Tickets, nil
}

func parseTicketPlan(raw string) ([]plannedTicket, error) {
	plan, err := parseTicketPlanEnvelope(raw)
	if err != nil {
		return nil, err
	}
	return plan.Tickets, nil
}

func parseTicketPlanEnvelope(raw string) (ticketPlan, error) {
	raw = extractJSON(raw)
	var plan ticketPlan
	if strings.HasPrefix(strings.TrimSpace(raw), "[") {
		if err := json.Unmarshal([]byte(raw), &plan.Tickets); err != nil {
			return ticketPlan{}, fmt.Errorf("parse planner ticket JSON: %w", err)
		}
		plan.Complexity = inferComplexity(len(plan.Tickets))
		plan.Rationale = "inferred from legacy array output"
	} else if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return ticketPlan{}, fmt.Errorf("parse planner ticket JSON: %w", err)
	}
	plan.Complexity = normalizeComplexity(plan.Complexity)
	if plan.Complexity == "" {
		return ticketPlan{}, fmt.Errorf("planner ticket plan missing complexity")
	}
	specs := plan.Tickets
	if len(specs) == 0 {
		return ticketPlan{}, fmt.Errorf("planner returned no tickets")
	}
	if max := maxTicketsForComplexity(plan.Complexity); len(specs) > max {
		return ticketPlan{}, fmt.Errorf("planner returned %d tickets for complexity %s; max is %d", len(specs), plan.Complexity, max)
	}
	seen := map[string]bool{}
	for i := range specs {
		specs[i].Type = strings.TrimSpace(specs[i].Type)
		specs[i].Title = strings.TrimSpace(specs[i].Title)
		specs[i].Body = strings.TrimSpace(specs[i].Body)
		specs[i].EstimatedComplexity = normalizeComplexity(specs[i].EstimatedComplexity)
		specs[i].SplitJustification = strings.TrimSpace(specs[i].SplitJustification)
		if specs[i].Type == "" {
			specs[i].Type = "feature"
		}
		if specs[i].Title == "" || specs[i].Body == "" {
			return ticketPlan{}, fmt.Errorf("planner ticket missing title/body")
		}
		if len(specs[i].AcceptanceCriteria) == 0 {
			return ticketPlan{}, fmt.Errorf("planner ticket %q missing acceptance_criteria", specs[i].Title)
		}
		for j := range specs[i].AcceptanceCriteria {
			specs[i].AcceptanceCriteria[j] = strings.TrimSpace(specs[i].AcceptanceCriteria[j])
			if specs[i].AcceptanceCriteria[j] == "" {
				return ticketPlan{}, fmt.Errorf("planner ticket %q has empty acceptance criterion", specs[i].Title)
			}
		}
		if seen[specs[i].Title] {
			return ticketPlan{}, fmt.Errorf("duplicate planner ticket title: %s", specs[i].Title)
		}
		seen[specs[i].Title] = true
	}
	if len(specs) > 1 {
		for _, spec := range specs {
			if !strongSplitJustification(spec.SplitJustification) {
				return ticketPlan{}, fmt.Errorf("planner split ticket %q missing strong split_justification", spec.Title)
			}
		}
	}
	for _, spec := range specs {
		for _, dep := range spec.DependsOnTitles {
			if !seen[dep] {
				return ticketPlan{}, fmt.Errorf("planner ticket %q depends on unknown title %q", spec.Title, dep)
			}
		}
	}
	plan.Tickets = specs
	return plan, nil
}

func normalizeComplexity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "xs", "s", "m", "l", "xl":
		return s
	default:
		return ""
	}
}

func inferComplexity(n int) string {
	switch {
	case n <= 1:
		return "s"
	case n <= 3:
		return "m"
	case n <= 6:
		return "l"
	default:
		return "xl"
	}
}

func maxTicketsForComplexity(complexity string) int {
	switch complexity {
	case "xs", "s":
		return 1
	case "m":
		return 3
	case "l":
		return 6
	case "xl":
		return 12
	default:
		return 1
	}
}

func strongSplitJustification(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 40 {
		return false
	}
	for _, marker := range []string{"dependency", "parallel", "migration", "cross-module", "risk", "independent", "separate", "sequence", "integration"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func (e *Engine) createPlannedTickets(ctx context.Context, epicID, runID string, specs []plannedTicket) ([]*state.Ticket, error) {
	byTitle := map[string]*state.Ticket{}
	var out []*state.Ticket
	existing, _ := e.DB.ListTicketsForRun(ctx, epicID, runID)
	for i := range existing {
		byTitle[existing[i].Title] = &existing[i]
	}
	for _, spec := range specs {
		if existingTicket := byTitle[spec.Title]; existingTicket != nil {
			out = append(out, existingTicket)
			continue
		}
		body := spec.Body
		if len(spec.AcceptanceCriteria) > 0 {
			body += "\n\nAcceptance criteria:\n"
			for _, criterion := range spec.AcceptanceCriteria {
				body += "- " + criterion + "\n"
			}
		}
		if spec.EstimatedComplexity != "" {
			body += "\nEstimated complexity: " + spec.EstimatedComplexity + "\n"
		}
		if spec.SplitJustification != "" {
			body += "\nSplit justification: " + spec.SplitJustification + "\n"
		}
		t, err := e.DB.CreateTicketForRun(ctx, epicID, runID, spec.Type, spec.Title, body, nil)
		if err != nil {
			return nil, err
		}
		byTitle[spec.Title] = t
		out = append(out, t)
	}
	for _, spec := range specs {
		t := byTitle[spec.Title]
		for _, title := range spec.DependsOnTitles {
			dep := byTitle[title]
			if dep == nil {
				return nil, fmt.Errorf("missing dependency %q for %q", title, spec.Title)
			}
			if err := e.DB.AddDependency(ctx, t.ID, dep.ID); err != nil {
				return nil, err
			}
			t.DependsOn = append(t.DependsOn, dep.ID)
		}
	}
	return out, nil
}

func (e *Engine) prepareTicketWorktree(ctx context.Context, integrationWorkdir string, r *state.Run, ticket *state.Ticket) (string, string, error) {
	branch := fmt.Sprintf("vessica/%s/%s-%s", r.EpicID, r.ID, ticket.ID)
	base := filepath.Join(filepath.Dir(integrationWorkdir), "tickets")
	workdir := filepath.Join(base, ticket.ID)
	_ = repo.GitCommandContext(ctx, "-C", integrationWorkdir, "worktree", "remove", "--force", workdir).Run()
	_ = os.RemoveAll(workdir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", "", err
	}
	out, err := repo.GitCommandContext(ctx, "-C", integrationWorkdir, "worktree", "add", "-B", branch, workdir, "HEAD").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git worktree add for ticket %s: %w: %s", ticket.ID, err, strings.TrimSpace(string(out)))
	}
	return workdir, branch, nil
}

func (e *Engine) commitTicketWork(ctx context.Context, workdir, ticketID, title string) (string, []string, error) {
	out, err := repo.GitCommandContext(ctx, "-C", workdir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("git status: %w: %s", err, strings.TrimSpace(string(out)))
	}
	files := statusFiles(string(out))
	if len(files) == 0 && !simulationMode() {
		return "", nil, fmt.Errorf("ticket %s produced no file changes", ticketID)
	}
	if _, err := repo.GitCommandContext(ctx, "-C", workdir, "add", "-A").CombinedOutput(); err != nil {
		return "", nil, err
	}
	args := []string{"-C", workdir, "commit", "-m", fmt.Sprintf("ves: %s %s", ticketID, title)}
	if simulationMode() && len(files) == 0 {
		args = append(args, "--allow-empty")
	}
	if out, err := repo.GitCommandContext(ctx, args...).CombinedOutput(); err != nil {
		return "", nil, fmt.Errorf("git commit ticket %s: %w: %s", ticketID, err, strings.TrimSpace(string(out)))
	}
	sha, err := repo.GitCommandContext(ctx, "-C", workdir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", files, err
	}
	return strings.TrimSpace(string(sha)), files, nil
}

func (e *Engine) mergeTicketBranch(ctx context.Context, integrationWorkdir, branch, runID, ticketID string) error {
	e.emit(ctx, runID, "merge.started", map[string]any{"ticket_id": ticketID, "branch": branch})
	out, err := repo.GitCommandContext(ctx, "-C", integrationWorkdir, "merge", "--no-ff", "--no-edit", branch).CombinedOutput()
	if err != nil {
		_, _ = repo.GitCommandContext(ctx, "-C", integrationWorkdir, "merge", "--abort").CombinedOutput()
		_, _ = e.DB.CreateRunEvidence(ctx, runID, "code", "merge", ticketID, "failed", map[string]any{"branch": branch, "output": redaction.Redact(string(out)), "error": err.Error()})
		e.emit(ctx, runID, "merge.failed", map[string]any{"ticket_id": ticketID, "branch": branch, "message": redaction.Redact(string(out))})
		return fmt.Errorf("merge ticket %s: %w", ticketID, err)
	}
	_, _ = e.DB.CreateRunEvidence(ctx, runID, "code", "merge", ticketID, "passed", map[string]any{"branch": branch, "output": truncate(redaction.Redact(string(out)), 2000)})
	e.emit(ctx, runID, "merge.completed", map[string]any{"ticket_id": ticketID, "branch": branch})
	return nil
}

func statusFiles(status string) []string {
	var files []string
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 3 {
			files = append(files, strings.TrimSpace(line[3:]))
		}
	}
	return files
}

func scenarioSteps(markdown string) []string {
	var steps []string
	for _, line := range strings.Split(markdown, "\n") {
		s := strings.TrimSpace(line)
		s = strings.TrimPrefix(s, "- [ ]")
		s = strings.TrimPrefix(s, "- [x]")
		s = strings.TrimPrefix(s, "-")
		if len(s) > 2 && s[1] == '.' && s[0] >= '0' && s[0] <= '9' {
			s = strings.TrimSpace(s[2:])
		}
		if len(s) > 3 && s[2] == '.' && s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9' {
			s = strings.TrimSpace(s[3:])
		}
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		lower := strings.ToLower(s)
		if strings.Contains(lower, "scenario") || strings.Contains(lower, "path") || strings.Contains(lower, "works") || strings.Contains(lower, "loads") || strings.Contains(lower, "handled") || strings.Contains(lower, "green") || strings.Contains(lower, "regression") {
			steps = append(steps, s)
		}
	}
	return steps
}

func ensurePlaywright(ctx context.Context, workdir string) error {
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node is required for Playwright validation")
	}
	cmd := exec.CommandContext(ctx, "node", "-e", "require('playwright');")
	cmd.Dir = workdir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("playwright package is required for validation: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runPlaywrightStep(ctx context.Context, workdir, url, step string) error {
	script := `
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', e => errors.push(String(e)));
  page.on('response', r => { if (r.status() >= 500) errors.push(r.url() + ' ' + r.status()); });
  await page.goto(process.argv[2], { waitUntil: 'domcontentloaded', timeout: 15000 });
  await page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => {});
  const title = await page.title().catch(() => '');
  const body = await page.locator('body').innerText({ timeout: 5000 }).catch(() => '');
  await browser.close();
  if (errors.length) throw new Error(errors.join('\n'));
  if (!title && !body.trim()) throw new Error('page rendered no visible content');
})().catch(err => { console.error(err.message || err); process.exit(1); });
`
	tmp, err := os.CreateTemp("", "ves-playwright-*.js")
	if err != nil {
		return err
	}
	path := tmp.Name()
	if _, err := tmp.WriteString(script); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return err
	}
	_ = tmp.Close()
	defer os.Remove(path)
	cmd := exec.CommandContext(ctx, "node", path, url, step)
	cmd.Dir = workdir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", step, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *Engine) invokeRunner(ctx context.Context, r *state.Run, phase, prompt, agentRole, workdir string) (runner.Result, error) {
	rn, err := runner.New(r.Runner)
	if err != nil {
		return runner.Result{}, err
	}
	if workdir == "" {
		workdir = e.runWorkdir(ctx, r)
	}
	in := runner.Input{
		RepoPath:        e.Root,
		Workdir:         workdir,
		Phase:           phase,
		AllowStub:       simulationMode(),
		Model:           r.Model,
		ReasoningEffort: r.ReasoningEffort,
	}
	if phase == "code" && agentRole == "coder" {
		in.Env = map[string]string{
			"VES_ENGINE_MANAGED_RUN": "1",
			"VES_RUN_ID":             r.ID,
		}
	}
	systemPrompt := e.agentSystemPrompt(agentRole, workdir, phase)
	promptRaw, _ := json.Marshal(map[string]any{
		"type":          "vessica.prompt",
		"role":          agentRole,
		"phase":         phase,
		"system_prompt": systemPrompt,
		"prompt":        prompt,
	})
	e.emitRunner(ctx, r, phase, agentRole, runner.Event{
		Type:    "agent.prompt",
		Message: "Prompt prepared (collapsed)",
		Data:    map[string]any{"kind": "prompt", "status": "completed"},
		Raw:     string(promptRaw),
	})
	runCtx, cancel := context.WithTimeout(ctx, runnerTimeout())
	defer cancel()
	if err := rn.Prepare(runCtx, in); err != nil {
		return runner.Result{}, err
	}
	if err := rn.Start(runCtx, runner.Task{Name: agentRole, Prompt: prompt, SystemPrompt: systemPrompt}); err != nil {
		return runner.Result{}, err
	}
	ch, err := rn.StreamEvents(runCtx)
	if err != nil {
		return runner.Result{}, err
	}
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				res, err := rn.CollectResult(context.Background())
				if err != nil {
					return res, err
				}
				if res.Status != "ok" {
					return res, runnerResultError(phase, res)
				}
				if !simulationMode() && res.Status == "ok" {
					if strings.TrimSpace(res.Evidence) == "" {
						return res, fmt.Errorf("runner result missing evidence for %s", phase)
					}
					if phase == "code" && len(res.FilesChanged) == 0 {
						// Some runners report file changes after process exit poorly; the ticket commit path remains authoritative.
						e.emit(ctx, r.ID, "warning", map[string]any{"message": "runner did not report changed files; git diff will be used", "role": agentRole})
					}
				}
				return res, nil
			}
			e.emitRunner(ctx, r, phase, agentRole, ev)
		case <-runCtx.Done():
			_ = rn.Cancel(context.Background())
			e.emit(ctx, r.ID, "warning", map[string]any{"message": "runner timeout; continuing", "role": agentRole})
			if simulationMode() {
				return runner.Result{Status: "ok", Output: "runner timeout", Model: "timeout-stub"}, nil
			}
			return runner.Result{}, fmt.Errorf("runner timeout in %s", phase)
		}
	}
}

func runnerResultError(phase string, result runner.Result) error {
	message := "runner returned status " + result.Status
	for _, line := range strings.Split(strings.TrimSpace(result.Output), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			message = value
		}
	}
	message = redaction.Redact(message)
	if len(message) > 1200 {
		message = message[:1200] + "..."
	}
	return fmt.Errorf("runner failed in %s: %s", phase, message)
}

func (e *Engine) agentSystemPrompt(role, workdir, phase string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return ""
	}
	candidates := []string{}
	if workdir != "" {
		candidates = append(candidates, filepath.Join(workdir, ".vessica", "agents", role, "AGENTS.md"))
	}
	if e.Root != "" {
		candidates = append(candidates, filepath.Join(e.Root, ".vessica", "agents", role, "AGENTS.md"))
	}
	for _, path := range candidates {
		if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
			return e.withEngineManagedOverlay(role, phase, strings.TrimSpace(string(b)))
		}
	}
	if prompt, err := pack.AgentPrompt(role); err == nil && strings.TrimSpace(prompt) != "" {
		return e.withEngineManagedOverlay(role, phase, strings.TrimSpace(prompt))
	}
	return e.withEngineManagedOverlay(role, phase, defaultAgentPrompt(role))
}

func (e *Engine) withEngineManagedOverlay(role, phase, prompt string) string {
	if role != "coder" {
		return prompt
	}
	packageManager := "For Node projects, use pnpm exclusively; do not run npm or npx."
	if phase != "code" {
		return strings.TrimSpace(prompt) + "\n\n" + packageManager
	}
	overlay := `## Engine-Managed Vessica Run

You are running inside ` + "`ves run epic`" + `. Vessica already claimed the ticket and will commit, merge, close the ticket, create evidence receipts, and update run state after you return.

Do not run Vessica lifecycle commands from inside this task: ` + "`ves ticket claim`" + `, ` + "`ves ticket close`" + `, ` + "`ves ticket heartbeat`" + `, ` + "`ves ticket release`" + `, or ` + "`ves memory add`" + `.

Do not spend time discovering the engine's internal generated agent id. Implement the requested change, run the relevant local checks, then stop and return a concise evidence summary with changed files and commands run.

` + packageManager
	if strings.TrimSpace(prompt) == "" {
		return overlay
	}
	return strings.TrimSpace(prompt) + "\n\n" + overlay
}

func defaultAgentPrompt(role string) string {
	switch role {
	case "planner":
		return "You are the Vessica planner. Produce lightweight planning artifacts and the fewest dependency-aware tickets a capable coding agent can safely build."
	case "product":
		return "You are the Vessica product agent. Write concise, testable PRDs focused on decisions and acceptance criteria."
	case "architect":
		return "You are the Vessica architect agent. Write concise ADRs capturing only consequential technical decisions."
	case "design":
		return "You are the Vessica design agent. Write lightweight DesignSpecs focused on implementation shape, interfaces, and risks."
	case "qa":
		return "You are the Vessica QA agent. Write a small set of high-signal test scenarios."
	case "coder":
		return "You are the Vessica coder. Implement the ticket end-to-end, including relevant tests, docs, and validation in one coherent change. Use pnpm exclusively for Node projects."
	case "build":
		return "You are the Vessica build agent. Fix build, lint, and test failures with the smallest correct change. Use pnpm exclusively for Node projects."
	case "validator":
		return "You are the Vessica validator. Fix validation failures and preserve user-facing behavior. Use pnpm exclusively for Node projects."
	default:
		return ""
	}
}

func (e *Engine) openSandbox(ctx context.Context, rec *state.Sandbox) (sandbox.Sandbox, error) {
	token, _ := auth.Token("github")
	hostWorkdir, err := e.prepareRunWorkdir(ctx, rec)
	if err != nil {
		return nil, err
	}
	opts := sandbox.CreateOpts{
		SandboxID:   rec.ID,
		WorkspaceID: rec.WorkspaceID,
		RunID:       rec.RunID,
		Branch:      rec.Branch,
		RemoteURL:   e.Config.Repo.Remote,
		Token:       token,
		Image:       e.sandboxImage(hostWorkdir),
		HostWorkdir: hostWorkdir,
		PreviewPort: e.previewPort(hostWorkdir),
		ExpiresAt:   retention.EffectiveExpiry(rec),
	}
	useDocker := !e.Local && rec.Backend == "docker" && exec.Command("docker", "info").Run() == nil
	var sb sandbox.Sandbox
	if useDocker {
		pullCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := sandbox.EnsureImage(pullCtx, opts.Image)
		cancel()
		if err != nil {
			useDocker = false
		} else {
			sb = sandbox.NewDocker(rec.ID)
		}
	}
	if !useDocker {
		e.Local = true
		sb = sandbox.NewLocalDev(rec.ID, hostWorkdir)
	}
	if err := sb.Create(ctx, opts); err != nil {
		return nil, err
	}
	if err := sb.Start(ctx); err != nil {
		return nil, err
	}
	if rec.Backend != "railway" || rec.ContainerID == "" {
		rec.ContainerID = sb.ContainerID()
	}
	rec.Status = "running"
	meta, _ := json.Marshal(map[string]any{"host_workdir": hostWorkdir, "branch": rec.Branch})
	rec.MetaJSON = string(meta)
	_ = e.DB.UpdateSandbox(ctx, rec)
	return sb, nil
}

func (e *Engine) prepareRunWorkdir(ctx context.Context, rec *state.Sandbox) (string, error) {
	base := filepath.Join(e.Root, ".vessica", "sandboxes", rec.ID)
	workdir := filepath.Join(base, "workspace")
	if st, err := os.Stat(filepath.Join(workdir, ".git")); err == nil && st.IsDir() {
		return workdir, nil
	}
	_ = os.RemoveAll(workdir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	if e.Config.Repo.Remote != "" {
		remote := repo.AuthenticatedRemote(e.Config.Repo.Remote)
		out, err := repo.GitCommandContext(ctx, "clone", remote, workdir).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git clone into sandbox: %w: %s", err, redaction.Redact(strings.TrimSpace(string(out))))
		}
		if out, err := repo.GitCommandContext(ctx, "-C", workdir, "remote", "set-url", "origin", e.Config.Repo.Remote).CombinedOutput(); err != nil {
			return "", fmt.Errorf("reset sandbox origin: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if rec.Branch != "" {
			if out, err := repo.GitCommandContext(ctx, "-C", workdir, "checkout", "-B", rec.Branch).CombinedOutput(); err != nil {
				return "", fmt.Errorf("git checkout sandbox branch: %w: %s", err, strings.TrimSpace(string(out)))
			}
		}
		return workdir, nil
	}
	if !simulationMode() {
		return "", fmt.Errorf("repo.remote is required for sandbox clone; set VES_RUNNER_MODE=stub for local simulation")
	}
	out, err := repo.GitCommandContext(ctx, "-C", e.Root, "worktree", "add", "-B", rec.Branch, workdir, "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git worktree add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return workdir, nil
}

func (e *Engine) runWorkdir(ctx context.Context, r *state.Run) string {
	sb, err := e.DB.GetSandboxForRun(ctx, r.ID)
	if err != nil {
		return e.Root
	}
	var meta struct {
		HostWorkdir string `json:"host_workdir"`
	}
	_ = json.Unmarshal([]byte(sb.MetaJSON), &meta)
	if meta.HostWorkdir != "" {
		return meta.HostWorkdir
	}
	return e.Root
}

func (e *Engine) previewPort(workdir string) int {
	if hy, err := harness.Load(workdir); err == nil && hy.Preview.Port > 0 {
		return hy.Preview.Port
	}
	if e.Config.Preview.Port > 0 {
		return e.Config.Preview.Port
	}
	return 3000
}

func (e *Engine) sandboxImage(workdir string) string {
	switch harness.Detect(workdir).Stack {
	case "node":
		return "node:24-bookworm"
	case "go":
		return sandbox.FallbackImage()
	case "python":
		return "python:3.13-bookworm"
	default:
		return sandbox.FallbackImage()
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func runnerTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("VES_RUNNER_TIMEOUT"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("VES_CODEX_TIMEOUT"))
	}
	if raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			return d
		}
	}
	return 20 * time.Minute
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// OpenPreview opens preview URL in browser when possible.
func OpenPreview(url string) error {
	for _, c := range [][]string{{"open", url}, {"xdg-open", url}} {
		cmd := exec.Command(c[0], c[1:]...)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("could not open browser for %s", url)
}

// EnsureBranchDir is a helper for tests.
func EnsureBranchDir(root, branch string) (string, error) {
	p := filepath.Join(root, ".vessica", "runs", strings.ReplaceAll(branch, "/", "_"))
	return p, os.MkdirAll(p, 0o755)
}
