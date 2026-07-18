package app

import (
	"context"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	runengine "github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

// SandboxDestroyFunc abstracts the local and hosted sandbox backends without
// leaking transport-specific behavior into dashboard or CLI handlers.
type SandboxDestroyFunc func(context.Context, *state.Sandbox, string) error

// RunLifecycle is the shared use-case service for run cancellation and sandbox
// retention. HTTP, CLI, and hosted adapters should delegate here.
type RunLifecycle struct {
	DB             *state.DB
	Root           string
	Config         config.Config
	DestroySandbox SandboxDestroyFunc
	RetainOnCancel bool
}

func NewRunLifecycle(db *state.DB, root string, cfg config.Config, destroy SandboxDestroyFunc) *RunLifecycle {
	return &RunLifecycle{DB: db, Root: root, Config: cfg, DestroySandbox: destroy}
}

func (s *RunLifecycle) Cancel(ctx context.Context, runID, source string) (*state.Run, error) {
	runRecord, err := s.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("load run for cancellation: %w", err)
	}
	switch runRecord.Status {
	case "completed", "cancelled", "failed":
		return nil, fmt.Errorf("run %s cannot be cancelled from status %s", runID, runRecord.Status)
	}
	runRecord.Status = "cancelled"
	runRecord.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = s.DB.CancelRunAndJobs(ctx, runID, runRecord.FinishedAt); err != nil {
		return nil, fmt.Errorf("persist run cancellation: %w", err)
	}
	if runRecord, err = s.DB.GetRun(ctx, runID); err != nil {
		return nil, fmt.Errorf("reload cancelled run: %w", err)
	}
	if _, err = s.DB.UpdateEpic(ctx, runRecord.EpicID, "", "", state.EpicStatusCancelled); err != nil {
		return nil, fmt.Errorf("persist cancelled epic: %w", err)
	}
	sandboxes, err := s.DB.ListSandboxesForRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list run sandboxes for cancellation: %w", err)
	}
	for i := range sandboxes {
		if s.RetainOnCancel {
			continue
		}
		if err = s.destroy(ctx, &sandboxes[i], "cancelled"); err != nil {
			return nil, fmt.Errorf("destroy sandbox %s during cancellation: %w", sandboxes[i].ID, err)
		}
	}
	if _, err = s.DB.AppendEvent(ctx, runID, "", "run.cancelled", map[string]any{"source": source, "sandbox_retained": s.RetainOnCancel}); err != nil {
		return nil, fmt.Errorf("record run cancellation event: %w", err)
	}
	engine := &runengine.Engine{DB: s.DB, Root: s.Root, Config: s.Config}
	engine.RecordRunKnowledge(ctx, runRecord, "run.cancelled", "Run was cancelled", "run:"+runRecord.ID+":cancelled")
	return runRecord, nil
}

func (s *RunLifecycle) Retain(ctx context.Context, sandboxID string, duration time.Duration) (*state.Sandbox, error) {
	record, err := s.DB.GetSandbox(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("load sandbox for retention: %w", err)
	}
	if record.Status == "destroyed" || record.Status == "expired" {
		return nil, fmt.Errorf("sandbox %s is no longer available", sandboxID)
	}
	if err = retention.Retain(ctx, s.DB, record, duration); err != nil {
		return nil, fmt.Errorf("retain sandbox %s: %w", sandboxID, err)
	}
	return record, nil
}

func (s *RunLifecycle) Destroy(ctx context.Context, sandboxID, reason string) (*state.Sandbox, error) {
	record, err := s.DB.GetSandbox(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("load sandbox for destruction: %w", err)
	}
	if err = s.destroy(ctx, record, reason); err != nil {
		return nil, fmt.Errorf("destroy sandbox %s: %w", sandboxID, err)
	}
	return record, nil
}

func (s *RunLifecycle) destroy(ctx context.Context, sandbox *state.Sandbox, reason string) error {
	if s.DestroySandbox != nil {
		return s.DestroySandbox(ctx, sandbox, reason)
	}
	return retention.Destroy(ctx, s.DB, s.Root, sandbox, reason)
}
