package controlplane

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

const oauthMaxBodyBytes = 64 << 10

var oauthConsentTemplate = template.Must(template.New("oauth-consent").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Authorize {{.ClientName}}</title>
  <style>
    :root { color-scheme: light dark; font-family: system-ui, sans-serif; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f4f5f7; color: #17202a; }
    main { width: min(34rem, calc(100% - 3rem)); padding: 2rem; border-radius: 1rem; background: white; box-shadow: 0 1rem 3rem rgba(0,0,0,.12); }
    h1 { margin-top: 0; font-size: 1.5rem; }
    ul { padding-left: 1.25rem; }
    .actions { display: flex; gap: .75rem; margin-top: 1.5rem; }
    button { padding: .7rem 1rem; border-radius: .5rem; border: 1px solid #aaa; font: inherit; cursor: pointer; }
    button[value="approve"] { background: #1167d8; color: white; border-color: #1167d8; }
    @media (prefers-color-scheme: dark) { body { background: #111827; color: #e5e7eb; } main { background: #1f2937; } }
  </style>
</head>
<body>
  <main>
    <h1>Authorize {{.ClientName}}</h1>
    <p><strong>{{.ClientName}}</strong> is requesting access to workspace <strong>{{.WorkspaceID}}</strong>.</p>
    <p>Requested permissions:</p>
    <ul>{{range .Scopes}}<li>{{.}}</li>{{end}}</ul>
    <form method="post" action="/oauth/authorize">
      <input type="hidden" name="response_type" value="{{.ResponseType}}">
      <input type="hidden" name="client_id" value="{{.ClientID}}">
      <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
      <input type="hidden" name="scope" value="{{.Scope}}">
      <input type="hidden" name="state" value="{{.State}}">
      <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
      <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
      <input type="hidden" name="resource" value="{{.Resource}}">
      <div class="actions">
        <button type="submit" name="consent" value="approve">Approve</button>
        <button type="submit" name="consent" value="deny">Deny</button>
      </div>
    </form>
  </main>
</body>
</html>`))

type oauthConsentPage struct {
	ClientID            string
	ClientName          string
	WorkspaceID         string
	ResponseType        string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
	Scopes              []string
}

type MCPDashboardActor struct {
	WorkspaceID string
	UserID      string
	Role        string
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
	mux.HandleFunc("POST /oauth/register", s.handleOAuthRegister)
	mux.HandleFunc("GET /oauth/authorize", s.handleOAuthAuthorize)
	mux.HandleFunc("POST /oauth/authorize", s.handleOAuthAuthorize)
	mux.HandleFunc("POST /oauth/token", s.handleOAuthToken)
	mux.HandleFunc("POST /oauth/revoke", s.handleOAuthRevoke)
}

func (s *Server) canonicalMCPResource() (string, string, error) {
	base, err := canonicalHTTPSOrigin(s.MCPPublicURL)
	if err != nil {
		return "", "", fmt.Errorf("VES_MCP_PUBLIC_URL must be a canonical HTTPS origin")
	}
	return base, base + "/mcp", nil
}

func canonicalHTTPSOrigin(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	parsed, err := url.Parse(configured)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("not a canonical HTTPS origin")
	}
	base := "https://" + parsed.Host
	return base, nil
}

func (s *Server) requireCanonicalMCP(w http.ResponseWriter) (string, string, bool) {
	base, resource, err := s.canonicalMCPResource()
	if err != nil {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", err.Error())
		return "", "", false
	}
	return base, resource, true
}

func (s *Server) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	base, resource, ok := s.requireCanonicalMCP(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resource": resource, "authorization_servers": []string{base},
		"scopes_supported": MCPScopes(), "bearer_methods_supported": []string{"header"},
	})
}

func (s *Server) handleOAuthAuthorizationServer(w http.ResponseWriter, r *http.Request) {
	base, _, ok := s.requireCanonicalMCP(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer": base, "authorization_endpoint": base + "/oauth/authorize",
		"token_endpoint": base + "/oauth/token", "revocation_endpoint": base + "/oauth/revoke",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": MCPScopes(),
	})
}

func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	base, resource, ok := s.requireCanonicalMCP(w)
	if !ok {
		return
	}
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
	client, redirectURI, scopes, err := s.validateAuthorizationRequest(r, resource)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if r.Method == http.MethodPost && !validOAuthConsentOrigin(r, base) {
		writeOAuthError(w, http.StatusForbidden, "access_denied", "authorization consent origin is not allowed")
		return
	}
	actor, err := s.MCPDashboardIdentity(r, false)
	workspace, workspaceErr := s.DB.GetWorkspace(r.Context())
	if err != nil || workspaceErr != nil || strings.TrimSpace(actor.UserID) == "" || actor.WorkspaceID != workspace.ID {
		writeOAuthError(w, http.StatusUnauthorized, "access_denied", "dashboard authorization is required")
		return
	}
	if r.Method == http.MethodGet {
		formAction := "'self'"
		if redirectTarget, parseErr := url.Parse(redirectURI); parseErr == nil && redirectTarget.Scheme == "https" && redirectTarget.Host != "" && redirectTarget.User == nil {
			formAction += " https://" + redirectTarget.Host
		}
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action "+formAction+"; frame-ancestors 'none'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = oauthConsentTemplate.Execute(w, oauthConsentPage{
			ClientID: client.ClientID, ClientName: client.Name, WorkspaceID: client.WorkspaceID,
			ResponseType: r.Form.Get("response_type"), RedirectURI: redirectURI,
			Scope: r.Form.Get("scope"), State: r.Form.Get("state"),
			CodeChallenge: r.Form.Get("code_challenge"), CodeChallengeMethod: r.Form.Get("code_challenge_method"),
			Resource: resource, Scopes: scopes,
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
		Resource:  resource,
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

func validOAuthConsentOrigin(r *http.Request, base string) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == base {
		return true
	}
	// Some browser privacy/extension paths omit Origin on a top-level form POST.
	// Sec-Fetch-Site is a browser-controlled forbidden header and preserves the
	// same-origin boundary for those submissions. The Strict session cookie is
	// still required independently by MCPDashboardIdentity.
	return (origin == "" || origin == "null") && r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

func (s *Server) validateAuthorizationRequest(r *http.Request, resource string) (*state.OAuthClient, string, []string, error) {
	if r.Form.Get("response_type") != "code" {
		return nil, "", nil, fmt.Errorf("response_type must be code")
	}
	if r.Form.Get("code_challenge_method") != "S256" || !validPKCEValue(r.Form.Get("code_challenge"), 43, 128) {
		return nil, "", nil, fmt.Errorf("S256 PKCE code_challenge is required")
	}
	if r.Form.Get("resource") != resource {
		return nil, "", nil, fmt.Errorf("resource must exactly match the canonical MCP resource")
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
	if !requireOAuthForm(w, r) {
		return
	}
	_, resource, ok := s.requireCanonicalMCP(w)
	if !ok {
		return
	}
	if !rejectOAuthCredentialQuery(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oauthMaxBodyBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthFormError(w, err)
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.exchangeAuthorizationCode(w, r, resource)
	case "refresh_token":
		s.exchangeRefreshToken(w, r, resource)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type is not supported")
	}
}

func (s *Server) exchangeAuthorizationCode(w http.ResponseWriter, r *http.Request, resource string) {
	if r.PostForm.Get("resource") != resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource must exactly match the canonical MCP resource")
		return
	}
	verifier := r.PostForm.Get("code_verifier")
	if !validPKCEValue(verifier, 43, 128) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code, err := s.agentApp().ExchangeOAuthAuthorizationCode(r.Context(), hashOAuthMaterial(r.PostForm.Get("code")), r.PostForm.Get("client_id"), r.PostForm.Get("redirect_uri"), challenge, resource)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
		return
	}
	s.issueOAuthTokenPair(w, r, code.ClientID, code.ActorID, code.ScopesJSON, id.New("oauthfamily"), resource)
}

func (s *Server) exchangeRefreshToken(w http.ResponseWriter, r *http.Request, resource string) {
	if r.PostForm.Get("resource") != resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource must exactly match the canonical MCP resource")
		return
	}
	refresh, err := s.agentApp().ConsumeOAuthRefreshToken(r.Context(), hashOAuthMaterial(r.PostForm.Get("refresh_token")), r.PostForm.Get("client_id"), resource)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}
	s.issueOAuthTokenPair(w, r, refresh.ClientID, refresh.ActorID, refresh.ScopesJSON, refresh.FamilyID, resource)
}

func (s *Server) issueOAuthTokenPair(w http.ResponseWriter, r *http.Request, clientID, actorID, scopesJSON, familyID, resource string) {
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
		ClientID: clientID, ActorID: actorID, TokenHash: hashOAuthMaterial(access), FamilyID: familyID, Resource: resource, ScopesJSON: scopesJSON,
		ExpiresAt: state.FormatTime(time.Now().Add(time.Hour)),
	}); err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "access token could not be stored")
		return
	}
	if _, err = s.agentApp().IssueOAuthRefreshToken(r.Context(), state.OAuthRefreshTokenInput{
		ClientID: clientID, ActorID: actorID, MaterialHash: hashOAuthMaterial(refresh), FamilyID: familyID, Resource: resource,
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
	if !requireOAuthForm(w, r) {
		return
	}
	if _, _, ok := s.requireCanonicalMCP(w); !ok {
		return
	}
	if !rejectOAuthCredentialQuery(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oauthMaxBodyBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthFormError(w, err)
		return
	}
	hash := hashOAuthMaterial(r.PostForm.Get("token"))
	if hash != hashOAuthMaterial("") {
		_ = s.agentApp().RevokeOAuthAccessToken(r.Context(), hash)
		_ = s.agentApp().RevokeOAuthRefreshToken(r.Context(), hash)
	}
	w.WriteHeader(http.StatusOK)
}

func rejectOAuthCredentialQuery(w http.ResponseWriter, r *http.Request) bool {
	query := r.URL.Query()
	for _, key := range []string{"grant_type", "code", "refresh_token", "client_id", "client_secret", "redirect_uri", "code_verifier", "resource", "token", "token_type_hint"} {
		if _, present := query[key]; present {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "OAuth credentials and grant parameters must be sent in the form body")
			return false
		}
	}
	return true
}

func requireOAuthForm(w http.ResponseWriter, r *http.Request) bool {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if contentType != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusUnsupportedMediaType, "invalid_request", "application/x-www-form-urlencoded content type is required")
		return false
	}
	return true
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
