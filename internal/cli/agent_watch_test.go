package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestWatchAgentRunDrainsEventsCommittedBeforeTerminalResult(t *testing.T) {
	eventRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/agent-runs/arun_fast/events":
			eventRequests++
			events := []state.AgentRunEvent{}
			if eventRequests == 1 {
				events = append(events, state.AgentRunEvent{ID: "evt_1", RunID: "arun_fast", Seq: 1, Type: "agent.run.started", PayloadJSON: `{}`, CreatedAt: "2026-07-21T00:00:00Z"})
			} else if eventRequests == 2 {
				events = append(events, state.AgentRunEvent{ID: "evt_2", RunID: "arun_fast", Seq: 2, Type: "agent.message.completed", PayloadJSON: `{"text":"done"}`, CreatedAt: "2026-07-21T00:00:01Z"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"events": events})
		case "/api/v1/agent-runs/arun_fast":
			_ = json.NewEncoder(w).Encode(map[string]any{"run": state.AgentRun{ID: "arun_fast", Status: "completed"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{Config: config.Config{Hosted: config.HostedConfig{ControlPlaneURL: server.URL}}}
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = originalStdout }()

	if err := watchAgentRun(context.Background(), app, "test-token", "arun_fast", 0, true); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	var output bytes.Buffer
	_, _ = io.Copy(&output, reader)
	_ = reader.Close()

	text := output.String()
	messageIndex := strings.Index(text, `"seq":2`)
	resultIndex := strings.Index(text, `"kind":"result"`)
	if eventRequests != 3 || messageIndex < 0 || resultIndex < 0 || messageIndex > resultIndex {
		t.Fatalf("event requests=%d output=%s", eventRequests, text)
	}
}
