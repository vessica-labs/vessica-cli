package state

import (
	"context"
	"testing"
)

func TestWorkspaceOwnsMultipleCanonicalRepositories(t *testing.T) {
	db, err := Open("sqlite", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ws, err := db.EnsureWorkspace(context.Background(), "/tmp/default", "hosted")
	if err != nil {
		t.Fatal(err)
	}
	one, err := db.EnsureRepository(context.Background(), ws.ID, "git@github.com:Acme/One.git")
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.EnsureRepository(context.Background(), ws.ID, "https://github.com/acme/one.git")
	if err != nil {
		t.Fatal(err)
	}
	if one.ID != again.ID {
		t.Fatalf("canonical repository duplicated: %s %s", one.ID, again.ID)
	}
	if _, err := db.EnsureRepository(context.Background(), ws.ID, "https://github.com/acme/two.git"); err != nil {
		t.Fatal(err)
	}
	repositories, err := db.ListRepositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 3 {
		t.Fatalf("repositories=%d", len(repositories))
	}
}

func TestEnsureWorkspaceWithIDUsesExplicitHostedIdentity(t *testing.T) {
	db, err := Open("sqlite", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	workspace, err := db.EnsureWorkspaceWithID(context.Background(), "ws_explicit", "hosted://project", "hosted")
	if err != nil {
		t.Fatal(err)
	}
	if workspace.ID != "ws_explicit" {
		t.Fatalf("workspace id = %q", workspace.ID)
	}
	if _, err := db.EnsureWorkspaceWithID(context.Background(), "ws_other", "hosted://project", "hosted"); err == nil {
		t.Fatal("expected explicit workspace identity conflict")
	}
}

func TestRunsAndArtifactsInheritEpicRepository(t *testing.T) {
	db, err := Open("sqlite", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	workspace, err := db.EnsureWorkspace(ctx, "/tmp/inheritance", "hosted")
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.EnsureRepository(ctx, workspace.ID, "https://github.com/acme/first.git")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.EnsureRepository(ctx, workspace.ID, "https://github.com/acme/second.git")
	if err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpicForRepository(ctx, first.ID, "Scoped", "body")
	if err != nil {
		t.Fatal(err)
	}
	db.Repository = second
	runRecord, err := db.CreateRun(ctx, epic.ID, "", "codex", "model", "high", "railway", 1, false, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := db.CreateArtifact(ctx, "adr", "Scoped", "body", epic.ID, runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runRecord.RepositoryID != first.ID || artifact.RepositoryID != first.ID {
		t.Fatalf("epic=%s run=%s artifact=%s other=%s", first.ID, runRecord.RepositoryID, artifact.RepositoryID, second.ID)
	}
}

func TestDatabaseRejectsCrossWorkspaceRepositoryReference(t *testing.T) {
	db, err := Open("sqlite", "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	first, err := db.EnsureWorkspace(ctx, "/tmp/first-workspace", "hosted")
	if err != nil {
		t.Fatal(err)
	}
	now := Now()
	if _, err := db.Exec(ctx, `INSERT INTO workspaces(id,root_path,profile,created_at,updated_at) VALUES(?,?,?,?,?)`, "ws_second", "/tmp/second-workspace", "hosted", now, now); err != nil {
		t.Fatal(err)
	}
	secondRepository, err := db.EnsureRepository(ctx, "ws_second", "https://github.com/acme/second-workspace.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO epics(id,workspace_id,repository_id,title,body,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, "epic_cross", first.ID, secondRepository.ID, "Cross", "", "draft", now, now); err == nil {
		t.Fatal("cross-workspace repository reference was accepted")
	}
}
