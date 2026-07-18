package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/runner"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
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
	eventMu    sync.Mutex
	eventErr   error
}

type runnerContextKey string

const runnerTicketIDKey runnerContextKey = "ticket_id"

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
		e.eventMu.Lock()
		if e.eventErr == nil {
			e.eventErr = fmt.Errorf("persist %s event: %w", typ, err)
		}
		e.eventMu.Unlock()
		if e.Stream {
			fmt.Fprintf(os.Stderr, "event persistence failed: %v\n", err)
		}
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

func (e *Engine) takeEventError() error {
	e.eventMu.Lock()
	defer e.eventMu.Unlock()
	err := e.eventErr
	e.eventErr = nil
	return err
}

func (e *Engine) emitRunner(ctx context.Context, r *state.Run, phase, role string, runnerEvent runner.Event) {
	payload := map[string]any{}
	for key, value := range runnerEvent.Data {
		payload[key] = value
	}
	payload["message"] = redaction.Redact(runnerEvent.Message)
	payload["role"] = role
	payload["phase"] = phase
	if ticketID, _ := ctx.Value(runnerTicketIDKey).(string); ticketID != "" {
		payload["ticket_id"] = ticketID
	}
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
	explicitStart := strings.TrimSpace(fromPhase) != ""
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
	if phases, phaseErr := e.DB.ListPhases(ctx, runID); phaseErr == nil {
		opts.StartAt = resumeStartPhase(opts.StartAt, explicitStart, phases)
	}
	r.Status = "running"
	r.Error = ""
	r.StopAfter = ""
	r.FinishedAt = ""
	if err := e.DB.UpdateRun(ctx, r); err != nil {
		return r, fmt.Errorf("persist resumed run: %w", err)
	}
	return e.execute(ctx, r, opts)
}

func (e *Engine) execute(ctx context.Context, r *state.Run, opts Options) (*state.Run, error) {
	e.Stream = opts.Stream
	e.EventsOnly = opts.EventsOnly
	e.StreamMode = opts.StreamMode
	r.Status = "running"
	r.StartedAt = state.Now()
	if err := e.DB.UpdateRun(ctx, r); err != nil {
		return r, fmt.Errorf("persist run start: %w", err)
	}
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
	existing, err := e.DB.ListPhases(ctx, r.ID)
	if err != nil {
		return r, fmt.Errorf("load phase state: %w", err)
	}
	for _, p := range existing {
		phaseStatus[p.Phase] = p.Status
	}
	for i := 0; i < startIdx; i++ {
		if phaseStatus[phases[i]] == "completed" {
			continue
		}
		if err := e.DB.SetPhaseStatus(ctx, r.ID, phases[i], "skipped", ""); err != nil {
			return r, fmt.Errorf("mark phase %s skipped: %w", phases[i], err)
		}
	}

	var runErr error
	for i := startIdx; i <= stopIdx; i++ {
		phase := phases[i]
		r.CurrentPhase = phase
		if err := e.DB.UpdateRun(ctx, r); err != nil {
			return r, fmt.Errorf("persist current phase %s: %w", phase, err)
		}
		if err := e.DB.SetPhaseStatus(ctx, r.ID, phase, "running", ""); err != nil {
			return r, fmt.Errorf("mark phase %s running: %w", phase, err)
		}
		e.emit(ctx, r.ID, "run.phase.started", map[string]any{"phase": phase})

		if err := e.runPhase(ctx, r, opts, phase); err != nil {
			phaseErr := err
			if cancelledRun, cancelErr := e.cancelledRun(ctx, r.ID, phaseErr); cancelledRun != nil {
				return cancelledRun, cancelErr
			}
			if persistErr := e.DB.SetPhaseStatus(ctx, r.ID, phase, "failed", phaseErr.Error()); persistErr != nil {
				phaseErr = errors.Join(phaseErr, fmt.Errorf("mark phase failed: %w", persistErr))
			}
			e.emit(ctx, r.ID, "error", map[string]any{"phase": phase, "message": redaction.Redact(phaseErr.Error())})
			r.Status = "failed"
			r.Error = phaseErr.Error()
			r.FinishedAt = state.Now()
			if persistErr := e.DB.UpdateRun(ctx, r); persistErr != nil {
				phaseErr = errors.Join(phaseErr, fmt.Errorf("persist failed run: %w", persistErr))
			}
			if sbRec, sbErr := e.DB.GetSandboxForRun(ctx, r.ID); sbErr == nil {
				if persistErr := retention.MarkFailed(ctx, e.DB, sbRec); persistErr != nil {
					phaseErr = errors.Join(phaseErr, fmt.Errorf("mark sandbox failed: %w", persistErr))
				}
			} else if !strings.Contains(sbErr.Error(), "not found") {
				phaseErr = errors.Join(phaseErr, fmt.Errorf("load failed run sandbox: %w", sbErr))
			}
			if eventErr := e.takeEventError(); eventErr != nil {
				phaseErr = errors.Join(phaseErr, eventErr)
			}
			runErr = phaseErr
			e.recordWorkflowKnowledge(ctx, r, "run.failed", "Run failed during "+phase+": "+redaction.Redact(phaseErr.Error()), "run:"+r.ID+":failed")
			break
		}
		if err := e.takeEventError(); err != nil {
			r.Status = "failed"
			r.Error = err.Error()
			if persistErr := e.DB.SetPhaseStatus(ctx, r.ID, phase, "failed", err.Error()); persistErr != nil {
				err = errors.Join(err, persistErr)
			}
			if persistErr := e.DB.UpdateRun(ctx, r); persistErr != nil {
				err = errors.Join(err, persistErr)
			}
			return r, err
		}
		if err := e.DB.SetPhaseStatus(ctx, r.ID, phase, "completed", ""); err != nil {
			return r, fmt.Errorf("mark phase %s completed: %w", phase, err)
		}
		e.emit(ctx, r.ID, "run.phase.completed", map[string]any{"phase": phase})
		if err := e.takeEventError(); err != nil {
			return r, err
		}
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
		if err := e.DB.UpdateRun(ctx, r); err != nil {
			return r, fmt.Errorf("persist terminal run: %w", err)
		}
		e.emit(ctx, r.ID, "run.completed", map[string]any{"status": r.Status})
		if err := e.takeEventError(); err != nil {
			r.Status = "failed"
			r.Error = err.Error()
			if persistErr := e.DB.UpdateRun(ctx, r); persistErr != nil {
				err = errors.Join(err, persistErr)
			}
			return r, err
		}
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
