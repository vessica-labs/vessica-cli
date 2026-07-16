package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsureHostedOwnerClaim(t *testing.T) {
	claimRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer service-token" {
			t.Fatalf("missing service authorization")
		}
		switch r.URL.Path {
		case "/api/v1/access/members":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/api/v1/access/owner-claims":
			claimRequests++
			if r.Header.Get("Idempotency-Key") != "owner-claim:onb_1" {
				t.Fatalf("unexpected idempotency key %q", r.Header.Get("Idempotency-Key"))
			}
			_, _ = w.Write([]byte(`{"data":{"token":"claim token"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	claimURL, required, err := ensureHostedOwnerClaim(context.Background(), server.URL, "service-token", "onb_1")
	if err != nil || !required || claimRequests != 1 || !strings.HasSuffix(claimURL, "?owner_claim=claim+token") {
		t.Fatalf("url=%q required=%v requests=%d err=%v", claimURL, required, claimRequests, err)
	}
}

func TestEnsureHostedOwnerClaimSkipsExistingOwner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/access/members" {
			_, _ = w.Write([]byte(`{"data":[{"role":"owner"}]}`))
			return
		}
		t.Fatalf("unexpected owner claim request")
	}))
	defer server.Close()

	claimURL, required, err := ensureHostedOwnerClaim(context.Background(), server.URL, "service-token", "onb_2")
	if err != nil || required || claimURL != "" {
		t.Fatalf("url=%q required=%v err=%v", claimURL, required, err)
	}
}
