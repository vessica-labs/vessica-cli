package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type PromptOptions struct {
	Prompt          string
	Model           string
	ReasoningEffort string
	Push            bool
}

type PromptResult struct {
	RunID        string   `json:"run_id"`
	SandboxID    string   `json:"sandbox_id"`
	Status       string   `json:"status"`
	Output       string   `json:"output,omitempty"`
	Model        string   `json:"model,omitempty"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	Pushed       bool     `json:"pushed"`
	PreviewURL   string   `json:"preview_url,omitempty"`
	LogPath      string   `json:"log_path,omitempty"`
}

// PromptSandbox applies a focused refinement directly to a run's integration
// checkout. It intentionally skips the epic planning and validation phases.
func (e *Engine) PromptSandbox(ctx context.Context, sandboxID string, opts PromptOptions) (*PromptResult, error) {
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	sandboxRecord, err := e.DB.GetSandbox(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	if sandboxRecord.RunID == "" {
		return nil, fmt.Errorf("sandbox %s is not attached to a run", sandboxID)
	}
	if sandboxRecord.Status == "destroyed" || sandboxRecord.ContainerID == "" {
		return nil, fmt.Errorf("sandbox %s is not running", sandboxID)
	}
	if sandboxRecord.Backend == "docker" && sandboxRecord.ContainerID != "local" {
		inspect := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", sandboxRecord.ContainerID)
		out, inspectErr := inspect.CombinedOutput()
		if inspectErr != nil || strings.TrimSpace(string(out)) != "true" {
			return nil, fmt.Errorf("sandbox %s Docker container is not running", sandboxID)
		}
	}

	runRecord, err := e.DB.GetRun(ctx, sandboxRecord.RunID)
	if err != nil {
		return nil, err
	}
	if runRecord.Status == "running" {
		return nil, fmt.Errorf("run %s is still active; wait for it to stop before prompting its sandbox directly", runRecord.ID)
	}
	workdir, err := sandboxHostWorkdir(sandboxRecord)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(workdir, ".git")); err != nil {
		return nil, fmt.Errorf("sandbox integration checkout is unavailable: %w", err)
	}
	status, err := repo.GitCommandContext(ctx, "-C", workdir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git status: %w: %s", err, strings.TrimSpace(string(status)))
	}
	if strings.TrimSpace(string(status)) != "" {
		return nil, fmt.Errorf("sandbox integration checkout has uncommitted changes; commit or discard them before prompting Codex")
	}
	if err := retention.Touch(ctx, e.DB, sandboxRecord); err != nil {
		return nil, err
	}

	runRecord.Runner = "codex"
	if opts.Model != "" {
		runRecord.Model = opts.Model
	} else if runRecord.Model == "" {
		runRecord.Model = e.Config.Runner.Model
	}
	if opts.ReasoningEffort != "" {
		runRecord.ReasoningEffort = opts.ReasoningEffort
	} else if runRecord.ReasoningEffort == "" {
		runRecord.ReasoningEffort = e.Config.Runner.ReasoningEffort
	}

	e.emit(ctx, runRecord.ID, "sandbox.prompt.started", map[string]any{"sandbox_id": sandboxID, "model": runRecord.Model})
	directPrompt := `Apply this refinement directly in the current integration checkout.

Do not create or manage Vessica tickets, artifacts, phases, receipts, sandboxes, or pull requests. Do not run ves lifecycle commands. Make the requested code changes, run only the focused checks useful for this request, and return a concise summary with changed files and checks run.

User request:
` + prompt
	result, err := e.invokeRunner(ctx, runRecord, "prompt", directPrompt, "coder", workdir)
	if err != nil {
		e.emit(ctx, runRecord.ID, "sandbox.prompt.failed", map[string]any{"sandbox_id": sandboxID, "message": redaction.Redact(err.Error())})
		return nil, err
	}
	if result.Status != "ok" {
		err := fmt.Errorf("Codex prompt failed: %s", truncate(result.Output, 500))
		e.emit(ctx, runRecord.ID, "sandbox.prompt.failed", map[string]any{"sandbox_id": sandboxID, "message": redaction.Redact(err.Error())})
		return nil, err
	}

	commit, files, err := commitSandboxPrompt(ctx, workdir)
	if err != nil {
		e.emit(ctx, runRecord.ID, "sandbox.prompt.failed", map[string]any{"sandbox_id": sandboxID, "message": redaction.Redact(err.Error())})
		return nil, err
	}
	pushed := false
	if commit != "" && opts.Push {
		if sandboxRecord.Branch == "" {
			return nil, fmt.Errorf("sandbox %s has no integration branch", sandboxID)
		}
		if e.Config.Repo.Remote == "" {
			return nil, fmt.Errorf("repo.remote is required to push sandbox refinements")
		}
		if err := repo.PushBranch(ctx, workdir, e.Config.Repo.Remote, sandboxRecord.Branch); err != nil {
			e.emit(ctx, runRecord.ID, "sandbox.prompt.failed", map[string]any{"sandbox_id": sandboxID, "message": redaction.Redact(err.Error()), "commit": commit})
			return nil, err
		}
		pushed = true
		e.emit(ctx, runRecord.ID, "repo.branch.updated", map[string]any{"sandbox_id": sandboxID, "branch": sandboxRecord.Branch, "commit": commit})
	}

	_, _ = e.DB.CreateRunEvidence(ctx, runRecord.ID, "prompt", "sandbox_prompt", "", "passed", map[string]any{
		"sandbox_id": sandboxID,
		"prompt":     prompt,
		"output":     truncate(result.Output, 4000),
		"model":      result.Model,
		"files":      files,
		"commit":     commit,
		"pushed":     pushed,
	})
	_ = retention.Touch(ctx, e.DB, sandboxRecord)
	e.emit(ctx, runRecord.ID, "sandbox.prompt.completed", map[string]any{
		"sandbox_id":  sandboxID,
		"files":       files,
		"commit":      commit,
		"pushed":      pushed,
		"preview_url": sandboxRecord.PreviewURL,
	})
	e.recordWorkflowKnowledge(ctx, runRecord, "run.refined", "Human refinement applied to retained sandbox", "run:"+runRecord.ID+":refinement:"+commit)
	return &PromptResult{
		RunID:        runRecord.ID,
		SandboxID:    sandboxID,
		Status:       "completed",
		Output:       result.Output,
		Model:        result.Model,
		FilesChanged: files,
		Commit:       commit,
		Pushed:       pushed,
		PreviewURL:   sandboxRecord.PreviewURL,
		LogPath:      filepath.ToSlash(filepath.Join(".vessica", "runs", runRecord.ID, "agent.jsonl")),
	}, nil
}

func sandboxHostWorkdir(sandboxRecord *state.Sandbox) (string, error) {
	var meta struct {
		HostWorkdir string `json:"host_workdir"`
	}
	if err := json.Unmarshal([]byte(sandboxRecord.MetaJSON), &meta); err != nil {
		return "", fmt.Errorf("read sandbox metadata: %w", err)
	}
	if strings.TrimSpace(meta.HostWorkdir) == "" {
		return "", fmt.Errorf("sandbox %s has no integration checkout", sandboxRecord.ID)
	}
	return meta.HostWorkdir, nil
}

func commitSandboxPrompt(ctx context.Context, workdir string) (string, []string, error) {
	status, err := repo.GitCommandContext(ctx, "-C", workdir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("git status: %w: %s", err, strings.TrimSpace(string(status)))
	}
	files := statusFiles(string(status))
	if len(files) == 0 {
		return "", nil, nil
	}
	if out, err := repo.GitCommandContext(ctx, "-C", workdir, "diff", "--check").CombinedOutput(); err != nil {
		return "", files, fmt.Errorf("git diff check: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := repo.GitCommandContext(ctx, "-C", workdir, "add", "-A").CombinedOutput(); err != nil {
		return "", files, fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := repo.GitCommandContext(ctx, "-C", workdir, "commit", "-m", "ves: apply sandbox refinement").CombinedOutput(); err != nil {
		return "", files, fmt.Errorf("git commit sandbox refinement: %w: %s", err, strings.TrimSpace(string(out)))
	}
	sha, err := repo.GitCommandContext(ctx, "-C", workdir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", files, err
	}
	return strings.TrimSpace(string(sha)), files, nil
}
