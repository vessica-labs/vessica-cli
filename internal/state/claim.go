package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) ExpireClaims(ctx context.Context) error {
	now := Now()
	rows, err := db.Query(ctx, `SELECT ticket_id FROM claims WHERE status='active' AND lease_until < ?`, now)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, tid := range ids {
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE claims SET status='expired' WHERE ticket_id=?`), tid); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE tickets SET status='ready', updated_at=? WHERE id=? AND status IN ('claimed','in_progress')`), now, tid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) ClaimTicket(ctx context.Context, ticketID, agentID string, lease time.Duration) (*Claim, *Ticket, error) {
	if lease <= 0 {
		lease = 45 * time.Minute
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)
	// Expire old leases and atomically transition this ticket. The conditional
	// update is the claim lock: concurrent Postgres claimers cannot both change
	// the same ready row.
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE tickets SET status='ready', updated_at=? WHERE id IN (SELECT ticket_id FROM claims WHERE status='active' AND lease_until < ?) AND status IN ('claimed','in_progress')`), nowStr, nowStr); err != nil {
		return nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE claims SET status='expired' WHERE status='active' AND lease_until < ?`), nowStr); err != nil {
		return nil, nil, err
	}
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE tickets SET status='claimed',updated_at=? WHERE id=? AND status='ready'`), nowStr, ticketID)
	if err != nil {
		return nil, nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, nil, err
	}
	if changed != 1 {
		var status string
		if scanErr := tx.QueryRowContext(ctx, db.Rebind(`SELECT status FROM tickets WHERE id=?`), ticketID).Scan(&status); scanErr == sql.ErrNoRows {
			return nil, nil, fmt.Errorf("ticket not found: %s", ticketID)
		} else if scanErr != nil {
			return nil, nil, scanErr
		}
		return nil, nil, fmt.Errorf("ticket not claimable (status=%s)", status)
	}

	leaseUntil := now.Add(lease).Format(time.RFC3339Nano)
	claim := &Claim{
		ID:          id.New(id.Claim),
		TicketID:    ticketID,
		AgentID:     agentID,
		LeaseUntil:  leaseUntil,
		HeartbeatAt: nowStr,
		Status:      "active",
		CreatedAt:   nowStr,
	}
	_, err = tx.ExecContext(ctx, db.Rebind(`DELETE FROM claims WHERE ticket_id=?`), ticketID)
	if err != nil {
		return nil, nil, err
	}
	_, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO claims(id, ticket_id, agent_id, lease_until, heartbeat_at, status, created_at) VALUES (?,?,?,?,?,?,?)`),
		claim.ID, claim.TicketID, claim.AgentID, claim.LeaseUntil, claim.HeartbeatAt, claim.Status, claim.CreatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("claim conflict: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	t, err := db.GetTicket(ctx, ticketID)
	return claim, t, err
}

func (db *DB) ClaimNext(ctx context.Context, epicID, agentID string, lease time.Duration) (*Claim, *Ticket, error) {
	ready, err := db.ReadyTickets(ctx, epicID)
	if err != nil {
		return nil, nil, err
	}
	if len(ready) == 0 {
		return nil, nil, fmt.Errorf("no ready tickets")
	}
	return db.ClaimTicket(ctx, ready[0].ID, agentID, lease)
}

func (db *DB) HeartbeatClaim(ctx context.Context, ticketID, agentID string, lease time.Duration) (*Claim, error) {
	if lease <= 0 {
		lease = 45 * time.Minute
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(lease).Format(time.RFC3339Nano)
	nowStr := now.Format(time.RFC3339Nano)
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, db.Rebind(`UPDATE claims SET heartbeat_at=?, lease_until=? WHERE ticket_id=? AND agent_id=? AND status='active'`), nowStr, leaseUntil, ticketID, agentID)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("no active claim for agent on ticket")
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE tickets SET status='in_progress', updated_at=? WHERE id=?`), nowStr, ticketID); err != nil {
		return nil, err
	}
	var c Claim
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id, ticket_id, agent_id, lease_until, heartbeat_at, status, created_at FROM claims WHERE ticket_id=?`), ticketID).
		Scan(&c.ID, &c.TicketID, &c.AgentID, &c.LeaseUntil, &c.HeartbeatAt, &c.Status, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &c, err
}

func (db *DB) ReleaseClaim(ctx context.Context, ticketID, agentID, reason string) error {
	now := Now()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, db.Rebind(`UPDATE claims SET status='released' WHERE ticket_id=? AND agent_id=? AND status='active'`), ticketID, agentID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no active claim to release")
	}
	_, err = tx.ExecContext(ctx, db.Rebind(`UPDATE tickets SET status='ready', updated_at=? WHERE id=?`), now, ticketID)
	_ = reason
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) CloseTicket(ctx context.Context, ticketID, agentID, evidenceReceiptID string) (*Ticket, error) {
	if evidenceReceiptID == "" {
		return nil, fmt.Errorf("evidence receipt required to close ticket")
	}
	now := Now()
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var claimAgent string
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT agent_id FROM claims WHERE ticket_id=? AND status='active'`), ticketID).Scan(&claimAgent)
	if err == nil && claimAgent != agentID {
		return nil, fmt.Errorf("ticket claimed by different agent")
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, db.Rebind(`UPDATE tickets SET status='closed', evidence_receipt_id=?, updated_at=? WHERE id=?`), evidenceReceiptID, now, ticketID)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE claims SET status='closed' WHERE ticket_id=?`), ticketID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return db.GetTicket(ctx, ticketID)
}
