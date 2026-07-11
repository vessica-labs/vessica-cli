package pack

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitPackInstallSyncAndUpdate(t *testing.T) {
	repo := newTestPackRepo(t)
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, ".vessica", ".gitignore"), "state/\n", 0o644)

	lock, err := Install(workspace, repo+"#main")
	if err != nil {
		t.Fatal(err)
	}
	firstSHA := lock.CommitSHA
	if firstSHA == "" || lock.Origin != repo || lock.Version != "main" {
		t.Fatalf("unexpected initial lock: %+v", lock)
	}
	assertFileContent(t, filepath.Join(workspace, ".vessica", "agents", "coder", "AGENTS.md"), "version one")
	assertFileContent(t, filepath.Join(workspace, ".vessica", ".gitignore"), "state/")

	writeTestFile(t, filepath.Join(repo, "agents", "coder", "AGENTS.md"), "version two\n", 0o644)
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "-c", "user.name=Vessica Tests", "-c", "user.email=tests@vessica.dev", "commit", "-m", "Update harness")
	secondSHA := strings.TrimSpace(runTestGit(t, repo, "rev-parse", "HEAD"))

	lock, err = Sync(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if lock.CommitSHA != firstSHA {
		t.Fatalf("sync moved lock: got %s want %s", lock.CommitSHA, firstSHA)
	}
	assertFileContent(t, filepath.Join(workspace, ".vessica", "agents", "coder", "AGENTS.md"), "version one")

	lock, err = Update(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if lock.CommitSHA != secondSHA {
		t.Fatalf("update did not move lock: got %s want %s", lock.CommitSHA, secondSHA)
	}
	assertFileContent(t, filepath.Join(workspace, ".vessica", "agents", "coder", "AGENTS.md"), "version two")
}

func TestPinInstallsRequestedRevision(t *testing.T) {
	repo := newTestPackRepo(t)
	workspace := t.TempDir()
	firstSHA := strings.TrimSpace(runTestGit(t, repo, "rev-parse", "HEAD"))

	if _, err := Install(workspace, repo+"#main"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(repo, "agents", "coder", "AGENTS.md"), "version two\n", 0o644)
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "-c", "user.name=Vessica Tests", "-c", "user.email=tests@vessica.dev", "commit", "-m", "Update harness")

	lock, err := Pin(workspace, firstSHA)
	if err != nil {
		t.Fatal(err)
	}
	if lock.CommitSHA != firstSHA || lock.Version != firstSHA {
		t.Fatalf("unexpected pinned lock: %+v", lock)
	}
	assertFileContent(t, filepath.Join(workspace, ".vessica", "agents", "coder", "AGENTS.md"), "version one")
}

func TestEmbeddedPackPreservesExistingTopLevelDocs(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "AGENTS.md"), "custom guidance\n", 0o644)

	lock, err := installEmbedded(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Origin != DefaultOrigin || !strings.HasPrefix(lock.CommitSHA, "embedded-") {
		t.Fatalf("unexpected embedded lock: %+v", lock)
	}
	assertFileContent(t, filepath.Join(workspace, "AGENTS.md"), "custom guidance")
	if _, err := os.Stat(filepath.Join(workspace, ".vessica", "pack.yaml")); err != nil {
		t.Fatalf("embedded pack manifest missing: %v", err)
	}
}

func newTestPackRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runTestGit(t, repo, "init", "-b", "main")
	writeTestFile(t, filepath.Join(repo, "pack.yaml"), "name: test-harness\nversion: 1.0.0\n", 0o644)
	writeTestFile(t, filepath.Join(repo, "harness.yaml"), "preview:\n  port: 3000\n", 0o644)
	writeTestFile(t, filepath.Join(repo, "agents", "coder", "AGENTS.md"), "version one\n", 0o644)
	writeTestFile(t, filepath.Join(repo, "docs", "AGENTS.md"), "default guidance\n", 0o644)
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "-c", "user.name=Vessica Tests", "-c", "user.email=tests@vessica.dev", "commit", "-m", "Initial harness")
	return repo
}

func writeTestFile(t *testing.T, name, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", dir}, args...)
	output, err := exec.Command("git", cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func assertFileContent(t *testing.T, name, want string) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != want {
		t.Fatalf("%s: got %q want %q", name, got, want)
	}
}
