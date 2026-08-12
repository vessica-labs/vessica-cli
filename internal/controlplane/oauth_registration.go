package controlplane

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

type oauthRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
}

func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "content type must be application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oauthMaxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request oauthRegistrationRequest
	if err = decoder.Decode(&request); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "client metadata must be one valid JSON object")
		return
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "client metadata must contain one JSON object")
		return
	}
	request.ClientName = strings.TrimSpace(request.ClientName)
	if request.ClientName == "" || len(request.ClientName) > 200 || strings.IndexFunc(request.ClientName, unicode.IsControl) >= 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "client_name is invalid")
		return
	}
	if len(request.RedirectURIs) == 0 || len(request.RedirectURIs) > 10 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "one to ten ChatGPT redirect URIs are required")
		return
	}
	seenRedirects := map[string]struct{}{}
	for _, redirect := range request.RedirectURIs {
		if !validChatGPTRedirectURI(redirect) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri is not an approved ChatGPT callback")
			return
		}
		if _, duplicate := seenRedirects[redirect]; duplicate {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris must be unique")
			return
		}
		seenRedirects[redirect] = struct{}{}
	}
	if request.TokenEndpointAuthMethod == "" {
		request.TokenEndpointAuthMethod = "none"
	}
	if request.TokenEndpointAuthMethod != "none" || !validOAuthStringSet(request.GrantTypes, []string{"authorization_code", "refresh_token"}) || !validOAuthStringSet(request.ResponseTypes, []string{"code"}) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "only a public authorization-code client with PKCE is supported")
		return
	}
	scopes := strings.Fields(request.Scope)
	if len(scopes) == 0 {
		scopes = MCPScopes()
	}
	seenScopes := map[string]struct{}{}
	for _, scope := range scopes {
		if !containsString(MCPScopes(), scope) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "requested scope is not supported")
			return
		}
		if _, duplicate := seenScopes[scope]; duplicate {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "requested scopes must be unique")
			return
		}
		seenScopes[scope] = struct{}{}
	}
	clientID, err := randomOAuthMaterial("vmdcr_", 24)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "client registration failed")
		return
	}
	redirectsJSON, _ := json.Marshal(request.RedirectURIs)
	scopesJSON, _ := json.Marshal(scopes)
	if _, err = s.agentApp().RegisterOAuthClient(r.Context(), state.OAuthClientInput{ClientID: clientID, Name: request.ClientName, RedirectURIsJSON: string(redirectsJSON), ScopesJSON: string(scopesJSON)}); err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "client registration failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id": clientID, "client_name": request.ClientName, "redirect_uris": request.RedirectURIs,
		"token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "scope": strings.Join(scopes, " "),
	})
}

func validChatGPTRedirectURI(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "chatgpt.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return (strings.HasPrefix(parsed.Path, "/connector/oauth/") && len(strings.TrimPrefix(parsed.Path, "/connector/oauth/")) > 0) || parsed.Path == "/connector_platform_oauth_redirect"
}

func validOAuthStringSet(actual, allowed []string) bool {
	if len(actual) == 0 {
		return true
	}
	seen := map[string]struct{}{}
	for _, value := range actual {
		if !containsString(allowed, value) {
			return false
		}
		seen[value] = struct{}{}
	}
	return len(seen) == len(actual) && len(seen) == len(allowed)
}
