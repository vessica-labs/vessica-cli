package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultOAuthAddress         = "127.0.0.1:8765"
	defaultRailwayOAuthClientID = "rlwy_oaci_Kt3FbcZOfSpunkgnBAubyr5J"
	defaultLinearOAuthClientID  = "8b5f581dc76091e8cc7939d6be635142"
)

type OAuthProvider struct {
	Name            string
	ClientID        string
	AuthorizeURL    string
	TokenURL        string
	CallbackPath    string
	Scopes          []string
	ScopeSeparator  string
	AuthorizeParams map[string]string
}

type OAuthCredential struct {
	Provider     string    `json:"provider"`
	ClientID     string    `json:"client_id"`
	TokenURL     string    `json:"token_url"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

var ErrSecretNotFound = errors.New("secret not found")

func Provider(name, clientID string) (OAuthProvider, error) {
	switch strings.ToLower(name) {
	case "railway":
		return OAuthProvider{
			Name: "railway", ClientID: clientID,
			AuthorizeURL:   "https://backboard.railway.com/oauth/auth",
			TokenURL:       "https://backboard.railway.com/oauth/token",
			CallbackPath:   "/oauth/railway/callback",
			Scopes:         []string{"openid", "profile", "email", "offline_access", "workspace:member", "project:member"},
			ScopeSeparator: " ", AuthorizeParams: map[string]string{"prompt": "consent"},
		}, nil
	case "linear":
		return OAuthProvider{
			Name: "linear", ClientID: clientID,
			AuthorizeURL:   "https://linear.app/oauth/authorize",
			TokenURL:       "https://api.linear.app/oauth/token",
			CallbackPath:   "/oauth/linear/callback",
			Scopes:         []string{"read", "write", "issues:create", "comments:create", "admin"},
			ScopeSeparator: ",",
		}, nil
	default:
		return OAuthProvider{}, fmt.Errorf("%s does not support browser OAuth", name)
	}
}

func OAuthClientID(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	key := "VES_" + strings.ToUpper(provider) + "_OAUTH_CLIENT_ID"
	if override := strings.TrimSpace(os.Getenv(key)); override != "" {
		return override
	}
	switch provider {
	case "railway":
		return defaultRailwayOAuthClientID
	case "linear":
		return defaultLinearOAuthClientID
	default:
		return ""
	}
}

func BrowserLogin(ctx context.Context, provider OAuthProvider, openURL func(string) error) (*OAuthCredential, error) {
	if strings.TrimSpace(provider.ClientID) == "" {
		return nil, fmt.Errorf("%s OAuth client ID is not configured; set VES_%s_OAUTH_CLIENT_ID", provider.Name, strings.ToUpper(provider.Name))
	}
	address := strings.TrimSpace(os.Getenv("VES_OAUTH_LISTEN_ADDRESS"))
	if address == "" {
		address = defaultOAuthAddress
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("start OAuth callback on %s: %w", address, err)
	}
	defer listener.Close()
	redirectURI := "http://" + listener.Addr().String() + provider.CallbackPath
	verifier, err := randomURLToken(48)
	if err != nil {
		return nil, err
	}
	state, err := randomURLToken(32)
	if err != nil {
		return nil, err
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"client_id":             {provider.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {strings.Join(provider.Scopes, provider.ScopeSeparator)},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challengeBytes[:])},
		"code_challenge_method": {"S256"},
	}
	for key, value := range provider.AuthorizeParams {
		query.Set(key, value)
	}
	authorizeURL := provider.AuthorizeURL + "?" + query.Encode()
	type callbackResult struct {
		code string
		err  error
	}
	result := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(provider.CallbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "OAuth state did not match. Return to the terminal and retry.", http.StatusBadRequest)
			result <- callbackResult{err: errors.New("OAuth callback state did not match")}
			return
		}
		if message := r.URL.Query().Get("error"); message != "" {
			detail := r.URL.Query().Get("error_description")
			http.Error(w, message+": "+detail, http.StatusBadRequest)
			result <- callbackResult{err: fmt.Errorf("OAuth authorization failed: %s: %s", message, detail)}
			return
		}
		code := strings.TrimSpace(r.URL.Query().Get("code"))
		if code == "" {
			http.Error(w, "Missing authorization code.", http.StatusBadRequest)
			result <- callbackResult{err: errors.New("OAuth callback did not include a code")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, "<!doctype html><title>Vessica connected</title><main style='font:16px system-ui;max-width:38rem;margin:12vh auto'><h1>%s connected</h1><p>You can close this window and return to Vessica.</p></main>", html.EscapeString(strings.Title(provider.Name)))
		result <- callbackResult{code: code}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer server.Shutdown(context.Background())
	if openURL == nil {
		openURL = OpenBrowser
	}
	if err := openURL(authorizeURL); err != nil {
		return nil, fmt.Errorf("open browser: %w (open %s manually)", err, authorizeURL)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	select {
	case <-waitCtx.Done():
		return nil, fmt.Errorf("OAuth login timed out: %w", waitCtx.Err())
	case response := <-result:
		if response.err != nil {
			return nil, response.err
		}
		return exchangeCode(waitCtx, provider, redirectURI, response.code, verifier)
	}
}

func exchangeCode(ctx context.Context, provider OAuthProvider, redirectURI, code, verifier string) (*OAuthCredential, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {provider.ClientID},
		"redirect_uri":  {redirectURI},
		"code":          {code},
		"code_verifier": {verifier},
	}
	credential, err := requestToken(ctx, provider.Name, provider.ClientID, provider.TokenURL, form)
	if err != nil {
		return nil, err
	}
	return credential, SaveOAuth(credential)
}

func RefreshIfNeeded(ctx context.Context, credential *OAuthCredential) (*OAuthCredential, error) {
	if credential == nil {
		return nil, errors.New("OAuth credential is nil")
	}
	if credential.ExpiresAt.IsZero() || time.Until(credential.ExpiresAt) > 5*time.Minute {
		return credential, nil
	}
	if credential.RefreshToken == "" {
		return nil, fmt.Errorf("%s access token expired and no refresh token is available", credential.Provider)
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {credential.ClientID},
		"refresh_token": {credential.RefreshToken},
	}
	refreshed, err := requestToken(ctx, credential.Provider, credential.ClientID, credential.TokenURL, form)
	if err != nil {
		return nil, err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = credential.RefreshToken
	}
	return refreshed, nil
}

func requestToken(ctx context.Context, provider, clientID, tokenURL string, form url.Values) (*OAuthCredential, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s token exchange failed (%d): %s", provider, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("decode %s token response: %w", provider, err)
	}
	if token.AccessToken == "" {
		return nil, fmt.Errorf("%s token response did not include an access token", provider)
	}
	now := time.Now().UTC()
	credential := &OAuthCredential{Provider: provider, ClientID: clientID, TokenURL: tokenURL, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, TokenType: token.TokenType, Scope: token.Scope, UpdatedAt: now}
	if token.ExpiresIn > 0 {
		credential.ExpiresAt = now.Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return credential, nil
}

func SaveOAuth(credential *OAuthCredential) error {
	if credential == nil || credential.Provider == "" {
		return errors.New("OAuth credential provider is required")
	}
	data, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return writeSecret(credential.Provider, data)
}

func LoadOAuth(provider string) (*OAuthCredential, error) {
	data, err := readSecret(strings.ToLower(provider))
	if err != nil {
		return nil, err
	}
	var credential OAuthCredential
	if err := json.Unmarshal(data, &credential); err != nil {
		return nil, err
	}
	return &credential, nil
}

func DeleteOAuth(provider string) error { return deleteSecret(strings.ToLower(provider)) }

// StoreSecret writes non-OAuth client credentials to the platform credential
// store. Callers persist only the returned reference in their own registries.
func StoreSecret(reference string, data []byte) error {
	return writeSecret(strings.ToLower(reference), data)
}

// LoadSecret resolves a client credential reference from the platform store.
func LoadSecret(reference string) ([]byte, error) {
	return readSecret(strings.ToLower(reference))
}

// IsSecretNotFound reports whether a platform credential has not been stored.
func IsSecretNotFound(err error) bool {
	return errors.Is(err, ErrSecretNotFound) || os.IsNotExist(err)
}

// DeleteSecret removes a non-OAuth client credential from the platform store.
func DeleteSecret(reference string) error {
	return deleteSecret(strings.ToLower(reference))
}

func MarshalOAuth(provider string) (string, error) {
	credential, err := LoadOAuth(provider)
	if err != nil {
		return "", err
	}
	updatedAt := credential.UpdatedAt
	credential, err = RefreshIfNeeded(context.Background(), credential)
	if err != nil {
		return "", err
	}
	if !credential.UpdatedAt.Equal(updatedAt) {
		if err := SaveOAuth(credential); err != nil {
			return "", err
		}
	}
	data, err := json.Marshal(credential)
	return string(data), err
}

func UnmarshalOAuth(raw string) (*OAuthCredential, error) {
	var credential OAuthCredential
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		return nil, err
	}
	if credential.Provider == "" || credential.AccessToken == "" {
		return nil, errors.New("invalid OAuth credential")
	}
	return &credential, nil
}

func writeSecret(provider string, data []byte) error {
	if useKeychain() {
		service := "com.vessica.auth." + provider
		out, err := exec.Command("/usr/bin/security", "add-generic-password", "-a", provider, "-s", service, "-w", string(data), "-U").CombinedOutput()
		if err != nil {
			return fmt.Errorf("store %s login in Keychain: %w: %s", provider, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	path, err := oauthPath(provider)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func readSecret(provider string) ([]byte, error) {
	if useKeychain() {
		service := "com.vessica.auth." + provider
		out, err := exec.Command("/usr/bin/security", "find-generic-password", "-a", provider, "-s", service, "-w").Output()
		if err != nil {
			return nil, fmt.Errorf("not logged in to %s: %w", provider, ErrSecretNotFound)
		}
		return []byte(strings.TrimSpace(string(out))), nil
	}
	path, err := oauthPath(provider)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func deleteSecret(provider string) error {
	if useKeychain() {
		_ = exec.Command("/usr/bin/security", "delete-generic-password", "-a", provider, "-s", "com.vessica.auth."+provider).Run()
		return nil
	}
	path, err := oauthPath(provider)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func oauthPath(provider string) (string, error) {
	dir, err := secretsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, provider+".oauth.json"), nil
}

func useKeychain() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("VES_AUTH_STORE")))
	return runtime.GOOS == "darwin" && mode != "file"
}

func randomURLToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func OpenBrowser(target string) error {
	commands := [][]string{{"open", target}, {"xdg-open", target}}
	for _, command := range commands {
		if path, err := exec.LookPath(command[0]); err == nil {
			if err := exec.Command(path, command[1:]...).Start(); err == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("no browser opener found")
}

func CodexStatus(ctx context.Context) (bool, string) {
	path, err := exec.LookPath("codex")
	if err != nil {
		return false, "codex CLI is not installed"
	}
	output, err := exec.CommandContext(ctx, path, "login", "status").CombinedOutput()
	detail := ""
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			detail = value
		}
	}
	return err == nil, detail
}

func CodexAuthJSON() ([]byte, error) {
	loggedIn, detail := CodexStatus(context.Background())
	if !loggedIn {
		return nil, fmt.Errorf("Codex is not logged in: %s", detail)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return nil, fmt.Errorf("read Codex login: %w", err)
	}
	if !json.Valid(data) {
		return nil, errors.New("Codex auth file is not valid JSON")
	}
	return data, nil
}
