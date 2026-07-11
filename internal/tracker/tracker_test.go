package tracker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestPushUpsertsMappingAndStatusCounts(t *testing.T) {
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
	epic, err := db.CreateEpic(ctx, "Mirror me", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := Push(ctx, db, "linear", "epic", epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Push(ctx, db, "linear", "epic", epic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected upserted mapping, got %s and %s", first.ID, second.ID)
	}
	st := GetStatus(ctx, db, "linear")
	if !st.Connected || st.Mappings != 1 {
		t.Fatalf("status=%#v", st)
	}
}
