package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrControlPlaneLeaseLost = errors.New("control-plane lease was taken over")

// ControlPlaneLease prevents two replicas from the same deployment from
// running control-plane loops concurrently. A new deployment may take over the
// lease so Railway can perform a rolling deployment without deadlocking startup.
type ControlPlaneLease struct {
	db       *DB
	name     string
	holderID string
}

// AcquireControlPlaneLease acquires the singleton lease or returns an explicit
// error naming the already-active replica. ttl is used only to recover leases
// left behind by a process that died without releasing them.
func (db *DB) AcquireControlPlaneLease(ctx context.Context, name, holderID, deploymentID, replicaID string, ttl time.Duration) (*ControlPlaneLease, error) {
	name = strings.TrimSpace(name)
	holderID = strings.TrimSpace(holderID)
	if name == "" || holderID == "" {
		return nil, fmt.Errorf("lease name and holder id are required")
	}
	if deploymentID = strings.TrimSpace(deploymentID); deploymentID == "" {
		deploymentID = "local"
	}
	if replicaID = strings.TrimSpace(replicaID); replicaID == "" {
		replicaID = holderID
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	txOptions := &sql.TxOptions{}
	if db.Dialect == "postgres" {
		txOptions.Isolation = sql.LevelReadCommitted
	}
	tx, err := db.SQL.BeginTx(ctx, txOptions)
	if err != nil {
		return nil, fmt.Errorf("begin control-plane lease: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	insert := db.Rebind(`INSERT INTO control_plane_leases(name,holder_id,deployment_id,replica_id,heartbeat_at,acquired_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(name) DO NOTHING`)
	if _, err = tx.ExecContext(ctx, insert, name, holderID, deploymentID, replicaID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return nil, fmt.Errorf("initialize control-plane lease: %w", err)
	}
	query := `SELECT holder_id,deployment_id,replica_id,heartbeat_at FROM control_plane_leases WHERE name=?`
	if db.Dialect == "postgres" {
		query += ` FOR UPDATE`
	}
	var currentHolder, currentDeployment, currentReplica, heartbeatRaw string
	if err = tx.QueryRowContext(ctx, db.Rebind(query), name).Scan(&currentHolder, &currentDeployment, &currentReplica, &heartbeatRaw); err != nil {
		return nil, fmt.Errorf("read control-plane lease: %w", err)
	}
	heartbeat, parseErr := time.Parse(time.RFC3339Nano, heartbeatRaw)
	active := parseErr == nil && heartbeat.After(now.Add(-ttl))
	if currentHolder != holderID && active && currentDeployment == deploymentID {
		return nil, fmt.Errorf("multiple control-plane replicas are unsupported: deployment %s already has active replica %s", deploymentID, currentReplica)
	}
	if currentHolder != holderID {
		update := db.Rebind(`UPDATE control_plane_leases SET holder_id=?,deployment_id=?,replica_id=?,heartbeat_at=?,acquired_at=? WHERE name=?`)
		if _, err = tx.ExecContext(ctx, update, holderID, deploymentID, replicaID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), name); err != nil {
			return nil, fmt.Errorf("take over control-plane lease: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit control-plane lease: %w", err)
	}
	return &ControlPlaneLease{db: db, name: name, holderID: holderID}, nil
}

// Heartbeat refreshes the lease and reports when another deployment took over.
func (l *ControlPlaneLease) Heartbeat(ctx context.Context) error {
	result, err := l.db.Exec(ctx, `UPDATE control_plane_leases SET heartbeat_at=? WHERE name=? AND holder_id=?`, Now(), l.name, l.holderID)
	if err != nil {
		return fmt.Errorf("heartbeat control-plane lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect control-plane lease heartbeat: %w", err)
	}
	if rows != 1 {
		return ErrControlPlaneLeaseLost
	}
	return nil
}

// Release removes the lease only if it is still owned by this process.
func (l *ControlPlaneLease) Release(ctx context.Context) error {
	if l == nil || l.db == nil {
		return nil
	}
	_, err := l.db.Exec(ctx, `DELETE FROM control_plane_leases WHERE name=? AND holder_id=?`, l.name, l.holderID)
	return err
}
