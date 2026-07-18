package controlplane

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
	"github.com/vessica-labs/vessica-cli/internal/sandbox"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
)

func (l *RailwayLauncher) enqueueRepositoryCheckpointRefresh(ctx context.Context, runRecord *state.Run, rs *sandbox.RailwaySandbox, repository *state.Repository) {
	if runRecord == nil || rs == nil || repository == nil {
		return
	}
	var output bytes.Buffer
	path := "/workspace/" + reposnapshot.CandidateFile
	code, err := rs.Exec(ctx, []string{"bash", "-lc", "test -r " + shellQuoteCP(path) + " && cat " + shellQuoteCP(path)}, &output, io.Discard)
	if err != nil || code != 0 || strings.TrimSpace(output.String()) == "" {
		return
	}
	var candidate reposnapshot.Checkpoint
	if json.Unmarshal(output.Bytes(), &candidate) != nil || !candidate.Ready(toolchain.Fingerprint()) {
		return
	}
	current, _ := reposnapshot.Parse(repository.MetadataJSON)
	if current.Name == candidate.Name {
		return
	}
	_, _ = l.DB.EnqueueJob(ctx, "refresh_repository_checkpoint", repositoryCheckpointRefreshPayload{
		RepositoryID: repository.ID, SandboxID: rs.ContainerID(), Checkpoint: candidate,
	}, runRecord.ID)
	_, _ = l.DB.AppendEvent(ctx, runRecord.ID, "", "run.infrastructure.stage", map[string]any{"stage": "repository_checkpoint_refresh", "status": "queued", "checkpoint": candidate.Name})
}

func (l *RailwayLauncher) RefreshRepositoryCheckpoint(ctx context.Context, repositoryID, sandboxID string, checkpoint reposnapshot.Checkpoint) error {
	if !checkpoint.Ready(toolchain.Fingerprint()) {
		return fmt.Errorf("repository checkpoint candidate is stale or invalid")
	}
	source := sandbox.NewRailway(l.CLIPath, l.Config.Hosted.ProjectID, l.Config.Hosted.EnvironmentID, sandboxID)
	if err := l.configureAuth(ctx, source); err != nil {
		return err
	}
	fork, err := source.Fork(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		_ = fork.Destroy(cleanup)
	}()
	encoded, _ := json.Marshal(checkpoint)
	script := strings.Join([]string{
		"set -euo pipefail",
		"rm -rf /workspace/runs /home/vessica-agent/.codex/auth.json /workspace/" + reposnapshot.CandidateFile,
		"git -C /workspace/repo worktree prune",
		"test -z \"$(git -C /workspace/repo status --porcelain)\"",
		"printf '%s' " + shellQuoteCP(base64.StdEncoding.EncodeToString(encoded)) + " | base64 -d >/workspace/" + reposnapshot.MarkerFile,
		"chmod 0644 /workspace/" + reposnapshot.MarkerFile,
	}, "\n")
	if code, execErr := fork.Exec(ctx, []string{"bash", "-lc", script}, io.Discard, io.Discard); execErr != nil || code != 0 {
		return fmt.Errorf("prepare repository checkpoint fork: exit %d: %w", code, execErr)
	}
	if err := fork.CreateCheckpoint(ctx, checkpoint.Name); err != nil {
		return err
	}
	repository, err := l.DB.GetRepository(ctx, repositoryID)
	if err != nil {
		return err
	}
	metadata, err := reposnapshot.Merge(repository.MetadataJSON, checkpoint)
	if err != nil {
		return err
	}
	_, err = l.DB.UpdateRepositoryMetadata(ctx, repositoryID, metadata)
	return err
}
