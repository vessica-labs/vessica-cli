package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/isolation"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
)

func recordBootstrapTimings(ctx context.Context, db *state.DB, runID string, local []map[string]any) {
	if db == nil || strings.TrimSpace(runID) == "" {
		return
	}
	parse := func(key string) int64 {
		value, _ := strconv.ParseInt(strings.TrimSpace(os.Getenv(key)), 10, 64)
		return value
	}
	requested := parse("VES_SANDBOX_REQUESTED_AT_MS")
	bootstrap := parse("VES_BOOTSTRAP_STARTED_AT_MS")
	verifyStarted := parse("VES_TOOLCHAIN_VERIFY_STARTED_AT_MS")
	verified := parse("VES_TOOLCHAIN_VERIFIED_AT_MS")
	authVerified := parse("VES_AUTH_VERIFIED_AT_MS")
	downloadStarted := parse("VES_WORKER_DOWNLOAD_STARTED_AT_MS")
	downloaded := parse("VES_WORKER_DOWNLOADED_AT_MS")
	remote := []struct {
		name       string
		start, end int64
	}{
		{"checkpoint_boot", requested, bootstrap},
		{"runtime_integrity", verifyStarted, verified},
		{"codex_auth_verify", verified, authVerified},
		{"worker_download", downloadStarted, downloaded},
	}
	for _, item := range remote {
		if item.start > 0 && item.end >= item.start {
			detail := map[string]any{"stage": item.name, "duration_ms": item.end - item.start, "status": "completed"}
			if item.name == "runtime_integrity" {
				detail["cache_hit"] = os.Getenv("VES_RUNTIME_ATTESTATION_CACHE_HIT") == "1"
				detail["mode"] = map[bool]string{true: "snapshot_attestation", false: "full_verify"}[detail["cache_hit"].(bool)]
			}
			if item.name == "worker_download" {
				detail["cache_hit"] = os.Getenv("VES_WORKER_CACHE_HIT") == "1"
				detail["mode"] = map[bool]string{true: "snapshot_binary", false: "verified_download"}[detail["cache_hit"].(bool)]
			}
			_, _ = db.AppendEvent(ctx, runID, "", "run.infrastructure.stage", detail)
		}
	}
	for _, item := range local {
		_, _ = db.AppendEvent(ctx, runID, "", "run.infrastructure.stage", item)
	}
	if requested > 0 {
		_, _ = db.AppendEvent(ctx, runID, "", "run.infrastructure.stage", map[string]any{"stage": "sandbox_to_worker_ready", "duration_ms": time.Now().UnixMilli() - requested, "status": "completed"})
	}
}

