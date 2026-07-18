package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

func TestHostedDashboardOpenCreatesFreshOwnerClaimWhenWorkspaceIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VES_AUTH_STORE", "file")
	var claimKeys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer service-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/access/members":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case "/api/v1/access/owner-claims":
			claimKeys = append(claimKeys, r.Header.Get("Idempotency-Key"))
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"token": "fresh claim"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	if err := saveRailwaySecrets(root, railwaySecrets{ServiceToken: "service-token"}); err != nil {
		t.Fatal(err)
	}
	app := &App{Root: root, Config: config.Config{Hosted: config.HostedConfig{ControlPlaneURL: server.URL}}}
	first, err := hostedDashboardOpenURL(context.Background(), app)
	if err != nil || first != server.URL+"/?owner_claim=fresh+claim" {
		t.Fatalf("url=%q err=%v", first, err)
	}
	second, err := hostedDashboardOpenURL(context.Background(), app)
	if err != nil || second != first {
		t.Fatalf("second url=%q err=%v", second, err)
	}
	if len(claimKeys) != 2 || claimKeys[0] == claimKeys[1] || !strings.HasPrefix(claimKeys[0], "owner-claim:dashboard_") {
		t.Fatalf("owner claim idempotency keys=%q", claimKeys)
	}
}

func TestHostedDashboardOpenUsesBareURLForExistingMember(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VES_AUTH_STORE", "file")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/access/members" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"role": "owner"}}})
	}))
	defer server.Close()

	root := t.TempDir()
	if err := saveRailwaySecrets(root, railwaySecrets{ServiceToken: "service-token"}); err != nil {
		t.Fatal(err)
	}
	app := &App{Root: root, Config: config.Config{Hosted: config.HostedConfig{ControlPlaneURL: server.URL + "/"}}}
	target, err := hostedDashboardOpenURL(context.Background(), app)
	if err != nil || target != server.URL {
		t.Fatalf("url=%q err=%v", target, err)
	}
}
