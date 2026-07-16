package controlplane

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestLinearWebhookIsAuthenticatedDurableAndDeduplicated(t *testing.T) {
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
	if _, err := db.UpsertTrackerIntegration(ctx, "linear", "connected", map[string]string{"team": "team-1"}, "", "secret"); err != nil {
		t.Fatal(err)
	}
	server := &Server{DB: db, Config: config.TeamDefaults(), LinearWebhookSecret: "signing-secret"}
	body, _ := json.Marshal(map[string]any{
		"action": "create", "type": "Issue", "webhookTimestamp": time.Now().UnixMilli(),
		"data": map[string]string{"id": "issue-1"},
	})
	signature := hmac.New(sha256.New, []byte("signing-secret"))
	_, _ = signature.Write(body)
	send := func(sig string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/linear", bytes.NewReader(body))
		req.Header.Set("Linear-Signature", sig)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		return rec
	}
	if got := send("bad").Code; got != http.StatusUnauthorized {
		t.Fatalf("invalid signature status=%d", got)
	}
	first := send(hex.EncodeToString(signature.Sum(nil)))
	if first.Code != http.StatusOK || !bytes.Contains(first.Body.Bytes(), []byte(`"duplicate":false`)) {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := send(hex.EncodeToString(signature.Sum(nil)))
	if second.Code != http.StatusOK || !bytes.Contains(second.Body.Bytes(), []byte(`"duplicate":true`)) {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	jobs, err := db.ListJobs(ctx, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%#v err=%v", jobs, err)
	}
}

func TestControlPlaneAPIRequiresBearerToken(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(context.Background(), root, "hosted"); err != nil {
		t.Fatal(err)
	}
	server := &Server{DB: db, APIToken: "api-secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
	req.Header.Set("Authorization", "Bearer api-secret")
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServiceIssuesSeparateUserScopedCLICredential(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(context.Background(), root, "hosted"); err != nil {
		t.Fatal(err)
	}
	handler := (&Server{DB: db, APIToken: "internal-service-token"}).Handler()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cli-credentials", bytes.NewBufferString(`{"subject":"octocat"}`))
	request.Header.Set("Authorization", "Bearer internal-service-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("issue status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var credential struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &credential); err != nil || credential.Token == "" || credential.Token == "internal-service-token" {
		t.Fatalf("credential=%#v err=%v", credential, err)
	}
	var stored string
	if err := db.QueryRow(context.Background(), `SELECT token_hash FROM cli_credentials`).Scan(&stored); err != nil || stored == credential.Token {
		t.Fatalf("CLI credential was not stored as a hash: stored=%q err=%v", stored, err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("user credential status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRunTerminalStatus(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled", "stopped"} {
		if !runTerminalStatus(status) {
			t.Fatalf("%s should be terminal", status)
		}
	}
	if runTerminalStatus("running") || runTerminalStatus("pending") {
		t.Fatal("active run status reported terminal")
	}
}
