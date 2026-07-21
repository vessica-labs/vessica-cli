package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) CreateAgentBuild(ctx context.Context, kind, description, agentID, createdBy string, review bool, timezone string) (*AgentBuildOperation, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	now := Now()
	op := &AgentBuildOperation{ID: id.New(id.AgentBuild), WorkspaceID: ws.ID, AgentID: agentID, Kind: kind, Description: description, Review: review, Status: "queued", WarningsJSON: "[]", UsageJSON: "{}", CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now}
	rv := 0
	if review {
		rv = 1
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_build_operations(id,workspace_id,agent_id,kind,description,review,status,warnings_json,usage_json,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`), op.ID, op.WorkspaceID, nullStr(agentID), kind, description, rv, op.Status, "[]", "{}", createdBy, now, now); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"timezone": timezone})
	if _, err = insertAgentTask(ctx, db, tx, ws.ID, "build", op.ID, string(payload), now); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return op, nil
}

func (db *DB) GetAgentBuild(ctx context.Context, buildID string) (*AgentBuildOperation, error) {
	var o AgentBuildOperation
	var agentID, errText sql.NullString
	var review int
	err := db.QueryRow(ctx, `SELECT id,workspace_id,agent_id,kind,description,review,status,warnings_json,usage_json,error,created_by,created_at,updated_at FROM agent_build_operations WHERE id=?`, buildID).Scan(&o.ID, &o.WorkspaceID, &agentID, &o.Kind, &o.Description, &review, &o.Status, &o.WarningsJSON, &o.UsageJSON, &errText, &o.CreatedBy, &o.CreatedAt, &o.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent build not found")
	}
	o.AgentID = agentID.String
	o.Error = errText.String
	o.Review = review != 0
	return &o, err
}

func (db *DB) ListAgentBuilds(ctx context.Context) ([]AgentBuildOperation, error) {
	rows, err := db.Query(ctx, `SELECT id,workspace_id,COALESCE(agent_id,''),kind,description,review,status,warnings_json,usage_json,COALESCE(error,''),created_by,created_at,updated_at FROM agent_build_operations ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentBuildOperation
	for rows.Next() {
		var operation AgentBuildOperation
		var review int
		if err = rows.Scan(&operation.ID, &operation.WorkspaceID, &operation.AgentID, &operation.Kind, &operation.Description, &review, &operation.Status, &operation.WarningsJSON, &operation.UsageJSON, &operation.Error, &operation.CreatedBy, &operation.CreatedAt, &operation.UpdatedAt); err != nil {
			return nil, err
		}
		operation.Review = review != 0
		out = append(out, operation)
	}
	return out, rows.Err()
}

func (db *DB) CompleteAgentBuild(ctx context.Context, buildID, fence, definitionJSON, warningsJSON, usageJSON string) (*AgentDraft, error) {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var wsID, agentID string
	var review int
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT b.workspace_id,COALESCE(b.agent_id,''),b.review FROM agent_build_operations b JOIN agent_runtime_tasks t ON t.subject_id=b.id AND t.kind='build' WHERE b.id=? AND t.fence_token=? AND t.status='running'`), buildID, fence).Scan(&wsID, &agentID, &review)
	if err == sql.ErrNoRows {
		return nil, ErrAgentFenceLost
	}
	if err != nil {
		return nil, err
	}
	now := Now()
	draft := &AgentDraft{ID: id.New(id.AgentDraft), OperationID: buildID, WorkspaceID: wsID, AgentID: agentID, DefinitionJSON: definitionJSON, WarningsJSON: warningsJSON, Status: "pending", CreatedAt: now}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_drafts(id,operation_id,workspace_id,agent_id,definition_json,warnings_json,status,created_at) VALUES(?,?,?,?,?,?,?,?)`), draft.ID, buildID, wsID, nullStr(agentID), definitionJSON, warningsJSON, "pending", now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_build_operations SET status=?,warnings_json=?,usage_json=?,updated_at=? WHERE id=?`), map[bool]string{true: "draft", false: "generated"}[review != 0], warningsJSON, usageJSON, now, buildID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status='completed',updated_at=? WHERE kind='build' AND subject_id=? AND fence_token=?`), now, buildID, fence); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return draft, nil
}

