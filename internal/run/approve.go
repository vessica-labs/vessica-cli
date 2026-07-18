package run

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type ApproveOptions struct {
	MergeMethod string
	KeepPreview bool
	KeepBranch  bool
}

type ApproveResult struct {
	RunID            string `json:"run_id"`
	SandboxID        string `json:"sandbox_id,omitempty"`
	PRURL            string `json:"pr_url"`
	MergeMethod      string `json:"merge_method"`
	MergeCommitSHA   string `json:"merge_commit_sha,omitempty"`
	Merged           bool   `json:"merged"`
	AlreadyMerged    bool   `json:"already_merged,omitempty"`
	BranchDeleted    bool   `json:"branch_deleted"`
	SandboxDestroyed bool   `json:"sandbox_destroyed"`
	ApprovedAt       string `json:"approved_at"`
}

var (
	approvalGetPullRequest = repo.GetPullRequest
	approvalMarkReady      = repo.MarkPullRequestReady
	approvalMerge          = repo.MergePullRequest
	approvalDeleteBranch   = repo.DeleteBranch
	approvalPushBranch     = repo.PushBranch
)

func (e *Engine) ApproveRun(ctx context.Context, runID string, opts ApproveOptions) (*ApproveResult, error) {
	method := strings.ToLower(strings.TrimSpace(opts.MergeMethod))
	if method == "" {
		method = "squash"
	}
	if method != "squash" && method != "merge" && method != "rebase" {
		return nil, fmt.Errorf("merge method must be squash, merge, or rebase")
	}
	runRecord, err := e.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if runRecord.Status != "completed" {
		return nil, fmt.Errorf("run %s must be completed before approval (current status: %s)", runID, runRecord.Status)
	}
	if strings.TrimSpace(runRecord.PRURL) == "" {
		return nil, fmt.Errorf("run %s has no pull request to approve", runID)
	}
	if e.Config.Repo.Remote == "" {
		return nil, fmt.Errorf("repo.remote is required to approve a pull request")
	}
	prNumber, err := repo.ParsePRNumber(runRecord.PRURL)
	if err != nil {
		return nil, err
	}
	sandboxRecord, err := e.DB.GetSandboxForRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	details, err := approvalGetPullRequest(ctx, e.Config.Repo.Remote, prNumber)
	if err != nil {
		return nil, err
	}
	if details.State == "closed" && !details.Merged {
		return nil, fmt.Errorf("pull request %d is closed without being merged", prNumber)
	}
	approvedAt := state.Now()
	result := &ApproveResult{
		RunID:       runID,
		SandboxID:   sandboxRecord.ID,
		PRURL:       runRecord.PRURL,
		MergeMethod: method,
		ApprovedAt:  approvedAt,
	}
	if details.Merged {
		result.Merged = true
		result.AlreadyMerged = true
		result.MergeCommitSHA = details.MergeCommitSHA
		return e.finishApproval(ctx, runRecord, sandboxRecord, opts, result, details.Head.Ref)
	}
	if sandboxRecord.Status == "destroyed" || sandboxRecord.Status == "expired" || sandboxRecord.ContainerID == "" {
		return nil, fmt.Errorf("run %s no longer has a live preview sandbox", runID)
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
		return nil, fmt.Errorf("sandbox integration checkout has uncommitted changes; use ves sandbox prompt or commit them before approval")
	}
	branch := strings.TrimSpace(sandboxRecord.Branch)
	if branch == "" || details.Head.Ref != branch {
		return nil, fmt.Errorf("preview branch %q does not match pull request head %q", branch, details.Head.Ref)
	}
	currentBranch, err := repo.GitCommandContext(ctx, "-C", workdir, "branch", "--show-current").Output()
	if err != nil || strings.TrimSpace(string(currentBranch)) != branch {
		return nil, fmt.Errorf("sandbox checkout is not on preview branch %s", branch)
	}
	e.emit(ctx, runID, "run.approval.started", map[string]any{"pr_url": runRecord.PRURL, "sandbox_id": sandboxRecord.ID, "merge_method": method})
	if err := approvalPushBranch(ctx, workdir, e.Config.Repo.Remote, branch); err != nil {
		return e.failApproval(ctx, runRecord, sandboxRecord, result, err)
	}
	headSHA, err := repo.GitCommandContext(ctx, "-C", workdir, "rev-parse", "HEAD").Output()
	if err != nil {
		return e.failApproval(ctx, runRecord, sandboxRecord, result, err)
	}
	expectedSHA := strings.TrimSpace(string(headSHA))
	details, err = e.waitForPullRequestHead(ctx, prNumber, expectedSHA)
	if err != nil {
		return e.failApproval(ctx, runRecord, sandboxRecord, result, err)
	}
	if details.Draft {
		if err := approvalMarkReady(ctx, details.NodeID); err != nil {
			return e.failApproval(ctx, runRecord, sandboxRecord, result, err)
		}
		e.emit(ctx, runID, "repo.pr.ready", map[string]any{"url": runRecord.PRURL})
	}
	mergeResult, err := approvalMerge(ctx, e.Config.Repo.Remote, prNumber, method, expectedSHA)
	if err != nil {
		return e.failApproval(ctx, runRecord, sandboxRecord, result, err)
	}
	result.Merged = true
	result.MergeCommitSHA = mergeResult.SHA
	e.emit(ctx, runID, "repo.pr.merged", map[string]any{"url": runRecord.PRURL, "merge_method": method, "merge_commit_sha": mergeResult.SHA})
	return e.finishApproval(ctx, runRecord, sandboxRecord, opts, result, branch)
}

