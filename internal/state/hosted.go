package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) UpsertTrackerIntegration(ctx context.Context, provider, status string, config any, webhookID, secretRef string) (*TrackerIntegration, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if status == "" {
		status = "connected"
	}
	b, _ := json.Marshal(config)
	now := Now()
	recordID := id.New("intg")
	query := `INSERT INTO tracker_integrations(id, workspace_id, provider, status, config_json, webhook_id, secret_ref, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id, provider) DO UPDATE SET status=excluded.status, config_json=excluded.config_json, webhook_id=excluded.webhook_id, secret_ref=excluded.secret_ref, updated_at=excluded.updated_at`
	if _, err := db.Exec(ctx, query, recordID, ws.ID, provider, status, string(b), nullStr(webhookID), nullStr(secretRef), now, now); err != nil {
		return nil, err
	}
	return db.GetTrackerIntegration(ctx, provider)
}

func (db *DB) GetTrackerIntegration(ctx context.Context, provider string) (*TrackerIntegration, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var record TrackerIntegration
	var webhookID, secretRef, lastSynced, lastError sql.NullString
	err = db.QueryRow(ctx, `SELECT id, workspace_id, provider, status, config_json, webhook_id, secret_ref, last_synced_at, last_error, created_at, updated_at
		FROM tracker_integrations WHERE workspace_id=? AND provider=?`, ws.ID, provider).
		Scan(&record.ID, &record.WorkspaceID, &record.Provider, &record.Status, &record.ConfigJSON, &webhookID, &secretRef, &lastSynced, &lastError, &record.CreatedAt, &record.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%s tracker integration not configured", provider)
	}
	record.WebhookID = webhookID.String
	record.SecretRef = secretRef.String
	record.LastSyncedAt = lastSynced.String
	record.LastError = lastError.String
	return &record, err
}

func (db *DB) SetTrackerIntegrationSync(ctx context.Context, provider, status, lastError string) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `UPDATE tracker_integrations SET status=?, last_synced_at=?, last_error=?, updated_at=? WHERE workspace_id=? AND provider=?`, status, Now(), nullStr(lastError), Now(), ws.ID, provider)
	return err
}

