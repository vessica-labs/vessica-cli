package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestPrepareRailwayRunWorkdirUsesIsolatedWorktreeAndSharesDependencies(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRuntimeTest(t, root, "init")
	gitRuntimeTest(t, root, "config", "user.email", "test@example.com")
	gitRuntimeTest(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("checkpoint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".venv", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRuntimeTest(t, root, "add", "README.md")
	gitRuntimeTest(t, root, "commit", "-m", "initial")

	engine := &Engine{Root: root}
	record := &state.Sandbox{RunID: "run_test", Branch: "vessica/epic/run_test"}
	workdir, err := engine.prepareRailwayRunWorkdir(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if workdir == root || !strings.Contains(workdir, filepath.Join("runs", record.RunID, "workspace")) {
		t.Fatalf("workdir=%q root=%q", workdir, root)
	}
	branch := strings.TrimSpace(string(gitRuntimeOutput(t, workdir, "branch", "--show-current")))
	if branch != record.Branch {
		t.Fatalf("branch=%q", branch)
	}
	dependencyPath := filepath.Join(workdir, ".venv")
	if info, err := os.Lstat(dependencyPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dependency path is not a symlink: info=%v err=%v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(workdir, "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("pnpm node_modules must be materialized per worktree, err=%v", err)
	}
}

func TestPrepareRailwayRunWorkdirMigratesBranchCheckedOutAtCheckpointRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRuntimeTest(t, root, "init")
	gitRuntimeTest(t, root, "config", "user.email", "test@example.com")
	gitRuntimeTest(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("checkpoint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRuntimeTest(t, root, "add", "README.md")
	gitRuntimeTest(t, root, "commit", "-m", "initial")
	branch := "vessica/epic/run_legacy"
	gitRuntimeTest(t, root, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("completed run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRuntimeTest(t, root, "commit", "-am", "completed run")
	wantHead := strings.TrimSpace(string(gitRuntimeOutput(t, root, "rev-parse", "HEAD")))

	engine := &Engine{Root: root}
	record := &state.Sandbox{RunID: "run_legacy", Branch: branch}
	workdir, err := engine.prepareRailwayRunWorkdir(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(gitRuntimeOutput(t, root, "branch", "--show-current"))); got != "" {
		t.Fatalf("checkpoint root branch=%q, want detached HEAD", got)
	}
	if got := strings.TrimSpace(string(gitRuntimeOutput(t, workdir, "branch", "--show-current"))); got != branch {
		t.Fatalf("worktree branch=%q, want %q", got, branch)
	}
	if got := strings.TrimSpace(string(gitRuntimeOutput(t, workdir, "rev-parse", "HEAD"))); got != wantHead {
		t.Fatalf("worktree head=%q, want %q", got, wantHead)
	}
}

func gitRuntimeTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = gitRuntimeOutput(t, dir, args...)
}

func gitRuntimeOutput(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return output
}
