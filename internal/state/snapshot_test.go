package state

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWorkplanSnapshotMapsWorkspaceAndExpiresSandboxes(t *testing.T) {
	ctx := context.Background()
	sourceRoot := t.TempDir()
	source, err := Open("sqlite", filepath.Join(sourceRoot, "source.db"), sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	sourceWS, _ := source.EnsureWorkspace(ctx, sourceRoot, "solo")
	epic, _ := source.CreateEpic(ctx, "Snapshot", "body")
	runRecord, _ := source.CreateRun(ctx, epic.ID, "", "codex", "model", "high", "docker", 1, true, "draft", "", "")
	sandbox, _ := source.CreateSandbox(ctx, runRecord.ID, "docker", "branch")
	sandbox.ContainerID = "container"
	sandbox.Status = "running"
	_ = source.UpdateSandbox(ctx, sandbox)
	_, _ = source.AppendEvent(ctx, runRecord.ID, sandbox.ID, "run.started", map[string]any{"ok": true})
	snap, err := source.ExportWorkplanSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := t.TempDir()
	target, err := Open("sqlite", filepath.Join(targetRoot, "target.db"), targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	targetWS, _ := target.EnsureWorkspace(ctx, targetRoot, "hosted")
	if err = target.ImportWorkplanSnapshot(ctx, snap); err != nil {
		t.Fatal(err)
	}
	got, err := target.GetRun(ctx, runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != targetWS.ID || got.WorkspaceID == sourceWS.ID {
		t.Fatalf("workspace mapping=%s", got.WorkspaceID)
	}
	gotSandbox, err := target.GetSandbox(ctx, sandbox.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSandbox.Status != "expired" || gotSandbox.ContainerID != "" {
		t.Fatalf("sandbox imported active: %#v", gotSandbox)
	}
}
