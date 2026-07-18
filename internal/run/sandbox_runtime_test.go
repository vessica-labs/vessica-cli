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
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "demo"), 0o755); err != nil {
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
	dependencyPath := filepath.Join(workdir, "node_modules")
	if info, err := os.Lstat(dependencyPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dependency path is not a symlink: info=%v err=%v", info, err)
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
