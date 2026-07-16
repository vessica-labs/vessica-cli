package state

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const WorkplanSnapshotSchema = "vessica.workplan/v1"

var snapshotTables = []string{"repositories", "epics", "artifacts", "artifact_versions", "artifact_sets", "tickets", "ticket_dependencies", "waves", "runs", "run_phases", "sandboxes", "events", "run_evidence", "receipts", "traces", "external_mappings", "packs", "harness_status", "onboarding_operations", "control_plane_deployments"}
var safeIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type WorkplanSnapshot struct {
	Schema            string                      `json:"schema"`
	SourceWorkspaceID string                      `json:"source_workspace_id"`
	Tables            map[string][]map[string]any `json:"tables"`
	Checksum          string                      `json:"checksum"`
}

func (db *DB) ExportWorkplanSnapshot(ctx context.Context) (WorkplanSnapshot, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return WorkplanSnapshot{}, err
	}
	snap := WorkplanSnapshot{Schema: WorkplanSnapshotSchema, SourceWorkspaceID: ws.ID, Tables: map[string][]map[string]any{}}
	for _, table := range snapshotTables {
		rows, err := db.Query(ctx, "SELECT * FROM "+table)
		if err != nil {
			return snap, err
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return snap, err
		}
		for rows.Next() {
			values := make([]any, len(cols))
			pointers := make([]any, len(cols))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				return snap, err
			}
			row := map[string]any{}
			for i, col := range cols {
				value := values[i]
				if raw, ok := value.([]byte); ok {
					value = string(raw)
				}
				row[col] = value
			}
			snap.Tables[table] = append(snap.Tables[table], row)
		}
		if err := rows.Close(); err != nil {
			return snap, err
		}
	}
	snap.Checksum = workplanChecksum(snap)
	return snap, nil
}
func (db *DB) ImportWorkplanSnapshot(ctx context.Context, snap WorkplanSnapshot) error {
	if snap.Schema != WorkplanSnapshotSchema {
		return fmt.Errorf("unsupported workplan snapshot schema")
	}
	if snap.Checksum == "" || snap.Checksum != workplanChecksum(snap) {
		return fmt.Errorf("workplan snapshot checksum mismatch")
	}
	target, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range snapshotTables {
		for _, row := range snap.Tables[table] {
			if _, ok := row["workspace_id"]; ok {
				row["workspace_id"] = target.ID
			}
			if table == "sandboxes" {
				row["container_id"] = nil
				row["preview_url"] = nil
				row["status"] = "expired"
				row["destroyed_at"] = Now()
			}
			if table == "runs" {
				if preview, ok := row["preview_url"].(string); ok && (strings.Contains(preview, "127.0.0.1") || strings.Contains(preview, "localhost")) {
					row["preview_url"] = nil
				}
			}
			cols := make([]string, 0, len(row))
			for col := range row {
				if !safeIdentifier.MatchString(col) {
					return fmt.Errorf("unsafe snapshot column")
				}
				cols = append(cols, col)
			}
			sort.Strings(cols)
			marks := make([]string, len(cols))
			values := make([]any, len(cols))
			for i, col := range cols {
				marks[i] = "?"
				values[i] = row[col]
			}
			query := fmt.Sprintf("INSERT INTO %s(%s) VALUES(%s) ON CONFLICT DO NOTHING", table, strings.Join(cols, ","), strings.Join(marks, ","))
			if _, err := tx.ExecContext(ctx, db.Rebind(query), values...); err != nil {
				return fmt.Errorf("import %s: %w", table, err)
			}
		}
	}
	return tx.Commit()
}
func workplanChecksum(snap WorkplanSnapshot) string {
	copy := snap
	copy.Checksum = ""
	raw, _ := json.Marshal(copy)
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
