package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestLocalSessionCSRFAndIdempotentMutation(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.EnsureWorkspace(context.Background(), root, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, _ := db.CreateEpic(context.Background(), "Dashboard", "body")
	runRecord, _ := db.CreateRun(context.Background(), epic.ID, "", "codex", "model", "high", "local", 1, false, "none", "", "")
	server := New(appservice.New(db, root, config.Defaults()), "local")
	server.Origin = "http://127.0.0.1:8765"
	launch := server.IssueLaunchToken()
	handler := server.Handler()
	exchange := httptest.NewRequest(http.MethodPost, "/auth/local/exchange", bytes.NewBufferString(`{"token":"`+launch+`"}`))
	exchange.Header.Set("Content-Type", "application/json")
	exchangeRec := httptest.NewRecorder()
	handler.ServeHTTP(exchangeRec, exchange)
	if exchangeRec.Code != 200 {
		t.Fatalf("exchange=%d %s", exchangeRec.Code, exchangeRec.Body.String())
	}
	var exchangeBody struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(exchangeRec.Body.Bytes(), &exchangeBody)
	cookies := exchangeRec.Result().Cookies()
	if len(cookies) == 0 || exchangeBody.Data.CSRF == "" {
		t.Fatal("session exchange omitted cookie or csrf")
	}
	request := func(csrf, key, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+runRecord.ID+"/cancel", bytes.NewBufferString(`{"confirmed":true}`))
		req.SetPathValue("id", runRecord.ID)
		req.AddCookie(cookies[0])
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := request("", "cancel-once", server.Origin); rec.Code != http.StatusForbidden {
		t.Fatalf("missing csrf=%d", rec.Code)
	}
	if rec := request(exchangeBody.Data.CSRF, "wrong-origin", "https://hostile.example"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong origin=%d", rec.Code)
	}
	if rec := request(exchangeBody.Data.CSRF, "cancel-once", server.Origin); rec.Code != http.StatusOK {
		t.Fatalf("cancel=%d %s", rec.Code, rec.Body.String())
	}
	if rec := request(exchangeBody.Data.CSRF, "cancel-once", server.Origin); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"replayed":true`) {
		t.Fatalf("replay=%d %s", rec.Code, rec.Body.String())
	}
	system := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	system.AddCookie(cookies[0])
	systemRec := httptest.NewRecorder()
	handler.ServeHTTP(systemRec, system)
	if systemRec.Code != 200 || systemRec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("system=%d headers=%v body=%s", systemRec.Code, systemRec.Header(), systemRec.Body.String())
	}
}

func TestRunStreamResumesAfterLastEventAndTerminates(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.EnsureWorkspace(context.Background(), root, "solo"); err != nil {
		t.Fatal(err)
	}
	epic, _ := db.CreateEpic(context.Background(), "Stream", "body")
	runRecord, _ := db.CreateRun(context.Background(), epic.ID, "", "codex", "model", "high", "local", 1, false, "none", "", "")
	first, _ := db.AppendEvent(context.Background(), runRecord.ID, "", "run.started", map[string]any{"message": "first"})
	second, _ := db.AppendEvent(context.Background(), runRecord.ID, "", "run.completed", map[string]any{"token": "ghp_abcdefghijklmnopqrstuvwxyz123456"})
	runRecord.Status = "completed"
	if err = db.UpdateRun(context.Background(), runRecord); err != nil {
		t.Fatal(err)
	}
	server := New(appservice.New(db, root, config.Defaults()), "local")
	server.ServiceToken = "stream-token"
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	req, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/api/v1/runs/"+runRecord.ID+"/stream", nil)
	req.Header.Set("Authorization", "Bearer stream-token")
	req.Header.Set("Last-Event-ID", fmt.Sprint(first.Seq))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, fmt.Sprintf("id: %d\n", first.Seq)) || !strings.Contains(body, fmt.Sprintf("id: %d\n", second.Seq)) {
		t.Fatalf("stream did not resume after Last-Event-ID: %s", body)
	}
	if !strings.Contains(body, "event: result") || strings.Contains(body, "ghp_abcdefghijklmnopqrstuvwxyz123456") {
		t.Fatalf("stream omitted terminal result or leaked secret: %s", body)
	}
}

func TestLocalLaunchRequiresLoopbackCLIHeader(t *testing.T) {
	root := t.TempDir()
	db, _ := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	defer db.Close()
	_, _ = db.EnsureWorkspace(context.Background(), root, "solo")
	handler := New(appservice.New(db, root, config.Defaults()), "local").Handler()
	req := httptest.NewRequest(http.MethodPost, "/auth/local/launch", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("launch without header=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/auth/local/launch", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Vessica-CLI", "1")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "launch_token") {
		t.Fatalf("launch=%d %s", rec.Code, rec.Body.String())
	}
}
