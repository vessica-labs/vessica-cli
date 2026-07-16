package state

import (
	"context"
	"database/sql"
	"fmt"
)

type OnboardingOperation struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	RepositoryID string `json:"repository_id"`
	Status       string `json:"status"`
	CurrentStage string `json:"current_stage"`
	DocumentJSON string `json:"document_json"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (db *DB) UpsertOnboardingOperation(ctx context.Context, operation OnboardingOperation) (*OnboardingOperation, error) {
	repository, err := db.GetRepository(ctx, operation.RepositoryID)
	if err != nil {
		return nil, err
	}
	if operation.ID == "" || operation.DocumentJSON == "" {
		return nil, fmt.Errorf("onboarding operation id and document are required")
	}
	now := Now()
	if operation.CreatedAt == "" {
		operation.CreatedAt = now
	}
	operation.UpdatedAt = now
	operation.WorkspaceID = repository.WorkspaceID
	_, err = db.Exec(ctx, `INSERT INTO onboarding_operations(id,workspace_id,repository_id,status,current_stage,document_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,current_stage=excluded.current_stage,document_json=excluded.document_json,updated_at=excluded.updated_at`,
		operation.ID, operation.WorkspaceID, operation.RepositoryID, operation.Status, operation.CurrentStage, operation.DocumentJSON, operation.CreatedAt, operation.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return db.GetOnboardingOperation(ctx, operation.ID)
}

func (db *DB) GetOnboardingOperation(ctx context.Context, operationID string) (*OnboardingOperation, error) {
	var operation OnboardingOperation
	err := db.QueryRow(ctx, `SELECT id,workspace_id,repository_id,status,current_stage,document_json,created_at,updated_at FROM onboarding_operations WHERE id=?`, operationID).
		Scan(&operation.ID, &operation.WorkspaceID, &operation.RepositoryID, &operation.Status, &operation.CurrentStage, &operation.DocumentJSON, &operation.CreatedAt, &operation.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("onboarding operation not found: %s", operationID)
	}
	return &operation, err
}
