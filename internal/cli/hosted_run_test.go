package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

func TestHostedRunReadersUseAuthenticatedControlPlaneAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VES_AUTH_STORE", "file")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/runs":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "run_hosted", "status": "running"}})
		case "/api/v1/runs/run_hosted":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "run_hosted", "status": "running", "current_phase": "code"})
		case "/api/v1/runs/run_hosted/events":
			if r.URL.Query().Get("after") != "4" {
				http.Error(w, "missing cursor", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "evt_hosted", "run_id": "run_hosted", "seq": 5, "type": "run.phase.started", "created_at": "2026-07-11T00:00:00Z"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	if err := saveRailwaySecrets(root, railwaySecrets{APIToken: "test-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".vessica", "secrets", "railway.json")); !os.IsNotExist(err) {
		t.Fatalf("Railway credential was written inside the repository: %v", err)
	}
	app := &App{Root: root, Config: config.Config{Hosted: config.HostedConfig{ControlPlaneURL: server.URL}}}
	runs, err := app.listHostedRuns(context.Background())
	if err != nil || len(runs) != 1 || runs[0].ID != "run_hosted" {
		t.Fatalf("runs=%#v err=%v", runs, err)
	}
	runRecord, err := app.getHostedRun(context.Background(), "run_hosted")
	if err != nil || runRecord.CurrentPhase != "code" {
		t.Fatalf("run=%#v err=%v", runRecord, err)
	}
	events, err := app.listHostedRunEvents(context.Background(), "run_hosted", 4)
	if err != nil || len(events) != 1 || events[0].Seq != 5 {
		t.Fatalf("events=%#v err=%v", events, err)
	}
}

func TestHostedCommandsUseControlPlaneWithoutCreatingLocalState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VES_AUTH_STORE", "file")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"workspace_id": "ws_hosted", "healthy": true})
		case "/api/v1/epics":
			if r.URL.Query().Get("repository_id") != "repo_hosted" {
				http.Error(w, "missing repository scope", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "epic_hosted", "repository_id": "repo_hosted", "title": "Hosted"}})
		case "/api/v1/runs":
			if r.URL.Query().Get("repository_id") != "repo_hosted" {
				http.Error(w, "missing repository scope", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "run_hosted", "repository_id": "repo_hosted", "status": "running"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	cfg := config.HostedDefaults()
	cfg.Attachment = config.AttachmentConfig{WorkspaceID: "ws_hosted", RepositoryID: "repo_hosted"}
	cfg.Hosted.ControlPlaneURL = server.URL
	cfg.Repo.Remote = "https://github.com/acme/service.git"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := saveRailwaySecrets(root, railwaySecrets{APIToken: "test-token"}); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"status", "--json"}, {"epic", "list", "--json"}, {"run", "list", "--json"}} {
		if output := runCLI(t, root, args...); !strings.Contains(output, `"ok":true`) {
			t.Fatalf("ves %v output=%s", args, output)
		}
	}
	for _, path := range []string{config.SQLitePath(root), filepath.Join(root, ".vessica", "runs"), filepath.Join(root, ".vessica", "sandboxes"), filepath.Join(root, ".vessica", "secrets")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("hosted command created repository-local runtime path %s: %v", path, err)
		}
	}
}
