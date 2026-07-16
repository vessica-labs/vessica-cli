package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func artifactPrefix(typ string) string {
	switch typ {
	case "prd":
		return id.PRD
	case "adr":
		return id.ADR
	case "design-spec":
		return id.Design
	case "test-scenarios":
		return id.Test
	default:
		return id.Artifact
	}
}

func (db *DB) CreateArtifact(ctx context.Context, typ, title, body, epicID, runID string) (*Artifact, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var repository *Repository
	if runID != "" {
		runRecord, runErr := db.GetRun(ctx, runID)
		if runErr != nil {
			return nil, runErr
		}
		repository, err = db.GetRepository(ctx, runRecord.RepositoryID)
	} else if epicID != "" {
		epic, epicErr := db.GetEpic(ctx, epicID)
		if epicErr != nil {
			return nil, epicErr
		}
		repository, err = db.GetRepository(ctx, epic.RepositoryID)
	} else {
		repository, err = db.GetRepository(ctx, "")
	}
	if err != nil {
		return nil, err
	}
	now := Now()
	a := &Artifact{
		ID:              id.New(artifactPrefix(typ)),
		WorkspaceID:     ws.ID,
		RepositoryID:    repository.ID,
		EpicID:          epicID,
		Type:            typ,
		Title:           title,
		Status:          "draft",
		Version:         1,
		Body:            body,
		FrontmatterJSON: "{}",
		SourceRunID:     runID,
		CreatedByJSON:   `{"actor_type":"cli"}`,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_, err = db.Exec(ctx, `INSERT INTO artifacts(id, workspace_id, repository_id, epic_id, type, title, status, version, body, frontmatter_json, source_run_id, created_by_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.WorkspaceID, a.RepositoryID, nullStr(a.EpicID), a.Type, a.Title, a.Status, a.Version, a.Body, a.FrontmatterJSON, nullStr(a.SourceRunID), a.CreatedByJSON, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_, _ = db.Exec(ctx, `INSERT INTO artifact_versions(id, artifact_id, version, body, created_at) VALUES (?,?,?,?,?)`,
		id.New("aver"), a.ID, 1, body, now)
	db.indexArtifact(ctx, a)
	return a, nil
}

func (db *DB) GetArtifact(ctx context.Context, artifactID string) (*Artifact, error) {
	var a Artifact
	var epicID, setID, runID sql.NullString
	err := db.QueryRow(ctx, `SELECT id, workspace_id, repository_id, epic_id, artifact_set_id, type, title, status, version, body, frontmatter_json, source_run_id, created_by_json, created_at, updated_at
		FROM artifacts WHERE id=?`, artifactID).
		Scan(&a.ID, &a.WorkspaceID, &a.RepositoryID, &epicID, &setID, &a.Type, &a.Title, &a.Status, &a.Version, &a.Body, &a.FrontmatterJSON, &runID, &a.CreatedByJSON, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("artifact not found: %s", artifactID)
	}
	if err != nil {
		return nil, err
	}
	a.EpicID = epicID.String
	a.ArtifactSetID = setID.String
	a.SourceRunID = runID.String
	return &a, nil
}

func (db *DB) ListArtifacts(ctx context.Context, epicID, typ string) ([]Artifact, error) {
	q := `SELECT id, workspace_id, repository_id, COALESCE(epic_id,''), COALESCE(artifact_set_id,''), type, title, status, version, body, frontmatter_json, COALESCE(source_run_id,''), created_by_json, created_at, updated_at FROM artifacts WHERE 1=1`
	var args []any
	if epicID != "" {
		q += ` AND epic_id=?`
		args = append(args, epicID)
	}
	if typ != "" {
		q += ` AND type=?`
		args = append(args, typ)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.RepositoryID, &a.EpicID, &a.ArtifactSetID, &a.Type, &a.Title, &a.Status, &a.Version, &a.Body, &a.FrontmatterJSON, &a.SourceRunID, &a.CreatedByJSON, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) ListArtifactsForRun(ctx context.Context, runID string) ([]Artifact, error) {
	r, err := db.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	q := `SELECT id, workspace_id, repository_id, COALESCE(epic_id,''), COALESCE(artifact_set_id,''), type, title, status, version, body, frontmatter_json, COALESCE(source_run_id,''), created_by_json, created_at, updated_at
		FROM artifacts WHERE source_run_id=?`
	args := []any{runID}
	if r.ArtifactSetID != "" {
		q += ` OR artifact_set_id=?`
		args = append(args, r.ArtifactSetID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.RepositoryID, &a.EpicID, &a.ArtifactSetID, &a.Type, &a.Title, &a.Status, &a.Version, &a.Body, &a.FrontmatterJSON, &a.SourceRunID, &a.CreatedByJSON, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) UpdateArtifact(ctx context.Context, artifactID, title, body, status string) (*Artifact, error) {
	a, err := db.GetArtifact(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	if title != "" {
		a.Title = title
	}
	if body != "" {
		a.Body = body
		a.Version++
		_, _ = db.Exec(ctx, `INSERT INTO artifact_versions(id, artifact_id, version, body, created_at) VALUES (?,?,?,?,?)`,
			id.New("aver"), a.ID, a.Version, body, Now())
	}
	if status != "" {
		a.Status = status
	}
	a.UpdatedAt = Now()
	_, err = db.Exec(ctx, `UPDATE artifacts SET title=?, body=?, status=?, version=?, updated_at=? WHERE id=?`,
		a.Title, a.Body, a.Status, a.Version, a.UpdatedAt, a.ID)
	db.indexArtifact(ctx, a)
	return a, err
}

func (db *DB) ApproveArtifact(ctx context.Context, artifactID string) (*Artifact, error) {
	return db.UpdateArtifact(ctx, artifactID, "", "", "approved")
}

func (db *DB) CreateArtifactSet(ctx context.Context, epicID, runID string, artifactIDs []string) (*ArtifactSet, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(artifactIDs)
	now := Now()
	s := &ArtifactSet{
		ID:              id.New(id.ArtifactSet),
		WorkspaceID:     ws.ID,
		EpicID:          epicID,
		Status:          "draft",
		ArtifactIDsJSON: string(b),
		SourceRunID:     runID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_, err = db.Exec(ctx, `INSERT INTO artifact_sets(id, workspace_id, epic_id, status, artifact_ids_json, source_run_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		s.ID, s.WorkspaceID, s.EpicID, s.Status, s.ArtifactIDsJSON, nullStr(s.SourceRunID), s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	for _, aid := range artifactIDs {
		_, _ = db.Exec(ctx, `UPDATE artifacts SET artifact_set_id=? WHERE id=?`, s.ID, aid)
	}
	return s, nil
}

func (db *DB) GetLatestApprovedArtifactSet(ctx context.Context, epicID string) (*ArtifactSet, error) {
	var s ArtifactSet
	err := db.QueryRow(ctx, `SELECT id, workspace_id, epic_id, status, artifact_ids_json, COALESCE(source_run_id,''), created_at, updated_at
		FROM artifact_sets WHERE epic_id=? AND status='approved' ORDER BY created_at DESC LIMIT 1`, epicID).
		Scan(&s.ID, &s.WorkspaceID, &s.EpicID, &s.Status, &s.ArtifactIDsJSON, &s.SourceRunID, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no approved artifact set for epic %s", epicID)
	}
	return &s, err
}

func (db *DB) ApproveArtifactSet(ctx context.Context, setID string) error {
	_, err := db.Exec(ctx, `UPDATE artifact_sets SET status='approved', updated_at=? WHERE id=?`, Now(), setID)
	return err
}

func (db *DB) indexArtifact(ctx context.Context, a *Artifact) {
	if db.Dialect != "sqlite" {
		return
	}
	_, _ = db.Exec(ctx, `DELETE FROM artifact_fts WHERE id=?`, a.ID)
	_, _ = db.Exec(ctx, `INSERT INTO artifact_fts(id, title, body) VALUES (?,?,?)`, a.ID, a.Title, a.Body)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
