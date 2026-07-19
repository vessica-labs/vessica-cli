package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/pack"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/runner"
	"github.com/vessica-labs/vessica-cli/internal/state"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

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
	if bundle.Complexity == "xs" && bundle.Ticket == nil {
		ticket := deterministicXSTicket(epic, []*state.Artifact{prd, adr, ts, design})
		bundle.Ticket = &ticket
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "plan", "planning_bundle", "", "passed", map[string]any{
		"complexity": bundle.Complexity,
		"rationale":  bundle.Rationale,
		"ticket":     bundle.Ticket,
		"model":      firstNonEmptyString(bundle.Mode, "model"),
	})
	e.emit(ctx, r.ID, "agent.progress", map[string]any{"message": "plan artifacts created", "artifacts": []string{prd.ID, adr.ID, ts.ID, design.ID}, "complexity": bundle.Complexity, "planning_mode": firstNonEmptyString(bundle.Mode, "model")})
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
	var epic *state.Epic
	if err := e.retryTicketizeState(ctx, r, "load_epic", func() error {
		var err error
		epic, err = e.DB.GetEpic(ctx, r.EpicID)
		return err
	}); err != nil {
		return err
	}
	var arts []state.Artifact
	if err := e.retryTicketizeState(ctx, r, "load_artifacts", func() error {
		var err error
		arts, err = e.DB.ListArtifacts(ctx, epic.ID, "")
		return err
	}); err != nil {
		return err
	}
	arts = artifactsForRun(arts, r.ID)
	var ids []string
	for _, a := range arts {
		ids = append(ids, a.ID)
	}
	var set *state.ArtifactSet
	if err := e.retryTicketizeState(ctx, r, "create_artifact_set", func() error {
		var err error
		set, err = e.DB.CreateArtifactSet(ctx, epic.ID, r.ID, ids)
		return err
	}); err != nil {
		return err
	}
	if err := e.retryTicketizeState(ctx, r, "approve_artifact_set", func() error {
		return e.DB.ApproveArtifactSet(ctx, set.ID)
	}); err != nil {
		return err
	}
	r.ArtifactSetID = set.ID
	if err := e.retryTicketizeState(ctx, r, "attach_artifact_set", func() error {
		return e.DB.UpdateRun(ctx, r)
	}); err != nil {
		return err
	}

	var specs []plannedTicket
	var fastPath bool
	if err := e.retryTicketizeState(ctx, r, "load_xs_ticket_plan", func() error {
		var err error
		specs, fastPath, err = e.xsTicketPlan(ctx, r, epic, arts)
		return err
	}); err != nil {
		return err
	}
	if !fastPath {
		var err error
		specs, err = e.planTickets(ctx, r, epic, arts)
		if err != nil {
			return err
		}
	}
	var created []*state.Ticket
	if err := e.retryTicketizeState(ctx, r, "persist_tickets", func() error {
		var err error
		created, err = e.createPlannedTickets(ctx, epic.ID, r.ID, specs)
		return err
	}); err != nil {
		return err
	}
	var waves []state.Wave
	if err := e.retryTicketizeState(ctx, r, "persist_waves", func() error {
		var err error
		waves, err = e.DB.ComputeWavesForRun(ctx, epic.ID, r.ID)
		return err
	}); err != nil {
		return err
	}
	if err := e.retryTicketizeState(ctx, r, "mark_epic_planned", func() error {
		_, err := e.DB.UpdateEpic(ctx, epic.ID, "", "", state.EpicStatusPlanned)
		return err
	}); err != nil {
		return err
	}
	var ticketIDs []string
	for _, t := range created {
		ticketIDs = append(ticketIDs, t.ID)
	}
	e.emit(ctx, r.ID, "agent.progress", map[string]any{
		"message":      "ticketized",
		"artifact_set": set.ID,
		"tickets":      ticketIDs,
		"waves":        len(waves),
		"fast_path":    fastPath,
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
					cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					defer cleanupCancel()
					if releaseErr := e.DB.ReleaseClaim(cleanupCtx, ticket.ID, agentID, "run ticket failed"); releaseErr != nil && errs[i] == nil {
						errs[i] = fmt.Errorf("release failed ticket claim: %w", releaseErr)
					}
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
- Make the code changes in this worktree and run only targeted checks needed while coding.
- Do not run repository-wide build, lint, or test commands and do not start a preview server; Vessica runs those gates once after integration.
- Return a concise evidence summary.

Use TDD where helpful. Return changed files and commands run.`, ticket.ID, ticket.Title, ticket.Body)
			ticketCtx := context.WithValue(ctx, runnerTicketIDKey, ticket.ID)
			res, err := e.invokeRunner(ticketCtx, r, "code", prompt, "coder", ticketWorkdir)
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
