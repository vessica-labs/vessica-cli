package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestOAuthRequiresCanonicalHTTPSResourceAndFormContentType(t *testing.T) {
	server, _ := mcpTestServer(t)
	handler := server.Handler()
	resource := "https://vessica.example/mcp"
	client, err := server.agentApp().RegisterOAuthClient(context.Background(), state.OAuthClientInput{
		ClientID: "resource-client", Name: "Resource client", RedirectURIsJSON: `["https://client.example/callback"]`, ScopesJSON: `["agents:read"]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "scope": {"agents:read"}, "code_challenge_method": {"S256"}, "code_challenge": {strings.Repeat("a", 43)}, "consent": {"approve"}}
	if rec := formRequest(handler, http.MethodPost, "/oauth/authorize", values); rec.Code != http.StatusBadRequest {
		t.Fatalf("authorize without resource=%d %s", rec.Code, rec.Body.String())
	}
	values.Set("resource", "https://other.example/mcp")
	if rec := formRequest(handler, http.MethodPost, "/oauth/authorize", values); rec.Code != http.StatusBadRequest {
		t.Fatalf("authorize wrong resource=%d %s", rec.Code, rec.Body.String())
	}
	values.Set("resource", resource)
	if rec := formRequest(handler, http.MethodPost, "/oauth/authorize", values); rec.Code != http.StatusSeeOther {
		t.Fatalf("authorize canonical resource=%d %s", rec.Code, rec.Body.String())
	}
	jsonToken := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(`{"grant_type":"refresh_token"}`))
	jsonToken.Header.Set("Content-Type", "application/json")
	jsonRec := httptest.NewRecorder()
	handler.ServeHTTP(jsonRec, jsonToken)
	if jsonRec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("JSON token content type=%d %s", jsonRec.Code, jsonRec.Body.String())
	}
	jsonRevoke := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(`{"token":"x"}`))
	jsonRevoke.Header.Set("Content-Type", "application/json")
	jsonRevokeRec := httptest.NewRecorder()
	handler.ServeHTTP(jsonRevokeRec, jsonRevoke)
	if jsonRevokeRec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("JSON revoke content type=%d %s", jsonRevokeRec.Code, jsonRevokeRec.Body.String())
	}
}

func TestMCPRejectsInvalidConfigurationAudienceAndBrowserOrigin(t *testing.T) {
	server, db := mcpTestServer(t)
	rawToken := "vma_" + strings.Repeat("z", 48)
	_, _ = server.agentApp().RegisterOAuthClient(context.Background(), state.OAuthClientInput{ClientID: "client", Name: "Client"})
	_, _ = db.IssueOAuthAccessToken(context.Background(), state.OAuthAccessTokenInput{ClientID: "client", ActorID: "user", TokenHash: hashOAuthMaterial(rawToken), Resource: "https://other.example/mcp", ExpiresAt: state.FormatTime(time.Now().Add(time.Hour))})
	request := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
		req.Header.Set("Authorization", "Bearer "+rawToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := request(""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong audience=%d %s", rec.Code, rec.Body.String())
	}
	if rec := request("https://hostile.example"); rec.Code != http.StatusForbidden {
		t.Fatalf("hostile origin=%d %s", rec.Code, rec.Body.String())
	}
	server.MCPPublicURL = "http://vessica.example"
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("insecure public URL discovery=%d %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthConsentRejectsDashboardActorFromAnotherWorkspace(t *testing.T) {
	server, db := mcpTestServer(t)
	server.MCPDashboardIdentity = func(_ *http.Request, _ bool) (MCPDashboardActor, error) {
		return MCPDashboardActor{WorkspaceID: "foreign", UserID: "user", Role: "owner"}, nil
	}
	_, _ = server.agentApp().RegisterOAuthClient(context.Background(), state.OAuthClientInput{ClientID: "client", Name: "Client", RedirectURIsJSON: `["https://client.example/callback"]`, ScopesJSON: `["agents:read"]`})
	values := url.Values{"response_type": {"code"}, "client_id": {"client"}, "redirect_uri": {"https://client.example/callback"}, "scope": {"agents:read"}, "resource": {"https://vessica.example/mcp"}, "code_challenge_method": {"S256"}, "code_challenge": {strings.Repeat("a", 43)}, "consent": {"approve"}}
	if rec := formRequest(server.Handler(), http.MethodPost, "/oauth/authorize", values); rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign consent=%d %s workspace=%s", rec.Code, rec.Body.String(), db.Workspace.ID)
	}
}

func TestFeatureOffPreservesDashboardFallback(t *testing.T) {
	server, _ := mcpTestServer(t)
	server.MCPEnabled = false
	server.Dashboard = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("feature-off dashboard fallback=%d", rec.Code)
	}
}

func TestMCPMalformedAuthorizationScopeAndUnknownToolAreAudited(t *testing.T) {
	server, db := mcpTestServer(t)
	ctx := context.Background()
	_, _ = server.agentApp().RegisterOAuthClient(ctx, state.OAuthClientInput{ClientID: "audit-client", Name: "Audit"})
	badScopeToken := "vma_" + strings.Repeat("q", 48)
	if _, err := db.IssueOAuthAccessToken(ctx, state.OAuthAccessTokenInput{ClientID: "audit-client", ActorID: "user_1", TokenHash: hashOAuthMaterial(badScopeToken), Resource: "https://vessica.example/mcp", ScopesJSON: `{`, ExpiresAt: state.FormatTime(time.Now().Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	request := func(token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		return rec
	}
	request(badScopeToken, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	request("vma_invalid", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"secret_tool","arguments":{"idempotency_key":"do-not-store"}}}`)
	goodToken := "vma_" + strings.Repeat("r", 48)
	if _, err := db.IssueOAuthAccessToken(ctx, state.OAuthAccessTokenInput{ClientID: "audit-client", ActorID: "user_1", TokenHash: hashOAuthMaterial(goodToken), Resource: "https://vessica.example/mcp", ScopesJSON: `[]`, ExpiresAt: state.FormatTime(time.Now().Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	request(goodToken, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"secret_tool","arguments":{"idempotency_key":"do-not-store"}}}`)
	request(goodToken, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"scheduled_write_probe","arguments":{"idempotency_key":2}}}`)
	var denials, leaked int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM action_ledger WHERE policy_decision='denied'`).Scan(&denials); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM action_ledger WHERE redacted_arguments_json LIKE '%do-not-store%'`).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if denials < 4 || leaked != 0 {
		t.Fatalf("denials=%d raw-key-leaks=%d", denials, leaked)
	}
}
