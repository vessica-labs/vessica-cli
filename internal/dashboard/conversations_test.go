package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestConversationRoutesAuthorizeOrderRunAndIsolateWorkspace(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "state.db")
	first, err := state.Open("sqlite", dbPath, root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err = first.EnsureWorkspace(context.Background(), "workspace-one", "hosted"); err != nil {
		t.Fatal(err)
	}
	agent, err := first.CreateAgent(context.Background(), "COS", "Chief of staff", `{"kind":"vessica.agent/v1","name":"COS","purpose":"Chief of staff","system_prompt":"Help","model":{"id":"gpt-5.6-terra","reasoning_effort":"medium"},"budget":{"daily_usd":"5.00","timezone":"UTC"}}`, `{}`, 5_000_000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	server := New(appservice.New(first, root, config.Defaults()), "hosted")
	server.ServiceToken = "service-token"
	handler := server.Handler()

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized conversations=%d %s", unauthorized.Code, unauthorized.Body.String())
	}

	request := func(method, path, body, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer service-token")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	created := request(http.MethodPost, "/api/v1/conversations", `{"title":"Morning priorities","agent_id":"`+agent.ID+`"}`, "conversation-create")
	if created.Code != http.StatusOK {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var createBody struct {
		Data struct {
			ID      string `json:"id"`
			AgentID string `json:"agent_id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	if createBody.Data.AgentID != agent.ID {
		t.Fatalf("selected agent=%q", createBody.Data.AgentID)
	}
	conversationID := createBody.Data.ID
	for index, message := range []string{"First", "Second"} {
		path := "/api/v1/conversations/" + conversationID + "/messages"
		rec := request(http.MethodPost, path, `{"message":"`+message+`"}`, "message-"+message)
		if rec.Code != http.StatusOK {
			t.Fatalf("message %d=%d %s", index, rec.Code, rec.Body.String())
		}
	}
	detail := request(http.MethodGet, "/api/v1/conversations/"+conversationID, "", "")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail=%d %s", detail.Code, detail.Body.String())
	}
	var detailBody struct {
		Data struct {
			Messages []struct {
				Sequence int64          `json:"sequence"`
				Metadata map[string]any `json:"metadata"`
			} `json:"messages"`
			Runs []state.AgentRun `json:"runs"`
		} `json:"data"`
	}
	if err = json.Unmarshal(detail.Body.Bytes(), &detailBody); err != nil {
		t.Fatal(err)
	}
	if len(detailBody.Data.Messages) != 2 || detailBody.Data.Messages[0].Sequence != 1 || detailBody.Data.Messages[1].Sequence != 2 {
		t.Fatalf("messages are not ordered: %#v", detailBody.Data.Messages)
	}
	if len(detailBody.Data.Runs) != 2 || detailBody.Data.Runs[0].Status != "queued" {
		t.Fatalf("run status missing: %#v", detailBody.Data.Runs)
	}
	if detailBody.Data.Messages[0].Metadata["run_id"] != detailBody.Data.Runs[0].ID {
		t.Fatalf("message does not link run: %#v", detailBody.Data.Messages[0])
	}

	second, err := state.Open("sqlite", dbPath, root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err = second.EnsureWorkspace(context.Background(), "workspace-two", "hosted"); err != nil {
		t.Fatal(err)
	}
	isolated := New(appservice.New(second, root, config.Defaults()), "hosted")
	isolated.ServiceToken = "service-token"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID, nil)
	req.Header.Set("Authorization", "Bearer service-token")
	rec := httptest.NewRecorder()
	isolated.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "First") {
		t.Fatalf("conversation crossed workspace boundary: %d %s", rec.Code, rec.Body.String())
	}
}
