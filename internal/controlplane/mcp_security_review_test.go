package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestOAuthTokenAndRevokeRejectCredentialQueryParameters(t *testing.T) {
	server, _ := mcpTestServer(t)
	for _, target := range []string{
		"/oauth/token?grant_type=refresh_token&refresh_token=vmr_query&client_id=client&resource=https%3A%2F%2Fvessica.example%2Fmcp",
		"/oauth/revoke?token=vmr_query",
	} {
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("query credential target=%s status=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}

func TestOAuthRefreshRevocationEndpointRevokesWholeFamily(t *testing.T) {
	server, db := mcpTestServer(t)
	ctx := context.Background()
	resource := "https://vessica.example/mcp"
	_, _ = server.agentApp().RegisterOAuthClient(ctx, state.OAuthClientInput{ClientID: "family-client", Name: "Family"})
	refreshRaw, accessRaw := "vmr_"+strings.Repeat("f", 48), "vma_"+strings.Repeat("a", 48)
	if _, err := db.IssueOAuthRefreshToken(ctx, state.OAuthRefreshTokenInput{ClientID: "family-client", ActorID: "user", MaterialHash: hashOAuthMaterial(refreshRaw), FamilyID: "family", Resource: resource, ExpiresAt: state.FormatTime(time.Now().Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IssueOAuthAccessToken(ctx, state.OAuthAccessTokenInput{ClientID: "family-client", ActorID: "user", TokenHash: hashOAuthMaterial(accessRaw), FamilyID: "family", Resource: resource, ExpiresAt: state.FormatTime(time.Now().Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	rec := formRequest(server.Handler(), http.MethodPost, "/oauth/revoke", url.Values{"token": {refreshRaw}})
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := db.GetOAuthRefreshToken(ctx, hashOAuthMaterial(refreshRaw), resource); err == nil {
		t.Fatal("revoked refresh token remained valid")
	}
	if _, err := db.GetOAuthAccessToken(ctx, hashOAuthMaterial(accessRaw), resource); err == nil {
		t.Fatal("family access token remained valid")
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
	schemaDenial := request(goodToken, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"scheduled_write_probe","arguments":{"idempotency_key":2}}}`)
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Error MCPToolError `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(schemaDenial.Body.Bytes(), &response); err != nil || !response.Result.IsError || response.Result.StructuredContent.Error.Code != "invalid_arguments" || response.Result.StructuredContent.Error.Retryable {
		t.Fatalf("schema denial is not stable structured content: err=%v body=%s", err, schemaDenial.Body.String())
	}
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

func TestMCPAgentReadsCannotEnumerateOrDirectlyAccessForeignWorkspace(t *testing.T) {
	server, db := mcpTestServer(t)
	ctx := context.Background()
	local, err := db.CreateAgent(ctx, "LOCAL", "local", `{}`, `{}`, 1, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	localTrigger, _ := db.TriggerAgentRun(ctx, state.AgentRunTriggerInput{AgentID: local.ID, IdempotencyKey: "local-run", Trigger: "mcp"})
	foreign, err := state.Open("sqlite", filepath.Join(db.Root, "state.db"), db.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	_, _ = foreign.EnsureWorkspace(ctx, "foreign", "hosted")
	foreignAgent, _ := foreign.CreateAgent(ctx, "FOREIGN", "foreign", `{}`, `{}`, 1, "UTC")
	foreignTrigger, _ := foreign.TriggerAgentRun(ctx, state.AgentRunTriggerInput{AgentID: foreignAgent.ID, IdempotencyKey: "foreign-run", Trigger: "mcp"})
	session := authorizedMCPSession(t, server, MCPScopes())
	for name, arguments := range map[string]map[string]any{
		"agents_list": {}, "agent_runs_list": {},
	} {
		result := callMCPTool(t, session, name, arguments)
		encoded, _ := json.Marshal(result.StructuredContent)
		if strings.Contains(string(encoded), foreignAgent.ID) || strings.Contains(string(encoded), "FOREIGN") || strings.Contains(string(encoded), foreignTrigger.AgentRunID) {
			t.Fatalf("%s enumerated foreign workspace: %s", name, encoded)
		}
		if !strings.Contains(string(encoded), local.ID) && !strings.Contains(string(encoded), localTrigger.AgentRunID) {
			t.Fatalf("%s omitted local data: %s", name, encoded)
		}
	}
	for name, arguments := range map[string]map[string]any{
		"agent_get": {"agent_id": foreignAgent.ID}, "agent_run_get": {"run_id": foreignTrigger.AgentRunID},
	} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil || !result.IsError {
			t.Fatalf("foreign %s result=%#v err=%v", name, result, err)
		}
	}
	foreignRuns := callMCPTool(t, session, "agent_runs_list", map[string]any{"agent_id": foreignAgent.ID})
	encodedForeignRuns, _ := json.Marshal(foreignRuns.StructuredContent)
	if strings.Contains(string(encodedForeignRuns), foreignTrigger.AgentRunID) {
		t.Fatalf("foreign agent filter enumerated runs: %s", encodedForeignRuns)
	}
}
