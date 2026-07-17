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
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "run_hosted", "status": "running", "current_phase": "code", "receipt_id": "rcpt_hosted"})
		case "/api/v1/runs/run_hosted/artifacts":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "art_hosted", "run_id": "run_hosted", "kind": "prd"}})
		case "/api/v1/runs/run_hosted/events":
			if r.URL.Query().Get("after") != "4" {
				http.Error(w, "missing cursor", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "evt_hosted", "run_id": "run_hosted", "seq": 5, "type": "run.phase.started", "created_at": "2026-07-11T00:00:00Z"}})
		case "/api/v1/receipts/rcpt_hosted":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rcpt_hosted", "preview_url": "https://preview.example"})
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
	artifacts, err := app.getHostedRunArtifacts(context.Background(), "run_hosted")
	if err != nil || len(artifacts) != 1 || artifacts[0].ID != "art_hosted" {
		t.Fatalf("artifacts=%#v err=%v", artifacts, err)
	}
	view, err := app.getHostedReceipt(context.Background(), "rcpt_hosted")
	if err != nil || view["id"] != "rcpt_hosted" {
		t.Fatalf("receipt=%#v err=%v", view, err)
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
		case "/api/v1/runs/run_hosted":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "run_hosted", "repository_id": "repo_hosted", "status": "completed", "receipt_id": "rcpt_hosted"})
		case "/api/v1/runs/run_hosted/artifacts":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "art_hosted", "run_id": "run_hosted", "kind": "prd"}})
		case "/api/v1/receipts/rcpt_hosted":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rcpt_hosted", "preview_url": "https://preview.example"})
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

	for _, args := range [][]string{{"status", "--json"}, {"epic", "list", "--json"}, {"run", "list", "--json"}, {"run", "artifacts", "run_hosted", "--json"}, {"run", "receipt", "run_hosted", "--json"}} {
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

func TestHostedLifecycleCommandsNeverOpenLocalRunEngine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VES_AUTH_STORE", "file")
	var mutations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/runs/run_hosted/resume" || r.URL.Path == "/api/v1/runs/run_hosted/cancel":
			if r.Header.Get("Idempotency-Key") == "" {
				http.Error(w, "missing key", http.StatusBadRequest)
				return
			}
			mutations = append(mutations, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]any{"run": map[string]any{"id": "run_hosted", "status": "pending"}})
		case r.URL.Path == "/api/v1/epics/epic_hosted/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"epic": map[string]any{"id": "epic_hosted"}, "tickets": 1, "ready": 1})
		case r.URL.Path == "/api/v1/sandboxes":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "sandbox_hosted", "backend": "railway", "status": "running"}})
		case r.URL.Path == "/api/v1/sandboxes/sandbox_hosted/logs":
			_ = json.NewEncoder(w).Encode(map[string]string{"logs": "sanitized"})
		case r.URL.Path == "/api/v1/sandboxes/sandbox_hosted":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "sandbox_hosted", "backend": "railway", "status": "running"})
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
	commands := [][]string{
		{"run", "resume", "run_hosted", "--from", "validate", "--no-stream", "--yes", "--json"},
		{"run", "cancel", "run_hosted", "--yes", "--json"},
		{"epic", "status", "epic_hosted", "--json"},
		{"sandbox", "list", "--json"},
		{"sandbox", "view", "sandbox_hosted", "--json"},
		{"sandbox", "logs", "sandbox_hosted", "--json"},
	}
	for _, args := range commands {
		if output := runCLI(t, root, args...); !strings.Contains(output, `"ok":true`) {
			t.Fatalf("ves %v output=%s", args, output)
		}
	}
	if len(mutations) != 2 {
		t.Fatalf("mutations=%v", mutations)
	}
	if _, err := os.Stat(config.SQLitePath(root)); !os.IsNotExist(err) {
		t.Fatalf("hosted lifecycle opened local state: %v", err)
	}
}
