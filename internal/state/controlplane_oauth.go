package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) UpsertOAuthClient(ctx context.Context, in OAuthClientInput) (*OAuthClient, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.ClientID == "" || in.Name == "" {
		return nil, fmt.Errorf("oauth client id and name are required")
	}
	if in.RedirectURIsJSON == "" {
		in.RedirectURIsJSON = "[]"
	}
	if in.ScopesJSON == "" {
		in.ScopesJSON = "[]"
	}
	now := Now()
	_, err = db.Exec(ctx, `INSERT INTO oauth_clients(id,workspace_id,client_id,name,redirect_uris_json,scopes_json,secret_hash,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,client_id) DO UPDATE SET name=excluded.name,redirect_uris_json=excluded.redirect_uris_json,scopes_json=excluded.scopes_json,secret_hash=excluded.secret_hash,updated_at=excluded.updated_at`, id.New("oauthclient"), ws.ID, in.ClientID, in.Name, in.RedirectURIsJSON, in.ScopesJSON, in.SecretHash, now, now)
	if err != nil {
		return nil, err
	}
	return db.GetOAuthClient(ctx, in.ClientID)
}
func (db *DB) GetOAuthClient(ctx context.Context, ref string) (*OAuthClient, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var v OAuthClient
	var revoked sql.NullString
	err = db.QueryRow(ctx, `SELECT id,workspace_id,client_id,name,redirect_uris_json,scopes_json,secret_hash,revoked_at,created_at,updated_at FROM oauth_clients WHERE workspace_id=? AND (id=? OR client_id=?)`, ws.ID, ref, ref).Scan(&v.ID, &v.WorkspaceID, &v.ClientID, &v.Name, &v.RedirectURIsJSON, &v.ScopesJSON, &v.SecretHash, &revoked, &v.CreatedAt, &v.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("oauth client not found")
	}
	v.RevokedAt = revoked.String
	return &v, err
}
func (db *DB) CreateOAuthAuthorizationCode(ctx context.Context, in OAuthAuthorizationCodeInput) (*OAuthAuthorizationCode, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.ClientID == "" || in.ActorID == "" || in.CodeHash == "" || in.ExpiresAt == "" || in.Resource == "" {
		return nil, fmt.Errorf("oauth authorization code fields are required")
	}
	if in.ScopesJSON == "" {
		in.ScopesJSON = "[]"
	}
	client, err := db.requireOAuthClient(ctx, in.ClientID)
	if err != nil {
		return nil, err
	}
	v := &OAuthAuthorizationCode{ID: id.New("oauthcode"), WorkspaceID: ws.ID, ClientID: client.ClientID, ActorID: in.ActorID, CodeHash: in.CodeHash, RedirectURI: in.RedirectURI, ScopesJSON: in.ScopesJSON, ExpiresAt: in.ExpiresAt, CodeChallenge: in.CodeChallenge, CodeChallengeMethod: in.CodeChallengeMethod, Resource: in.Resource, CreatedAt: Now()}
	_, err = db.Exec(ctx, `INSERT INTO oauth_authorization_codes(id,workspace_id,client_id,actor_id,code_hash,redirect_uri,scopes_json,expires_at,code_challenge,code_challenge_method,resource,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, client.ID, v.ActorID, v.CodeHash, v.RedirectURI, v.ScopesJSON, v.ExpiresAt, v.CodeChallenge, v.CodeChallengeMethod, v.Resource, v.CreatedAt)
	return v, err
}

// ExchangeOAuthAuthorizationCode consumes one authorization code only when all
// redirect, client, and PKCE bindings match. A bad verifier therefore cannot
// burn a valid code, and concurrent exchanges still have one winner.
func (db *DB) ExchangeOAuthAuthorizationCode(ctx context.Context, codeHash, clientID, redirectURI, codeChallenge, resource string) (*OAuthAuthorizationCode, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	now := Now()
	result, err := db.Exec(ctx, `UPDATE oauth_authorization_codes SET consumed_at=? WHERE workspace_id=? AND code_hash=? AND client_id=(SELECT id FROM oauth_clients WHERE workspace_id=? AND client_id=? AND revoked_at IS NULL) AND redirect_uri=? AND code_challenge=? AND code_challenge_method='S256' AND resource=? AND expires_at>? AND consumed_at IS NULL AND revoked_at IS NULL`, now, ws.ID, codeHash, ws.ID, clientID, redirectURI, codeChallenge, resource, now)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, fmt.Errorf("oauth authorization code expired, revoked, invalid, or PKCE verification failed")
	}
	var v OAuthAuthorizationCode
	var revoked sql.NullString
	err = db.QueryRow(ctx, `SELECT oc.id,oc.workspace_id,c.client_id,oc.actor_id,oc.code_hash,oc.redirect_uri,oc.scopes_json,oc.expires_at,oc.code_challenge,oc.code_challenge_method,oc.resource,oc.consumed_at,oc.revoked_at,oc.created_at FROM oauth_authorization_codes oc JOIN oauth_clients c ON c.id=oc.client_id WHERE oc.workspace_id=? AND oc.code_hash=?`, ws.ID, codeHash).Scan(&v.ID, &v.WorkspaceID, &v.ClientID, &v.ActorID, &v.CodeHash, &v.RedirectURI, &v.ScopesJSON, &v.ExpiresAt, &v.CodeChallenge, &v.CodeChallengeMethod, &v.Resource, &v.ConsumedAt, &revoked, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	v.RevokedAt = revoked.String
	return &v, nil
}
func (db *DB) IssueOAuthAccessToken(ctx context.Context, in OAuthAccessTokenInput) (*OAuthAccessToken, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.ClientID == "" || in.ActorID == "" || in.TokenHash == "" || in.ExpiresAt == "" || in.Resource == "" {
		return nil, fmt.Errorf("oauth access token fields are required")
	}
	if in.ScopesJSON == "" {
		in.ScopesJSON = "[]"
	}
	client, err := db.requireOAuthClient(ctx, in.ClientID)
	if err != nil {
		return nil, err
	}
	v := &OAuthAccessToken{ID: id.New("oauthaccess"), WorkspaceID: ws.ID, ClientID: client.ClientID, ActorID: in.ActorID, TokenHash: in.TokenHash, FamilyID: in.FamilyID, Resource: in.Resource, ScopesJSON: in.ScopesJSON, ExpiresAt: in.ExpiresAt, CreatedAt: Now()}
	result, err := db.Exec(ctx, `INSERT INTO oauth_access_tokens(id,workspace_id,client_id,actor_id,token_hash,family_id,resource,scopes_json,expires_at,created_at) SELECT ?,?,?,?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM oauth_refresh_tokens WHERE workspace_id=? AND family_id=? AND revoked_at IS NOT NULL)`, v.ID, v.WorkspaceID, client.ID, v.ActorID, v.TokenHash, v.FamilyID, v.Resource, v.ScopesJSON, v.ExpiresAt, v.CreatedAt, ws.ID, v.FamilyID)
	if err == nil {
		changed, _ := result.RowsAffected()
		if changed != 1 {
			err = fmt.Errorf("oauth token family is revoked")
		}
	}
	return v, err
}
func (db *DB) GetOAuthAccessToken(ctx context.Context, tokenHash, resource string) (*OAuthAccessToken, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var v OAuthAccessToken
	var revoked sql.NullString
	err = db.QueryRow(ctx, `SELECT oat.id,oat.workspace_id,oc.client_id,oat.actor_id,oat.token_hash,oat.family_id,oat.resource,oat.scopes_json,oat.expires_at,oat.revoked_at,oat.created_at FROM oauth_access_tokens oat JOIN oauth_clients oc ON oc.id=oat.client_id WHERE oat.workspace_id=? AND oat.token_hash=? AND oat.resource=? AND oat.expires_at>? AND oat.revoked_at IS NULL AND oc.revoked_at IS NULL`, ws.ID, tokenHash, resource, Now()).Scan(&v.ID, &v.WorkspaceID, &v.ClientID, &v.ActorID, &v.TokenHash, &v.FamilyID, &v.Resource, &v.ScopesJSON, &v.ExpiresAt, &revoked, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("oauth access token expired, revoked, or invalid")
	}
	v.RevokedAt = revoked.String
	return &v, err
}

func (db *DB) IssueOAuthRefreshToken(ctx context.Context, in OAuthRefreshTokenInput) (*OAuthRefreshToken, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.ClientID == "" || in.ActorID == "" || in.MaterialHash == "" || in.FamilyID == "" || in.ExpiresAt == "" || in.Resource == "" {
		return nil, fmt.Errorf("oauth refresh token fields are required")
	}
	if in.ScopesJSON == "" {
		in.ScopesJSON = "[]"
	}
	client, err := db.requireOAuthClient(ctx, in.ClientID)
	if err != nil {
		return nil, err
	}
	v := &OAuthRefreshToken{ID: id.New("oauthrefresh"), WorkspaceID: ws.ID, ClientID: client.ClientID, ActorID: in.ActorID, MaterialHash: in.MaterialHash, FamilyID: in.FamilyID, Resource: in.Resource, ScopesJSON: in.ScopesJSON, ExpiresAt: in.ExpiresAt, CreatedAt: Now()}
	result, err := db.Exec(ctx, `INSERT INTO oauth_refresh_tokens(id,workspace_id,client_id,actor_id,material_hash,family_id,resource,scopes_json,expires_at,created_at) SELECT ?,?,?,?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM oauth_refresh_tokens WHERE workspace_id=? AND family_id=? AND revoked_at IS NOT NULL)`, v.ID, v.WorkspaceID, client.ID, v.ActorID, v.MaterialHash, v.FamilyID, v.Resource, v.ScopesJSON, v.ExpiresAt, v.CreatedAt, ws.ID, v.FamilyID)
	if err == nil {
		changed, _ := result.RowsAffected()
		if changed != 1 {
			err = fmt.Errorf("oauth refresh token family is revoked")
		}
	}
	return v, err
}

func (db *DB) GetOAuthRefreshToken(ctx context.Context, materialHash, resource string) (*OAuthRefreshToken, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var v OAuthRefreshToken
	var replaced, revoked sql.NullString
	err = db.QueryRow(ctx, `SELECT ort.id,ort.workspace_id,oc.client_id,ort.actor_id,ort.material_hash,ort.family_id,ort.resource,ort.scopes_json,ort.expires_at,ort.replaced_at,ort.revoked_at,ort.created_at FROM oauth_refresh_tokens ort JOIN oauth_clients oc ON oc.id=ort.client_id WHERE ort.workspace_id=? AND ort.material_hash=? AND ort.resource=? AND ort.expires_at>? AND ort.replaced_at IS NULL AND ort.revoked_at IS NULL AND oc.revoked_at IS NULL`, ws.ID, materialHash, resource, Now()).Scan(&v.ID, &v.WorkspaceID, &v.ClientID, &v.ActorID, &v.MaterialHash, &v.FamilyID, &v.Resource, &v.ScopesJSON, &v.ExpiresAt, &replaced, &revoked, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("oauth refresh token expired, rotated, revoked, or invalid")
	}
	v.ReplacedAt = replaced.String
	v.RevokedAt = revoked.String
	return &v, err
}

// ConsumeOAuthRefreshToken atomically rotates a refresh credential. Token
// issuance happens after this one-time state transition and never stores raw
// credential material.
func (db *DB) ConsumeOAuthRefreshToken(ctx context.Context, materialHash, clientID, resource string) (*OAuthRefreshToken, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	now := Now()
	result, err := db.Exec(ctx, `UPDATE oauth_refresh_tokens SET replaced_at=? WHERE workspace_id=? AND material_hash=? AND client_id=(SELECT id FROM oauth_clients WHERE workspace_id=? AND client_id=? AND revoked_at IS NULL) AND resource=? AND expires_at>? AND replaced_at IS NULL AND revoked_at IS NULL`, now, ws.ID, materialHash, ws.ID, clientID, resource, now)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		var familyID string
		var replaced, revoked sql.NullString
		lookupErr := db.QueryRow(ctx, `SELECT ort.family_id,ort.replaced_at,ort.revoked_at FROM oauth_refresh_tokens ort JOIN oauth_clients oc ON oc.id=ort.client_id WHERE ort.workspace_id=? AND ort.material_hash=? AND oc.client_id=? AND ort.resource=?`, ws.ID, materialHash, clientID, resource).Scan(&familyID, &replaced, &revoked)
		if lookupErr == nil && (replaced.Valid || revoked.Valid) {
			tx, txErr := db.SQL.BeginTx(ctx, nil)
			if txErr != nil {
				return nil, txErr
			}
			if _, txErr = tx.ExecContext(ctx, db.Rebind(`UPDATE oauth_refresh_tokens SET revoked_at=COALESCE(revoked_at,?) WHERE workspace_id=? AND family_id=?`), now, ws.ID, familyID); txErr == nil {
				_, txErr = tx.ExecContext(ctx, db.Rebind(`UPDATE oauth_access_tokens SET revoked_at=COALESCE(revoked_at,?) WHERE workspace_id=? AND family_id=?`), now, ws.ID, familyID)
			}
			if txErr != nil {
				_ = tx.Rollback()
				return nil, txErr
			}
			if txErr = tx.Commit(); txErr != nil {
				return nil, txErr
			}
		}
		return nil, fmt.Errorf("oauth refresh token expired, replayed, revoked, invalid, or belongs to another client")
	}
	var v OAuthRefreshToken
	var replaced, revoked sql.NullString
	err = db.QueryRow(ctx, `SELECT ort.id,ort.workspace_id,oc.client_id,ort.actor_id,ort.material_hash,ort.family_id,ort.resource,ort.scopes_json,ort.expires_at,ort.replaced_at,ort.revoked_at,ort.created_at FROM oauth_refresh_tokens ort JOIN oauth_clients oc ON oc.id=ort.client_id WHERE ort.workspace_id=? AND ort.material_hash=?`, ws.ID, materialHash).Scan(&v.ID, &v.WorkspaceID, &v.ClientID, &v.ActorID, &v.MaterialHash, &v.FamilyID, &v.Resource, &v.ScopesJSON, &v.ExpiresAt, &replaced, &revoked, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	v.ReplacedAt, v.RevokedAt = replaced.String, revoked.String
	return &v, nil
}

func (db *DB) RevokeOAuthRefreshToken(ctx context.Context, materialHash string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	result, err := db.Exec(ctx, `UPDATE oauth_refresh_tokens SET revoked_at=COALESCE(revoked_at,?) WHERE workspace_id=? AND material_hash=?`, Now(), ws.ID, materialHash)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return fmt.Errorf("oauth refresh token not found")
	}
	return nil
}

func (db *DB) RevokeOAuthAccessToken(ctx context.Context, tokenHash string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	result, err := db.Exec(ctx, `UPDATE oauth_access_tokens SET revoked_at=COALESCE(revoked_at,?) WHERE workspace_id=? AND token_hash=?`, Now(), ws.ID, tokenHash)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return fmt.Errorf("oauth access token not found")
	}
	return nil
}

func (db *DB) requireOAuthClient(ctx context.Context, clientID string) (*OAuthClient, error) {
	client, err := db.GetOAuthClient(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if client.RevokedAt != "" {
		return nil, fmt.Errorf("oauth client is revoked")
	}
	return client, nil
}
