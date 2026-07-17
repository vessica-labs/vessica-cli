package controlplane

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestCredentialManagerEncryptsAndReturnsAccessToken(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	credential := auth.OAuthCredential{Provider: "linear", ClientID: "client", TokenURL: "https://example.invalid/token", AccessToken: "super-secret-access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour), UpdatedAt: time.Now()}
	raw, _ := json.Marshal(credential)
	key := base64.RawStdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	manager, err := NewCredentialManager(ctx, db, key, map[string]string{"linear": string(raw)})
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Token(ctx, "linear")
	if err != nil || token != credential.AccessToken {
		t.Fatalf("token=%q err=%v", token, err)
	}
	encrypted, err := db.GetHostedCredential(ctx, "linear")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, credential.AccessToken) || strings.Contains(encrypted, credential.RefreshToken) {
		t.Fatal("credential was stored in plaintext")
	}
}

func TestCredentialManagerRejectsInvalidKey(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := NewCredentialManager(context.Background(), db, "short", nil); err == nil {
		t.Fatal("expected invalid key error")
	}
}

func TestCredentialManagerPersistsRotatedRefreshToken(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "rotated-access", "refresh_token": "rotated-refresh", "expires_in": 3600})
	}))
	defer server.Close()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	credential := auth.OAuthCredential{Provider: "railway", ClientID: "client", TokenURL: server.URL, AccessToken: "expired", RefreshToken: "original-refresh", ExpiresAt: time.Now().Add(-time.Minute), UpdatedAt: time.Now().Add(-time.Hour)}
	raw, _ := json.Marshal(credential)
	key := base64.RawStdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	manager, err := NewCredentialManager(ctx, db, key, map[string]string{"railway": string(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if token, err := manager.Token(ctx, "railway"); err != nil || token != "rotated-access" {
		t.Fatalf("token=%q err=%v", token, err)
	}
	reloaded, err := NewCredentialManager(ctx, db, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token, err := reloaded.Token(ctx, "railway"); err != nil || token != "rotated-access" {
		t.Fatalf("reloaded token=%q err=%v", token, err)
	}
}

func TestCredentialManagerReplacesCredentialWhenSwitchingLinearWorkspaces(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := base64.RawStdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	oldCredential := oauthJSON(t, "linear", "brightwire-access", time.Now().Add(-time.Hour))
	if _, err := NewCredentialManager(ctx, db, key, map[string]string{"linear": oldCredential}); err != nil {
		t.Fatal(err)
	}

	newCredential := oauthJSON(t, "linear", "vessica-labs-access", time.Now())
	manager, err := NewCredentialManager(ctx, db, key, map[string]string{"linear": newCredential})
	if err != nil {
		t.Fatal(err)
	}
	if token, err := manager.Token(ctx, "linear"); err != nil || token != "vessica-labs-access" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestCredentialManagerPreservesNewerDatabaseCredential(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := base64.RawStdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	manager, err := NewCredentialManager(ctx, db, key, map[string]string{
		"linear": oauthJSON(t, "linear", "initial-access", time.Now().Add(-2*time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}
	refreshed := oauthJSON(t, "linear", "database-refreshed-access", time.Now())
	if err := manager.persist(ctx, "linear", []byte(refreshed)); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewCredentialManager(ctx, db, key, map[string]string{
		"linear": oauthJSON(t, "linear", "stale-environment-access", time.Now().Add(-time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if token, err := reloaded.Token(ctx, "linear"); err != nil || token != "database-refreshed-access" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestCredentialRotationValidatesBeforeReplacingStoredCredential(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := base64.RawStdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	manager, err := NewCredentialManagerWithValidator(ctx, db, key, nil, func(_ context.Context, _ string, token string) error {
		if token == "rejected-access" {
			return context.Canceled
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Rotate(ctx, "linear", oauthJSON(t, "linear", "accepted-access", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := manager.Rotate(ctx, "linear", oauthJSON(t, "linear", "rejected-access", time.Now().Add(time.Minute))); err == nil {
		t.Fatal("expected rejected rotation")
	}
	if token, err := manager.Token(ctx, "linear"); err != nil || token != "accepted-access" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func TestValidBootstrapCredentialRepairsCorruptStoredCredential(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PutHostedCredential(ctx, "linear", "corrupt-ciphertext"); err != nil {
		t.Fatal(err)
	}
	key := base64.RawStdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	manager, err := NewCredentialManagerWithValidator(ctx, db, key, map[string]string{
		"linear": oauthJSON(t, "linear", "replacement-access", time.Now()),
	}, func(context.Context, string, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if token, err := manager.Token(ctx, "linear"); err != nil || token != "replacement-access" {
		t.Fatalf("token=%q err=%v", token, err)
	}
}

func oauthJSON(t *testing.T, provider, accessToken string, updatedAt time.Time) string {
	t.Helper()
	raw, err := json.Marshal(auth.OAuthCredential{
		Provider: provider, ClientID: "client", TokenURL: "https://example.invalid/token",
		AccessToken: accessToken, ExpiresAt: time.Now().Add(time.Hour), UpdatedAt: updatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
