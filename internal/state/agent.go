package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

var ErrAgentFenceLost = errors.New("agent runtime fence lost")

func DefaultAgentRateSnapshot() map[string]any {
	return map[string]any{
		"model":                             "gpt-5.6-terra",
		"version":                           "2026-07-20",
		"input_microusd_per_million":        1_250_000,
		"cached_input_microusd_per_million": 125_000,
		"output_microusd_per_million":       10_000_000,
	}
}

func (db *DB) CreateAgent(ctx context.Context, name, purpose, definitionJSON, provenanceJSON string, dailyLimit int64, timezone string) (*Agent, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if timezone == "" {
		timezone = "UTC"
	}
	if provenanceJSON == "" {
		provenanceJSON = "{}"
	}
	now := Now()
	a := &Agent{ID: id.New(id.Agent), WorkspaceID: ws.ID, Name: name, Purpose: purpose, State: "active", CurrentVersion: 1, CreatedAt: now, UpdatedAt: now}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agents(id,workspace_id,name,name_key,purpose,state,current_version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`), a.ID, a.WorkspaceID, a.Name, strings.ToLower(a.Name), a.Purpose, a.State, 1, now, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_versions(agent_id,version,definition_json,provenance_json,created_at) VALUES(?,?,?,?,?)`), a.ID, 1, definitionJSON, provenanceJSON, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_budget_policies(agent_id,daily_limit_microusd,timezone,updated_at) VALUES(?,?,?,?)`), a.ID, dailyLimit, timezone, now); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return a, nil
}

func (db *DB) GetAgent(ctx context.Context, ref string) (*Agent, error) {
	var a Agent
	err := db.QueryRow(ctx, `SELECT id,workspace_id,name,purpose,state,current_version,created_at,updated_at FROM agents WHERE id=? OR name_key=?`, ref, strings.ToLower(ref)).Scan(&a.ID, &a.WorkspaceID, &a.Name, &a.Purpose, &a.State, &a.CurrentVersion, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found: %s", ref)
	}
	return &a, err
}

func (db *DB) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := db.Query(ctx, `SELECT id,workspace_id,name,purpose,state,current_version,created_at,updated_at FROM agents ORDER BY name_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		var a Agent
		if err = rows.Scan(&a.ID, &a.WorkspaceID, &a.Name, &a.Purpose, &a.State, &a.CurrentVersion, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) ListActiveAgents(ctx context.Context) ([]Agent, error) {
	rows, err := db.Query(ctx, `SELECT id,workspace_id,name,purpose,state,current_version,created_at,updated_at FROM agents WHERE state='active' ORDER BY name_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		var agent Agent
		if err = rows.Scan(&agent.ID, &agent.WorkspaceID, &agent.Name, &agent.Purpose, &agent.State, &agent.CurrentVersion, &agent.CreatedAt, &agent.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, agent)
	}
	return out, rows.Err()
}

func (db *DB) GetAgentVersion(ctx context.Context, agentID string, version int) (*AgentVersion, error) {
	var v AgentVersion
	err := db.QueryRow(ctx, `SELECT agent_id,version,definition_json,provenance_json,created_at FROM agent_versions WHERE agent_id=? AND version=?`, agentID, version).Scan(&v.AgentID, &v.Version, &v.DefinitionJSON, &v.ProvenanceJSON, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent version not found")
	}
	return &v, err
}

func (db *DB) ListAgentVersions(ctx context.Context, agentID string) ([]AgentVersion, error) {
	rows, err := db.Query(ctx, `SELECT agent_id,version,definition_json,provenance_json,created_at FROM agent_versions WHERE agent_id=? ORDER BY version DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentVersion
	for rows.Next() {
		var version AgentVersion
		if err = rows.Scan(&version.AgentID, &version.Version, &version.DefinitionJSON, &version.ProvenanceJSON, &version.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, version)
	}
	return out, rows.Err()
}

func (db *DB) AddAgentVersion(ctx context.Context, agentID, purpose, definitionJSON, provenanceJSON string) (*AgentVersion, error) {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var next int
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT current_version+1 FROM agents WHERE id=?`), agentID).Scan(&next); err != nil {
		return nil, err
	}
	now := Now()
	if _, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_versions(agent_id,version,definition_json,provenance_json,created_at) VALUES(?,?,?,?,?)`), agentID, next, definitionJSON, provenanceJSON, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agents SET current_version=?,purpose=?,updated_at=? WHERE id=?`), next, purpose, now, agentID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &AgentVersion{AgentID: agentID, Version: next, DefinitionJSON: definitionJSON, ProvenanceJSON: provenanceJSON, CreatedAt: now}, nil
}

func (db *DB) SetAgentBudget(ctx context.Context, agentID string, limit int64, timezone string) error {
	if limit <= 0 {
		return fmt.Errorf("budget must be positive")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}
	_, err := db.Exec(ctx, `INSERT INTO agent_budget_policies(agent_id,daily_limit_microusd,timezone,updated_at) VALUES(?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET daily_limit_microusd=excluded.daily_limit_microusd,timezone=excluded.timezone,updated_at=excluded.updated_at`, agentID, limit, timezone, Now())
	if err == nil {
		_, err = db.Exec(ctx, `UPDATE agent_budget_periods SET limit_microusd=?,updated_at=? WHERE agent_id=? AND period_end>?`, limit, Now(), agentID, Now())
	}
	return err
}

func (db *DB) SetAgentSchedule(ctx context.Context, agentID, cron, timezone, nextDue string, enabled bool) error {
	value := 0
	if enabled {
		value = 1
	}
	_, err := db.Exec(ctx, `INSERT INTO agent_schedules(agent_id,enabled,cron,timezone,next_due_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET enabled=excluded.enabled,cron=excluded.cron,timezone=excluded.timezone,next_due_at=excluded.next_due_at,updated_at=excluded.updated_at`, agentID, value, cron, timezone, nullStr(nextDue), Now())
	return err
}

func (db *DB) DisableAgentSchedule(ctx context.Context, agentID string) error {
	_, err := db.Exec(ctx, `UPDATE agent_schedules SET enabled=0,updated_at=? WHERE agent_id=?`, Now(), agentID)
	return err
}

func (db *DB) GetAgentSchedule(ctx context.Context, agentID string) (*AgentSchedule, error) {
	var out AgentSchedule
	var enabled int
	var next, last sql.NullString
	err := db.QueryRow(ctx, `SELECT agent_id,enabled,cron,timezone,next_due_at,last_due_at,updated_at FROM agent_schedules WHERE agent_id=?`, agentID).Scan(&out.AgentID, &enabled, &out.Cron, &out.Timezone, &next, &last, &out.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	out.Enabled = enabled != 0
	out.NextDueAt = next.String
	out.LastDueAt = last.String
	return &out, err
}

func (db *DB) DueAgentSchedules(ctx context.Context, now string) ([]AgentSchedule, error) {
	rows, err := db.Query(ctx, `SELECT agent_id,enabled,cron,timezone,COALESCE(next_due_at,''),COALESCE(last_due_at,''),updated_at FROM agent_schedules WHERE enabled=1 AND next_due_at<=? ORDER BY next_due_at`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentSchedule
	for rows.Next() {
		var s AgentSchedule
		var enabled int
		if err = rows.Scan(&s.AgentID, &enabled, &s.Cron, &s.Timezone, &s.NextDueAt, &s.LastDueAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.Enabled = enabled != 0
		out = append(out, s)
	}
	return out, rows.Err()
}

func (db *DB) AdvanceAgentSchedule(ctx context.Context, agentID, due, next string) error {
	_, err := db.Exec(ctx, `UPDATE agent_schedules SET last_due_at=?,next_due_at=?,updated_at=? WHERE agent_id=? AND next_due_at=?`, due, next, Now(), agentID, due)
	return err
}

func (db *DB) AgentBudget(ctx context.Context, agentID string) (limit, reserved, spent int64, timezone, periodStart, periodEnd string, err error) {
	if err = db.QueryRow(ctx, `SELECT daily_limit_microusd,timezone FROM agent_budget_policies WHERE agent_id=?`, agentID).Scan(&limit, &timezone); err != nil {
		return
	}
	start, end, e := budgetPeriod(time.Now().UTC(), timezone)
	if e != nil {
		err = e
		return
	}
	periodStart, periodEnd = start.Format(time.RFC3339), end.Format(time.RFC3339)
	_, err = db.Exec(ctx, `INSERT INTO agent_budget_periods(agent_id,period_start,period_end,limit_microusd,reserved_microusd,spent_microusd,updated_at) VALUES(?,?,?,?,0,0,?) ON CONFLICT(agent_id,period_start) DO NOTHING`, agentID, periodStart, periodEnd, limit, Now())
	if err != nil {
		return
	}
	err = db.QueryRow(ctx, `SELECT limit_microusd,reserved_microusd,spent_microusd FROM agent_budget_periods WHERE agent_id=? AND period_start=?`, agentID, periodStart).Scan(&limit, &reserved, &spent)
	return
}

func budgetPeriod(now time.Time, timezone string) (time.Time, time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return start.UTC(), start.AddDate(0, 0, 1).UTC(), nil
}

func (db *DB) reservationEstimate(ctx context.Context, agentID string, limit int64) int64 {
	var avg sql.NullFloat64
	_ = db.QueryRow(ctx, `SELECT AVG(amount_microusd) FROM (SELECT amount_microusd FROM agent_budget_ledger WHERE agent_id=? AND kind='settlement' ORDER BY created_at DESC LIMIT 10) recent`, agentID).Scan(&avg)
	v := int64(1_000_000)
	if avg.Valid {
		v = int64(avg.Float64)
	}
	if v < 100_000 {
		v = 100_000
	}
	if v > limit {
		v = limit
	}
	return v
}

func (db *DB) CreateAgentRun(ctx context.Context, agentID, trigger, inputJSON, repositoryID, parentRunID string, rateSnapshot any) (*AgentRun, error) {
	a, err := db.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if a.State != "active" {
		return nil, fmt.Errorf("agent is %s", a.State)
	}
	limit, _, _, _, periodStart, _, err := db.AgentBudget(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	reserve := db.reservationEstimate(ctx, a.ID, limit)
	now := Now()
	rid := id.New(id.AgentRun)
	root := rid
	depth := 0
	if parentRunID != "" {
		parent, pe := db.GetAgentRun(ctx, parentRunID)
		if pe != nil {
			return nil, pe
		}
		if parent.NestingDepth >= 3 {
			return nil, fmt.Errorf("agent nesting limit exceeded")
		}
		root = parent.RootRunID
		depth = parent.NestingDepth + 1
	}
	if rateSnapshot == nil {
		rateSnapshot = DefaultAgentRateSnapshot()
	}
	rates, _ := json.Marshal(rateSnapshot)
	if len(rates) == 0 || string(rates) == "null" {
		rates = []byte("{}")
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, db.Rebind(`UPDATE agent_budget_periods SET reserved_microusd=reserved_microusd+?,updated_at=? WHERE agent_id=? AND period_start=? AND limit_microusd-spent_microusd-reserved_microusd>=?`), reserve, now, a.ID, periodStart, reserve)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	status := "queued"
	if n == 0 {
		status = "budget_blocked"
		reserve = 0
	}
	r := &AgentRun{ID: rid, WorkspaceID: a.WorkspaceID, AgentID: a.ID, DefinitionVersion: a.CurrentVersion, Trigger: trigger, InputJSON: inputJSON, OriginatingRepositoryID: repositoryID, ParentRunID: parentRunID, RootRunID: root, NestingDepth: depth, Status: status, BudgetPeriodStart: periodStart, ReservationMicroUSD: reserve, RateSnapshotJSON: string(rates), ResolvedKnowledgeJSON: "[]", CreatedAt: now, UpdatedAt: now}
	_, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_runs(id,workspace_id,agent_id,definition_version,trigger,input_json,originating_repository_id,parent_run_id,root_run_id,nesting_depth,status,budget_period_start,reservation_microusd,rate_snapshot_json,resolved_knowledge_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`), r.ID, r.WorkspaceID, r.AgentID, r.DefinitionVersion, r.Trigger, r.InputJSON, nullStr(repositoryID), nullStr(parentRunID), r.RootRunID, r.NestingDepth, r.Status, r.BudgetPeriodStart, r.ReservationMicroUSD, r.RateSnapshotJSON, r.ResolvedKnowledgeJSON, now, now)
	if err != nil {
		return nil, err
	}
	if status == "queued" {
		taskKind := "run"
		if trigger == "eval" {
			taskKind = "eval"
		}
		_, err = insertAgentTask(ctx, db, tx, a.WorkspaceID, taskKind, r.ID, "{}", now)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r, nil
}

func insertAgentTask(ctx context.Context, db *DB, tx *sql.Tx, workspaceID, kind, subjectID, payload, available string) (string, error) {
	tid := id.New(id.AgentTask)
	_, err := tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_runtime_tasks(id,workspace_id,kind,subject_id,status,available_at,payload_json,created_at,updated_at) VALUES(?,?,?,?, 'queued',?,?,?,?) ON CONFLICT(kind,subject_id) DO NOTHING`), tid, workspaceID, kind, subjectID, available, payload, Now(), Now())
	return tid, err
}

func (db *DB) GetAgentRun(ctx context.Context, runID string) (*AgentRun, error) {
	var r AgentRun
	var repo, parent, out, terminal, cancel, started, finished sql.NullString
	err := db.QueryRow(ctx, `SELECT id,workspace_id,agent_id,definition_version,trigger,input_json,originating_repository_id,parent_run_id,root_run_id,nesting_depth,status,budget_period_start,reservation_microusd,rate_snapshot_json,resolved_knowledge_json,output_json,terminal_error,cancel_requested_at,created_at,updated_at,started_at,finished_at FROM agent_runs WHERE id=?`, runID).Scan(&r.ID, &r.WorkspaceID, &r.AgentID, &r.DefinitionVersion, &r.Trigger, &r.InputJSON, &repo, &parent, &r.RootRunID, &r.NestingDepth, &r.Status, &r.BudgetPeriodStart, &r.ReservationMicroUSD, &r.RateSnapshotJSON, &r.ResolvedKnowledgeJSON, &out, &terminal, &cancel, &r.CreatedAt, &r.UpdatedAt, &started, &finished)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent run not found: %s", runID)
	}
	r.OriginatingRepositoryID = repo.String
	r.ParentRunID = parent.String
	r.OutputJSON = out.String
	r.TerminalError = terminal.String
	r.CancelRequestedAt = cancel.String
	r.StartedAt = started.String
	r.FinishedAt = finished.String
	return &r, err
}

func (db *DB) SetAgentRunResolvedKnowledge(ctx context.Context, runID, resolvedJSON string) error {
	_, err := db.Exec(ctx, `UPDATE agent_runs SET resolved_knowledge_json=?,updated_at=? WHERE id=? AND status IN ('queued','budget_blocked')`, resolvedJSON, Now(), runID)
	return err
}

func (db *DB) ListAgentRuns(ctx context.Context, agentID string) ([]AgentRun, error) {
	q := `SELECT id FROM agent_runs`
	args := []any{}
	if agentID != "" {
		q += ` WHERE agent_id=?`
		args = append(args, agentID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentRun
	for rows.Next() {
		var rid string
		if err = rows.Scan(&rid); err != nil {
			return nil, err
		}
		r, e := db.GetAgentRun(ctx, rid)
		if e != nil {
			return nil, e
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (db *DB) HasActiveAgentRun(ctx context.Context, agentID, trigger string) (bool, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM agent_runs WHERE agent_id=? AND trigger=? AND status IN ('queued','budget_blocked','running')`, agentID, trigger).Scan(&n)
	return n > 0, err
}

func (db *DB) CancelAgentRun(ctx context.Context, runID string) error {
	r, err := db.GetAgentRun(ctx, runID)
	if err != nil {
		return err
	}
	now := Now()
	if r.Status == "running" {
		_, err = db.Exec(ctx, `UPDATE agent_runs SET cancel_requested_at=?,updated_at=? WHERE id=?`, now, now, runID)
		return err
	}
	if r.Status != "queued" && r.Status != "budget_blocked" {
		return fmt.Errorf("agent run cannot be cancelled from %s", r.Status)
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if r.ReservationMicroUSD > 0 {
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_budget_periods SET reserved_microusd=reserved_microusd-?,updated_at=? WHERE agent_id=? AND period_start=?`), r.ReservationMicroUSD, now, r.AgentID, r.BudgetPeriodStart); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runs SET status='cancelled',cancel_requested_at=?,updated_at=?,finished_at=? WHERE id=?`), now, now, now, runID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status='cancelled',updated_at=? WHERE kind IN ('run','eval') AND subject_id=?`), now, runID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ReleaseBudgetBlockedRuns(ctx context.Context, agentID string) (int, error) {
	rows, err := db.Query(ctx, `SELECT id FROM agent_runs WHERE agent_id=? AND status='budget_blocked' ORDER BY created_at`, agentID)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var rid string
		if err = rows.Scan(&rid); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, rid)
	}
	rows.Close()
	count := 0
	for _, rid := range ids {
		r, err := db.GetAgentRun(ctx, rid)
		if err != nil {
			return count, err
		}
		limit, _, _, _, period, _, err := db.AgentBudget(ctx, agentID)
		if err != nil {
			return count, err
		}
		reserve := db.reservationEstimate(ctx, agentID, limit)
		tx, err := db.SQL.BeginTx(ctx, nil)
		if err != nil {
			return count, err
		}
		res, err := tx.ExecContext(ctx, db.Rebind(`UPDATE agent_budget_periods SET reserved_microusd=reserved_microusd+?,updated_at=? WHERE agent_id=? AND period_start=? AND limit_microusd-spent_microusd-reserved_microusd>=?`), reserve, Now(), agentID, period, reserve)
		if err != nil {
			tx.Rollback()
			return count, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			tx.Rollback()
			break
		}
		if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runs SET status='queued',budget_period_start=?,reservation_microusd=?,updated_at=? WHERE id=? AND status='budget_blocked'`), period, reserve, Now(), rid); err != nil {
			tx.Rollback()
			return count, err
		}
		taskKind := "run"
		if r.Trigger == "eval" {
			taskKind = "eval"
		}
		if _, err = insertAgentTask(ctx, db, tx, r.WorkspaceID, taskKind, rid, "{}", Now()); err != nil {
			tx.Rollback()
			return count, err
		}
		if taskKind == "eval" {
			if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_evaluations SET status='queued',updated_at=? WHERE critic_run_id=? AND status='budget_blocked'`), Now(), rid); err != nil {
				tx.Rollback()
				return count, err
			}
		}
		if err = tx.Commit(); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
