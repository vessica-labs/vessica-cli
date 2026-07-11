package run

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestPromptSandboxCommitsAndRecordsEvidence(t *testing.T) {
	t.Setenv("VES_RUNNER_MODE", "stub")
	root, db, runRecord, sandboxRecord := promptSandboxFixture(t)
	defer db.Close()

	engine := &Engine{DB: db, Root: root, Config: config.Defaults()}
	result, err := engine.PromptSandbox(context.Background(), sandboxRecord.ID, PromptOptions{
		Prompt: "Tighten the heading copy.",
		Push:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.Commit == "" || result.Pushed {
		t.Fatalf("result=%#v", result)
	}
	if len(result.FilesChanged) != 1 || result.FilesChanged[0] != ".vessica-runner-stub" {
		t.Fatalf("files=%v", result.FilesChanged)
	}
	status := gitOutput(t, root, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("dirty checkout after prompt: %s", status)
	}
	storedRun, err := db.GetRun(context.Background(), runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedRun.Status != "completed" {
		t.Fatalf("run status changed: %s", storedRun.Status)
	}
	evidence, err := db.ListRunEvidence(context.Background(), runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].Kind != "sandbox_prompt" || evidence[0].Status != "passed" {
		t.Fatalf("evidence=%#v", evidence)
	}
	events, err := db.ListEvents(context.Background(), runRecord.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Type != "sandbox.prompt.completed" {
		t.Fatalf("events=%#v", events)
	}
}

func TestPromptSandboxRejectsDirtyCheckout(t *testing.T) {
	t.Setenv("VES_RUNNER_MODE", "stub")
	root, db, _, sandboxRecord := promptSandboxFixture(t)
	defer db.Close()
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := &Engine{DB: db, Root: root, Config: config.Defaults()}
	_, err := engine.PromptSandbox(context.Background(), sandboxRecord.ID, PromptOptions{Prompt: "Make a change."})
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("err=%v", err)
	}
}

func promptSandboxFixture(t *testing.T) (string, *state.DB, *state.Run, *state.Sandbox) {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Vessica Test")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".vessica/state/\n.vessica/runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "initial")

	db, err := state.Open("sqlite", "", root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, root, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "Prompt", "body")
	if err != nil {
		t.Fatal(err)
	}
	runRecord, err := db.CreateRun(ctx, epic.ID, "", "codex", "test-model", "high", "local", 1, true, "draft", "", "")
	if err != nil {
		t.Fatal(err)
	}
	runRecord.Status = "completed"
	if err := db.UpdateRun(ctx, runRecord); err != nil {
		t.Fatal(err)
	}
	sandboxRecord, err := db.CreateSandbox(ctx, runRecord.ID, "local", "vessica/prompt-test")
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]string{"host_workdir": root})
	sandboxRecord.ContainerID = "local"
	sandboxRecord.Status = "running"
	sandboxRecord.PreviewURL = "http://127.0.0.1:43210"
	sandboxRecord.MetaJSON = string(meta)
	if err := db.UpdateSandbox(ctx, sandboxRecord); err != nil {
		t.Fatal(err)
	}
	return root, db, runRecord, sandboxRecord
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
