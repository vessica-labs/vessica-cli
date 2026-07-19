package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/isolation"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

// materializeWorktreeDependencies is a fallback for repositories whose
// checkpoint did not contain a dependency tree. Purpose-built Railway
// checkpoints normally project local copy-on-write dependency directories when
// the worktree is created, avoiding package-manager work on the critical path.
func (e *Engine) materializeWorktreeDependencies(ctx context.Context, r *state.Run, workdir string) error {
	changed, err := dependencyContractChanged(e.Root, workdir)
	if err != nil {
		return fmt.Errorf("compare worktree dependency contract: %w", err)
	}
	if changed {
		stack, install := reposnapshot.DependencyInstallCommand(workdir)
		if strings.TrimSpace(install) == "" {
			return nil
		}
		started := time.Now()
		installCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		output, installErr := isolation.CommandContext(installCtx, workdir, "bash", "-lc", "export CI=true\n"+install).CombinedOutput()
		detail := map[string]any{
			"stage": "worktree_dependencies", "duration_ms": time.Since(started).Milliseconds(),
			"status": "completed", "cache_hit": false, "mode": "manifest_refresh",
			"reason": "dependency_fingerprint_changed", "stack": stack,
		}
		if installErr != nil {
			detail["status"] = "failed"
			e.emit(ctx, r.ID, "run.infrastructure.stage", detail)
			return fmt.Errorf("refresh changed worktree dependencies: %w: %s", installErr, redaction.Redact(truncate(strings.TrimSpace(string(output)), 2000)))
		}
		e.emit(ctx, r.ID, "run.infrastructure.stage", detail)
		return nil
	}

	target := filepath.Join(workdir, "node_modules")
	if _, err := os.Stat(target); err == nil {
		mode := "existing"
		if info, linkErr := os.Lstat(target); linkErr == nil && info.Mode()&os.ModeSymlink != 0 {
			mode = "snapshot_symlink"
		}
		e.emit(ctx, r.ID, "run.infrastructure.stage", map[string]any{
			"stage": "worktree_dependencies", "duration_ms": 0, "status": "completed",
			"cache_hit": true, "mode": mode,
		})
		return nil
	}
	install := strings.TrimSpace(harness.PreviewInstallCommand(workdir))
	if install == "" {
		return nil
	}
	started := time.Now()
	installCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	detail := map[string]any{
		"stage":     "worktree_dependencies",
		"cache_hit": false,
		"status":    "completed",
	}

	if r.SandboxBackend == "railway" {
		source := filepath.Join(e.Root, "node_modules")
		if source != target {
			if info, statErr := os.Stat(source); statErr == nil && info.IsDir() {
				projected := target + ".vessica-projecting"
				_ = os.RemoveAll(projected)
				output, copyErr := isolation.CommandContext(installCtx, workdir, "cp", "-a", "--reflink=always", source, projected).CombinedOutput()
				projectionErr := copyErr
				if copyErr == nil {
					if renameErr := os.Rename(projected, target); renameErr == nil {
						detail["mode"] = "reflink"
						detail["cache_hit"] = true
						detail["duration_ms"] = time.Since(started).Milliseconds()
						e.emit(ctx, r.ID, "run.infrastructure.stage", detail)
						return nil
					} else {
						projectionErr = renameErr
					}
				}
				_ = os.RemoveAll(projected)
				detail["reflink_fallback"] = truncate(redaction.Redact(strings.TrimSpace(fmt.Sprintf("%v: %s", projectionErr, output))), 500)
			}
		}

		if offline := offlineInstallCommand(install); offline != install {
			output, offlineErr := isolation.CommandContext(installCtx, workdir, "bash", "-lc", "export CI=true\n"+offline).CombinedOutput()
			if offlineErr == nil {
				detail["mode"] = "offline_install"
				detail["cache_hit"] = true
				detail["duration_ms"] = time.Since(started).Milliseconds()
				e.emit(ctx, r.ID, "run.infrastructure.stage", detail)
				return nil
			}
			detail["offline_fallback"] = truncate(redaction.Redact(strings.TrimSpace(string(output))), 500)
		}
	}

	output, err := isolation.CommandContext(installCtx, workdir, "bash", "-lc", "export CI=true\n"+install).CombinedOutput()
	detail["mode"] = "install"
	detail["duration_ms"] = time.Since(started).Milliseconds()
	if err != nil {
		detail["status"] = "failed"
		e.emit(ctx, r.ID, "run.infrastructure.stage", detail)
		return fmt.Errorf("materialize worktree dependencies: %w: %s", err, redaction.Redact(truncate(strings.TrimSpace(string(output)), 2000)))
	}
	e.emit(ctx, r.ID, "run.infrastructure.stage", detail)
	return nil
}

func dependencyContractChanged(root, workdir string) (bool, error) {
	if filepath.Clean(root) == filepath.Clean(workdir) {
		return false, nil
	}
	base, err := reposnapshot.DependencyFingerprint(root)
	if err != nil {
		return false, err
	}
	current, err := reposnapshot.DependencyFingerprint(workdir)
	if err != nil {
		return false, err
	}
	return base != current, nil
}

func offlineInstallCommand(command string) string {
	if strings.Contains(command, "pnpm install ") && !strings.Contains(command, "pnpm install --offline") {
		return strings.Replace(command, "pnpm install ", "pnpm install --offline ", 1)
	}
	return command
}
