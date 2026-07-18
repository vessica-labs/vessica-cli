package controlplane

import (
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

func TestSyncRunToLinearProjectsOrderedArtifactsAndTicketLifecycleComments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.EnsureWorkspace(ctx, root, "hosted"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "Linear projection", "body")
	if err != nil {
		t.Fatal(err)
	}
	runRecord, err := db.CreateRun(ctx, epic.ID, "", "codex", "model", "high", "railway", 1, false, "draft", "", "")
	if err != nil {
		t.Fatal(err)
	}
	integration, err := db.UpsertTrackerIntegration(ctx, "linear", "connected", map[string]string{"team": "team-1"}, "", "oauth:linear")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertExternalMapping(ctx, "linear", "epic", epic.ID, "linear-epic", nil, "synced"); err != nil {
		t.Fatal(err)
	}

	artifacts := map[string]*state.Artifact{}
	for _, spec := range []struct{ typ, title, body string }{
		{"test-scenarios", "Harness tests title", "# Test Scenarios\n\nScenarios."},
		{"design-spec", "Harness design title", "# Design Spec\n\nDesign."},
		{"prd", "Harness PRD title", "# PRD\n\nRequirements."},
		{"adr", "Harness ADR title", "# ADR\n\nDecision."},
	} {
		artifact, createErr := db.CreateArtifact(ctx, spec.typ, spec.title, spec.body, epic.ID, runRecord.ID)
		if createErr != nil {
			t.Fatal(createErr)
		}
		artifacts[spec.typ] = artifact
	}
	ticket, err := db.CreateTicketForRun(ctx, epic.ID, runRecord.ID, "feature", "Implement", "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertExternalMapping(ctx, "linear", "ticket", ticket.ID, "linear-ticket", nil, "synced"); err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct {
		typ     string
		payload map[string]any
	}{
		{"ticket.claimed", map[string]any{"ticket_id": ticket.ID}},
		{"agent.output", map[string]any{"ticket_id": ticket.ID, "message": "Implemented and tested the change."}},
		{"ticket.closed", map[string]any{"ticket_id": ticket.ID, "commit": "abc123", "files": []string{"one.go", "two.go"}}},
		{"ticket.failed", map[string]any{"ticket_id": ticket.ID, "error": "token=super-secret-value example failure"}},
		{"agent.message", map[string]any{"ticket_id": ticket.ID, "message": "intermediate chatter"}},
	} {
		if _, err := db.AppendEvent(ctx, runRecord.ID, "", event.typ, event.payload); err != nil {
			t.Fatal(err)
		}
	}

	server := &Server{DB: db, Config: config.TeamDefaults()}
	if err := server.SyncRunToLinear(ctx, runRecord.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(ctx, `SELECT idempotency_key, payload_json FROM outbox_messages WHERE integration_id=? AND operation='linear.comment' ORDER BY created_at`, integration.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var keys, bodies []string
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			IssueID string `json:"issue_id"`
			Body    string `json:"body"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
		bodies = append(bodies, payload.IssueID+"\n"+payload.Body)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 8 {
		t.Fatalf("comment count=%d keys=%v", len(keys), keys)
	}
	wantArtifactKeys := []string{
		"linear:artifact:" + artifacts["prd"].ID + ":v1",
		"linear:artifact:" + artifacts["adr"].ID + ":v1",
		"linear:artifact:" + artifacts["design-spec"].ID + ":v1",
		"linear:artifact:" + artifacts["test-scenarios"].ID + ":v1",
	}
	for i, want := range wantArtifactKeys {
		if keys[i] != want {
			t.Fatalf("artifact comment %d key=%q want=%q; all=%v", i, keys[i], want, keys)
		}
		marker := "<!-- vessica:artifact:" + strings.TrimPrefix(strings.TrimSuffix(want, ":v1"), "linear:artifact:") + ":v1 -->"
		if strings.HasPrefix(bodies[i], "linear-epic\n## ") || !strings.HasSuffix(bodies[i], marker) {
			t.Fatalf("artifact comment body=%q", bodies[i])
		}
	}
	for i, title := range []string{"Coding started", "Agent summary", "Coding completed", "Coding error"} {
		body := bodies[i+4]
		if !strings.HasPrefix(body, "linear-ticket\n**"+title+"**") {
			t.Fatalf("ticket comment %d=%q", i, body)
		}
	}
	if strings.Contains(bodies[7], "super-secret-value") || !strings.Contains(bodies[7], "[REDACTED]") {
		t.Fatalf("coding error was not redacted: %q", bodies[7])
	}
}

func TestCompleteLinearParentWhenEverySubIssueIsDone(t *testing.T) {
	ctx := context.Background()
	var updatedIssue, updatedState string
	linearServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		switch {
		case strings.Contains(request.Query, "VessicaIssueState"):
			updatedIssue, _ = request.Variables["id"].(string)
			updatedState, _ = request.Variables["stateId"].(string)
			_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":true}}}`))
		case request.Variables["id"] == "child-1":
			_, _ = w.Write([]byte(`{"data":{"issue":{"id":"child-1","parent":{"id":"parent-1"},"state":{"id":"done"}}}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{"issue":{"id":"parent-1","parent":null,"state":{"id":"wip"},"children":{"nodes":[{"id":"child-1","state":{"id":"done"}},{"id":"child-2","state":{"id":"done"}}]}}}}`))
		}
	}))
	t.Cleanup(linearServer.Close)
	linear := tracker.NewLinearClient("token")
	linear.Endpoint = linearServer.URL
	linear.HTTPClient = linearServer.Client()
	cfg := config.TeamDefaults()
	cfg.Tracker.DoneStateID = "done"
	server := &Server{Linear: linear, Config: cfg}
	if err := server.completeLinearParentIfAllChildrenDone(ctx, "child-1"); err != nil {
		t.Fatal(err)
	}
	if updatedIssue != "parent-1" || updatedState != "done" {
		t.Fatalf("updated issue=%q state=%q", updatedIssue, updatedState)
	}
}

func TestParentRemainsOpenWhileAnySubIssueIsNotDone(t *testing.T) {
	ctx := context.Background()
	updates := 0
	linearServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if strings.Contains(request.Query, "VessicaIssueState") {
			updates++
			_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":true}}}`))
			return
		}
		if request.Variables["id"] == "child-1" {
			_, _ = w.Write([]byte(`{"data":{"issue":{"id":"child-1","parent":{"id":"parent-1"},"state":{"id":"done"}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"issue":{"id":"parent-1","state":{"id":"wip"},"children":{"nodes":[{"id":"child-1","state":{"id":"done"}},{"id":"child-2","state":{"id":"wip"}}]}}}}`))
	}))
	t.Cleanup(linearServer.Close)
	linear := tracker.NewLinearClient("token")
	linear.Endpoint = linearServer.URL
	linear.HTTPClient = linearServer.Client()
	cfg := config.TeamDefaults()
	cfg.Tracker.DoneStateID = "done"
	server := &Server{Linear: linear, Config: cfg}
	if err := server.completeLinearParentIfAllChildrenDone(ctx, "child-1"); err != nil {
		t.Fatal(err)
	}
	if updates != 0 {
		t.Fatalf("parent update count=%d", updates)
	}
}
