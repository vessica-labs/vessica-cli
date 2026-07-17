package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) EnsureWorkspace(ctx context.Context, root, profile string) (*Workspace, error) {
	return db.EnsureWorkspaceWithID(ctx, "", root, profile)
}

// EnsureWorkspaceWithID creates the workspace with desiredID when it does not
// exist. Hosted provisioning uses one explicit identity across the control
// plane and knowledge service instead of deriving identity from local state.
func (db *DB) EnsureWorkspaceWithID(ctx context.Context, desiredID, root, profile string) (*Workspace, error) {
	return db.ensureWorkspaceWithID(ctx, desiredID, root, profile, true)
}

// EnsureHostedWorkspaceWithID creates only the hosted workspace identity.
// Hosted repositories are attached explicitly through the repository API and
// must not be synthesized from the control plane's hosted:// bootstrap key.
func (db *DB) EnsureHostedWorkspaceWithID(ctx context.Context, desiredID, root string) (*Workspace, error) {
	return db.ensureWorkspaceWithID(ctx, desiredID, root, "hosted", false)
}

func (db *DB) ensureWorkspaceWithID(ctx context.Context, desiredID, root, profile string, ensureRepository bool) (*Workspace, error) {
	var ws Workspace
	err := db.QueryRow(ctx, `SELECT id, root_path, profile, created_at, updated_at FROM workspaces WHERE root_path = ?`, root).
		Scan(&ws.ID, &ws.RootPath, &ws.Profile, &ws.CreatedAt, &ws.UpdatedAt)
	if err == nil {
		if desiredID != "" && ws.ID != desiredID {
			return nil, fmt.Errorf("workspace identity conflict: database has %s, expected %s", ws.ID, desiredID)
		}
		db.Workspace = &ws
		if ensureRepository {
			_, _ = db.EnsureRepository(ctx, ws.ID, root)
		}
		return &ws, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	now := Now()
	if desiredID == "" {
		desiredID = id.New(id.Workspace)
	}
	ws = Workspace{
		ID:        desiredID,
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
	if ensureRepository {
		if _, repoErr := db.EnsureRepository(ctx, ws.ID, root); repoErr != nil {
			return nil, repoErr
		}
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
