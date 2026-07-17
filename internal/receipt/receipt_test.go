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
	run.ReceiptID = rcpt.ID
	run.PreviewURL = "https://preview.example/previews/" + run.ID + "/?cap=public-capability"
	if err := db.UpdateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	updated, err := Finalize(ctx, db, run)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != rcpt.ID {
		t.Fatalf("receipt duplicated during publication: first=%s updated=%s", rcpt.ID, updated.ID)
	}
	updatedView, _ := ViewJSON(updated)
	updatedBody := updatedView["body"].(map[string]any)
	if updatedBody["preview_url"] != run.PreviewURL {
		t.Fatalf("preview URL was not refreshed: %#v", updatedBody["preview_url"])
	}
}
