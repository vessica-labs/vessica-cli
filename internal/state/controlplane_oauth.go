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
	if in.ClientID == "" || in.ActorID == "" || in.CodeHash == "" || in.ExpiresAt == "" {
		return nil, fmt.Errorf("oauth authorization code fields are required")
	}
	if in.ScopesJSON == "" {
		in.ScopesJSON = "[]"
	}
	if err = db.requireOAuthClient(ctx, in.ClientID); err != nil {
		return nil, err
	}
	v := &OAuthAuthorizationCode{ID: id.New("oauthcode"), WorkspaceID: ws.ID, ClientID: in.ClientID, ActorID: in.ActorID, CodeHash: in.CodeHash, RedirectURI: in.RedirectURI, ScopesJSON: in.ScopesJSON, ExpiresAt: in.ExpiresAt, CreatedAt: Now()}
	_, err = db.Exec(ctx, `INSERT INTO oauth_authorization_codes(id,workspace_id,client_id,actor_id,code_hash,redirect_uri,scopes_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.ClientID, v.ActorID, v.CodeHash, v.RedirectURI, v.ScopesJSON, v.ExpiresAt, v.CreatedAt)
	return v, err
}
func (db *DB) ConsumeOAuthAuthorizationCode(ctx context.Context, codeHash string) (*OAuthAuthorizationCode, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var v OAuthAuthorizationCode
	var consumed, revoked sql.NullString
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,client_id,actor_id,code_hash,redirect_uri,scopes_json,expires_at,consumed_at,revoked_at,created_at FROM oauth_authorization_codes WHERE workspace_id=? AND code_hash=? AND expires_at>? AND consumed_at IS NULL AND revoked_at IS NULL`), ws.ID, codeHash, Now()).Scan(&v.ID, &v.WorkspaceID, &v.ClientID, &v.ActorID, &v.CodeHash, &v.RedirectURI, &v.ScopesJSON, &v.ExpiresAt, &consumed, &revoked, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("oauth authorization code expired, revoked, or invalid")
	}
	if err != nil {
		return nil, err
	}
	v.ConsumedAt = Now()
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE oauth_authorization_codes SET consumed_at=? WHERE id=? AND consumed_at IS NULL`), v.ConsumedAt, v.ID); err != nil {
		return nil, err
	}
	return &v, tx.Commit()
}
func (db *DB) IssueOAuthAccessToken(ctx context.Context, in OAuthAccessTokenInput) (*OAuthAccessToken, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.ClientID == "" || in.ActorID == "" || in.TokenHash == "" || in.ExpiresAt == "" {
		return nil, fmt.Errorf("oauth access token fields are required")
	}
	if in.ScopesJSON == "" {
		in.ScopesJSON = "[]"
	}
	if err = db.requireOAuthClient(ctx, in.ClientID); err != nil {
		return nil, err
	}
	v := &OAuthAccessToken{ID: id.New("oauthaccess"), WorkspaceID: ws.ID, ClientID: in.ClientID, ActorID: in.ActorID, TokenHash: in.TokenHash, ScopesJSON: in.ScopesJSON, ExpiresAt: in.ExpiresAt, CreatedAt: Now()}
	_, err = db.Exec(ctx, `INSERT INTO oauth_access_tokens(id,workspace_id,client_id,actor_id,token_hash,scopes_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.ClientID, v.ActorID, v.TokenHash, v.ScopesJSON, v.ExpiresAt, v.CreatedAt)
	return v, err
}
func (db *DB) GetOAuthAccessToken(ctx context.Context, tokenHash string) (*OAuthAccessToken, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var v OAuthAccessToken
	var revoked sql.NullString
	err = db.QueryRow(ctx, `SELECT id,workspace_id,client_id,actor_id,token_hash,scopes_json,expires_at,revoked_at,created_at FROM oauth_access_tokens WHERE workspace_id=? AND token_hash=? AND expires_at>? AND revoked_at IS NULL`, ws.ID, tokenHash, Now()).Scan(&v.ID, &v.WorkspaceID, &v.ClientID, &v.ActorID, &v.TokenHash, &v.ScopesJSON, &v.ExpiresAt, &revoked, &v.CreatedAt)
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
	if in.ClientID == "" || in.ActorID == "" || in.MaterialHash == "" || in.FamilyID == "" || in.ExpiresAt == "" {
		return nil, fmt.Errorf("oauth refresh token fields are required")
	}
	if in.ScopesJSON == "" {
		in.ScopesJSON = "[]"
	}
	if err = db.requireOAuthClient(ctx, in.ClientID); err != nil {
		return nil, err
	}
	v := &OAuthRefreshToken{ID: id.New("oauthrefresh"), WorkspaceID: ws.ID, ClientID: in.ClientID, ActorID: in.ActorID, MaterialHash: in.MaterialHash, FamilyID: in.FamilyID, ScopesJSON: in.ScopesJSON, ExpiresAt: in.ExpiresAt, CreatedAt: Now()}
	_, err = db.Exec(ctx, `INSERT INTO oauth_refresh_tokens(id,workspace_id,client_id,actor_id,material_hash,family_id,scopes_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.ClientID, v.ActorID, v.MaterialHash, v.FamilyID, v.ScopesJSON, v.ExpiresAt, v.CreatedAt)
	return v, err
}

func (db *DB) GetOAuthRefreshToken(ctx context.Context, materialHash string) (*OAuthRefreshToken, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var v OAuthRefreshToken
	var replaced, revoked sql.NullString
	err = db.QueryRow(ctx, `SELECT id,workspace_id,client_id,actor_id,material_hash,family_id,scopes_json,expires_at,replaced_at,revoked_at,created_at FROM oauth_refresh_tokens WHERE workspace_id=? AND material_hash=? AND expires_at>? AND replaced_at IS NULL AND revoked_at IS NULL`, ws.ID, materialHash, Now()).Scan(&v.ID, &v.WorkspaceID, &v.ClientID, &v.ActorID, &v.MaterialHash, &v.FamilyID, &v.ScopesJSON, &v.ExpiresAt, &replaced, &revoked, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("oauth refresh token expired, rotated, revoked, or invalid")
	}
	v.ReplacedAt = replaced.String
	v.RevokedAt = revoked.String
	return &v, err
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

func (db *DB) requireOAuthClient(ctx context.Context, clientID string) error {
	client, err := db.GetOAuthClient(ctx, clientID)
	if err != nil {
		return err
	}
	if client.RevokedAt != "" {
		return fmt.Errorf("oauth client is revoked")
	}
	return nil
}
