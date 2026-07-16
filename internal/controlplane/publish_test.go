package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

func TestPublishEpicCreatesHostedGraphAndRealLinearIssues(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(ctx, root, "hosted"); err != nil {
		t.Fatal(err)
	}
	repository, err := db.GetRepository(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertTrackerIntegration(ctx, "linear", "connected", map[string]string{"team": "team_1"}, "", "oauth:linear"); err != nil {
		t.Fatal(err)
	}
	linearServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		switch {
		case strings.Contains(request.Query, "VessicaIssueLabels"):
			_, _ = w.Write([]byte(`{"data":{"issueLabels":{"nodes":[{"id":"label_1","name":"Vessica"}]}}}`))
		case strings.Contains(request.Query, "VessicaSubIssue"):
			_, _ = w.Write([]byte(`{"data":{"issueCreate":{"success":true,"issue":{"id":"child_1","identifier":"ENG-2","title":"Child","url":"https://linear.app/ENG-2","team":{"id":"team_1"},"state":{"id":"todo"}}}}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{"issueCreate":{"success":true,"issue":{"id":"parent_1","identifier":"ENG-1","title":"Epic","url":"https://linear.app/ENG-1","team":{"id":"team_1"},"state":{"id":"todo"}}}}}`))
		}
	}))
	defer linearServer.Close()
	linear := tracker.NewLinearClient("token")
	linear.Endpoint = linearServer.URL
	linear.HTTPClient = linearServer.Client()
	cfg := config.TeamDefaults()
	cfg.Tracker.TeamID = "team_1"
	cfg.Tracker.TodoStateID = "todo"
	cfg.Tracker.TriggerLabel = "Vessica"
	server := &Server{DB: db, Config: cfg, Linear: linear, APIToken: "secret"}
	body := []byte(`{"repository_id":"` + repository.ID + `","spec":{"title":"Epic","body":"Body","tickets":[{"key":"child","title":"Child"}]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/epics", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Idempotency-Key", "publish-1")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"linear_identifier":"ENG-1"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response publishEpicResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Epic == nil || len(response.Tickets) != 1 || len(response.LinearTickets) != 1 {
		t.Fatalf("response=%#v", response)
	}
	start := httptest.NewRequest(http.MethodPost, "/api/v1/epics/"+response.Epic.ID+"/runs", nil)
	start.Header.Set("Authorization", "Bearer secret")
	start.Header.Set("Idempotency-Key", "run-1")
	startRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(startRec, start)
	if startRec.Code != http.StatusAccepted {
		t.Fatalf("start status=%d body=%s", startRec.Code, startRec.Body.String())
	}
}

func TestEpicReadersRequireAndEnforceRepositoryScope(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	workspace, err := db.EnsureWorkspace(ctx, root, "hosted")
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.GetRepository(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.EnsureRepository(ctx, workspace.ID, "https://github.com/acme/second.git")
	if err != nil {
		t.Fatal(err)
	}
	firstEpic, _ := db.CreateEpicForRepository(ctx, first.ID, "First", "")
	_, _ = db.CreateEpicForRepository(ctx, second.ID, "Second", "")
	server := &Server{DB: db, APIToken: "secret"}

	list := httptest.NewRequest(http.MethodGet, "/api/v1/epics?repository_id="+first.ID, nil)
	list.Header.Set("Authorization", "Bearer secret")
	listRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRecorder, list)
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), `"title":"First"`) || strings.Contains(listRecorder.Body.String(), `"title":"Second"`) {
		t.Fatalf("status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}

	wrongRepo := httptest.NewRequest(http.MethodGet, "/api/v1/epics/"+firstEpic.ID+"?repository_id="+second.ID, nil)
	wrongRepo.Header.Set("Authorization", "Bearer secret")
	wrongRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(wrongRecorder, wrongRepo)
	if wrongRecorder.Code != http.StatusNotFound {
		t.Fatalf("cross-repository epic read status=%d body=%s", wrongRecorder.Code, wrongRecorder.Body.String())
	}
}
