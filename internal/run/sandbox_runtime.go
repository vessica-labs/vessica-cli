package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	workdirStarted := time.Now()
	hostWorkdir := e.Root
	workdirMode := "repository_checkpoint"
	if rec.Backend != "railway" {
		var err error
		hostWorkdir, err = e.prepareRunWorkdir(ctx, rec)
		if err != nil {
			return nil, err
		}
		workdirMode = "isolated_clone"
	} else {
		var err error
		hostWorkdir, err = e.prepareRailwayRunWorkdir(ctx, rec)
		if err != nil {
			return nil, err
		}
		workdirMode = "repository_checkpoint_worktree"
	}
	e.emit(ctx, rec.RunID, "run.infrastructure.stage", map[string]any{"stage": "integration_workdir", "duration_ms": time.Since(workdirStarted).Milliseconds(), "status": "completed", "mode": workdirMode, "cache_hit": rec.Backend == "railway"})
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
	metaDocument := map[string]any{}
	_ = json.Unmarshal([]byte(rec.MetaJSON), &metaDocument)
	metaDocument["host_workdir"] = hostWorkdir
	metaDocument["branch"] = rec.Branch
	meta, _ := json.Marshal(metaDocument)
	rec.MetaJSON = string(meta)
	_ = e.DB.UpdateSandbox(ctx, rec)
	return sb, nil
}

func (e *Engine) prepareRailwayRunWorkdir(ctx context.Context, rec *state.Sandbox) (string, error) {
	base := filepath.Join(filepath.Dir(e.Root), "runs", rec.RunID)
	workdir := filepath.Join(base, "workspace")
	if _, err := os.Stat(filepath.Join(workdir, ".git")); err == nil {
		if err := projectCheckpointDependencies(ctx, e.Root, workdir); err != nil {
			return "", err
		}
		return workdir, nil
	}
	gitAtRoot := []string{"-c", "safe.directory=" + e.Root, "-C", e.Root}
	_ = repo.GitCommandContext(ctx, append(gitAtRoot, "worktree", "remove", "--force", workdir)...).Run()
	_ = os.RemoveAll(workdir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	branch := strings.TrimSpace(rec.Branch)
	if branch == "" {
		branch = "vessica/run/" + rec.RunID
	}
	startPoint := "HEAD"
	if output, err := repo.GitCommandContext(ctx, append(gitAtRoot, "rev-parse", "HEAD")...).CombinedOutput(); err == nil {
		startPoint = strings.TrimSpace(string(output))
	}
	// Checkpoints created by older workers may have the run branch checked out
	// at the repository root. Git permits a branch in only one worktree, so
	// detach that legacy checkout before moving the branch into the isolated
	// run worktree. The captured start point preserves the completed run commit.
	if output, err := repo.GitCommandContext(ctx, append(gitAtRoot, "branch", "--show-current")...).CombinedOutput(); err == nil && strings.TrimSpace(string(output)) == branch {
		if output, err := repo.GitCommandContext(ctx, append(gitAtRoot, "checkout", "--detach", startPoint)...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("detach legacy hosted run checkout: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	out, err := repo.GitCommandContext(ctx, append(gitAtRoot, "worktree", "add", "-B", branch, workdir, startPoint)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("create hosted run worktree: %w: %s", err, strings.TrimSpace(string(out)))
	}
	// Project dependency trees into the worktree as local directories. Railway
	// snapshots support copy-on-write reflinks, which retain near-instant warm
	// starts without exposing tools such as Turbopack to cross-worktree symlinks.
	if err := projectCheckpointDependencies(ctx, e.Root, workdir); err != nil {
		return "", err
	}
	return workdir, nil
}

func projectCheckpointDependencies(ctx context.Context, root, workdir string) error {
	var trees []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		depth := strings.Count(filepath.ToSlash(relative), "/")
		if entry.IsDir() {
			name := entry.Name()
			if relative == ".git" || name == "runs" {
				return filepath.SkipDir
			}
			dependency := name == "node_modules" || name == ".venv" || name == "target" || name == ".gradle" || (name == "bundle" && filepath.Base(filepath.Dir(path)) == "vendor")
			if dependency {
				trees = append(trees, relative)
				return filepath.SkipDir
			}
			if depth >= 3 {
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, relative := range trees {
		source, target := filepath.Join(root, relative), filepath.Join(workdir, relative)
		if info, err := os.Lstat(target); err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			if err := os.Remove(target); err != nil {
				return fmt.Errorf("replace legacy dependency symlink %s: %w", relative, err)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		projected := target + ".vessica-projecting"
		_ = os.RemoveAll(projected)
		args := []string{"-a", "--reflink=auto", source, projected}
		if runtime.GOOS != "linux" {
			args = []string{"-R", source, projected}
		}
		if output, err := exec.CommandContext(ctx, "cp", args...).CombinedOutput(); err != nil {
			_ = os.RemoveAll(projected)
			return fmt.Errorf("project repository dependency tree %s: %w: %s", relative, err, strings.TrimSpace(string(output)))
		}
		if err := os.Rename(projected, target); err != nil {
			_ = os.RemoveAll(projected)
			return err
		}
	}
	return nil
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

func (e *Engine) migrateRetainedRailwayWorkdir(ctx context.Context, r *state.Run) (string, error) {
	if r.SandboxBackend != "railway" {
		return e.Root, nil
	}
	// Runs retained from before repository-checkpoint worktrees stored the
	// checkpoint root as their workdir. Migrate them lazily so a resume from PR
	// keeps the completed commit while restoring the isolation invariant.
	sbRecord, err := e.DB.GetSandboxForRun(ctx, r.ID)
	if err != nil {
		return "", fmt.Errorf("load retained Railway sandbox: %w", err)
	}
	migrationStarted := time.Now()
	workdir, err := e.prepareRailwayRunWorkdir(ctx, sbRecord)
	if err != nil {
		return "", err
	}
	metadata := map[string]any{}
	_ = json.Unmarshal([]byte(sbRecord.MetaJSON), &metadata)
	metadata["host_workdir"] = workdir
	metadata["branch"] = sbRecord.Branch
	encoded, _ := json.Marshal(metadata)
	sbRecord.MetaJSON = string(encoded)
	if err := e.DB.UpdateSandbox(ctx, sbRecord); err != nil {
		return "", fmt.Errorf("persist retained Railway worktree: %w", err)
	}
	e.emit(ctx, r.ID, "run.infrastructure.stage", map[string]any{"stage": "integration_workdir", "duration_ms": time.Since(migrationStarted).Milliseconds(), "status": "completed", "mode": "repository_checkpoint_worktree_migration", "cache_hit": true})
	return workdir, nil
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
	stack := harness.Detect(workdir).Stack
	switch {
	case strings.Contains(stack, "node"):
		return "node:24-bookworm"
	case strings.Contains(stack, "go"):
		return sandbox.FallbackImage()
	case strings.Contains(stack, "python"):
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

func previewConnectionFailure(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "err_connection_refused") || strings.Contains(message, "connection refused") || strings.Contains(message, "econnrefused")
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
