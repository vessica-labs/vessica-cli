package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) EnqueueCloudOrchestrationTask(ctx context.Context, kind, subjectID, payloadJSON string) (*CloudOrchestrationTask, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if kind == "" || subjectID == "" {
		return nil, fmt.Errorf("orchestration kind and subject are required")
	}
	if payloadJSON == "" {
		payloadJSON = "{}"
	}
	now := Now()
	if _, err = db.Exec(ctx, `INSERT INTO cloud_orchestration_tasks(id,workspace_id,kind,subject_id,state,payload_json,available_at,created_at,updated_at) VALUES(?,?,?,?, 'pending',?,?,?,?) ON CONFLICT(workspace_id,kind,subject_id) DO NOTHING`, id.New("orch"), workspace.ID, kind, subjectID, payloadJSON, now, now, now); err != nil {
		return nil, err
	}
	return db.GetCloudOrchestrationTask(ctx, kind, subjectID)
}

func (db *DB) enqueueCloudOrchestrationTaskTx(ctx context.Context, tx *sql.Tx, workspaceID, kind, subjectID, payloadJSON, now string) error {
	_, err := tx.ExecContext(ctx, db.Rebind(`INSERT INTO cloud_orchestration_tasks(id,workspace_id,kind,subject_id,state,payload_json,available_at,created_at,updated_at) VALUES(?,?,?,?, 'pending',?,?,?,?) ON CONFLICT(workspace_id,kind,subject_id) DO NOTHING`), id.New("orch"), workspaceID, kind, subjectID, payloadJSON, now, now, now)
	return err
}

func (db *DB) GetCloudOrchestrationTask(ctx context.Context, kind, subjectID string) (*CloudOrchestrationTask, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return scanCloudOrchestrationTask(db.QueryRow(ctx, `SELECT id,workspace_id,kind,subject_id,state,payload_json,COALESCE(run_id,''),COALESCE(artifact_id,''),attempts,available_at,COALESCE(lease_owner,''),COALESCE(lease_until,''),COALESCE(last_error,''),created_at,updated_at,COALESCE(completed_at,'') FROM cloud_orchestration_tasks WHERE workspace_id=? AND kind=? AND subject_id=?`, workspace.ID, kind, subjectID))
}

func (db *DB) ClaimCloudOrchestrationTask(ctx context.Context, owner string, lease time.Duration) (*CloudOrchestrationTask, error) {
	return db.ClaimCloudOrchestrationTaskKind(ctx, owner, "", lease)
}

func (db *DB) ClaimCloudOrchestrationTaskKind(ctx context.Context, owner, kind string, lease time.Duration) (*CloudOrchestrationTask, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if owner == "" {
		return nil, fmt.Errorf("orchestration worker is required")
	}
	if lease <= 0 {
		lease = time.Minute
	}
	nowTime := time.Now().UTC()
	now, leaseUntil := FormatTime(nowTime), FormatTime(nowTime.Add(lease))
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	query := `SELECT id FROM cloud_orchestration_tasks WHERE workspace_id=?`
	args := []any{workspace.ID}
	if kind != "" {
		query += ` AND kind=?`
		args = append(args, kind)
	}
	query += ` AND ((state IN ('pending','waiting') AND available_at<=?) OR (state='processing' AND lease_until<?)) ORDER BY available_at,created_at LIMIT 1`
	args = append(args, now, now)
	if db.Dialect == "postgres" {
		query += " FOR UPDATE SKIP LOCKED"
	}
	var taskID string
	if err = tx.QueryRowContext(ctx, db.Rebind(query), args...).Scan(&taskID); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE cloud_orchestration_tasks SET state='processing',attempts=attempts+1,lease_owner=?,lease_until=?,updated_at=? WHERE id=? AND workspace_id=? AND ((state IN ('pending','waiting') AND available_at<=?) OR (state='processing' AND lease_until<?))`), owner, leaseUntil, now, taskID, workspace.ID, now, now)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return nil, nil
	}
	task, err := scanCloudOrchestrationTask(tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,kind,subject_id,state,payload_json,COALESCE(run_id,''),COALESCE(artifact_id,''),attempts,available_at,COALESCE(lease_owner,''),COALESCE(lease_until,''),COALESCE(last_error,''),created_at,updated_at,COALESCE(completed_at,'') FROM cloud_orchestration_tasks WHERE id=? AND workspace_id=?`), taskID, workspace.ID))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

type OutlookBatchCoverage struct {
	Count          int
	OldestSourceAt string
	NewestSourceAt string
	EmailCursor    string
	CalendarCursor string
}

func (db *DB) OutlookBatchCoverage(ctx context.Context, batchID string) (OutlookBatchCoverage, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return OutlookBatchCoverage{}, err
	}
	var value OutlookBatchCoverage
	if err = db.QueryRow(ctx, `SELECT COUNT(*),COALESCE(MIN(message_at),''),COALESCE(MAX(message_at),'') FROM outlook_ingestion_items WHERE workspace_id=? AND batch_id=? AND state='completed'`, workspace.ID, batchID).Scan(&value.Count, &value.OldestSourceAt, &value.NewestSourceAt); err != nil {
		return value, err
	}
	_ = db.QueryRow(ctx, `SELECT checkpoint_value FROM source_checkpoints WHERE workspace_id=? AND source_type='outlook_email' AND source_id='outlook'`, workspace.ID).Scan(&value.EmailCursor)
	_ = db.QueryRow(ctx, `SELECT checkpoint_value FROM source_checkpoints WHERE workspace_id=? AND source_type='outlook_calendar' AND source_id='outlook'`, workspace.ID).Scan(&value.CalendarCursor)
	return value, nil
}

