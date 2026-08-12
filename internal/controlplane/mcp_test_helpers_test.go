package controlplane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func formRequest(handler http.Handler, method, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func oauthConsentRequest(handler http.Handler, values url.Values, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", origin)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func structuredHasCode(value any, want string) bool {
	raw, _ := json.Marshal(value)
	var parsed struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &parsed)
	return parsed.Error != nil && parsed.Error.Code == want
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func authorizedMCPSession(t *testing.T, server *Server, scopes []string) *mcp.ClientSession {
	t.Helper()
	rawToken := "vma_" + strings.Repeat("s", 48)
	if _, err := server.agentApp().RegisterOAuthClient(context.Background(), state.OAuthClientInput{ClientID: "client-tools", Name: "MCP tools"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.DB.IssueOAuthAccessToken(context.Background(), state.OAuthAccessTokenInput{
		ClientID: "client-tools", ActorID: "user_1", TokenHash: hashOAuthMaterial(rawToken),
		Resource: "https://vessica.example/mcp", ScopesJSON: mustJSON(scopes), ExpiresAt: state.FormatTime(time.Now().Add(time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	client := &http.Client{Transport: bearerTransport{token: rawToken, base: http.DefaultTransport}}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(
		context.Background(),
		&mcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp", HTTPClient: client, DisableStandaloneSSE: true},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callMCPTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil || result.IsError {
		t.Fatalf("call %s result=%#v err=%v", name, result, err)
	}
	if result.StructuredContent == nil {
		t.Fatalf("call %s omitted structured content", name)
	}
	return result
}

func decodeStructured(t *testing.T, value any, destination any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, destination); err != nil {
		t.Fatal(err)
	}
}

func rawMCPTools(t *testing.T, client *http.Client, endpoint string) []*mcp.Tool {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":99,"method":"tools/list","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Result struct {
			Tools []*mcp.Tool `json:"tools"`
		} `json:"result"`
	}
	if err = json.Unmarshal(raw, &result); err != nil || len(result.Result.Tools) == 0 {
		t.Fatalf("decode raw tools: %v body=%s", err, raw)
	}
	return result.Result.Tools
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}
