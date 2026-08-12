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
	"time"

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

	consent := httptest.NewRequest(http.MethodPost, "/oauth/authorize", nil)
	consent.AddCookie(cookies[0])
	consent.Header.Set("X-CSRF-Token", exchangeBody.Data.CSRF)
	consent.Header.Set("Origin", server.Origin)
	identity, err := server.AuthorizeExternalRequest(consent, true)
	if err != nil || identity.UserID == "" || identity.Role != "owner" {
		t.Fatalf("external consent identity=%#v err=%v", identity, err)
	}
	consent.Header.Del("X-CSRF-Token")
	if _, err = server.AuthorizeExternalRequest(consent, true); err == nil {
		t.Fatal("external consent accepted a mutation without CSRF")
	}
}

func TestExternalIdentityRequiresCurrentWorkspaceMembershipAndRole(t *testing.T) {
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
	user, _ := first.UpsertDashboardUser(context.Background(), "github-1", "user", "User", "")
	_ = first.UpsertMembership(context.Background(), user.ID, "owner")
	rawSession := "session-one"
	_, _ = first.CreateDashboardSession(context.Background(), user.ID, "owner", digest(rawSession), digest("csrf"), state.FormatTime(time.Now().Add(time.Hour)))

	second, err := state.Open("sqlite", dbPath, root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err = second.EnsureWorkspace(context.Background(), "workspace-two", "hosted"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: rawSession})
	server := New(appservice.New(second, root, config.Defaults()), "hosted")
	if _, err = server.AuthorizeExternalRequest(request, false); err == nil {
		t.Fatal("dashboard session crossed workspace boundary")
	}

	secondSession := "session-two"
	_ = second.UpsertMembership(context.Background(), user.ID, "owner")
	_, _ = second.CreateDashboardSession(context.Background(), user.ID, "owner", digest(secondSession), digest("csrf"), state.FormatTime(time.Now().Add(time.Hour)))
	_ = second.UpsertMembership(context.Background(), user.ID, "member")
	request = httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: secondSession})
	identity, err := server.AuthorizeExternalRequest(request, false)
	if err != nil || identity.Role != "member" || identity.WorkspaceID != second.Workspace.ID {
		t.Fatalf("identity did not use current membership: %#v err=%v", identity, err)
	}
}

func TestCookieRoutesRejectOwnerSessionFromAnotherWorkspaceAndRevalidateRole(t *testing.T) {
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
	user, _ := first.UpsertDashboardUser(context.Background(), "github-owner", "owner", "Owner", "")
	if err = first.UpsertMembership(context.Background(), user.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	const rawSession = "workspace-one-owner-session"
	if _, err = first.CreateDashboardSession(context.Background(), user.ID, "owner", digest(rawSession), digest("csrf"), state.FormatTime(time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}

	second, err := state.Open("sqlite", dbPath, root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err = second.EnsureWorkspace(context.Background(), "workspace-two", "hosted"); err != nil {
		t.Fatal(err)
	}
	server := New(appservice.New(second, root, config.Defaults()), "hosted")
	for _, route := range []string{"/auth/session", "/api/v1/system", "/api/v1/conversations", "/api/v1/operator", "/internal/dashboard/metrics"} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: rawSession})
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("cross-workspace cookie route %s=%d %s", route, rec.Code, rec.Body.String())
		}
	}

	const currentSession = "workspace-two-session"
	if err = second.UpsertMembership(context.Background(), user.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err = second.CreateDashboardSession(context.Background(), user.ID, "owner", digest(currentSession), digest("csrf"), state.FormatTime(time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err = second.UpsertMembership(context.Background(), user.ID, "member"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/operator", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: currentSession})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stale owner role was trusted: %d %s", rec.Code, rec.Body.String())
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
