package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthDynamicClientRegistrationForChatGPT(t *testing.T) {
	server, db := mcpTestServer(t)
	handler := server.Handler()
	metadata := httptest.NewRecorder()
	handler.ServeHTTP(metadata, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	var discovery map[string]any
	if err := json.Unmarshal(metadata.Body.Bytes(), &discovery); err != nil {
		t.Fatal(err)
	}
	if discovery["registration_endpoint"] != "https://vessica.example/oauth/register" {
		t.Fatalf("registration endpoint=%#v", discovery["registration_endpoint"])
	}

	register := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	rec := register(`{
		"client_name":"ChatGPT Vessica staging",
		"redirect_uris":["https://chatgpt.com/connector/oauth/callback_123"],
		"token_endpoint_auth_method":"none",
		"grant_types":["authorization_code","refresh_token"],
		"response_types":["code"],
		"scope":"knowledge:read knowledge:write"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("registration status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ClientID                string   `json:"client_id"`
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ClientID == "" || created.ClientName != "ChatGPT Vessica staging" || created.TokenEndpointAuthMethod != "none" || len(created.RedirectURIs) != 1 {
		t.Fatalf("registration=%#v", created)
	}
	stored, err := db.GetOAuthClient(context.Background(), created.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SecretHash != "" || stored.RedirectURIsJSON != `["https://chatgpt.com/connector/oauth/callback_123"]` || stored.ScopesJSON != `["knowledge:read","knowledge:write"]` {
		t.Fatalf("stored client=%#v", stored)
	}

	for name, body := range map[string]string{
		"foreign redirect": `{"client_name":"bad","redirect_uris":["https://evil.example/callback"],"token_endpoint_auth_method":"none"}`,
		"redirect query":   `{"client_name":"bad","redirect_uris":["https://chatgpt.com/connector/oauth/callback?next=evil"],"token_endpoint_auth_method":"none"}`,
		"client secret":    `{"client_name":"bad","redirect_uris":["https://chatgpt.com/connector/oauth/callback"],"token_endpoint_auth_method":"client_secret_post"}`,
		"partial grants":   `{"client_name":"bad","redirect_uris":["https://chatgpt.com/connector/oauth/callback"],"token_endpoint_auth_method":"none","grant_types":["refresh_token"]}`,
		"unknown scope":    `{"client_name":"bad","redirect_uris":["https://chatgpt.com/connector/oauth/callback"],"token_endpoint_auth_method":"none","scope":"admin:*"}`,
		"wrong content":    `not-json`,
	} {
		t.Run(name, func(t *testing.T) {
			bad := register(body)
			if bad.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", bad.Code, bad.Body.String())
			}
		})
	}
	oversized := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(strings.Repeat("x", oauthMaxBodyBytes+1)))
	oversized.Header.Set("Content-Type", "application/json")
	oversizedRec := httptest.NewRecorder()
	handler.ServeHTTP(oversizedRec, oversized)
	if oversizedRec.Code != http.StatusBadRequest {
		t.Fatalf("oversized registration status=%d body=%s", oversizedRec.Code, oversizedRec.Body.String())
	}
}
