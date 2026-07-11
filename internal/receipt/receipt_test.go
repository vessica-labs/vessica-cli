package receipt

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestFinalizeIncludesEvidenceSections(t *testing.T) {
	dir := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(dir, "v.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "E", "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.CreateRun(ctx, epic.ID, "", "codex", "gpt-5.6-terra", "high", "docker", 1, true, "draft", "", "")
	if err != nil {
		t.Fatal(err)
	}
	run.Status = "completed"
	run.StartedAt = state.Now()
	run.FinishedAt = state.Now()
	if err := db.UpdateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateRunEvidence(ctx, run.ID, "build", "test", "", "passed", map[string]any{"command": "go test ./..."}); err != nil {
		t.Fatal(err)
	}
	rcpt, err := Finalize(ctx, db, run)
	if err != nil {
		t.Fatal(err)
	}
	body, err := ViewJSON(rcpt)
	if err != nil {
		t.Fatal(err)
	}
	receiptBody := body["body"].(map[string]any)
	if receiptBody["evidence_count"].(float64) != 1 {
		t.Fatalf("body=%#v", receiptBody)
	}
}
