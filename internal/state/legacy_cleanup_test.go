package state

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationRemovesLegacyMemoryTables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE memories(id TEXT PRIMARY KEY); CREATE VIRTUAL TABLE memory_fts USING fts5(id,body);`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()
	db, err := Open("sqlite", path, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"memories", "memory_fts"} {
		var count int
		if err := db.SQL.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("legacy table %s still exists", table)
		}
	}
}
