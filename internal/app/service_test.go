package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestRawLogIsBoundedAndRedacted(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.EnsureWorkspace(context.Background(), root, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(context.Background(), "Raw log", "body")
	if err != nil {
		t.Fatal(err)
	}
	runRecord, err := db.CreateRun(context.Background(), epic.ID, "", "codex", "model", "high", "local", 1, false, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".vessica", "runs", runRecord.ID)
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "agent.jsonl"), []byte(`{"OPENAI_API_KEY":"sk-secret-value"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := New(db, root, config.Defaults()).RawLog(context.Background(), runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := result["content"].(string)
	if strings.Contains(content, "sk-secret-value") {
		t.Fatalf("raw secret was not redacted: %s", content)
	}
}
