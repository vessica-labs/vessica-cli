package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) UpsertAgentTaskCheckpoint(ctx context.Context, in AgentTaskCheckpointInput) (*AgentTaskCheckpoint, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	if in.AgentID == "" || in.AgentRunID == "" || in.CheckpointKey == "" || in.StateJSON == "" {
		return nil, fmt.Errorf("agent checkpoint fields are required")
	}
	if e = db.requireWorkspaceRecord(ctx, "agents", in.AgentID); e != nil {
		return nil, e
	}
	if e = db.requireAgentRunForAgent(ctx, in.AgentID, in.AgentRunID); e != nil {
		return nil, e
	}
	if in.Status == "" {
		in.Status = "saved"
	}
	now := Now()
	_, e = db.Exec(ctx, `INSERT INTO agent_task_checkpoints(id,workspace_id,agent_id,agent_run_id,checkpoint_key,state_json,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,agent_run_id,checkpoint_key) DO UPDATE SET state_json=excluded.state_json,status=excluded.status,updated_at=excluded.updated_at`, id.New("atcp"), ws.ID, in.AgentID, in.AgentRunID, in.CheckpointKey, in.StateJSON, in.Status, now, now)
	if e != nil {
		return nil, e
	}
	var v AgentTaskCheckpoint
	e = db.QueryRow(ctx, `SELECT id,workspace_id,agent_id,agent_run_id,checkpoint_key,state_json,status,created_at,updated_at FROM agent_task_checkpoints WHERE workspace_id=? AND agent_run_id=? AND checkpoint_key=?`, ws.ID, in.AgentRunID, in.CheckpointKey).Scan(&v.ID, &v.WorkspaceID, &v.AgentID, &v.AgentRunID, &v.CheckpointKey, &v.StateJSON, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return &v, e
}

func (db *DB) TriggerAgentRun(ctx context.Context, in AgentRunTriggerInput) (*AgentRunTrigger, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.AgentID == "" || in.IdempotencyKey == "" || in.Trigger == "" {
		return nil, fmt.Errorf("agent trigger id, trigger, and idempotency key are required")
	}
	if in.InputJSON == "" {
		in.InputJSON = "{}"
	}
	if err = db.requireWorkspaceRecord(ctx, "agents", in.AgentID); err != nil {
		return nil, err
	}
	if in.RateSnapshot == nil {
		in.RateSnapshot = DefaultAgentRateSnapshot()
	}
	rates, _ := json.Marshal(in.RateSnapshot)
	if len(rates) == 0 || string(rates) == "null" {
		rates, _ = json.Marshal(DefaultAgentRateSnapshot())
	}
	now := Now()
	trigger := &AgentRunTrigger{ID: id.New("atrigger"), WorkspaceID: ws.ID, AgentID: in.AgentID, IdempotencyKey: in.IdempotencyKey, Trigger: in.Trigger, InputJSON: in.InputJSON, RepositoryID: in.RepositoryID, ParentRunID: in.ParentRunID, RateSnapshotJSON: string(rates), State: "accepted", CreatedAt: now, UpdatedAt: now}
	_, err = db.Exec(ctx, `INSERT INTO agent_run_triggers(id,workspace_id,agent_id,idempotency_key,trigger,input_json,repository_id,parent_run_id,rate_snapshot_json,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(workspace_id,agent_id,idempotency_key) DO NOTHING`, trigger.ID, trigger.WorkspaceID, trigger.AgentID, trigger.IdempotencyKey, trigger.Trigger, trigger.InputJSON, nullStr(trigger.RepositoryID), nullStr(trigger.ParentRunID), trigger.RateSnapshotJSON, trigger.State, now, now)
	if err != nil {
		return nil, err
	}
	existing, err := db.getAgentRunTrigger(ctx, in.AgentID, in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing.AgentRunID != "" {
		return existing, nil
	}
	if run, e := db.findAgentRunForTrigger(ctx, existing.ID); e == nil {
		_, err = db.Exec(ctx, `UPDATE agent_run_triggers SET agent_run_id=?,state='created',claim_token=NULL,lease_until=NULL,updated_at=? WHERE id=? AND workspace_id=? AND agent_run_id IS NULL`, run.ID, Now(), existing.ID, ws.ID)
		if err != nil {
			return nil, err
		}
		return db.getAgentRunTrigger(ctx, in.AgentID, in.IdempotencyKey)
	}
	claimToken := id.New("triggerclaim")
	claim, err := db.Exec(ctx, `UPDATE agent_run_triggers SET state='creating',claim_token=?,lease_until=?,updated_at=? WHERE id=? AND workspace_id=? AND agent_run_id IS NULL AND (state='accepted' OR (state='creating' AND lease_until<?))`, claimToken, FormatTime(time.Now().Add(time.Minute)), Now(), existing.ID, ws.ID, Now())
	if err != nil {
		return nil, err
	}
	changed, _ := claim.RowsAffected()
	if changed == 0 {
		return db.getAgentRunTrigger(ctx, in.AgentID, in.IdempotencyKey)
	}
	var persisted any
	_ = json.Unmarshal([]byte(existing.RateSnapshotJSON), &persisted)
	run, err := db.CreateAgentRunForTrigger(ctx, existing.AgentID, existing.Trigger, existing.InputJSON, existing.RepositoryID, existing.ParentRunID, persisted, existing.ID)
	if err != nil {
		if recovered, e := db.findAgentRunForTrigger(ctx, existing.ID); e == nil {
			run = recovered
		} else {
			_, _ = db.Exec(ctx, `UPDATE agent_run_triggers SET state='accepted',claim_token=NULL,lease_until=NULL,updated_at=? WHERE id=? AND claim_token=?`, Now(), existing.ID, claimToken)
			return nil, err
		}
	}
	_, err = db.Exec(ctx, `UPDATE agent_run_triggers SET agent_run_id=?,state='created',claim_token=NULL,lease_until=NULL,updated_at=? WHERE id=? AND workspace_id=? AND claim_token=? AND agent_run_id IS NULL`, run.ID, Now(), existing.ID, ws.ID, claimToken)
	if err != nil {
		return nil, err
	}
	return db.getAgentRunTrigger(ctx, in.AgentID, in.IdempotencyKey)
}

func (db *DB) getAgentRunTrigger(ctx context.Context, agentID, key string) (*AgentRunTrigger, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	var v AgentRunTrigger
	var repo, parent, run, claim, lease sql.NullString
	e = db.QueryRow(ctx, `SELECT id,workspace_id,agent_id,idempotency_key,trigger,input_json,repository_id,parent_run_id,agent_run_id,state,claim_token,lease_until,rate_snapshot_json,created_at,updated_at FROM agent_run_triggers WHERE workspace_id=? AND agent_id=? AND idempotency_key=?`, ws.ID, agentID, key).Scan(&v.ID, &v.WorkspaceID, &v.AgentID, &v.IdempotencyKey, &v.Trigger, &v.InputJSON, &repo, &parent, &run, &v.State, &claim, &lease, &v.RateSnapshotJSON, &v.CreatedAt, &v.UpdatedAt)
	if e == sql.ErrNoRows {
		return nil, fmt.Errorf("agent trigger not found")
	}
	v.RepositoryID = repo.String
	v.ParentRunID = parent.String
	v.AgentRunID = run.String
	v.ClaimToken = claim.String
	v.LeaseUntil = lease.String
	return &v, e
}
func (db *DB) findAgentRunForTrigger(ctx context.Context, triggerID string) (*AgentRun, error) {
	ws, e := db.GetWorkspace(ctx)
	if e != nil {
		return nil, e
	}
	var runID string
	e = db.QueryRow(ctx, `SELECT id FROM agent_runs WHERE workspace_id=? AND trigger_id=?`, ws.ID, triggerID).Scan(&runID)
	if e != nil {
		return nil, e
	}
	return db.GetAgentRun(ctx, runID)
}