func (db *DB) GetAgentDraft(ctx context.Context, draftID string) (*AgentDraft, error) {
	var d AgentDraft
	var agent, activated sql.NullString
	err := db.QueryRow(ctx, `SELECT id,operation_id,workspace_id,agent_id,definition_json,warnings_json,status,created_at,activated_at FROM agent_drafts WHERE id=?`, draftID).Scan(&d.ID, &d.OperationID, &d.WorkspaceID, &agent, &d.DefinitionJSON, &d.WarningsJSON, &d.Status, &d.CreatedAt, &activated)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent draft not found")
	}
	d.AgentID = agent.String
	d.ActivatedAt = activated.String
	return &d, err
}

func (db *DB) MarkDraftActivated(ctx context.Context, draftID, agentID string) error {
	now := Now()
	_, err := db.Exec(ctx, `UPDATE agent_drafts SET status='activated',agent_id=?,activated_at=? WHERE id=? AND status='pending'`, agentID, now, draftID)
	if err == nil {
		_, err = db.Exec(ctx, `UPDATE agent_build_operations SET status='completed',agent_id=?,updated_at=? WHERE id=(SELECT operation_id FROM agent_drafts WHERE id=?)`, agentID, now, draftID)
	}
	return err
}

func (db *DB) ClaimAgentRuntimeTask(ctx context.Context, workerID string, lease time.Duration) (*AgentRuntimeTask, *AgentRunAttempt, error) {
	if err := db.expireExhaustedAgentTask(ctx); err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	leaseUntil := FormatTime(now.Add(lease))
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var task AgentRuntimeTask
	q := `SELECT id,workspace_id,kind,subject_id,status,available_at,attempts,max_attempts,payload_json,created_at,updated_at FROM agent_runtime_tasks WHERE ((status='queued' AND available_at<=?) OR (status='running' AND lease_until<?)) AND attempts<max_attempts ORDER BY available_at,created_at LIMIT 1`
	if err = tx.QueryRowContext(ctx, db.Rebind(q), FormatTime(now), FormatTime(now)).Scan(&task.ID, &task.WorkspaceID, &task.Kind, &task.SubjectID, &task.Status, &task.AvailableAt, &task.Attempts, &task.MaxAttempts, &task.PayloadJSON, &task.CreatedAt, &task.UpdatedAt); err == sql.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	previousStatus := task.Status
	fence := id.New("fence")
	res, err := tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status='running',attempts=attempts+1,lease_owner=?,lease_until=?,fence_token=?,updated_at=? WHERE id=? AND ((status='queued' AND available_at<=?) OR (status='running' AND lease_until<?))`), workerID, leaseUntil, fence, Now(), task.ID, FormatTime(now), FormatTime(now))
	if err != nil {
		return nil, nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, nil, nil
	}
	task.Status = "running"
	task.Attempts++
	task.LeaseOwner = workerID
	task.LeaseUntil = leaseUntil
	task.FenceToken = fence
	var attempt *AgentRunAttempt
	if task.Kind == "run" || task.Kind == "eval" {
		if previousStatus == "running" {
			if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_run_attempts SET status='expired',error='attempt lease expired',finished_at=? WHERE run_id=? AND status='running'`), FormatTime(now), task.SubjectID); err != nil {
				return nil, nil, err
			}
		}
		aid := id.New(id.AgentAttempt)
		attempt = &AgentRunAttempt{ID: aid, RunID: task.SubjectID, AttemptNumber: task.Attempts, WorkerID: workerID, FenceToken: fence, Status: "running", LeaseUntil: leaseUntil, HeartbeatAt: Now(), UsageJSON: "{}", StartedAt: Now()}
		if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_run_attempts(id,run_id,attempt_number,worker_id,fence_token,status,lease_until,heartbeat_at,usage_json,started_at) VALUES(?,?,?,?,?,'running',?,?,?,?)`), aid, task.SubjectID, task.Attempts, workerID, fence, leaseUntil, attempt.HeartbeatAt, "{}", attempt.StartedAt); err != nil {
			return nil, nil, err
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runs SET status='running',started_at=COALESCE(started_at,?),updated_at=? WHERE id=?`), Now(), Now(), task.SubjectID); err != nil {
			return nil, nil, err
		}
		if task.Kind == "eval" {
			if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_evaluations SET status='running',updated_at=? WHERE critic_run_id=? AND status IN ('queued','budget_blocked')`), Now(), task.SubjectID); err != nil {
				return nil, nil, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &task, attempt, nil
}

func (db *DB) expireExhaustedAgentTask(ctx context.Context) error {
	now := Now()
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var taskID, kind, subjectID, fence string
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id,kind,subject_id,COALESCE(fence_token,'') FROM agent_runtime_tasks WHERE status='running' AND lease_until<? AND attempts>=max_attempts ORDER BY lease_until LIMIT 1`), now).Scan(&taskID, &kind, &subjectID, &fence)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status='expiring',updated_at=? WHERE id=? AND status='running' AND fence_token=?`), now, taskID, fence)
	if err != nil {
		return err
	}
	if changed, _ := res.RowsAffected(); changed != 1 {
		return nil
	}
	if kind == "build" {
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_build_operations SET status='failed',error='runtime attempts exhausted',updated_at=? WHERE id=?`), now, subjectID); err != nil {
			return err
		}
	} else {
		var agentID, period string
		var reservation int64
		if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT agent_id,budget_period_start,reservation_microusd FROM agent_runs WHERE id=?`), subjectID).Scan(&agentID, &period, &reservation); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_run_attempts SET status='expired',error='attempt lease expired',finished_at=? WHERE run_id=? AND fence_token=? AND status='running'`), now, subjectID, fence); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runs SET status='failed',terminal_error='runtime attempts exhausted',updated_at=?,finished_at=? WHERE id=?`), now, now, subjectID); err != nil {
			return err
		}
		if kind == "eval" {
			if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_evaluations SET status='failed',summary='runtime attempts exhausted',updated_at=? WHERE critic_run_id=? AND status!='completed'`), now, subjectID); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_budget_periods SET reserved_microusd=reserved_microusd-?,updated_at=? WHERE agent_id=? AND period_start=?`), reservation, now, agentID, period); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status='failed',last_error='runtime attempts exhausted',updated_at=? WHERE id=? AND status='expiring' AND fence_token=?`), now, taskID, fence); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ClaimAgentRuntimeTaskForRun(ctx context.Context, runID, workerID string, lease time.Duration) (*AgentRuntimeTask, *AgentRunAttempt, error) {
	now := time.Now().UTC()
	leaseUntil := FormatTime(now.Add(lease))
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var task AgentRuntimeTask
	err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id,workspace_id,kind,subject_id,status,available_at,attempts,max_attempts,payload_json,created_at,updated_at FROM agent_runtime_tasks WHERE subject_id=? AND status='queued' AND available_at<=? AND attempts<max_attempts`), runID, FormatTime(now)).Scan(&task.ID, &task.WorkspaceID, &task.Kind, &task.SubjectID, &task.Status, &task.AvailableAt, &task.Attempts, &task.MaxAttempts, &task.PayloadJSON, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return nil, nil, err
	}
	fence := id.New("fence")
	res, err := tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status='running',attempts=attempts+1,lease_owner=?,lease_until=?,fence_token=?,updated_at=? WHERE id=? AND status='queued'`), workerID, leaseUntil, fence, Now(), task.ID)
	if err != nil {
		return nil, nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, nil, ErrAgentFenceLost
	}
	task.Status = "running"
	task.Attempts++
	task.LeaseOwner = workerID
	task.LeaseUntil = leaseUntil
	task.FenceToken = fence
	attempt := &AgentRunAttempt{ID: id.New(id.AgentAttempt), RunID: runID, AttemptNumber: task.Attempts, WorkerID: workerID, FenceToken: fence, Status: "running", LeaseUntil: leaseUntil, HeartbeatAt: Now(), UsageJSON: "{}", StartedAt: Now()}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_run_attempts(id,run_id,attempt_number,worker_id,fence_token,status,lease_until,heartbeat_at,usage_json,started_at) VALUES(?,?,?,?,?,'running',?,?,?,?)`), attempt.ID, runID, attempt.AttemptNumber, workerID, fence, leaseUntil, attempt.HeartbeatAt, "{}", attempt.StartedAt); err != nil {
		return nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runs SET status='running',started_at=COALESCE(started_at,?),updated_at=? WHERE id=?`), Now(), Now(), runID); err != nil {
		return nil, nil, err
	}
	if task.Kind == "eval" {
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_evaluations SET status='running',updated_at=? WHERE critic_run_id=? AND status IN ('queued','budget_blocked')`), Now(), runID); err != nil {
			return nil, nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &task, attempt, nil
}

func (db *DB) AgentAttemptWorker(ctx context.Context, runID, fence string) (string, error) {
	var worker string
	err := db.QueryRow(ctx, `SELECT worker_id FROM agent_run_attempts WHERE run_id=? AND fence_token=? AND status='running'`, runID, fence).Scan(&worker)
	if err == sql.ErrNoRows {
		return "", ErrAgentFenceLost
	}
	return worker, err
}

func (db *DB) HeartbeatAgentAttempt(ctx context.Context, subjectID, fence string, lease time.Duration) (bool, error) {
	until := FormatTime(time.Now().Add(lease))
	res, err := db.Exec(ctx, `UPDATE agent_runtime_tasks SET lease_until=?,updated_at=? WHERE subject_id=? AND fence_token=? AND status='running'`, until, Now(), subjectID, fence)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return false, ErrAgentFenceLost
	}
	_, err = db.Exec(ctx, `UPDATE agent_run_attempts SET lease_until=?,heartbeat_at=? WHERE run_id=? AND fence_token=? AND status='running'`, until, Now(), subjectID, fence)
	if err != nil {
		return false, err
	}
	var cancelled int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runs WHERE id=? AND cancel_requested_at IS NOT NULL`, subjectID).Scan(&cancelled)
	return cancelled > 0, nil
}

