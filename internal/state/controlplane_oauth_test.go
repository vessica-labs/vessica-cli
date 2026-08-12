package state

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Break caught: accepting or serializing raw OAuth credentials would expose a
// bearer secret; a client must instead be resolved by its stored hash only.
func TestOAuthCredentialsUseHashedLookupAndRedactedJSON(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	client, err := db.UpsertOAuthClient(ctx, OAuthClientInput{
		ClientID:         "mcp-client",
		Name:             "MCP client",
		RedirectURIsJSON: `["https://example.test/callback"]`,
		ScopesJSON:       `["agents.read"]`,
		SecretHash:       "client-secret-hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.ClientID != "mcp-client" {
		t.Fatalf("client=%#v", client)
	}
	if _, err = db.CreateOAuthAuthorizationCode(ctx, OAuthAuthorizationCodeInput{
		ClientID:   client.ClientID,
		ActorID:    "user_1",
		CodeHash:   "authorization-code-hash",
		ScopesJSON: `["agents.read"]`,
		ExpiresAt:  FormatTime(time.Now().Add(time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.IssueOAuthAccessToken(ctx, OAuthAccessTokenInput{
		ClientID:   client.ClientID,
		ActorID:    "user_1",
		TokenHash:  "access-token-hash",
		ScopesJSON: `["agents.read"]`,
		ExpiresAt:  FormatTime(time.Now().Add(time.Hour)),
	}); err != nil {
		t.Fatal(err)
	}
	access, err := db.GetOAuthAccessToken(ctx, "access-token-hash")
	if err != nil {
		t.Fatal(err)
	}
	if access.ActorID != "user_1" || access.ClientID != client.ClientID || access.TokenHash != "access-token-hash" {
		t.Fatalf("access=%#v", access)
	}
	raw, err := json.Marshal(access)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "access-token-hash") {
		t.Fatalf("access JSON exposed token hash: %s", raw)
	}
}

// Break caught: a revoked refresh token can still be used to mint another
// access token, defeating rotation or explicit revocation.
func TestOAuthRefreshTokenRevocationUsesHashOnly(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	client, err := db.UpsertOAuthClient(ctx, OAuthClientInput{ClientID: "web", Name: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := db.IssueOAuthRefreshToken(ctx, OAuthRefreshTokenInput{ClientID: client.ClientID, ActorID: "user_1", MaterialHash: "refresh-hash", FamilyID: "family-1", ExpiresAt: FormatTime(time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.RevokeOAuthRefreshToken(ctx, "refresh-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetOAuthRefreshToken(ctx, "refresh-hash"); err == nil {
		t.Fatal("revoked refresh token was accepted")
	}
	raw, err := json.Marshal(refresh)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "refresh-hash") {
		t.Fatalf("refresh JSON exposed token hash: %s", raw)
	}
}

// Break caught: an OAuth client from one workspace can be used to issue a
// token in another workspace sharing the same control database.
func TestOAuthTokenCannotCrossWorkspaceBoundary(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	first, err := Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err = first.EnsureWorkspace(ctx, "workspace-one", "hosted"); err != nil {
		t.Fatal(err)
	}
	client, err := first.UpsertOAuthClient(ctx, OAuthClientInput{ClientID: "mcp", Name: "MCP"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err = second.EnsureWorkspace(ctx, "workspace-two", "hosted"); err != nil {
		t.Fatal(err)
	}
	if _, err = second.IssueOAuthAccessToken(ctx, OAuthAccessTokenInput{ClientID: client.ClientID, ActorID: "user_2", TokenHash: "other-workspace-token-hash", ExpiresAt: FormatTime(time.Now().Add(time.Hour))}); err == nil {
		t.Fatal("cross-workspace OAuth client was accepted")
	}
}
