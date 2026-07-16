package state

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenWithoutMigrationsRequiresExplicitMigration(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenWithOptions("sqlite", filepath.Join(dir, "state.db"), dir, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.VerifySchema(ctx); err == nil {
		t.Fatal("expected schema verification to fail before migration")
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.VerifySchema(ctx); err != nil {
		t.Fatal(err)
	}
}
