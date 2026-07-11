package run

import (
	"context"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/repo"
)

func TestApproveRunMarksReadyMergesAndRecordsEvidence(t *testing.T) {
	root, db, runRecord, sandboxRecord := promptSandboxFixture(t)
	defer db.Close()
	runGit(t, root, "checkout", "-b", sandboxRecord.Branch)
	runRecord.PRURL = "https://github.com/acme/demo/pull/7"
	if err := db.UpdateRun(context.Background(), runRecord); err != nil {
		t.Fatal(err)
	}
	details := &repo.PRDetails{HTMLURL: runRecord.PRURL, NodeID: "PR_node", Number: 7, State: "open", Draft: true}
	details.Head.Ref = sandboxRecord.Branch
	var pushed, ready, merged, deleted bool
	oldGet, oldReady, oldMerge := approvalGetPullRequest, approvalMarkReady, approvalMerge
	oldDelete, oldPush := approvalDeleteBranch, approvalPushBranch
	approvalGetPullRequest = func(context.Context, string, int) (*repo.PRDetails, error) {
		copy := *details
		return &copy, nil
	}
	approvalPushBranch = func(ctx context.Context, workdir, remote, branch string) error {
		pushed = true
		details.Head.SHA = strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD"))
		return nil
	}
	approvalMarkReady = func(context.Context, string) error {
		ready = true
		return nil
	}
	approvalMerge = func(_ context.Context, _ string, number int, method, sha string) (*repo.MergeResult, error) {
		merged = number == 7 && method == "squash" && sha == details.Head.SHA
		return &repo.MergeResult{SHA: "merge_sha", Merged: true}, nil
	}
	approvalDeleteBranch = func(context.Context, string, string) error {
		deleted = true
		return nil
	}
	defer func() {
		approvalGetPullRequest, approvalMarkReady, approvalMerge = oldGet, oldReady, oldMerge
		approvalDeleteBranch, approvalPushBranch = oldDelete, oldPush
	}()

	cfg := config.Defaults()
	cfg.Repo.Remote = "git@github.com:acme/demo.git"
	engine := &Engine{DB: db, Root: root, Config: cfg}
	result, err := engine.ApproveRun(context.Background(), runRecord.ID, ApproveOptions{MergeMethod: "squash", KeepPreview: true})
	if err != nil {
		t.Fatal(err)
	}
	if !pushed || !ready || !merged || !deleted {
		t.Fatalf("calls push=%v ready=%v merge=%v delete=%v", pushed, ready, merged, deleted)
	}
	if !result.Merged || result.MergeCommitSHA != "merge_sha" || !result.BranchDeleted || result.SandboxDestroyed {
		t.Fatalf("result=%#v", result)
	}
	storedRun, err := db.GetRun(context.Background(), runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedRun.PRMode != "merged" {
		t.Fatalf("pr_mode=%q", storedRun.PRMode)
	}
	evidence, err := db.ListRunEvidence(context.Background(), runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].Phase != "approve" || evidence[0].Status != "passed" {
		t.Fatalf("evidence=%#v", evidence)
	}
}

func TestApproveRunRejectsUnsupportedMergeMethod(t *testing.T) {
	engine := &Engine{}
	if _, err := engine.ApproveRun(context.Background(), "run_test", ApproveOptions{MergeMethod: "octopus"}); err == nil {
		t.Fatal("expected merge method error")
	}
}
