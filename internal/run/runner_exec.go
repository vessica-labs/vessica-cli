package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/pack"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/runner"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

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
				res, err := rn.CollectResult(runCtx)
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
				if strings.TrimSpace(res.Output) != "" {
					e.emitRunner(ctx, r, phase, agentRole, runner.Event{
						Type:    "agent.output",
						Message: res.Output,
						Data:    map[string]any{"kind": "summary", "status": "completed"},
					})
				}
				return res, nil
			}
			e.emitRunner(ctx, r, phase, agentRole, ev)
		case <-runCtx.Done():
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = rn.Cancel(cleanupCtx)
			cleanupCancel()
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