func (db *DB) AppendAgentRunEvents(ctx context.Context, runID, fence string, events []AgentRunEvent) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attemptID string
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT id FROM agent_run_attempts WHERE run_id=? AND fence_token=? AND status='running' AND lease_until>?`), runID, fence, Now()).Scan(&attemptID); err == sql.ErrNoRows {
		return ErrAgentFenceLost
	}
	if err != nil {
		return err
	}
	for _, event := range events {
		var exists int
		if e := tx.QueryRowContext(ctx, db.Rebind(`SELECT COUNT(*) FROM agent_run_events WHERE attempt_id=? AND attempt_ordinal=?`), attemptID, event.AttemptOrdinal).Scan(&exists); e != nil {
			return e
		}
		if exists > 0 {
			continue
		}
		seq, e := db.nextSequenceTx(ctx, tx, "agent-run:"+runID)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_run_events(id,run_id,attempt_id,seq,attempt_ordinal,type,payload_json,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(attempt_id,attempt_ordinal) DO NOTHING`), id.New(id.Event), runID, attemptID, seq, event.AttemptOrdinal, event.Type, event.PayloadJSON, Now())
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (db *DB) CheckpointAgentUsage(ctx context.Context, runID, fence, usageJSON string) error {
	res, err := db.Exec(ctx, `UPDATE agent_run_attempts SET usage_json=?,heartbeat_at=? WHERE run_id=? AND fence_token=? AND status='running'`, usageJSON, Now(), runID, fence)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrAgentFenceLost
	}
	return nil
}