func (e *Engine) waitForPullRequestHead(ctx context.Context, prNumber int, expectedSHA string) (*repo.PRDetails, error) {
	var details *repo.PRDetails
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		details, err = approvalGetPullRequest(ctx, e.Config.Repo.Remote, prNumber)
		if err == nil && details.Head.SHA == expectedSHA {
			return details, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("pull request head did not update to preview commit %s", expectedSHA)
}

func (e *Engine) failApproval(ctx context.Context, runRecord *state.Run, sandboxRecord *state.Sandbox, result *ApproveResult, approvalErr error) (*ApproveResult, error) {
	_, _ = e.DB.CreateRunEvidence(ctx, runRecord.ID, "approve", "pr_merge", "", "failed", map[string]any{
		"sandbox_id": sandboxRecord.ID,
		"pr_url":     runRecord.PRURL,
		"error":      redaction.Redact(approvalErr.Error()),
	})
	e.emit(ctx, runRecord.ID, "run.approval.failed", map[string]any{"pr_url": runRecord.PRURL, "message": redaction.Redact(approvalErr.Error())})
	return result, approvalErr
}

func (e *Engine) finishApproval(ctx context.Context, runRecord *state.Run, sandboxRecord *state.Sandbox, opts ApproveOptions, result *ApproveResult, branch string) (*ApproveResult, error) {
	if !opts.KeepBranch && branch != "" {
		if err := approvalDeleteBranch(ctx, e.Config.Repo.Remote, branch); err != nil {
			e.emit(ctx, runRecord.ID, "warning", map[string]any{"message": "pull request merged, but branch cleanup failed: " + redaction.Redact(err.Error())})
		} else {
			result.BranchDeleted = true
		}
	}
	if sandboxRecord.Status == "destroyed" || sandboxRecord.Status == "expired" {
		result.SandboxDestroyed = true
	} else if opts.KeepPreview {
		_ = retention.Touch(ctx, e.DB, sandboxRecord)
	} else if err := retention.Destroy(ctx, e.DB, e.Root, sandboxRecord, "merged"); err != nil {
		e.emit(ctx, runRecord.ID, "warning", map[string]any{"message": "pull request merged, but sandbox cleanup failed: " + redaction.Redact(err.Error())})
	} else {
		result.SandboxDestroyed = true
		e.emit(ctx, runRecord.ID, "sandbox.destroyed", map[string]any{"sandbox_id": sandboxRecord.ID, "reason": "merged"})
	}
	runRecord.PRMode = "merged"
	_ = e.DB.UpdateRun(ctx, runRecord)
	if _, err := e.DB.UpdateEpic(ctx, runRecord.EpicID, "", "", state.EpicStatusCompleted); err != nil {
		return nil, fmt.Errorf("mark epic completed: %w", err)
	}
	_, _ = e.DB.CreateRunEvidence(ctx, runRecord.ID, "approve", "pr_merge", "", "passed", map[string]any{
		"sandbox_id":        sandboxRecord.ID,
		"pr_url":            runRecord.PRURL,
		"merge_method":      result.MergeMethod,
		"merge_commit_sha":  result.MergeCommitSHA,
		"already_merged":    result.AlreadyMerged,
		"approved_at":       result.ApprovedAt,
		"branch_deleted":    result.BranchDeleted,
		"sandbox_destroyed": result.SandboxDestroyed,
	})
	e.emit(ctx, runRecord.ID, "run.approved", map[string]any{"pr_url": runRecord.PRURL, "merge_commit_sha": result.MergeCommitSHA, "approved_at": result.ApprovedAt})
	e.recordWorkflowKnowledge(ctx, runRecord, "run.approved", "Run approved and pull request merged", "run:"+runRecord.ID+":approved")
	e.recordWorkflowKnowledge(ctx, runRecord, "epic.completed", "Epic completed after approved merge", "epic:"+runRecord.EpicID+":completed")
	return result, nil
}
