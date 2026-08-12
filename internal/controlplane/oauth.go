package controlplane

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

const oauthMaxBodyBytes = 64 << 10

type MCPDashboardActor struct {
	UserID string
	Role   string
}

var mcpScopes = []string{
	"knowledge:read",
	"knowledge:write",
	"agents:read",
	"agents:run",
	"conversations:write",
	"sources:manage",
}

func MCPScopes() []string { return append([]string(nil), mcpScopes...) }

func hashOAuthMaterial(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randomOAuthMaterial(prefix string, bytesCount int) (string, error) {
	raw := make([]byte, bytesCount)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate OAuth material: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Server) registerOAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.handleOAuthProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.handleOAuthProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.handleOAuthAuthorizationServer)
	mux.HandleFunc("GET /oauth/authorize", s.handleOAuthAuthorize)
	mux.HandleFunc("POST /oauth/authorize", s.handleOAuthAuthorize)
	mux.HandleFunc("POST /oauth/token", s.handleOAuthToken)
	mux.HandleFunc("POST /oauth/revoke", s.handleOAuthRevoke)
}

func (s *Server) oauthBaseURL(r *http.Request) string {
	if configured := strings.TrimRight(strings.TrimSpace(s.MCPPublicURL), "/"); configured != "" {
		return configured
	}
	scheme := "https"
	if r.TLS == nil && (strings.HasPrefix(r.Host, "127.0.0.1") || strings.HasPrefix(r.Host, "localhost")) {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	base := s.oauthBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource": base + "/mcp", "authorization_servers": []string{base},
		"scopes_supported": MCPScopes(), "bearer_methods_supported": []string{"header"},
	})
}

func (s *Server) handleOAuthAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	base := s.oauthBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer": base, "authorization_endpoint": base + "/oauth/authorize",
		"token_endpoint": base + "/oauth/token", "revocation_endpoint": base + "/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": MCPScopes(),
	})
}

func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if s.MCPDashboardIdentity == nil {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "dashboard identity is unavailable")
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, oauthMaxBodyBytes)
		if err := r.ParseForm(); err != nil {
			writeOAuthFormError(w, err)
			return
		}
	} else {
		r.Form = r.URL.Query()
	}
	client, redirectURI, scopes, err := s.validateAuthorizationRequest(r)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	actor, err := s.MCPDashboardIdentity(r, r.Method == http.MethodPost)
	if err != nil || strings.TrimSpace(actor.UserID) == "" {
		writeOAuthError(w, http.StatusUnauthorized, "access_denied", "dashboard authorization is required")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{
			"consent_required": true, "client_id": client.ClientID, "client_name": client.Name,
			"workspace_id": client.WorkspaceID, "actor_id": actor.UserID, "scopes": scopes,
		})
		return
	}
	if r.Form.Get("consent") != "approve" {
		redirectOAuthError(w, r, redirectURI, "access_denied", "consent was not approved")
		return
	}
	code, err := randomOAuthMaterial("vmc_", 32)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "authorization code generation failed")
		return
	}
	_, err = s.agentApp().CreateOAuthAuthorizationCode(r.Context(), state.OAuthAuthorizationCodeInput{
		ClientID: client.ClientID, ActorID: actor.UserID, CodeHash: hashOAuthMaterial(code),
		RedirectURI: redirectURI, ScopesJSON: mustMarshalJSON(scopes),
		CodeChallenge: r.Form.Get("code_challenge"), CodeChallengeMethod: "S256",
		ExpiresAt: state.FormatTime(time.Now().Add(5 * time.Minute)),
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "authorization code could not be stored")
		return
	}
	target, _ := url.Parse(redirectURI)
	query := target.Query()
	query.Set("code", code)
	if requestState := r.Form.Get("state"); requestState != "" {
		query.Set("state", requestState)
	}
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func (s *Server) validateAuthorizationRequest(r *http.Request) (*state.OAuthClient, string, []string, error) {
	if r.Form.Get("response_type") != "code" {
		return nil, "", nil, fmt.Errorf("response_type must be code")
	}
	if r.Form.Get("code_challenge_method") != "S256" || !validPKCEValue(r.Form.Get("code_challenge"), 43, 128) {
		return nil, "", nil, fmt.Errorf("S256 PKCE code_challenge is required")
	}
	client, err := s.DB.GetOAuthClient(r.Context(), strings.TrimSpace(r.Form.Get("client_id")))
	if err != nil || client.RevokedAt != "" {
		return nil, "", nil, fmt.Errorf("OAuth client is invalid")
	}
	redirectURI := strings.TrimSpace(r.Form.Get("redirect_uri"))
	var redirects []string
	if json.Unmarshal([]byte(client.RedirectURIsJSON), &redirects) != nil || !containsString(redirects, redirectURI) {
		return nil, "", nil, fmt.Errorf("redirect_uri is not registered")
	}
	requested := strings.Fields(r.Form.Get("scope"))
	var allowed []string
	if json.Unmarshal([]byte(client.ScopesJSON), &allowed) != nil || len(requested) == 0 {
		return nil, "", nil, fmt.Errorf("at least one valid scope is required")
	}
	for _, scope := range requested {
		if !containsString(MCPScopes(), scope) || !containsString(allowed, scope) {
			return nil, "", nil, fmt.Errorf("requested scope is not allowed")
		}
	}
	return client, redirectURI, requested, nil
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, oauthMaxBodyBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthFormError(w, err)
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.exchangeAuthorizationCode(w, r)
	case "refresh_token":
		s.exchangeRefreshToken(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type is not supported")
	}
}

