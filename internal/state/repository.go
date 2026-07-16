package state

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func CanonicalRepositoryRemote(remote string) string {
	v := strings.TrimSpace(remote)
	if strings.HasPrefix(v, "git@") {
		v = strings.TrimPrefix(v, "git@")
		v = strings.Replace(v, ":", "/", 1)
		v = "https://" + v
	}
	if parsed, err := url.Parse(v); err == nil && parsed.Host != "" {
		v = strings.ToLower(parsed.Host) + "/" + strings.TrimPrefix(parsed.Path, "/")
	}
	v = strings.TrimSuffix(strings.TrimSuffix(v, "/"), ".git")
	return strings.ToLower(v)
}

func repositoryName(remote string) string {
	name := path.Base(CanonicalRepositoryRemote(remote))
	if name == "." || name == "/" || name == "" {
		return "repository"
	}
	return name
}

func (db *DB) EnsureRepository(ctx context.Context, workspaceID, remote string) (*Repository, error) {
	canonical := CanonicalRepositoryRemote(remote)
	if canonical == "" {
		return nil, fmt.Errorf("repository remote is required")
	}
	var v Repository
	err := db.QueryRow(ctx, `SELECT id,workspace_id,provider,canonical_remote,remote,display_name,default_branch,status,metadata_json,created_at,updated_at FROM repositories WHERE workspace_id=? AND canonical_remote=?`, workspaceID, canonical).
		Scan(&v.ID, &v.WorkspaceID, &v.Provider, &v.CanonicalRemote, &v.Remote, &v.DisplayName, &v.DefaultBranch, &v.Status, &v.MetadataJSON, &v.CreatedAt, &v.UpdatedAt)
	if err == nil {
		db.Repository = &v
		return &v, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	now := Now()
	v = Repository{ID: id.New("repo"), WorkspaceID: workspaceID, Provider: "github", CanonicalRemote: canonical, Remote: strings.TrimSpace(remote), DisplayName: repositoryName(remote), DefaultBranch: "main", Status: "active", MetadataJSON: "{}", CreatedAt: now, UpdatedAt: now}
	if _, err := db.Exec(ctx, `INSERT INTO repositories(id,workspace_id,provider,canonical_remote,remote,display_name,default_branch,status,metadata_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.WorkspaceID, v.Provider, v.CanonicalRemote, v.Remote, v.DisplayName, v.DefaultBranch, v.Status, v.MetadataJSON, v.CreatedAt, v.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert repository: %w", err)
	}
	db.Repository = &v
	return &v, nil
}

func (db *DB) GetRepository(ctx context.Context, repositoryID string) (*Repository, error) {
	if db.Repository != nil && (repositoryID == "" || db.Repository.ID == repositoryID) {
		return db.Repository, nil
	}
	query := `SELECT id,workspace_id,provider,canonical_remote,remote,display_name,default_branch,status,metadata_json,created_at,updated_at FROM repositories`
	args := []any{}
	if repositoryID != "" {
		query += ` WHERE id=?`
		args = append(args, repositoryID)
	}
	query += ` ORDER BY created_at LIMIT 1`
	var v Repository
	if err := db.QueryRow(ctx, query, args...).Scan(&v.ID, &v.WorkspaceID, &v.Provider, &v.CanonicalRemote, &v.Remote, &v.DisplayName, &v.DefaultBranch, &v.Status, &v.MetadataJSON, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	if repositoryID == "" {
		db.Repository = &v
	}
	return &v, nil
}

func (db *DB) ListRepositories(ctx context.Context) ([]Repository, error) {
	rows, err := db.Query(ctx, `SELECT id,workspace_id,provider,canonical_remote,remote,display_name,default_branch,status,metadata_json,created_at,updated_at FROM repositories ORDER BY display_name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Repository
	for rows.Next() {
		var v Repository
		if err := rows.Scan(&v.ID, &v.WorkspaceID, &v.Provider, &v.CanonicalRemote, &v.Remote, &v.DisplayName, &v.DefaultBranch, &v.Status, &v.MetadataJSON, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
