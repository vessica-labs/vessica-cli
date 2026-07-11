package run

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/sandbox"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type previewSandbox struct {
	command string
	url     string
}

func (s *previewSandbox) Create(context.Context, sandbox.CreateOpts) error { return nil }
func (s *previewSandbox) Start(context.Context) error                      { return nil }
func (s *previewSandbox) Exec(context.Context, []string, io.Writer, io.Writer) (int, error) {
	return 0, nil
}
func (s *previewSandbox) Stream(context.Context, io.Writer, io.Writer) error { return nil }
func (s *previewSandbox) ExposePort(context.Context, int) (string, error)    { return s.url, nil }
func (s *previewSandbox) StartPreview(_ context.Context, command string, _ int, _ string) (string, error) {
	s.command = command
	return s.url, nil
}
func (s *previewSandbox) StopPreview(context.Context) error             { return nil }
func (s *previewSandbox) RefreshLease(context.Context, time.Time) error { return nil }
func (s *previewSandbox) PreviewURL() string                            { return s.url }
func (s *previewSandbox) Destroy(context.Context) error                 { return nil }
func (s *previewSandbox) Status(context.Context) (string, error)        { return "running", nil }
func (s *previewSandbox) ID() string                                    { return "sbx_preview" }
func (s *previewSandbox) ContainerID() string                           { return "local" }
func (s *previewSandbox) Workdir() string                               { return "" }

func TestStartPreviewInSandboxUsesWatchedNodeCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".vessica", "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"start":"node server.mjs"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := state.Open("sqlite", "", root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, root, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "Preview", "body")
	if err != nil {
		t.Fatal(err)
	}
	runRecord, err := db.CreateRun(ctx, epic.ID, "", "codex", "model", "high", "local", 1, true, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sandboxRecord, err := db.CreateSandbox(ctx, runRecord.ID, "local", "preview-test")
	if err != nil {
		t.Fatal(err)
	}
	fake := &previewSandbox{url: "http://127.0.0.1:43210"}
	engine := &Engine{DB: db, Root: root, Config: config.Defaults()}
	if err := engine.startPreviewInSandbox(ctx, runRecord, sandboxRecord, fake, root, "code"); err != nil {
		t.Fatal(err)
	}
	if fake.command != "PORT=3000 node --watch-path=. server.mjs" {
		t.Fatalf("command=%q", fake.command)
	}
	stored, err := db.GetRun(ctx, runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PreviewURL != fake.url {
		t.Fatalf("preview_url=%q", stored.PreviewURL)
	}
	events, err := db.ListEvents(ctx, runRecord.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Type != "preview.ready" {
		t.Fatalf("events=%#v", events)
	}
	sandboxRecord.Status = "destroyed"
	sandboxRecord.ContainerID = ""
	if err := db.UpdateSandbox(ctx, sandboxRecord); err != nil {
		t.Fatal(err)
	}
	if err := engine.phasePreview(ctx, stored); err == nil {
		t.Fatal("expected destroyed preview sandbox error")
	}
	cleared, err := db.GetRun(ctx, runRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.PreviewURL != "" {
		t.Fatalf("stale preview URL was not cleared: %q", cleared.PreviewURL)
	}
}
