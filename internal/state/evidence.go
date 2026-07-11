package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) CreateRunEvidence(ctx context.Context, runID, phase, kind, ticketID, status string, body any) (*RunEvidence, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if status == "" {
		status = "ok"
	}
	b, _ := json.Marshal(body)
	ev := &RunEvidence{
		ID:          id.New("evid"),
		RunID:       runID,
		WorkspaceID: ws.ID,
		Phase:       phase,
		Kind:        kind,
		TicketID:    ticketID,
		Status:      status,
		BodyJSON:    string(b),
		CreatedAt:   Now(),
	}
	_, err = db.Exec(ctx, `INSERT INTO run_evidence(id, run_id, workspace_id, phase, kind, ticket_id, status, body_json, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		ev.ID, ev.RunID, ev.WorkspaceID, ev.Phase, ev.Kind, nullStr(ev.TicketID), ev.Status, ev.BodyJSON, ev.CreatedAt)
	return ev, err
}

func (db *DB) ListRunEvidence(ctx context.Context, runID string) ([]RunEvidence, error) {
	rows, err := db.Query(ctx, `SELECT id, run_id, workspace_id, phase, kind, COALESCE(ticket_id,''), status, body_json, created_at FROM run_evidence WHERE run_id=? ORDER BY created_at`, runID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []RunEvidence
	for rows.Next() {
		var ev RunEvidence
		if err := rows.Scan(&ev.ID, &ev.RunID, &ev.WorkspaceID, &ev.Phase, &ev.Kind, &ev.TicketID, &ev.Status, &ev.BodyJSON, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
