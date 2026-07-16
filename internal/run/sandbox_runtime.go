package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/sandbox"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

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
