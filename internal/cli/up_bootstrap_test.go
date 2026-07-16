package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

func TestHostedBootstrapCreatesNoRepositoryLocalRuntimeState(t *testing.T) {
	root := t.TempDir()
	app := &App{Root: root}
	remote := "https://github.com/acme/service.git"

	if err := ensureHostedBootstrap(context.Background(), app, remote); err != nil {
		t.Fatal(err)
	}
	if app.DB != nil {
		t.Fatal("hosted bootstrap opened a local datastore")
	}
	if app.Config.State.Backend != "hosted" || app.Config.Knowledge.Mode != "hosted" {
		t.Fatalf("hosted bootstrap config = %#v", app.Config)
	}
	if app.Config.Repo.Remote != remote {
		t.Fatalf("remote = %q", app.Config.Repo.Remote)
	}

	for _, path := range []string{
		config.SQLitePath(root),
		filepath.Join(root, config.DirName, "cache"),
		filepath.Join(root, config.DirName, "runs"),
		filepath.Join(root, config.DirName, "sandboxes"),
		filepath.Join(root, config.DirName, "secrets"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("hosted bootstrap created repository-local runtime path %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("hosted bootstrap changed .gitignore: %v", err)
	}
}

func TestHostedAttachmentLoadDoesNotOpenLocalDatastore(t *testing.T) {
	root := t.TempDir()
	cfg := config.HostedDefaults()
	cfg.Attachment = config.AttachmentConfig{WorkspaceID: "ws_test", RepositoryID: "repo_test"}
	cfg.Hosted.ControlPlaneURL = "https://control.example"
	cfg.Repo.Remote = "https://github.com/acme/service.git"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	app := &App{Root: root}
	if err := app.loadWorkspaceWithoutGC(context.Background()); err != nil {
		t.Fatal(err)
	}
	if app.DB != nil {
		t.Fatal("hosted attachment load opened a local datastore")
	}
	if app.Config.State.Backend != "hosted" || app.Config.Knowledge.Mode != "hosted" {
		t.Fatalf("hosted attachment loaded local authority: %#v", app.Config)
	}
	for _, path := range []string{config.SQLitePath(root), filepath.Join(root, config.DirName, "state", "knowledge.db")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("hosted attachment load created local state %s: %v", path, err)
		}
	}
}