// ReceiveWebhook atomically persists a provider delivery and enqueues its processing job.
func (db *DB) ReceiveWebhook(ctx context.Context, integration *TrackerIntegration, deliveryID, eventType string, payload []byte) (*WebhookDelivery, *Job, bool, error) {
	if integration == nil {
		return nil, nil, false, fmt.Errorf("tracker integration is required")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	defer tx.Rollback()
	now := Now()
	delivery := &WebhookDelivery{ID: id.New("whd"), IntegrationID: integration.ID, Provider: integration.Provider, DeliveryID: deliveryID, EventType: eventType, PayloadJSON: string(payload), Status: "pending", CreatedAt: now}
	insert := db.Rebind(`INSERT INTO webhook_deliveries(id, integration_id, provider, delivery_id, event_type, payload_json, status, attempts, created_at)
		VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(provider, delivery_id) DO NOTHING`)
	result, err := tx.ExecContext(ctx, insert, delivery.ID, delivery.IntegrationID, delivery.Provider, delivery.DeliveryID, delivery.EventType, delivery.PayloadJSON, delivery.Status, 0, delivery.CreatedAt)
	if err != nil {
		return nil, nil, false, err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		if err := tx.Commit(); err != nil {
			return nil, nil, true, err
		}
		return delivery, nil, true, nil
	}
	jobPayload, _ := json.Marshal(map[string]string{"delivery_id": delivery.ID})
	job := &Job{ID: id.New("job"), WorkspaceID: integration.WorkspaceID, Kind: "tracker_webhook", Status: "pending", PayloadJSON: string(jobPayload), MaxAttempts: 5, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	jobInsert := db.Rebind(`INSERT INTO jobs(id, workspace_id, kind, status, payload_json, attempts, max_attempts, available_at, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`)
	if _, err := tx.ExecContext(ctx, jobInsert, job.ID, job.WorkspaceID, job.Kind, job.Status, job.PayloadJSON, 0, job.MaxAttempts, job.AvailableAt, job.CreatedAt, job.UpdatedAt); err != nil {
		return nil, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	return delivery, job, false, nil
}

func (db *DB) GetWebhookDelivery(ctx context.Context, deliveryID string) (*WebhookDelivery, error) {
	var record WebhookDelivery
	var lastError, processedAt sql.NullString
	err := db.QueryRow(ctx, `SELECT id, integration_id, provider, delivery_id, event_type, payload_json, status, attempts, last_error, created_at, processed_at FROM webhook_deliveries WHERE id=?`, deliveryID).
		Scan(&record.ID, &record.IntegrationID, &record.Provider, &record.DeliveryID, &record.EventType, &record.PayloadJSON, &record.Status, &record.Attempts, &lastError, &record.CreatedAt, &processedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("webhook delivery not found: %s", deliveryID)
	}
	record.LastError = lastError.String
	record.ProcessedAt = processedAt.String
	return &record, err
}

func (db *DB) CompleteWebhookDelivery(ctx context.Context, deliveryID string) error {
	_, err := db.Exec(ctx, `UPDATE webhook_deliveries SET status='processed', attempts=attempts+1, processed_at=?, last_error=NULL WHERE id=?`, Now(), deliveryID)
	return err
}

func (db *DB) FailWebhookDelivery(ctx context.Context, deliveryID, message string) error {
	_, err := db.Exec(ctx, `UPDATE webhook_deliveries SET status='failed', attempts=attempts+1, last_error=? WHERE id=?`, message, deliveryID)
	return err
}

func (db *DB) EnqueueJob(ctx context.Context, kind string, payload any, runID string) (*Job, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(payload)
	var repositoryID string
	if runID != "" {
		if runRecord, runErr := db.GetRun(ctx, runID); runErr == nil {
			repositoryID = runRecord.RepositoryID
		}
	} else {
		var routed struct {
			EpicID string `json:"epic_id"`
		}
		if json.Unmarshal(b, &routed) == nil && routed.EpicID != "" {
			if epic, epicErr := db.GetEpic(ctx, routed.EpicID); epicErr == nil {
				repositoryID = epic.RepositoryID
			}
		}
	}
	now := Now()
	job := &Job{ID: id.New("job"), WorkspaceID: ws.ID, RepositoryID: repositoryID, Kind: kind, Status: "pending", PayloadJSON: string(b), RunID: runID, MaxAttempts: 5, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	_, err = db.Exec(ctx, `INSERT INTO jobs(id, workspace_id, repository_id, kind, status, payload_json, run_id, attempts, max_attempts, available_at, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, job.ID, job.WorkspaceID, nullStr(job.RepositoryID), job.Kind, job.Status, job.PayloadJSON, nullStr(job.RunID), 0, job.MaxAttempts, job.AvailableAt, job.CreatedAt, job.UpdatedAt)
	return job, err
}

func (db *DB) ClaimJob(ctx context.Context, owner string, lease time.Duration) (*Job, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := Now()
	query := `SELECT id, workspace_id, COALESCE(repository_id,''), kind, status, payload_json, COALESCE(run_id,''), attempts, max_attempts, COALESCE(lease_owner,''), COALESCE(lease_until,''), available_at, COALESCE(last_error,''), created_at, updated_at
		FROM jobs WHERE status IN ('pending','retry','running') AND available_at<=? AND (status!='running' OR lease_until IS NULL OR lease_until='' OR lease_until<?) ORDER BY created_at LIMIT 1`
	if db.Dialect == "postgres" {
		query += " FOR UPDATE SKIP LOCKED"
	}
	var job Job
	err = tx.QueryRowContext(ctx, db.Rebind(query), now, now).Scan(&job.ID, &job.WorkspaceID, &job.RepositoryID, &job.Kind, &job.Status, &job.PayloadJSON, &job.RunID, &job.Attempts, &job.MaxAttempts, &job.LeaseOwner, &job.LeaseUntil, &job.AvailableAt, &job.LastError, &job.CreatedAt, &job.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leaseUntil := time.Now().UTC().Add(lease).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, db.Rebind(`UPDATE jobs SET status='running', attempts=attempts+1, lease_owner=?, lease_until=?, updated_at=? WHERE id=?`), owner, leaseUntil, now, job.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	job.Status = "running"
	job.Attempts++
	job.LeaseOwner = owner
	job.LeaseUntil = leaseUntil
	return &job, nil
}

func (db *DB) CompleteJob(ctx context.Context, jobID string) error {
	_, err := db.Exec(ctx, `UPDATE jobs SET status='completed', lease_owner=NULL, lease_until=NULL, last_error=NULL, updated_at=? WHERE id=? AND status!='cancelled'`, Now(), jobID)
	return err
}

// CancelJobsForRun atomically makes every queued or leased job for a run
// ineligible for another claim and releases its worker lease.
func (db *DB) CancelJobsForRun(ctx context.Context, runID string) (int64, error) {
	result, err := db.Exec(ctx, `UPDATE jobs SET status='cancelled', lease_owner=NULL, lease_until=NULL, last_error=NULL, updated_at=? WHERE run_id=? AND status IN ('pending','retry','running')`, Now(), runID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CancelRunAndJobs persists the terminal run and releases all associated job
// leases in one transaction, so a cancellation cannot be half-applied.
func (db *DB) CancelRunAndJobs(ctx context.Context, runID, finishedAt string) (int64, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := Now()
	if _, err := tx.ExecContext(ctx, db.Rebind(`UPDATE runs SET status='cancelled', finished_at=?, updated_at=? WHERE id=?`), finishedAt, now, runID); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, db.Rebind(`UPDATE jobs SET status='cancelled', lease_owner=NULL, lease_until=NULL, last_error=NULL, updated_at=? WHERE run_id=? AND status IN ('pending','retry','running')`), now, runID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (db *DB) SetJobRunID(ctx context.Context, jobID, runID string) error {
	repositoryID := ""
	if runRecord, err := db.GetRun(ctx, runID); err == nil {
		repositoryID = runRecord.RepositoryID
	}
	_, err := db.Exec(ctx, `UPDATE jobs SET run_id=?, repository_id=?, updated_at=? WHERE id=?`, nullStr(runID), nullStr(repositoryID), Now(), jobID)
	return err
}

func (db *DB) FailJob(ctx context.Context, job *Job, message string) error {
	if job == nil {
		return fmt.Errorf("job is required")
	}
	status := "retry"
	if job.Attempts >= job.MaxAttempts {
		status = "failed"
	}
	backoff := time.Duration(job.Attempts*job.Attempts) * time.Minute
	available := time.Now().UTC().Add(backoff).Format(time.RFC3339Nano)
	_, err := db.Exec(ctx, `UPDATE jobs SET status=?, lease_owner=NULL, lease_until=NULL, available_at=?, last_error=?, updated_at=? WHERE id=? AND status!='cancelled'`, status, available, message, Now(), job.ID)
	return err
}

func (db *DB) ListJobs(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(ctx, `SELECT id, workspace_id, COALESCE(repository_id,''), kind, status, payload_json, COALESCE(run_id,''), attempts, max_attempts, COALESCE(lease_owner,''), COALESCE(lease_until,''), available_at, COALESCE(last_error,''), created_at, updated_at FROM jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var job Job
		if err := rows.Scan(&job.ID, &job.WorkspaceID, &job.RepositoryID, &job.Kind, &job.Status, &job.PayloadJSON, &job.RunID, &job.Attempts, &job.MaxAttempts, &job.LeaseOwner, &job.LeaseUntil, &job.AvailableAt, &job.LastError, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (db *DB) EnqueueOutbox(ctx context.Context, integrationID, operation, idempotencyKey string, payload any) (*OutboxMessage, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(payload)
	now := Now()
	message := &OutboxMessage{ID: id.New("out"), WorkspaceID: ws.ID, IntegrationID: integrationID, Operation: operation, IdempotencyKey: idempotencyKey, PayloadJSON: string(b), Status: "pending", AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	_, err = db.Exec(ctx, `INSERT INTO outbox_messages(id, workspace_id, integration_id, operation, idempotency_key, payload_json, status, attempts, available_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(idempotency_key) DO UPDATE SET payload_json=excluded.payload_json, status=CASE WHEN outbox_messages.status='completed' THEN outbox_messages.status ELSE 'pending' END, updated_at=excluded.updated_at`, message.ID, message.WorkspaceID, message.IntegrationID, message.Operation, message.IdempotencyKey, message.PayloadJSON, message.Status, 0, message.AvailableAt, message.CreatedAt, message.UpdatedAt)
	return message, err
}

func (db *DB) ClaimOutbox(ctx context.Context) (*OutboxMessage, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := Now()
	stale := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339Nano)
	query := `SELECT id, workspace_id, integration_id, operation, idempotency_key, payload_json, status, attempts, available_at, COALESCE(last_error,''), created_at, updated_at
		FROM outbox_messages WHERE (status IN ('pending','retry') AND available_at<=?) OR (status='running' AND updated_at<?) ORDER BY created_at LIMIT 1`
	if db.Dialect == "postgres" {
		query += " FOR UPDATE SKIP LOCKED"
	}
	var message OutboxMessage
	if err := tx.QueryRowContext(ctx, db.Rebind(query), now, stale).Scan(&message.ID, &message.WorkspaceID, &message.IntegrationID, &message.Operation, &message.IdempotencyKey, &message.PayloadJSON, &message.Status, &message.Attempts, &message.AvailableAt, &message.LastError, &message.CreatedAt, &message.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, db.Rebind(`UPDATE outbox_messages SET status='running', attempts=attempts+1, updated_at=? WHERE id=?`), now, message.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	message.Status = "running"
	message.Attempts++
	return &message, nil
}

func (db *DB) CompleteOutbox(ctx context.Context, messageID string) error {
	_, err := db.Exec(ctx, `UPDATE outbox_messages SET status='completed', last_error=NULL, updated_at=? WHERE id=?`, Now(), messageID)
	return err
}

func (db *DB) FailOutbox(ctx context.Context, message *OutboxMessage, failure string) error {
	if message == nil {
		return fmt.Errorf("outbox message is required")
	}
	status := "retry"
	if message.Attempts >= 8 {
		status = "failed"
	}
	available := time.Now().UTC().Add(time.Duration(message.Attempts*message.Attempts) * time.Minute).Format(time.RFC3339Nano)
	_, err := db.Exec(ctx, `UPDATE outbox_messages SET status=?, available_at=?, last_error=?, updated_at=? WHERE id=?`, status, available, failure, Now(), message.ID)
	return err
}

func (db *DB) UpsertControlPlaneDeployment(ctx context.Context, deployment *ControlPlaneDeployment) (*ControlPlaneDeployment, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if deployment == nil {
		return nil, fmt.Errorf("deployment is required")
	}
	now := Now()
	if deployment.ID == "" {
		deployment.ID = id.New("cpd")
	}
	deployment.WorkspaceID = ws.ID
	if deployment.Status == "" {
		deployment.Status = "provisioning"
	}
	if deployment.MetaJSON == "" {
		deployment.MetaJSON = "{}"
	}
	if deployment.CreatedAt == "" {
		deployment.CreatedAt = now
	}
	deployment.UpdatedAt = now
	_, err = db.Exec(ctx, `INSERT INTO control_plane_deployments(id, workspace_id, provider, project_id, environment_id, service_id, postgres_service_id, public_url, version, status, meta_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id, provider) DO UPDATE SET project_id=excluded.project_id, environment_id=excluded.environment_id, service_id=excluded.service_id, postgres_service_id=excluded.postgres_service_id, public_url=excluded.public_url, version=excluded.version, status=excluded.status, meta_json=excluded.meta_json, updated_at=excluded.updated_at`, deployment.ID, deployment.WorkspaceID, deployment.Provider, deployment.ProjectID, deployment.EnvironmentID, deployment.ServiceID, nullStr(deployment.PostgresServiceID), nullStr(deployment.PublicURL), nullStr(deployment.Version), deployment.Status, deployment.MetaJSON, deployment.CreatedAt, deployment.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return db.GetControlPlaneDeployment(ctx, deployment.Provider)
}

func (db *DB) GetControlPlaneDeployment(ctx context.Context, provider string) (*ControlPlaneDeployment, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	var deployment ControlPlaneDeployment
	var postgresID, publicURL, version sql.NullString
	err = db.QueryRow(ctx, `SELECT id, workspace_id, provider, project_id, environment_id, service_id, postgres_service_id, public_url, version, status, meta_json, created_at, updated_at FROM control_plane_deployments WHERE workspace_id=? AND provider=?`, ws.ID, provider).
		Scan(&deployment.ID, &deployment.WorkspaceID, &deployment.Provider, &deployment.ProjectID, &deployment.EnvironmentID, &deployment.ServiceID, &postgresID, &publicURL, &version, &deployment.Status, &deployment.MetaJSON, &deployment.CreatedAt, &deployment.UpdatedAt)
	deployment.PostgresServiceID = postgresID.String
	deployment.PublicURL = publicURL.String
	deployment.Version = version.String
	return &deployment, err
}

func (db *DB) GetExternalMappingByExternal(ctx context.Context, provider, entityType, externalID string) (*ExternalMapping, error) {
	var mapping ExternalMapping
	err := db.QueryRow(ctx, `SELECT id, workspace_id, COALESCE(repository_id,''), provider, entity_type, local_id, external_id, meta_json, created_at, updated_at FROM external_mappings WHERE provider=? AND entity_type=? AND external_id=?`, provider, entityType, externalID).
		Scan(&mapping.ID, &mapping.WorkspaceID, &mapping.RepositoryID, &mapping.Provider, &mapping.EntityType, &mapping.LocalID, &mapping.ExternalID, &mapping.MetaJSON, &mapping.CreatedAt, &mapping.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mapping not found")
	}
	return &mapping, err
}

func (db *DB) GetExternalMapping(ctx context.Context, provider, entityType, localID string) (*ExternalMapping, error) {
	var mapping ExternalMapping
	err := db.QueryRow(ctx, `SELECT id, workspace_id, COALESCE(repository_id,''), provider, entity_type, local_id, external_id, meta_json, created_at, updated_at FROM external_mappings WHERE provider=? AND entity_type=? AND local_id=?`, provider, entityType, localID).
		Scan(&mapping.ID, &mapping.WorkspaceID, &mapping.RepositoryID, &mapping.Provider, &mapping.EntityType, &mapping.LocalID, &mapping.ExternalID, &mapping.MetaJSON, &mapping.CreatedAt, &mapping.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mapping not found")
	}
	return &mapping, err
}

func (db *DB) UpsertExternalMapping(ctx context.Context, provider, entityType, localID, externalID string, meta any, syncStatus string) (*ExternalMapping, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if syncStatus == "" {
		syncStatus = "synced"
	}
	b, _ := json.Marshal(meta)
	repositoryID := ""
	switch entityType {
	case "epic":
		if epic, getErr := db.GetEpic(ctx, localID); getErr == nil {
			repositoryID = epic.RepositoryID
		}
	case "run":
		if runRecord, getErr := db.GetRun(ctx, localID); getErr == nil {
			repositoryID = runRecord.RepositoryID
		}
	case "artifact":
		if artifact, getErr := db.GetArtifact(ctx, localID); getErr == nil {
			repositoryID = artifact.RepositoryID
		}
	}
	now := Now()
	mappingID := id.New("map")
	_, err = db.Exec(ctx, `INSERT INTO external_mappings(id, workspace_id, repository_id, provider, entity_type, local_id, external_id, meta_json, created_at, updated_at, sync_status, last_synced_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(provider, entity_type, local_id) DO UPDATE SET repository_id=excluded.repository_id, external_id=excluded.external_id, meta_json=excluded.meta_json, updated_at=excluded.updated_at, sync_status=excluded.sync_status, last_synced_at=excluded.last_synced_at, last_error=NULL`, mappingID, ws.ID, nullStr(repositoryID), provider, entityType, localID, externalID, string(b), now, now, syncStatus, now)
	if err != nil {
		return nil, err
	}
	return db.GetExternalMappingByExternal(ctx, provider, entityType, externalID)
}
