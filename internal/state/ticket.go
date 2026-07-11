package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) CreateTicket(ctx context.Context, epicID, typ, title, body string, dependsOn []string) (*Ticket, error) {
	return db.createTicket(ctx, epicID, "", typ, title, body, dependsOn, "", "")
}

func (db *DB) CreateTicketWithMeta(ctx context.Context, epicID, typ, title, body string, dependsOn []string, discoveredFromRunID, testStep string) (*Ticket, error) {
	return db.createTicket(ctx, epicID, "", typ, title, body, dependsOn, discoveredFromRunID, testStep)
}

func (db *DB) CreateTicketForRun(ctx context.Context, epicID, sourceRunID, typ, title, body string, dependsOn []string) (*Ticket, error) {
	return db.createTicket(ctx, epicID, sourceRunID, typ, title, body, dependsOn, "", "")
}

func (db *DB) createTicket(ctx context.Context, epicID, sourceRunID, typ, title, body string, dependsOn []string, discoveredFromRunID, testStep string) (*Ticket, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if typ == "" {
		typ = "feature"
	}
	now := Now()
	t := &Ticket{
		ID:                  id.New(id.Ticket),
		WorkspaceID:         ws.ID,
		EpicID:              epicID,
		SourceRunID:         sourceRunID,
		Type:                typ,
		Title:               title,
		Body:                body,
		Status:              "ready",
		CreatedAt:           now,
		UpdatedAt:           now,
		DependsOn:           dependsOn,
		DiscoveredFromRunID: discoveredFromRunID,
		TestStep:            testStep,
	}
	_, err = db.Exec(ctx, `INSERT INTO tickets(id, workspace_id, epic_id, source_run_id, type, title, body, status, discovered_from_run_id, test_step, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.WorkspaceID, t.EpicID, nullStr(t.SourceRunID), t.Type, t.Title, t.Body, t.Status, nullStr(t.DiscoveredFromRunID), nullStr(t.TestStep), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	for _, dep := range dependsOn {
		_, _ = db.Exec(ctx, `INSERT INTO ticket_dependencies(ticket_id, depends_on) VALUES (?,?)`, t.ID, dep)
	}
	return t, nil
}

func (db *DB) GetTicket(ctx context.Context, ticketID string) (*Ticket, error) {
	var t Ticket
	var sourceRunID, waveID, evidence, discovered, testStep, external sql.NullString
	err := db.QueryRow(ctx, `SELECT id, workspace_id, epic_id, source_run_id, wave_id, type, title, body, status, evidence_receipt_id, discovered_from_run_id, test_step, external_id, created_at, updated_at
		FROM tickets WHERE id=?`, ticketID).
		Scan(&t.ID, &t.WorkspaceID, &t.EpicID, &sourceRunID, &waveID, &t.Type, &t.Title, &t.Body, &t.Status, &evidence, &discovered, &testStep, &external, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("ticket not found: %s", ticketID)
	}
	if err != nil {
		return nil, err
	}
	t.SourceRunID = sourceRunID.String
	t.WaveID = waveID.String
	t.EvidenceReceiptID = evidence.String
	t.DiscoveredFromRunID = discovered.String
	t.TestStep = testStep.String
	t.ExternalID = external.String
	deps, _ := db.ticketDeps(ctx, t.ID)
	t.DependsOn = deps
	return &t, nil
}

func (db *DB) ticketDeps(ctx context.Context, ticketID string) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT depends_on FROM ticket_dependencies WHERE ticket_id=?`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (db *DB) ListTickets(ctx context.Context, epicID string) ([]Ticket, error) {
	q := `SELECT id, workspace_id, epic_id, COALESCE(source_run_id,''), COALESCE(wave_id,''), type, title, body, status, COALESCE(evidence_receipt_id,''), COALESCE(discovered_from_run_id,''), COALESCE(test_step,''), COALESCE(external_id,''), created_at, updated_at FROM tickets`
	var args []any
	if epicID != "" {
		q += ` WHERE epic_id=?`
		args = append(args, epicID)
	}
	q += ` ORDER BY created_at`
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var out []Ticket
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.EpicID, &t.SourceRunID, &t.WaveID, &t.Type, &t.Title, &t.Body, &t.Status, &t.EvidenceReceiptID, &t.DiscoveredFromRunID, &t.TestStep, &t.ExternalID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	for i := range out {
		deps, err := db.ticketDeps(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].DependsOn = deps
	}
	return out, nil
}

func (db *DB) ListTicketsForRun(ctx context.Context, epicID, runID string) ([]Ticket, error) {
	if runID == "" {
		return db.ListTickets(ctx, epicID)
	}
	q := `SELECT id, workspace_id, epic_id, COALESCE(source_run_id,''), COALESCE(wave_id,''), type, title, body, status, COALESCE(evidence_receipt_id,''), COALESCE(discovered_from_run_id,''), COALESCE(test_step,''), COALESCE(external_id,''), created_at, updated_at FROM tickets WHERE epic_id=? AND source_run_id=? ORDER BY created_at`
	rows, err := db.Query(ctx, q, epicID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.EpicID, &t.SourceRunID, &t.WaveID, &t.Type, &t.Title, &t.Body, &t.Status, &t.EvidenceReceiptID, &t.DiscoveredFromRunID, &t.TestStep, &t.ExternalID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		deps, err := db.ticketDeps(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].DependsOn = deps
	}
	return out, nil
}

func (db *DB) UpdateTicket(ctx context.Context, ticketID, title, body, status, typ string) (*Ticket, error) {
	t, err := db.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if title != "" {
		t.Title = title
	}
	if body != "" {
		t.Body = body
	}
	if status != "" {
		t.Status = status
	}
	if typ != "" {
		t.Type = typ
	}
	t.UpdatedAt = Now()
	_, err = db.Exec(ctx, `UPDATE tickets SET title=?, body=?, status=?, type=?, updated_at=? WHERE id=?`,
		t.Title, t.Body, t.Status, t.Type, t.UpdatedAt, t.ID)
	return t, err
}

func (db *DB) DeleteTicket(ctx context.Context, ticketID string) error {
	_, _ = db.Exec(ctx, `DELETE FROM ticket_dependencies WHERE ticket_id=? OR depends_on=?`, ticketID, ticketID)
	_, err := db.Exec(ctx, `DELETE FROM tickets WHERE id=?`, ticketID)
	return err
}

func (db *DB) AddDependency(ctx context.Context, ticketID, dependsOn string) error {
	if db.Dialect == "postgres" {
		_, err := db.Exec(ctx, `INSERT INTO ticket_dependencies(ticket_id, depends_on) VALUES (?,?) ON CONFLICT DO NOTHING`, ticketID, dependsOn)
		return err
	}
	_, err := db.Exec(ctx, `INSERT OR IGNORE INTO ticket_dependencies(ticket_id, depends_on) VALUES (?,?)`, ticketID, dependsOn)
	return err
}

func (db *DB) RemoveDependency(ctx context.Context, ticketID, dependsOn string) error {
	_, err := db.Exec(ctx, `DELETE FROM ticket_dependencies WHERE ticket_id=? AND depends_on=?`, ticketID, dependsOn)
	return err
}

// ReadyTickets returns dependency-unblocked ready tickets, releasing expired claims first.
func (db *DB) ReadyTickets(ctx context.Context, epicID string) ([]Ticket, error) {
	if err := db.ExpireClaims(ctx); err != nil {
		return nil, err
	}
	tickets, err := db.ListTickets(ctx, epicID)
	if err != nil {
		return nil, err
	}
	closed := map[string]bool{}
	for _, t := range tickets {
		if t.Status == "closed" {
			closed[t.ID] = true
		}
	}
	var ready []Ticket
	for _, t := range tickets {
		if t.Status != "ready" {
			continue
		}
		ok := true
		for _, d := range t.DependsOn {
			if !closed[d] {
				ok = false
				break
			}
		}
		if ok {
			ready = append(ready, t)
		}
	}
	return ready, nil
}

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
	for _, tid := range ids {
		_, _ = db.Exec(ctx, `UPDATE claims SET status='expired' WHERE ticket_id=?`, tid)
		_, _ = db.Exec(ctx, `UPDATE tickets SET status='ready', updated_at=? WHERE id=? AND status IN ('claimed','in_progress')`, now, tid)
	}
	return nil
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
	// expire inside tx
	_, _ = tx.ExecContext(ctx, db.Rebind(`UPDATE tickets SET status='ready', updated_at=? WHERE id IN (SELECT ticket_id FROM claims WHERE status='active' AND lease_until < ?) AND status IN ('claimed','in_progress')`), nowStr, nowStr)
	_, _ = tx.ExecContext(ctx, db.Rebind(`UPDATE claims SET status='expired' WHERE status='active' AND lease_until < ?`), nowStr)

	var status string
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT status FROM tickets WHERE id=?`), ticketID).Scan(&status)
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("ticket not found: %s", ticketID)
	}
	if err != nil {
		return nil, nil, err
	}
	if status != "ready" {
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
	_, err = tx.ExecContext(ctx, db.Rebind(`UPDATE tickets SET status='claimed', updated_at=? WHERE id=?`), nowStr, ticketID)
	if err != nil {
		return nil, nil, err
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
	res, err := db.Exec(ctx, `UPDATE claims SET heartbeat_at=?, lease_until=? WHERE ticket_id=? AND agent_id=? AND status='active'`,
		nowStr, leaseUntil, ticketID, agentID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, fmt.Errorf("no active claim for agent on ticket")
	}
	_, _ = db.Exec(ctx, `UPDATE tickets SET status='in_progress', updated_at=? WHERE id=?`, nowStr, ticketID)
	var c Claim
	err = db.QueryRow(ctx, `SELECT id, ticket_id, agent_id, lease_until, heartbeat_at, status, created_at FROM claims WHERE ticket_id=?`, ticketID).
		Scan(&c.ID, &c.TicketID, &c.AgentID, &c.LeaseUntil, &c.HeartbeatAt, &c.Status, &c.CreatedAt)
	return &c, err
}

func (db *DB) ReleaseClaim(ctx context.Context, ticketID, agentID, reason string) error {
	now := Now()
	res, err := db.Exec(ctx, `UPDATE claims SET status='released' WHERE ticket_id=? AND agent_id=? AND status='active'`, ticketID, agentID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no active claim to release")
	}
	_, err = db.Exec(ctx, `UPDATE tickets SET status='ready', updated_at=? WHERE id=?`, now, ticketID)
	_ = reason
	return err
}

func (db *DB) CloseTicket(ctx context.Context, ticketID, agentID, evidenceReceiptID string) (*Ticket, error) {
	if evidenceReceiptID == "" {
		return nil, fmt.Errorf("evidence receipt required to close ticket")
	}
	now := Now()
	var claimAgent string
	err := db.QueryRow(ctx, `SELECT agent_id FROM claims WHERE ticket_id=? AND status='active'`, ticketID).Scan(&claimAgent)
	if err == nil && claimAgent != agentID {
		return nil, fmt.Errorf("ticket claimed by different agent")
	}
	_, err = db.Exec(ctx, `UPDATE tickets SET status='closed', evidence_receipt_id=?, updated_at=? WHERE id=?`, evidenceReceiptID, now, ticketID)
	if err != nil {
		return nil, err
	}
	_, _ = db.Exec(ctx, `UPDATE claims SET status='closed' WHERE ticket_id=?`, ticketID)
	return db.GetTicket(ctx, ticketID)
}

// ComputeWaves builds topological waves for an epic and persists them.
func (db *DB) ComputeWaves(ctx context.Context, epicID string) ([]Wave, error) {
	tickets, err := db.ListTickets(ctx, epicID)
	if err != nil {
		return nil, err
	}
	return db.computeWaves(ctx, epicID, "", tickets)
}

func (db *DB) ComputeWavesForRun(ctx context.Context, epicID, runID string) ([]Wave, error) {
	tickets, err := db.ListTicketsForRun(ctx, epicID, runID)
	if err != nil {
		return nil, err
	}
	return db.computeWaves(ctx, epicID, runID, tickets)
}

func (db *DB) computeWaves(ctx context.Context, epicID, sourceRunID string, tickets []Ticket) ([]Wave, error) {
	deps := map[string][]string{}
	remaining := map[string]int{}
	byID := map[string]Ticket{}
	for _, t := range tickets {
		if t.EpicID != epicID {
			return nil, fmt.Errorf("ticket %s belongs to epic %s, not %s", t.ID, t.EpicID, epicID)
		}
		if sourceRunID != "" && t.SourceRunID != sourceRunID {
			return nil, fmt.Errorf("ticket %s belongs to run %s, not %s", t.ID, t.SourceRunID, sourceRunID)
		}
		byID[t.ID] = t
		deps[t.ID] = t.DependsOn
		remaining[t.ID] = len(t.DependsOn)
	}
	dependents := map[string][]string{}
	for tid, ds := range deps {
		for _, d := range ds {
			if _, ok := byID[d]; !ok {
				return nil, fmt.Errorf("ticket %s depends on ticket %s outside this wave set", tid, d)
			}
			dependents[d] = append(dependents[d], tid)
		}
	}

	_, _ = db.Exec(ctx, `DELETE FROM waves WHERE epic_id=?`, epicID)
	_, _ = db.Exec(ctx, `UPDATE tickets SET wave_id=NULL WHERE epic_id=?`, epicID)
	var waves []Wave
	assigned := map[string]bool{}
	waveIdx := 0
	for len(assigned) < len(tickets) {
		var layer []string
		for tid, n := range remaining {
			if assigned[tid] {
				continue
			}
			if n == 0 {
				layer = append(layer, tid)
			}
		}
		if len(layer) == 0 {
			// cycle — put remaining in one wave
			for tid := range remaining {
				if !assigned[tid] {
					layer = append(layer, tid)
				}
			}
		}
		now := Now()
		w := Wave{
			ID:          id.New(id.Wave),
			EpicID:      epicID,
			SourceRunID: sourceRunID,
			Index:       waveIdx,
			Status:      "pending",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		_, err := db.Exec(ctx, `INSERT INTO waves(id, epic_id, source_run_id, index_n, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			w.ID, w.EpicID, nullStr(w.SourceRunID), w.Index, w.Status, w.CreatedAt, w.UpdatedAt)
		if err != nil {
			return nil, err
		}
		for _, tid := range layer {
			assigned[tid] = true
			_, _ = db.Exec(ctx, `UPDATE tickets SET wave_id=?, updated_at=? WHERE id=?`, w.ID, now, tid)
			for _, dep := range dependents[tid] {
				remaining[dep]--
			}
		}
		waves = append(waves, w)
		waveIdx++
		if waveIdx > len(tickets)+1 {
			break
		}
	}
	return waves, nil
}