func (s *Server) exchangeAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	verifier := r.Form.Get("code_verifier")
	if !validPKCEValue(verifier, 43, 128) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code, err := s.agentApp().ExchangeOAuthAuthorizationCode(r.Context(), hashOAuthMaterial(r.Form.Get("code")), r.Form.Get("client_id"), r.Form.Get("redirect_uri"), challenge)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}
	s.issueOAuthTokenPair(w, r, code.ClientID, code.ActorID, code.ScopesJSON, id.New("oauthfamily"))
}

func (s *Server) exchangeRefreshToken(w http.ResponseWriter, r *http.Request) {
	refresh, err := s.agentApp().ConsumeOAuthRefreshToken(r.Context(), hashOAuthMaterial(r.Form.Get("refresh_token")), r.Form.Get("client_id"))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}
	s.issueOAuthTokenPair(w, r, refresh.ClientID, refresh.ActorID, refresh.ScopesJSON, refresh.FamilyID)
}

func (s *Server) issueOAuthTokenPair(w http.ResponseWriter, r *http.Request, clientID, actorID, scopesJSON, familyID string) {
	access, err := randomOAuthMaterial("vma_", 36)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "access token generation failed")
		return
	}
	refresh, err := randomOAuthMaterial("vmr_", 36)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "refresh token generation failed")
		return
	}
	if _, err = s.agentApp().IssueOAuthAccessToken(r.Context(), state.OAuthAccessTokenInput{
		ClientID: clientID, ActorID: actorID, TokenHash: hashOAuthMaterial(access), ScopesJSON: scopesJSON,
		ExpiresAt: state.FormatTime(time.Now().Add(time.Hour)),
	}); err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "access token could not be stored")
		return
	}
	if _, err = s.agentApp().IssueOAuthRefreshToken(r.Context(), state.OAuthRefreshTokenInput{
		ClientID: clientID, ActorID: actorID, MaterialHash: hashOAuthMaterial(refresh), FamilyID: familyID,
		ScopesJSON: scopesJSON, ExpiresAt: state.FormatTime(time.Now().Add(30 * 24 * time.Hour)),
	}); err != nil {
		_ = s.agentApp().RevokeOAuthAccessToken(r.Context(), hashOAuthMaterial(access))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "refresh token could not be stored")
		return
	}
	var scopes []string
	_ = json.Unmarshal([]byte(scopesJSON), &scopes)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "refresh_token": refresh, "token_type": "Bearer",
		"expires_in": 3600, "scope": strings.Join(scopes, " "),
	})
}

func (s *Server) handleOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, oauthMaxBodyBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthFormError(w, err)
		return
	}
	hash := hashOAuthMaterial(r.Form.Get("token"))
	if hash != hashOAuthMaterial("") {
		_ = s.agentApp().RevokeOAuthAccessToken(r.Context(), hash)
		_ = s.agentApp().RevokeOAuthRefreshToken(r.Context(), hash)
	}
	w.WriteHeader(http.StatusOK)
}

func validPKCEValue(value string, minLen, maxLen int) bool {
	if len(value) < minLen || len(value) > maxLen {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("-._~", r) {
			continue
		}
		return false
	}
	return true
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func mustMarshalJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func writeOAuthFormError(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "request body too large") {
		writeOAuthError(w, http.StatusRequestEntityTooLarge, "invalid_request", "request body exceeds limit")
		return
	}
	writeOAuthError(w, http.StatusBadRequest, "invalid_request", "form body is invalid")
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func redirectOAuthError(w http.ResponseWriter, r *http.Request, redirectURI, code, description string) {
	target, err := url.Parse(redirectURI)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, code, description)
		return
	}
	query := target.Query()
	query.Set("error", code)
	query.Set("error_description", description)
	if requestState := r.Form.Get("state"); requestState != "" {
		query.Set("state", requestState)
	}
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}