func ensureWorkerRepo(ctx context.Context, root, remote string) (map[string]any, error) {
	if remote == "" {
		return nil, fmt.Errorf("VES_REPO_REMOTE is required")
	}
	markerPath := filepath.Join(filepath.Dir(root), reposnapshot.MarkerFile)
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		if markerRaw, markerErr := os.ReadFile(markerPath); markerErr == nil {
			var marker reposnapshot.Checkpoint
			_ = json.Unmarshal(markerRaw, &marker)
			// Railway can restore checkpoint files with their original numeric
			// owner while the downloaded worker runs as root. Trust only this
			// resolved repository path for orchestration Git commands; repository
			// build and model processes still run as the isolated agent user.
			gitAtRoot := []string{"-c", "safe.directory=" + root, "-C", root}
			fetchStarted := time.Now()
			out, fetchErr := repo.GitCommandContext(ctx, append(gitAtRoot, "fetch", "--quiet", "--prune", repo.AuthenticatedRemote(remote), "+refs/heads/*:refs/remotes/origin/*")...).CombinedOutput()
			if fetchErr != nil {
				return nil, fmt.Errorf("fetch repository checkpoint delta: %w: %s", fetchErr, strings.TrimSpace(string(out)))
			}
			target := "origin/HEAD"
			if _, err := repo.GitCommandContext(ctx, append(gitAtRoot, "rev-parse", "--verify", target)...).CombinedOutput(); err != nil {
				target = "origin/main"
			}
			out, resetErr := repo.GitCommandContext(ctx, append(gitAtRoot, "reset", "--hard", target)...).CombinedOutput()
			if resetErr != nil {
				return nil, fmt.Errorf("reset repository checkpoint: %w: %s", resetErr, strings.TrimSpace(string(out)))
			}
			dependencyFingerprint, fingerprintErr := reposnapshot.DependencyFingerprint(root)
			if fingerprintErr != nil {
				return nil, fingerprintErr
			}
			dependenciesUpdated := marker.DependencyState != "ready" || dependencyFingerprint != marker.DependencyFingerprint
			stack, install := reposnapshot.DependencyInstallCommand(root)
			dependencyMS := int64(0)
			if dependenciesUpdated && install != "" {
				if err := isolation.PrepareWorkdir(ctx, root); err != nil {
					return nil, err
				}
				dependencyStarted := time.Now()
				command := isolation.CommandContext(ctx, root, "bash", "-lc", install)
				if output, err := command.CombinedOutput(); err != nil {
					return nil, fmt.Errorf("refresh repository dependencies: %w: %s", err, strings.TrimSpace(string(output)))
				}
				dependencyMS = time.Since(dependencyStarted).Milliseconds()
			}
			commitOutput, commitErr := repo.GitCommandContext(ctx, append(gitAtRoot, "rev-parse", "HEAD")...).Output()
			if commitErr != nil {
				return nil, fmt.Errorf("resolve refreshed checkpoint commit: %w", commitErr)
			}
			files, inventoryErr := reposnapshot.RepositoryFiles(root)
			if inventoryErr != nil {
				return nil, inventoryErr
			}
			specification, specificationFingerprint := reposnapshot.InferSpecification(files, stack)
			commit := strings.TrimSpace(string(commitOutput))
			candidate := reposnapshot.Checkpoint{
				SchemaVersion: reposnapshot.SchemaVersion, Status: "ready", BaseCommit: commit,
				DependencyFingerprint: dependencyFingerprint, ToolchainFingerprint: toolchain.Fingerprint(),
				Stack: stack, DependencyState: map[bool]string{true: "ready", false: marker.DependencyState}[install != ""],
				Specification: specification, SpecificationFingerprint: specificationFingerprint,
				PreparedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			candidate.Name = reposnapshot.Name(state.CanonicalRepositoryRemote(remote), commit, dependencyFingerprint, specificationFingerprint, toolchain.Fingerprint())
			candidateRaw, _ := json.Marshal(candidate)
			candidatePath := filepath.Join(filepath.Dir(root), reposnapshot.CandidateFile)
			if err := os.WriteFile(candidatePath, candidateRaw, 0o644); err != nil {
				return nil, fmt.Errorf("write repository checkpoint candidate: %w", err)
			}
			_ = os.Remove(markerPath)
			return map[string]any{"cache_hit": true, "mode": "checkpoint_delta", "stack": stack, "base_commit": marker.BaseCommit, "target_commit": commit, "checkpoint_refresh_needed": candidate.Name != marker.Name, "dependency_cache_hit": !dependenciesUpdated, "dependency_refresh_ms": dependencyMS, "git_sync_ms": time.Since(fetchStarted).Milliseconds()}, nil
		}
		return map[string]any{"cache_hit": true, "mode": "retained_sandbox"}, nil
	}
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return nil, err
	}
	out, err := repo.GitCommandContext(ctx, "clone", repo.AuthenticatedRemote(remote), root).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("clone worker repository: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := repo.GitCommandContext(ctx, "-C", root, "remote", "set-url", "origin", remote).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("reset worker origin: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return map[string]any{"cache_hit": false, "mode": "full_clone"}, nil
}