func (db *DB) ListWaves(ctx context.Context, epicID string) ([]Wave, error) {
	rows, err := db.Query(ctx, `SELECT id, epic_id, COALESCE(source_run_id,''), index_n, status, created_at, updated_at FROM waves WHERE epic_id=? ORDER BY index_n`, epicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Wave
	for rows.Next() {
		var w Wave
		if err := rows.Scan(&w.ID, &w.EpicID, &w.SourceRunID, &w.Index, &w.Status, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (db *DB) ListWavesForRun(ctx context.Context, epicID, runID string) ([]Wave, error) {
	rows, err := db.Query(ctx, `SELECT id, epic_id, COALESCE(source_run_id,''), index_n, status, created_at, updated_at FROM waves WHERE epic_id=? AND source_run_id=? ORDER BY index_n`, epicID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Wave
	for rows.Next() {
		var w Wave
		if err := rows.Scan(&w.ID, &w.EpicID, &w.SourceRunID, &w.Index, &w.Status, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (db *DB) GetWave(ctx context.Context, waveID string) (*Wave, error) {
	var w Wave
	err := db.QueryRow(ctx, `SELECT id, epic_id, COALESCE(source_run_id,''), index_n, status, created_at, updated_at FROM waves WHERE id=?`, waveID).
		Scan(&w.ID, &w.EpicID, &w.SourceRunID, &w.Index, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("wave not found: %s", waveID)
	}
	return &w, err
}
