package state

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCreateArtifactSetIsIdempotentForRun(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "state.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "Retry ticketization", "body")
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.CreateArtifactSet(ctx, epic.ID, "run_retry", []string{"art_1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateArtifactSet(ctx, epic.ID, "run_retry", []string{"art_1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("artifact set replay created duplicate: %s != %s", first.ID, second.ID)
	}

	var count int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM artifact_sets WHERE source_run_id=?`, "run_retry").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("artifact set count=%d", count)
	}
}
