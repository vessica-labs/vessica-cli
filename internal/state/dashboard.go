package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

type DashboardUser struct {
	ID          string `json:"id"`
	GitHubID    string `json:"github_id"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}
type WorkspaceMembership struct {
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}
type DashboardSession struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	TokenHash   string `json:"-"`
	CSRFHash    string `json:"-"`
	ExpiresAt   string `json:"expires_at"`
	CreatedAt   string `json:"created_at"`
	LastSeenAt  string `json:"last_seen_at"`
}
type DashboardInvitation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	GitHubLogin string `json:"github_login"`
	Role        string `json:"role"`
	TokenHash   string `json:"-"`
	ExpiresAt   string `json:"expires_at"`
	AcceptedAt  string `json:"accepted_at,omitempty"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}
type DashboardAudit struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	UserID       string `json:"user_id,omitempty"`
	Action       string `json:"action"`
	TargetType   string `json:"target_type"`
	TargetID     string `json:"target_id"`
	RequestID    string `json:"request_id"`
	MetadataJSON string `json:"metadata_json"`
	CreatedAt    string `json:"created_at"`
}
type HostingOperation struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	Stage          string `json:"stage"`
	InputJSON      string `json:"input_json"`
	ResultJSON     string `json:"result_json"`
	Error          string `json:"error,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
	CreatedBy      string `json:"created_by"`
	Attempts       int    `json:"attempts"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	FinishedAt     string `json:"finished_at,omitempty"`
}
type HostingOperationEvent struct {
	ID          string `json:"id"`
	OperationID string `json:"operation_id"`
	Seq         int64  `json:"seq"`
	Stage       string `json:"stage"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	DetailJSON  string `json:"detail_json"`
	CreatedAt   string `json:"created_at"`
}

func (db *DB) UpsertDashboardUser(ctx context.Context, githubID, login, displayName, avatarURL string) (*DashboardUser, error) {
	now := Now()
	userID := id.New("usr")
	_, err := db.Exec(ctx, `INSERT INTO dashboard_users(id,github_id,login,display_name,avatar_url,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(github_id) DO UPDATE SET login=excluded.login,display_name=excluded.display_name,avatar_url=excluded.avatar_url,updated_at=excluded.updated_at`, userID, githubID, login, displayName, avatarURL, now, now)
	if err != nil {
		return nil, err
	}
	var v DashboardUser
	err = db.QueryRow(ctx, `SELECT id,github_id,login,display_name,avatar_url,created_at,updated_at FROM dashboard_users WHERE github_id=?`, githubID).Scan(&v.ID, &v.GitHubID, &v.Login, &v.DisplayName, &v.AvatarURL, &v.CreatedAt, &v.UpdatedAt)
	return &v, err
}

func (db *DB) UpsertMembership(ctx context.Context, userID, role string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if role != "owner" && role != "member" {
		return fmt.Errorf("invalid dashboard role: %s", role)
	}
	now := Now()
	_, err = db.Exec(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,role,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(workspace_id,user_id) DO UPDATE SET role=excluded.role,updated_at=excluded.updated_at`, ws.ID, userID, role, now, now)
	return err
}
func (db *DB) GetMembership(ctx context.Context, userID string) (*WorkspaceMembership, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var v WorkspaceMembership
	err = db.QueryRow(ctx, `SELECT workspace_id,user_id,role,created_at,updated_at FROM workspace_memberships WHERE workspace_id=? AND user_id=?`, ws.ID, userID).Scan(&v.WorkspaceID, &v.UserID, &v.Role, &v.CreatedAt, &v.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("dashboard membership not found")
	}
	return &v, err
}
func (db *DB) ListMemberships(ctx context.Context) ([]map[string]any, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT u.id,u.login,u.display_name,u.avatar_url,m.role,m.created_at FROM workspace_memberships m JOIN dashboard_users u ON u.id=m.user_id WHERE m.workspace_id=? ORDER BY u.login`, ws.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var uid, login, name, avatar, role, created string
		if err := rows.Scan(&uid, &login, &name, &avatar, &role, &created); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": uid, "login": login, "display_name": name, "avatar_url": avatar, "role": role, "created_at": created})
	}
	return out, rows.Err()
}

func (db *DB) CreateDashboardSession(ctx context.Context, userID, role, tokenHash, csrfHash, expiresAt string) (*DashboardSession, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	v := &DashboardSession{ID: id.New("sess"), WorkspaceID: ws.ID, UserID: userID, Role: role, TokenHash: tokenHash, CSRFHash: csrfHash, ExpiresAt: expiresAt, CreatedAt: Now(), LastSeenAt: Now()}
	_, err = db.Exec(ctx, `INSERT INTO dashboard_sessions(id,workspace_id,user_id,role,token_hash,csrf_hash,expires_at,created_at,last_seen_at) VALUES(?,?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.UserID, v.Role, v.TokenHash, v.CSRFHash, v.ExpiresAt, v.CreatedAt, v.LastSeenAt)
	return v, err
}
func (db *DB) GetDashboardSession(ctx context.Context, tokenHash string) (*DashboardSession, error) {
	var v DashboardSession
	err := db.QueryRow(ctx, `SELECT id,workspace_id,user_id,role,token_hash,csrf_hash,expires_at,created_at,last_seen_at FROM dashboard_sessions WHERE token_hash=? AND expires_at>?`, tokenHash, Now()).Scan(&v.ID, &v.WorkspaceID, &v.UserID, &v.Role, &v.TokenHash, &v.CSRFHash, &v.ExpiresAt, &v.CreatedAt, &v.LastSeenAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("dashboard session expired or invalid")
	}
	if err == nil {
		_, _ = db.Exec(ctx, `UPDATE dashboard_sessions SET last_seen_at=? WHERE id=?`, Now(), v.ID)
	}
	return &v, err
}
func (db *DB) DeleteDashboardSession(ctx context.Context, tokenHash string) error {
	_, err := db.Exec(ctx, `DELETE FROM dashboard_sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (db *DB) CreateInvitation(ctx context.Context, login, role, tokenHash, expiresAt, createdBy string) (*DashboardInvitation, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	v := &DashboardInvitation{ID: id.New("inv"), WorkspaceID: ws.ID, GitHubLogin: login, Role: role, TokenHash: tokenHash, ExpiresAt: expiresAt, CreatedBy: createdBy, CreatedAt: Now()}
	_, err = db.Exec(ctx, `INSERT INTO dashboard_invitations(id,workspace_id,github_login,role,token_hash,expires_at,created_by,created_at) VALUES(?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.GitHubLogin, v.Role, v.TokenHash, v.ExpiresAt, v.CreatedBy, v.CreatedAt)
	return v, err
}
func (db *DB) AcceptInvitation(ctx context.Context, tokenHash, login, userID string) (*DashboardInvitation, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var v DashboardInvitation
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,github_login,role,token_hash,expires_at,COALESCE(accepted_at,''),created_by,created_at FROM dashboard_invitations WHERE token_hash=? AND accepted_at IS NULL AND expires_at>? AND LOWER(github_login)=LOWER(?)`), tokenHash, Now(), login).Scan(&v.ID, &v.WorkspaceID, &v.GitHubLogin, &v.Role, &v.TokenHash, &v.ExpiresAt, &v.AcceptedAt, &v.CreatedBy, &v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("invitation expired, invalid, or intended for another GitHub user")
	}
	now := Now()
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE dashboard_invitations SET accepted_at=? WHERE id=?`), now, v.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO workspace_memberships(workspace_id,user_id,role,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(workspace_id,user_id) DO UPDATE SET role=excluded.role,updated_at=excluded.updated_at`), v.WorkspaceID, userID, v.Role, now, now); err != nil {
		return nil, err
	}
	return &v, tx.Commit()
}

func (db *DB) CreateOwnerClaim(ctx context.Context, tokenHash, expiresAt string) (string, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return "", err
	}
	claimID := id.New("claim")
	_, err = db.Exec(ctx, `INSERT INTO dashboard_owner_claims(id,workspace_id,token_hash,expires_at,created_at) VALUES(?,?,?,?,?)`, claimID, ws.ID, tokenHash, expiresAt, Now())
	return claimID, err
}
func (db *DB) ClaimOwner(ctx context.Context, tokenHash, userID string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	var claimID string
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id FROM dashboard_owner_claims WHERE workspace_id=? AND token_hash=? AND claimed_at IS NULL AND expires_at>?`), ws.ID, tokenHash, Now()).Scan(&claimID)
	if err != nil {
		return fmt.Errorf("owner claim expired or invalid")
	}
	now := Now()
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE dashboard_owner_claims SET claimed_by=?,claimed_at=? WHERE id=?`), userID, now, claimID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO workspace_memberships(workspace_id,user_id,role,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(workspace_id,user_id) DO UPDATE SET role=excluded.role,updated_at=excluded.updated_at`), ws.ID, userID, "owner", now, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) AppendDashboardAudit(ctx context.Context, userID, action, targetType, targetID, requestID string, metadata any) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(metadata)
	_, err = db.Exec(ctx, `INSERT INTO dashboard_audit_events(id,workspace_id,user_id,action,target_type,target_id,request_id,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, id.New("audit"), ws.ID, nullStr(userID), action, targetType, targetID, requestID, string(raw), Now())
	return err
}
func (db *DB) ListDashboardAudit(ctx context.Context, limit int) ([]DashboardAudit, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(ctx, `SELECT id,workspace_id,COALESCE(user_id,''),action,target_type,target_id,request_id,metadata_json,created_at FROM dashboard_audit_events WHERE workspace_id=? ORDER BY created_at DESC LIMIT ?`, ws.ID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DashboardAudit
	for rows.Next() {
		var v DashboardAudit
		if err := rows.Scan(&v.ID, &v.WorkspaceID, &v.UserID, &v.Action, &v.TargetType, &v.TargetID, &v.RequestID, &v.MetadataJSON, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (db *DB) GetDashboardIdempotency(ctx context.Context, actorID, key string) (json.RawMessage, bool, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, false, err
	}
	var raw string
	err = db.QueryRow(ctx, `SELECT result_json FROM dashboard_idempotency WHERE workspace_id=? AND actor_id=? AND key=?`, ws.ID, actorID, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(raw), true, nil
}
func (db *DB) PutDashboardIdempotency(ctx context.Context, actorID, key, action string, result any) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO dashboard_idempotency(workspace_id,actor_id,key,action,result_json,created_at) VALUES(?,?,?,?,?,?) ON CONFLICT(workspace_id,actor_id,key) DO NOTHING`, ws.ID, actorID, key, action, string(raw), Now())
	return err
}