func (db *DB) CompleteAgentRun(ctx context.Context, runID, fence, outputJSON, usageJSON string, actualMicroUSD int64) error {
	return db.finishAgentRun(ctx, runID, fence, "completed", outputJSON, "", usageJSON, actualMicroUSD)
}
func (db *DB) FailAgentRun(ctx context.Context, runID, fence, errorText, usageJSON string, actualMicroUSD int64) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var agentID, period, cancelRequested string
	var reservation int64
	var attempts, maxAttempts int
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT r.agent_id,r.budget_period_start,r.reservation_microusd,t.attempts,t.max_attempts,COALESCE(r.cancel_requested_at,'') FROM agent_runs r JOIN agent_run_attempts a ON a.run_id=r.id JOIN agent_runtime_tasks t ON t.subject_id=r.id WHERE r.id=? AND a.fence_token=? AND a.status='running' AND t.fence_token=? AND t.status='running'`), runID, fence, fence).Scan(&agentID, &period, &reservation, &attempts, &maxAttempts, &cancelRequested); err == sql.ErrNoRows {
		return ErrAgentFenceLost
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	nowText := FormatTime(now)
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_run_attempts SET status='failed',usage_json=?,error=?,finished_at=? WHERE run_id=? AND fence_token=?`), usageJSON, errorText, nowText, runID, fence); err != nil {
		return err
	}
	if actualMicroUSD > 0 {
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_budget_periods SET spent_microusd=spent_microusd+?,updated_at=? WHERE agent_id=? AND period_start=?`), actualMicroUSD, nowText, agentID, period); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_budget_ledger(id,agent_id,run_id,period_start,kind,amount_microusd,usage_json,created_at) VALUES(?,?,?,?, 'failed_attempt',?,?,?)`), id.New("aledger"), agentID, runID, period, actualMicroUSD, usageJSON, nowText); err != nil {
			return err
		}
	}
	if attempts < maxAttempts && cancelRequested == "" {
		delay := 5 * time.Second
		if attempts > 1 {
			delay = 30 * time.Second
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runs SET status='queued',terminal_error=NULL,updated_at=? WHERE id=?`), nowText, runID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status='queued',available_at=?,lease_owner=NULL,lease_until=NULL,fence_token=NULL,last_error=?,updated_at=? WHERE subject_id=? AND fence_token=?`), FormatTime(now.Add(delay)), errorText, nowText, runID, fence); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_evaluations SET status='queued',updated_at=? WHERE critic_run_id=? AND status='running'`), nowText, runID); err != nil {
			return err
		}
		return tx.Commit()
	}
	terminalStatus := "failed"
	if cancelRequested != "" {
		terminalStatus = "cancelled"
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runs SET status=?,terminal_error=?,updated_at=?,finished_at=? WHERE id=?`), terminalStatus, nullStr(errorText), nowText, nowText, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status=?,lease_owner=NULL,lease_until=NULL,last_error=?,updated_at=? WHERE subject_id=? AND fence_token=?`), terminalStatus, errorText, nowText, runID, fence); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_budget_periods SET reserved_microusd=reserved_microusd-?,updated_at=? WHERE agent_id=? AND period_start=?`), reservation, nowText, agentID, period); err != nil {
		return err
	}
	var totalActual int64
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COALESCE(SUM(amount_microusd),0) FROM agent_budget_ledger WHERE run_id=? AND kind='failed_attempt'`), runID).Scan(&totalActual); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_budget_ledger(id,agent_id,run_id,period_start,kind,amount_microusd,usage_json,created_at) VALUES(?,?,?,?, 'settlement',?,?,?)`), id.New("aledger"), agentID, runID, period, totalActual, usageJSON, nowText); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) finishAgentRun(ctx context.Context, runID, fence, status, output, errorText, usage string, actual int64) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var agentID, period string
	var reservation int64
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT r.agent_id,r.budget_period_start,r.reservation_microusd FROM agent_runs r JOIN agent_run_attempts a ON a.run_id=r.id WHERE r.id=? AND a.fence_token=? AND a.status='running'`), runID, fence).Scan(&agentID, &period, &reservation); err == sql.ErrNoRows {
		return ErrAgentFenceLost
	}
	if err != nil {
		return err
	}
	now := Now()
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_run_attempts SET status=?,usage_json=?,error=?,finished_at=? WHERE run_id=? AND fence_token=?`), status, usage, nullStr(errorText), now, runID, fence); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runs SET status=?,output_json=?,terminal_error=?,updated_at=?,finished_at=? WHERE id=?`), status, nullStr(output), nullStr(errorText), now, now, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status=?,updated_at=? WHERE subject_id=? AND fence_token=?`), status, now, runID, fence); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_budget_periods SET reserved_microusd=reserved_microusd-?,spent_microusd=spent_microusd+?,updated_at=? WHERE agent_id=? AND period_start=?`), reservation, actual, now, agentID, period); err != nil {
		return err
	}
	var failedAttemptCost int64
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT COALESCE(SUM(amount_microusd),0) FROM agent_budget_ledger WHERE run_id=? AND kind='failed_attempt'`), runID).Scan(&failedAttemptCost); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_budget_ledger(id,agent_id,run_id,period_start,kind,amount_microusd,usage_json,created_at) VALUES(?,?,?,?, 'settlement',?,?,?)`), id.New("aledger"), agentID, runID, period, actual+failedAttemptCost, usage, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}
