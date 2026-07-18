package state

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

const (
	EpicStatusDraft      = "draft"
	EpicStatusPlanned    = "planned"
	EpicStatusInReview   = "in_review"
	EpicStatusCompleted  = "completed"
	EpicStatusFailed     = "failed"
	EpicStatusCancelled  = "cancelled"
	EpicStatusRolledBack = "rolled_back"
)

func (db *DB) CreateEpic(ctx context.Context, title, body string) (*Epic, error) {
	repository, err := db.GetRepository(ctx, "")
	if err != nil {
		return nil, err
	}
	return db.CreateEpicForRepository(ctx, repository.ID, title, body)
}

func (db *DB) CreateEpicForRepository(ctx context.Context, repositoryID, title, body string) (*Epic, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	repository, err := db.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if repository.WorkspaceID != ws.ID {
		return nil, fmt.Errorf("repository %s does not belong to workspace %s", repository.ID, ws.ID)
	}
	now := Now()
	e := &Epic{
		ID:           id.New(id.Epic),
		WorkspaceID:  ws.ID,
		RepositoryID: repository.ID,
		Title:        title,
		Body:         body,
		Status:       EpicStatusDraft,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err = db.Exec(ctx, `INSERT INTO epics(id, workspace_id, repository_id, title, body, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		e.ID, e.WorkspaceID, e.RepositoryID, e.Title, e.Body, e.Status, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (db *DB) GetEpic(ctx context.Context, epicID string) (*Epic, error) {
	var e Epic
	err := db.QueryRow(ctx, `SELECT id, workspace_id, repository_id, title, body, status, COALESCE(external_id,''), created_at, updated_at FROM epics WHERE id = ?`, epicID).
		Scan(&e.ID, &e.WorkspaceID, &e.RepositoryID, &e.Title, &e.Body, &e.Status, &e.ExternalID, &e.CreatedAt, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("epic not found: %s", epicID)
	}
	return &e, err
}

func (db *DB) ListEpics(ctx context.Context) ([]Epic, error) {
	return db.listEpics(ctx, "", nil)
}

// ListEpicsForRepository returns only epics owned by repositoryID. Hosted
// transports use this method so records from sibling repositories in the same
// workspace cannot leak into a repository-scoped client.
func (db *DB) ListEpicsForRepository(ctx context.Context, repositoryID string) ([]Epic, error) {
	if _, err := db.GetRepository(ctx, repositoryID); err != nil {
		return nil, err
	}
	return db.listEpics(ctx, ` WHERE repository_id = ?`, []any{repositoryID})
}

func (db *DB) listEpics(ctx context.Context, where string, args []any) ([]Epic, error) {
	query := `SELECT id, workspace_id, repository_id, title, body, status, COALESCE(external_id,''), created_at, updated_at FROM epics` + where + ` ORDER BY created_at DESC`
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Epic
	for rows.Next() {
		var e Epic
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.RepositoryID, &e.Title, &e.Body, &e.Status, &e.ExternalID, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (db *DB) UpdateEpic(ctx context.Context, epicID, title, body, status string) (*Epic, error) {
	e, err := db.GetEpic(ctx, epicID)
	if err != nil {
		return nil, err
	}
	if title != "" {
		e.Title = title
	}
	if body != "" {
		e.Body = body
	}
	if status != "" {
		e.Status = status
	}
	e.UpdatedAt = Now()
	_, err = db.Exec(ctx, `UPDATE epics SET title=?, body=?, status=?, updated_at=? WHERE id=?`, e.Title, e.Body, e.Status, e.UpdatedAt, e.ID)
	return e, err
}

func (db *DB) DeleteEpic(ctx context.Context, epicID string) error {
	_, err := db.Exec(ctx, `DELETE FROM epics WHERE id=?`, epicID)
	return err
}

func (db *DB) SetEpicExternalID(ctx context.Context, epicID, externalID string) error {
	_, err := db.Exec(ctx, `UPDATE epics SET external_id=?, updated_at=? WHERE id=?`, nullStr(externalID), Now(), epicID)
	return err
}