func (db *DB) CreateHostingOperation(ctx context.Context, kind, idempotencyKey, createdBy string, input any) (*HostingOperation, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(input)
	v := &HostingOperation{ID: id.New("hop"), WorkspaceID: ws.ID, Kind: kind, Status: "pending", Stage: "queued", InputJSON: string(raw), ResultJSON: "{}", IdempotencyKey: idempotencyKey, CreatedBy: createdBy, CreatedAt: Now(), UpdatedAt: Now()}
	_, err = db.Exec(ctx, `INSERT INTO hosting_operations(id,workspace_id,kind,status,stage,input_json,result_json,idempotency_key,created_by,attempts,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.Kind, v.Status, v.Stage, v.InputJSON, v.ResultJSON, v.IdempotencyKey, v.CreatedBy, 0, v.CreatedAt, v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return v, nil
}
func (db *DB) GetHostingOperation(ctx context.Context, operationID string) (*HostingOperation, error) {
	var v HostingOperation
	err := db.QueryRow(ctx, `SELECT id,workspace_id,kind,status,stage,input_json,result_json,COALESCE(error,''),idempotency_key,created_by,attempts,created_at,updated_at,COALESCE(finished_at,'') FROM hosting_operations WHERE id=?`, operationID).Scan(&v.ID, &v.WorkspaceID, &v.Kind, &v.Status, &v.Stage, &v.InputJSON, &v.ResultJSON, &v.Error, &v.IdempotencyKey, &v.CreatedBy, &v.Attempts, &v.CreatedAt, &v.UpdatedAt, &v.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("hosting operation not found: %s", operationID)
	}
	return &v, err
}
func (db *DB) BeginHostingOperation(ctx context.Context, operationID, stage string) error {
	result, err := db.Exec(ctx, `UPDATE hosting_operations SET status='running',stage=?,error=NULL,finished_at=NULL,attempts=attempts+1,updated_at=? WHERE id=? AND status IN ('pending','failed')`, stage, Now(), operationID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("hosting operation cannot start from its current state")
	}
	return nil
}
func (db *DB) UpdateHostingOperation(ctx context.Context, operationID, status, stage string, result any, operationErr error) error {
	raw, _ := json.Marshal(result)
	message, finished := "", ""
	if operationErr != nil {
		message = operationErr.Error()
	}
	if status == "completed" || status == "failed" {
		finished = Now()
	}
	_, err := db.Exec(ctx, `UPDATE hosting_operations SET status=?,stage=?,result_json=?,error=?,updated_at=?,finished_at=? WHERE id=?`, status, stage, string(raw), nullStr(message), Now(), nullStr(finished), operationID)
	return err
}
func (db *DB) AppendHostingOperationEvent(ctx context.Context, operationID, stage, status, message string, detail any) (*HostingOperationEvent, error) {
	var seq int64
	_ = db.QueryRow(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM hosting_operation_events WHERE operation_id=?`, operationID).Scan(&seq)
	raw, _ := json.Marshal(detail)
	v := &HostingOperationEvent{ID: id.New("hope"), OperationID: operationID, Seq: seq, Stage: stage, Status: status, Message: message, DetailJSON: string(raw), CreatedAt: Now()}
	_, err := db.Exec(ctx, `INSERT INTO hosting_operation_events(id,operation_id,seq,stage,status,message,detail_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, v.ID, v.OperationID, v.Seq, v.Stage, v.Status, v.Message, v.DetailJSON, v.CreatedAt)
	return v, err
}
func (db *DB) ListHostingOperationEvents(ctx context.Context, operationID string, after int64) ([]HostingOperationEvent, error) {
	rows, err := db.Query(ctx, `SELECT id,operation_id,seq,stage,status,message,detail_json,created_at FROM hosting_operation_events WHERE operation_id=? AND seq>? ORDER BY seq`, operationID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostingOperationEvent
	for rows.Next() {
		var v HostingOperationEvent
		if err := rows.Scan(&v.ID, &v.OperationID, &v.Seq, &v.Stage, &v.Status, &v.Message, &v.DetailJSON, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
