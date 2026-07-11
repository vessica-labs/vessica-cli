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
