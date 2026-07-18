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
	"github.com/vessica-labs/vessica-cli/internal/state"
)

// materializeWorktreeDependencies creates the path-specific links used by a
// Node package manager. The repository checkpoint already contains the package
// store, but pnpm node_modules metadata cannot safely be shared by symlink
// across Git worktrees.
func (e *Engine) materializeWorktreeDependencies(ctx context.Context, r *state.Run, workdir string) error {
	if _, err := os.Stat(filepath.Join(workdir, "node_modules")); err == nil {
		return nil
	}
	install := strings.TrimSpace(harness.PreviewInstallCommand(workdir))
	if install == "" {
		return nil
	}
	started := time.Now()
	installCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	output, err := isolation.CommandContext(installCtx, workdir, "bash", "-lc", "export CI=true\n"+install).CombinedOutput()
	detail := map[string]any{
		"stage":       "worktree_dependencies",
		"duration_ms": time.Since(started).Milliseconds(),
		"cache_hit":   r.SandboxBackend == "railway",
		"status":      "completed",
	}
	if err != nil {
		detail["status"] = "failed"
		e.emit(ctx, r.ID, "run.infrastructure.stage", detail)
		return fmt.Errorf("materialize worktree dependencies: %w: %s", err, redaction.Redact(truncate(strings.TrimSpace(string(output)), 2000)))
	}
	e.emit(ctx, r.ID, "run.infrastructure.stage", detail)
	return nil
}
