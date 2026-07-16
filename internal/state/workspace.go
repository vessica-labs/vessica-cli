package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) EnsureWorkspace(ctx context.Context, root, profile string) (*Workspace, error) {
	var ws Workspace
	err := db.QueryRow(ctx, `SELECT id, root_path, profile, created_at, updated_at FROM workspaces WHERE root_path = ?`, root).
		Scan(&ws.ID, &ws.RootPath, &ws.Profile, &ws.CreatedAt, &ws.UpdatedAt)
	if err == nil {
		db.Workspace = &ws
		_, _ = db.EnsureRepository(ctx, ws.ID, root)
		return &ws, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	now := Now()
	ws = Workspace{
		ID:        id.New(id.Workspace),
		RootPath:  root,
		Profile:   profile,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err = db.Exec(ctx, `INSERT INTO workspaces(id, root_path, profile, created_at, updated_at) VALUES (?,?,?,?,?)`,
		ws.ID, ws.RootPath, ws.Profile, ws.CreatedAt, ws.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert workspace: %w", err)
	}
	db.Workspace = &ws
	if _, repoErr := db.EnsureRepository(ctx, ws.ID, root); repoErr != nil {
		return nil, repoErr
	}
	return &ws, nil
}

func (db *DB) GetWorkspace(ctx context.Context) (*Workspace, error) {
	if db.Workspace != nil {
		return db.Workspace, nil
	}
	var ws Workspace
	err := db.QueryRow(ctx, `SELECT id, root_path, profile, created_at, updated_at FROM workspaces ORDER BY created_at LIMIT 1`).
		Scan(&ws.ID, &ws.RootPath, &ws.Profile, &ws.CreatedAt, &ws.UpdatedAt)
	if err != nil {
		return nil, err
	}
	db.Workspace = &ws
	return &ws, nil
}
