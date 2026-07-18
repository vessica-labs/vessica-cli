package run

import (
	"context"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type RollbackResult struct {
	RunID            string `json:"run_id"`
	PRURL            string `json:"pr_url"`
	RolledBack       bool   `json:"rolled_back"`
	SandboxDestroyed bool   `json:"sandbox_destroyed"`
}

func (e *Engine) RollbackRun(ctx context.Context, runID string) (*RollbackResult, error) {
	r, err := e.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	result := &RollbackResult{RunID: runID, PRURL: r.PRURL}
	if r.PRMode == "rolled_back" {
		result.RolledBack = true
		return result, nil
	}
	if r.PRMode == "merged" {
		return nil, fmt.Errorf("merged runs cannot be rolled back")
	}
	if r.PRURL == "" {
		return nil, fmt.Errorf("run %s has no pull request to roll back", runID)
	}
	number, err := repo.ParsePRNumber(r.PRURL)
	if err != nil {
		return nil, err
	}
	if err := repo.CommentPullRequest(ctx, e.Config.Repo.Remote, number, fmt.Sprintf("Rolled back through Vessica for run `%s`.", runID)); err != nil {
		return nil, err
	}
	if err := repo.ClosePullRequest(ctx, e.Config.Repo.Remote, number); err != nil {
		return nil, err
	}
	r.PRMode = "rolled_back"
	if err := e.DB.UpdateRun(ctx, r); err != nil {
		return nil, err
	}
	if _, err := e.DB.UpdateEpic(ctx, r.EpicID, "", "", state.EpicStatusRolledBack); err != nil {
		return nil, fmt.Errorf("mark epic rolled back: %w", err)
	}
	_, _ = e.DB.CreateRunEvidence(ctx, runID, "rollback", "pr_close", "", "passed", map[string]any{"rolled_back_at": state.Now()})
	if sandboxRecord, getErr := e.DB.GetSandboxForRun(ctx, runID); getErr == nil {
		if destroyErr := retention.Destroy(ctx, e.DB, e.Root, sandboxRecord, "rolled_back"); destroyErr != nil {
			return nil, destroyErr
		}
		result.SandboxDestroyed = true
	}
	e.emit(ctx, runID, "run.rolled_back", map[string]any{"pr_url": r.PRURL})
	e.recordWorkflowKnowledge(ctx, r, "run.rolled_back", "Run was rolled back and its pull request closed", "run:"+runID+":rolled_back")
	result.RolledBack = true
	return result, nil
}
