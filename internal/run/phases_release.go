package run

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/isolation"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/sandbox"
	"github.com/vessica-labs/vessica-cli/internal/state"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func (e *Engine) phaseBuild(ctx context.Context, r *state.Run) error {
	workdir := e.runWorkdir(ctx, r)
	if err := isolation.PrepareWorkdir(ctx, workdir); err != nil {
		return err
	}
	if err := e.materializeWorktreeDependencies(ctx, r, workdir); err != nil {
		return err
	}
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
		out, err := isolation.CommandContext(ctx, workdir, "bash", "-lc", cmd).CombinedOutput()
		msg := redaction.Redact(string(out))
		e.emit(ctx, r.ID, "build.output", map[string]any{"step": c.name, "output": truncate(msg, 4000)})
		if err != nil {
			if _, fixErr := e.invokeRunner(ctx, r, "build", "Fix build failure: "+c.name+"\n"+msg, "build", workdir); fixErr != nil && !simulationMode() {
				return fixErr
			}
			out2, err2 := isolation.CommandContext(ctx, workdir, "bash", "-lc", cmd).CombinedOutput()
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
	if r.SandboxBackend == "railway" {
		// The loopback address is validation-only. Never allow it to flow into
		// hosted PR, receipt, Linear, or dashboard projections.
		defer func() { r.PreviewURL = "" }()
	}
	if err := isolation.PrepareWorkdir(ctx, e.runWorkdir(ctx, r)); err != nil {
		return err
	}
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
		transientFailures := 0
		for attempt := 1; attempt <= 3; attempt++ {
			if err := waitForPreviewHealth(ctx, r.PreviewURL, 30*time.Second); err != nil {
				return fmt.Errorf("validation preview unavailable before %s: %w", stepID, err)
			}
			e.emit(ctx, r.ID, "validation.step", map[string]any{"step_id": stepID, "step": step, "attempt": attempt})
			lastErr = runPlaywrightStep(ctx, e.runWorkdir(ctx, r), r.PreviewURL, step)
			if lastErr == nil {
				_, _ = e.DB.ResolveValidationFailure(ctx, r.ID, stepID)
				_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "validate", "validation_step", "", "passed", map[string]any{"step_id": stepID, "step": step, "attempt": attempt})
				e.emit(ctx, r.ID, "validation.step", map[string]any{"step_id": stepID, "step": step, "status": "passed"})
				break
			}
			if previewConnectionFailure(lastErr) {
				transientFailures++
				if err := waitForPreviewHealth(ctx, r.PreviewURL, 30*time.Second); err != nil {
					return fmt.Errorf("validation preview became unavailable during %s: %w", stepID, err)
				}
				if transientFailures >= 3 {
					return fmt.Errorf("validation preview transport failed repeatedly during %s", stepID)
				}
				attempt-- // transport startup races do not consume a product-validation attempt
				continue
			}
			_, _ = e.invokeRunner(ctx, r, "validate", "Fix validation failure for "+step+"\n"+lastErr.Error(), "validator", e.runWorkdir(ctx, r))
		}
		if lastErr != nil {
			failures = append(failures, stepID)
			_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "validate", "validation_step", "", "failed", map[string]any{"step_id": stepID, "step": step, "error": lastErr.Error()})
			bug, _ := e.DB.CreateTicketWithMeta(ctx, r.EpicID, "bug", "Validation failed: "+step, lastErr.Error(), nil, r.ID, stepID)
			if bug != nil {
				e.emit(ctx, r.ID, "ticket.created", map[string]any{"ticket_id": bug.ID, "type": "bug", "test_step": stepID})
				e.recordWorkflowKnowledge(ctx, r, "ticket.discovered", "Validation discovered follow-up: "+bug.Title, "ticket:"+bug.ID+":discovered", knowledge.ExternalRef{System: "vessica.ticket", ID: bug.ID})
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

func waitForPreviewHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if previewURLHealthy(ctx, url) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("preview did not become healthy within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
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
	sbRec.PreviewPort = port
	if sbRec.Backend == "railway" {
		var metadata map[string]any
		_ = json.Unmarshal([]byte(sbRec.MetaJSON), &metadata)
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["validation_preview_url"] = url
		encoded, _ := json.Marshal(metadata)
		sbRec.MetaJSON = string(encoded)
		sbRec.PreviewURL = ""
	} else {
		r.PreviewURL = url
		_ = e.DB.UpdateRun(ctx, r)
		sbRec.PreviewURL = url
	}
	_ = e.DB.UpdateSandbox(ctx, sbRec)
	_ = retention.Touch(ctx, e.DB, sbRec)
	evidence := map[string]any{"phase": phase, "command": command, "port": port, "healthcheck": hy.Preview.Healthcheck}
	if sbRec.Backend != "railway" {
		evidence["url"] = url
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "preview", "preview", "", "passed", evidence)
	e.emit(ctx, r.ID, "preview.ready", evidence)
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
	latest, err := e.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if latest.SandboxBackend == "railway" && latest.PreviewURL == "" {
		if sbRec, sandboxErr := e.DB.GetSandboxForRun(ctx, runID); sandboxErr == nil {
			var metadata map[string]any
			if json.Unmarshal([]byte(sbRec.MetaJSON), &metadata) == nil {
				latest.PreviewURL, _ = metadata["validation_preview_url"].(string)
			}
		}
	}
	return latest, nil
}

func (e *Engine) phasePR(ctx context.Context, r *state.Run) error {
	if r.PRURL != "" {
		e.emit(ctx, r.ID, "repo.pr.created", map[string]any{"url": r.PRURL, "draft": true, "existing": true})
		return nil
	}
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
	if workdir == e.Root {
		var err error
		workdir, err = e.migrateRetainedRailwayWorkdir(ctx, r)
		if err != nil {
			return err
		}
	}
	if workdir == e.Root && !simulationMode() {
		return fmt.Errorf("refusing to create PR from workspace root; run checkout was not isolated")
	}
	if out, err := repo.GitCommandContext(ctx, "-C", workdir, "checkout", "-B", branch).CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout branch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	// Preview processes must never leak runtime output into the proposed source change.
	_ = os.Remove(filepath.Join(workdir, ".vessica-preview.log"))
	_ = os.Remove(filepath.Join(workdir, ".vessica-preview.pid"))
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
	if finalSandbox != nil && shouldDestroySandboxAfterReceipt(r) {
		if err := retention.Destroy(ctx, e.DB, e.Root, finalSandbox, "no_preview"); err != nil {
			e.emit(ctx, r.ID, "warning", map[string]any{"message": "could not clean up non-preview sandbox: " + err.Error()})
		} else {
			e.emit(ctx, r.ID, "sandbox.destroyed", map[string]any{"sandbox_id": finalSandbox.ID, "reason": "no_preview"})
		}
	}
	return nil
}