func (db *DB) LinkCloudOrchestrationRun(ctx context.Context, taskID, owner, runID string) error {
	return db.updateCloudOrchestrationFence(ctx, taskID, owner, `UPDATE cloud_orchestration_tasks SET run_id=?,state='waiting',available_at=?,lease_owner=NULL,lease_until=NULL,last_error=NULL,updated_at=? WHERE id=? AND workspace_id=? AND state='processing' AND lease_owner=?`, runID, Now(), Now())
}

func (db *DB) RescheduleCloudOrchestrationTask(ctx context.Context, taskID, owner, errorText string, availableAt time.Time) error {
	return db.updateCloudOrchestrationFence(ctx, taskID, owner, `UPDATE cloud_orchestration_tasks SET state='waiting',available_at=?,lease_owner=NULL,lease_until=NULL,last_error=?,updated_at=? WHERE id=? AND workspace_id=? AND state='processing' AND lease_owner=?`, FormatTime(availableAt), errorText, Now())
}

func (db *DB) CompleteCloudOrchestrationTask(ctx context.Context, taskID, owner, artifactID string) error {
	now := Now()
	return db.updateCloudOrchestrationFence(ctx, taskID, owner, `UPDATE cloud_orchestration_tasks SET state='completed',artifact_id=?,completed_at=?,lease_owner=NULL,lease_until=NULL,last_error=NULL,updated_at=? WHERE id=? AND workspace_id=? AND state='processing' AND lease_owner=?`, artifactID, now, now)
}

func (db *DB) updateCloudOrchestrationFence(ctx context.Context, taskID, owner, query string, args ...any) error {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	args = append(args, taskID, workspace.ID, owner)
	result, err := db.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("cloud orchestration fence lost")
	}
	return nil
}

func (db *DB) OutlookIngestionItem(ctx context.Context, itemID string) (*OutlookIngestionItem, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	item, err := scanOutlookItem(db.QueryRow(ctx, `SELECT id,workspace_id,batch_id,source_id,internet_message_id,conversation_id,message_at,normalized_json,state,error,created_at,updated_at FROM outlook_ingestion_items WHERE id=? AND workspace_id=?`, itemID, workspace.ID))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("outlook item not found")
	}
	return item, err
}

func (db *DB) OutlookIngestionBatch(ctx context.Context, batchID string) (*OutlookIngestionBatch, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var value OutlookIngestionBatch
	var storedError, completedAt sql.NullString
	err = db.QueryRow(ctx, `SELECT id,workspace_id,idempotency_key,submitted_by,state,checkpoint_json,warnings_json,error,created_at,updated_at,completed_at FROM outlook_ingestion_batches WHERE id=? AND workspace_id=?`, batchID, workspace.ID).Scan(&value.ID, &value.WorkspaceID, &value.IdempotencyKey, &value.SubmittedBy, &value.State, &value.CheckpointJSON, &value.WarningsJSON, &storedError, &value.CreatedAt, &value.UpdatedAt, &completedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("outlook batch not found")
	}
	value.Error, value.CompletedAt = storedError.String, completedAt.String
	return &value, err
}

func scanCloudOrchestrationTask(row interface{ Scan(...any) error }) (*CloudOrchestrationTask, error) {
	var value CloudOrchestrationTask
	err := row.Scan(&value.ID, &value.WorkspaceID, &value.Kind, &value.SubjectID, &value.State, &value.PayloadJSON, &value.RunID, &value.ArtifactID, &value.Attempts, &value.AvailableAt, &value.LeaseOwner, &value.LeaseUntil, &value.LastError, &value.CreatedAt, &value.UpdatedAt, &value.CompletedAt)
	return &value, err
}

func (db *DB) CanonicalKnowledgeArtifact(ctx context.Context, canonicalKey string) (string, error) {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return "", err
	}
	var artifactID string
	err = db.QueryRow(ctx, `SELECT artifact_id FROM canonical_knowledge_artifacts WHERE workspace_id=? AND canonical_key=?`, workspace.ID, canonicalKey).Scan(&artifactID)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("canonical knowledge artifact not found")
	}
	return artifactID, err
}

func (db *DB) UpsertCanonicalKnowledgeArtifact(ctx context.Context, canonicalKey, artifactID string) error {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if canonicalKey == "" || artifactID == "" {
		return fmt.Errorf("canonical key and artifact id are required")
	}
	now := Now()
	_, err = db.Exec(ctx, `INSERT INTO canonical_knowledge_artifacts(workspace_id,canonical_key,artifact_id,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(workspace_id,canonical_key) DO UPDATE SET artifact_id=excluded.artifact_id,updated_at=excluded.updated_at`, workspace.ID, canonicalKey, artifactID, now, now)
	return err
}
