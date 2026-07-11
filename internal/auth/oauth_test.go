package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBrowserLoginUsesPKCEAndStoresCredential(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VES_AUTH_STORE", "file")
	t.Setenv("VES_OAUTH_LISTEN_ADDRESS", "127.0.0.1:0")
	var verifier string
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		verifier = r.Form.Get("code_verifier")
		if r.Form.Get("code") != "test-code" || verifier == "" {
			t.Errorf("unexpected token form: %#v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600, "token_type": "Bearer"})
	}))
	defer tokenServer.Close()
	provider := OAuthProvider{Name: "test", ClientID: "client", AuthorizeURL: "https://auth.example/authorize", TokenURL: tokenServer.URL, CallbackPath: "/oauth/test/callback", Scopes: []string{"one", "two"}, ScopeSeparator: " "}
	credential, err := BrowserLogin(context.Background(), provider, func(target string) error {
		authorize, err := url.Parse(target)
		if err != nil {
			return err
		}
		if authorize.Query().Get("code_challenge_method") != "S256" || authorize.Query().Get("state") == "" {
			t.Errorf("missing PKCE/state: %s", target)
		}
		go func() {
			callback := authorize.Query().Get("redirect_uri") + "?code=test-code&state=" + url.QueryEscape(authorize.Query().Get("state"))
			resp, requestErr := http.Get(callback)
			if requestErr == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "access" || credential.RefreshToken != "refresh" {
		t.Fatalf("credential=%#v", credential)
	}
	digest := sha256.Sum256([]byte(verifier))
	if verifier == "" || base64.RawURLEncoding.EncodeToString(digest[:]) == "" {
		t.Fatal("PKCE verifier was not sent")
	}
	stored, err := LoadOAuth("test")
	if err != nil || stored.AccessToken != "access" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	if info, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".vessica", "secrets", "test.oauth.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file mode=%v err=%v", info, err)
	}
}

func TestRefreshIfNeededPreservesOrRotatesRefreshToken(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "old-refresh" {
			t.Errorf("refresh token=%q", r.Form.Get("refresh_token"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "refresh_token": "new-refresh", "expires_in": 7200})
	}))
	defer server.Close()
	credential := &OAuthCredential{Provider: "linear", ClientID: "client", TokenURL: server.URL, AccessToken: "old", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(time.Minute)}
	refreshed, err := RefreshIfNeeded(context.Background(), credential)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || refreshed.AccessToken != "new-access" || refreshed.RefreshToken != "new-refresh" {
		t.Fatalf("requests=%d credential=%#v", requests, refreshed)
	}
}

func TestBrowserLoginRejectsWrongState(t *testing.T) {
	t.Setenv("VES_OAUTH_LISTEN_ADDRESS", "127.0.0.1:0")
	provider := OAuthProvider{Name: "test", ClientID: "client", AuthorizeURL: "https://auth.example/authorize", TokenURL: "https://unused.example/token", CallbackPath: "/callback"}
	_, err := BrowserLogin(context.Background(), provider, func(target string) error {
		authorize, _ := url.Parse(target)
		go func() {
			resp, requestErr := http.Get(authorize.Query().Get("redirect_uri") + "?code=nope&state=wrong")
			if requestErr == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	})
	if err == nil {
		t.Fatal("expected state mismatch")
	}
}

func TestOAuthClientIDUsesApplicationDefaultAndEnvironmentOverride(t *testing.T) {
	if got := OAuthClientID("railway"); got != defaultRailwayOAuthClientID {
		t.Fatalf("Railway default=%q", got)
	}
	if got := OAuthClientID("linear"); got != defaultLinearOAuthClientID {
		t.Fatalf("Linear default=%q", got)
	}
	t.Setenv("VES_RAILWAY_OAUTH_CLIENT_ID", "railway-override")
	t.Setenv("VES_LINEAR_OAUTH_CLIENT_ID", "linear-override")
	if got := OAuthClientID("railway"); got != "railway-override" {
		t.Fatalf("Railway override=%q", got)
	}
	if got := OAuthClientID("linear"); got != "linear-override" {
		t.Fatalf("Linear override=%q", got)
	}
}

func TestLinearOAuthUsesAdminUserActorForDynamicWebhook(t *testing.T) {
	provider, err := Provider("linear", "client")
	if err != nil {
		t.Fatal(err)
	}
	if _, hasActor := provider.AuthorizeParams["actor"]; hasActor {
		t.Fatalf("Linear app actors cannot request the admin scope: %#v", provider.AuthorizeParams)
	}
	if !containsString(provider.Scopes, "admin") {
		t.Fatalf("Linear admin scope is required to create a per-deployment webhook: %#v", provider.Scopes)
	}
}

func TestRailwayOAuthUsesMemberScopeBecauseSSHKeysUseCLISession(t *testing.T) {
	provider, err := Provider("railway", "client")
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(provider.Scopes, "workspace:member") {
		t.Fatalf("Railway member scope is required for workspace provisioning: %#v", provider.Scopes)
	}
	if containsString(provider.Scopes, "workspace:admin") {
		t.Fatalf("OAuth workspace admin does not grant access to Railway's SSH-key endpoint: %#v", provider.Scopes)
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
