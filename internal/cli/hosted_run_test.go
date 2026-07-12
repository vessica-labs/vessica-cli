package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

func TestHostedRunReadersUseAuthenticatedControlPlaneAPI(t *testing.T) {
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
