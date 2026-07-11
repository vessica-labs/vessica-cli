package state

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) CreateMemory(ctx context.Context, typ, title, body, source, importance string) (*Memory, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if typ == "" {
		typ = "insight"
	}
	if importance == "" {
		importance = "medium"
	}
	now := Now()
	m := &Memory{
		ID:              id.New(id.Memory),
		WorkspaceID:     ws.ID,
		Type:            typ,
		Scope:           "repo",
		Title:           title,
		Body:            body,
		FrontmatterJSON: "{}",
		Importance:      importance,
		Visibility:      "private",
		PermissionsJSON: `{"user":"rw","org":"none","public":"none"}`,
		Source:          source,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_, err = db.Exec(ctx, `INSERT INTO memories(id, workspace_id, type, scope, title, body, frontmatter_json, importance, visibility, permissions_json, source, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.WorkspaceID, m.Type, m.Scope, m.Title, m.Body, m.FrontmatterJSON, m.Importance, m.Visibility, m.PermissionsJSON, nullStr(m.Source), m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	db.indexMemory(ctx, m)
	return m, nil
}

func (db *DB) GetMemory(ctx context.Context, memoryID string) (*Memory, error) {
	var m Memory
	var source sql.NullString
	err := db.QueryRow(ctx, `SELECT id, workspace_id, type, scope, title, body, frontmatter_json, importance, visibility, permissions_json, source, created_at, updated_at FROM memories WHERE id=?`, memoryID).
		Scan(&m.ID, &m.WorkspaceID, &m.Type, &m.Scope, &m.Title, &m.Body, &m.FrontmatterJSON, &m.Importance, &m.Visibility, &m.PermissionsJSON, &source, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("memory not found: %s", memoryID)
	}
	m.Source = source.String
	return &m, err
}

func (db *DB) ListMemories(ctx context.Context) ([]Memory, error) {
	rows, err := db.Query(ctx, `SELECT id, workspace_id, type, scope, title, body, frontmatter_json, importance, visibility, permissions_json, COALESCE(source,''), created_at, updated_at FROM memories ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Type, &m.Scope, &m.Title, &m.Body, &m.FrontmatterJSON, &m.Importance, &m.Visibility, &m.PermissionsJSON, &m.Source, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) UpdateMemory(ctx context.Context, memoryID, title, body string) (*Memory, error) {
	m, err := db.GetMemory(ctx, memoryID)
	if err != nil {
		return nil, err
	}
	if title != "" {
		m.Title = title
	}
	if body != "" {
		m.Body = body
	}
	m.UpdatedAt = Now()
	_, err = db.Exec(ctx, `UPDATE memories SET title=?, body=?, updated_at=? WHERE id=?`, m.Title, m.Body, m.UpdatedAt, m.ID)
	db.indexMemory(ctx, m)
	return m, err
}

func (db *DB) DeleteMemory(ctx context.Context, memoryID string) error {
	if db.Dialect == "sqlite" {
		_, _ = db.Exec(ctx, `DELETE FROM memory_fts WHERE id=?`, memoryID)
	}
	_, err := db.Exec(ctx, `DELETE FROM memories WHERE id=?`, memoryID)
	return err
}

func (db *DB) SetMemoryVisibility(ctx context.Context, memoryID, visibility string) error {
	_, err := db.Exec(ctx, `UPDATE memories SET visibility=?, updated_at=? WHERE id=?`, visibility, Now(), memoryID)
	return err
}

func (db *DB) SearchMemory(ctx context.Context, query string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}
	if db.Dialect == "sqlite" {
		rows, err := db.Query(ctx, `SELECT m.id, m.workspace_id, m.type, m.scope, m.title, m.body, m.frontmatter_json, m.importance, m.visibility, m.permissions_json, COALESCE(m.source,''), m.created_at, m.updated_at
			FROM memory_fts f JOIN memories m ON m.id = f.id WHERE memory_fts MATCH ? LIMIT ?`, query, limit)
		if err == nil {
			defer rows.Close()
			var out []Memory
			for rows.Next() {
				var m Memory
				if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Type, &m.Scope, &m.Title, &m.Body, &m.FrontmatterJSON, &m.Importance, &m.Visibility, &m.PermissionsJSON, &m.Source, &m.CreatedAt, &m.UpdatedAt); err != nil {
					return nil, err
				}
				out = append(out, m)
			}
			if len(out) > 0 || rows.Err() == nil {
				return out, rows.Err()
			}
		}
	}
	// Fallback LIKE search
	like := "%" + strings.ReplaceAll(query, " ", "%") + "%"
	rows, err := db.Query(ctx, `SELECT id, workspace_id, type, scope, title, body, frontmatter_json, importance, visibility, permissions_json, COALESCE(source,''), created_at, updated_at
		FROM memories WHERE title LIKE ? OR body LIKE ? ORDER BY updated_at DESC LIMIT ?`, like, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Type, &m.Scope, &m.Title, &m.Body, &m.FrontmatterJSON, &m.Importance, &m.Visibility, &m.PermissionsJSON, &m.Source, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) indexMemory(ctx context.Context, m *Memory) {
	if db.Dialect != "sqlite" {
		return
	}
	_, _ = db.Exec(ctx, `DELETE FROM memory_fts WHERE id=?`, m.ID)
	_, _ = db.Exec(ctx, `INSERT INTO memory_fts(id, title, body) VALUES (?,?,?)`, m.ID, m.Title, m.Body)
}
