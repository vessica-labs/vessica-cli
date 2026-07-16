package state

import "context"

type migration struct {
	version int
	sql     string
}

var migrations = []migration{{version: 2, sql: `
CREATE TABLE IF NOT EXISTS dashboard_users(id TEXT PRIMARY KEY,github_id TEXT NOT NULL UNIQUE,login TEXT NOT NULL,display_name TEXT NOT NULL DEFAULT '',avatar_url TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS workspace_memberships(workspace_id TEXT NOT NULL,user_id TEXT NOT NULL,role TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(workspace_id,user_id),FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,FOREIGN KEY(user_id) REFERENCES dashboard_users(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS dashboard_sessions(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,user_id TEXT NOT NULL,role TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,csrf_hash TEXT NOT NULL,expires_at TEXT NOT NULL,created_at TEXT NOT NULL,last_seen_at TEXT NOT NULL,FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,FOREIGN KEY(user_id) REFERENCES dashboard_users(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS dashboard_invitations(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,github_login TEXT NOT NULL,role TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,expires_at TEXT NOT NULL,accepted_at TEXT,created_by TEXT NOT NULL,created_at TEXT NOT NULL,FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS dashboard_owner_claims(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,expires_at TEXT NOT NULL,claimed_by TEXT,claimed_at TEXT,created_at TEXT NOT NULL,FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS dashboard_audit_events(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,user_id TEXT,action TEXT NOT NULL,target_type TEXT NOT NULL,target_id TEXT NOT NULL,request_id TEXT NOT NULL,metadata_json TEXT NOT NULL DEFAULT '{}',created_at TEXT NOT NULL,FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS hosting_operations(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,kind TEXT NOT NULL,status TEXT NOT NULL,stage TEXT NOT NULL,input_json TEXT NOT NULL DEFAULT '{}',result_json TEXT NOT NULL DEFAULT '{}',error TEXT,idempotency_key TEXT NOT NULL,created_by TEXT NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,finished_at TEXT,UNIQUE(workspace_id,idempotency_key),FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS hosting_operation_events(id TEXT PRIMARY KEY,operation_id TEXT NOT NULL,seq INTEGER NOT NULL,stage TEXT NOT NULL,status TEXT NOT NULL,message TEXT NOT NULL,detail_json TEXT NOT NULL DEFAULT '{}',created_at TEXT NOT NULL,UNIQUE(operation_id,seq),FOREIGN KEY(operation_id) REFERENCES hosting_operations(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS dashboard_idempotency(workspace_id TEXT NOT NULL,actor_id TEXT NOT NULL,key TEXT NOT NULL,action TEXT NOT NULL,result_json TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(workspace_id,actor_id,key));
CREATE INDEX IF NOT EXISTS idx_dashboard_sessions_expiry ON dashboard_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_dashboard_audit_workspace ON dashboard_audit_events(workspace_id,created_at);
CREATE INDEX IF NOT EXISTS idx_hosting_operation_events ON hosting_operation_events(operation_id,seq);
`}, {version: 3, sql: `
CREATE TABLE IF NOT EXISTS event_sequences(scope TEXT PRIMARY KEY,last_seq INTEGER NOT NULL);
INSERT INTO event_sequences(scope,last_seq)
SELECT COALESCE(run_id,''),MAX(seq) FROM events GROUP BY COALESCE(run_id,'')
ON CONFLICT(scope) DO UPDATE SET last_seq=CASE WHEN excluded.last_seq>event_sequences.last_seq THEN excluded.last_seq ELSE event_sequences.last_seq END;
INSERT INTO event_sequences(scope,last_seq)
SELECT 'hosting:' || operation_id,MAX(seq) FROM hosting_operation_events GROUP BY operation_id
ON CONFLICT(scope) DO UPDATE SET last_seq=CASE WHEN excluded.last_seq>event_sequences.last_seq THEN excluded.last_seq ELSE event_sequences.last_seq END;
`}, {version: 4, sql: `
CREATE TABLE IF NOT EXISTS control_plane_leases(
name TEXT PRIMARY KEY,
holder_id TEXT NOT NULL,
deployment_id TEXT NOT NULL,
replica_id TEXT NOT NULL,
heartbeat_at TEXT NOT NULL,
acquired_at TEXT NOT NULL
);
`}}

func latestMigrationVersion() int {
	if len(migrations) == 0 {
		return 1
	}
	return migrations[len(migrations)-1].version
}

func (db *DB) applyMigrations(ctx context.Context) error {
	for _, m := range migrations {
		var n int
		if err := db.SQL.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`), m.version).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		tx, err := db.SQL.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, m.sql); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)`), m.version, Now()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
