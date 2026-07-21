package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

type AgentEvaluation struct {
	ID             string  `json:"id"`
	EvaluatedRunID string  `json:"evaluated_run_id"`
	CriticAgentID  string  `json:"critic_agent_id"`
	CriticRunID    string  `json:"critic_run_id,omitempty"`
	Status         string  `json:"status"`
	Score          float64 `json:"score"`
	Passed         bool    `json:"passed"`
	Summary        string  `json:"summary,omitempty"`
	FindingsJSON   string  `json:"findings_json"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func (db *DB) QueueAgentEvaluation(ctx context.Context, evaluatedRunID string) (*AgentEvaluation, error) {
	run, err := db.GetAgentRun(ctx, evaluatedRunID)
	if err != nil {
		return nil, err
	}
	if run.Trigger == "eval" {
		return nil, nil
	}
	version, err := db.GetAgentVersion(ctx, run.AgentID, run.DefinitionVersion)
	if err != nil {
		return nil, err
	}
	var definition struct {
		CriticAgentID string `json:"eval_critic_agent_id"`
	}
	if err = json.Unmarshal([]byte(version.DefinitionJSON), &definition); err != nil || definition.CriticAgentID == "" {
		return nil, err
	}
	now := Now()
	evaluation := &AgentEvaluation{ID: id.New(id.AgentEvaluation), EvaluatedRunID: run.ID, CriticAgentID: definition.CriticAgentID, Status: "pending", FindingsJSON: "[]", CreatedAt: now, UpdatedAt: now}
	_, err = db.Exec(ctx, `INSERT INTO agent_evaluations(id,evaluated_run_id,critic_agent_id,status,findings_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(evaluated_run_id) DO NOTHING`, evaluation.ID, run.ID, definition.CriticAgentID, "pending", "[]", now, now)
	if err != nil {
		return nil, err
	}
	input, _ := json.Marshal(map[string]any{"prompt": "Evaluate the supplied agent output for correctness and fitness for purpose.", "evaluated_run_id": run.ID, "evaluated_output": json.RawMessage(run.OutputJSON)})
	criticRun, err := db.CreateAgentRun(ctx, definition.CriticAgentID, "eval", string(input), run.OriginatingRepositoryID, "", nil)
	if err != nil {
		return evaluation, nil
	}
	evaluation.CriticRunID = criticRun.ID
	_, err = db.Exec(ctx, `UPDATE agent_evaluations SET critic_run_id=?,status=?,updated_at=? WHERE evaluated_run_id=?`, criticRun.ID, map[bool]string{true: "budget_blocked", false: "queued"}[criticRun.Status == "budget_blocked"], Now(), run.ID)
	return evaluation, err
}

func (db *DB) CompleteAgentEvaluation(ctx context.Context, criticRunID, outputJSON string) error {
	var evaluatedRunID, agentID string
	err := db.QueryRow(ctx, `SELECT e.evaluated_run_id,r.agent_id FROM agent_evaluations e JOIN agent_runs r ON r.id=e.evaluated_run_id WHERE e.critic_run_id=?`, criticRunID).Scan(&evaluatedRunID, &agentID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var result struct {
		Score    float64          `json:"score"`
		Passed   bool             `json:"passed"`
		Summary  string           `json:"summary"`
		Findings []map[string]any `json:"findings"`
	}
	if err = json.Unmarshal([]byte(outputJSON), &result); err != nil {
		return fmt.Errorf("invalid critic output: %w", err)
	}
	if result.Score < 0 || result.Score > 1 {
		return fmt.Errorf("critic score out of range")
	}
	findings, _ := json.Marshal(result.Findings)
	passed := 0
	if result.Passed {
		passed = 1
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := Now()
	if _, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_evaluations SET status='completed',score=?,passed=?,summary=?,findings_json=?,updated_at=? WHERE critic_run_id=?`), result.Score, passed, result.Summary, string(findings), now, criticRunID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO agent_eval_stats(agent_id,evaluation_count,cumulative_score,mean_score,pass_count,pass_rate,updated_at) VALUES(?,1,?,?,?, ?,?) ON CONFLICT(agent_id) DO UPDATE SET evaluation_count=agent_eval_stats.evaluation_count+1,cumulative_score=agent_eval_stats.cumulative_score+excluded.cumulative_score,mean_score=(agent_eval_stats.cumulative_score+excluded.cumulative_score)/(agent_eval_stats.evaluation_count+1),pass_count=agent_eval_stats.pass_count+excluded.pass_count,pass_rate=(agent_eval_stats.pass_count+excluded.pass_count)*1.0/(agent_eval_stats.evaluation_count+1),updated_at=excluded.updated_at`), agentID, result.Score, result.Score, passed, float64(passed), now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) FailAgentEvaluation(ctx context.Context, criticRunID, errorText string) error {
	_, err := db.Exec(ctx, `UPDATE agent_evaluations SET status='failed',summary=?,updated_at=? WHERE critic_run_id=? AND status!='completed'`, errorText, Now(), criticRunID)
	return err
}

func (db *DB) ListAgentEvaluations(ctx context.Context, agentID string) ([]AgentEvaluation, error) {
	rows, err := db.Query(ctx, `SELECT e.id,e.evaluated_run_id,e.critic_agent_id,COALESCE(e.critic_run_id,''),e.status,COALESCE(e.score,0),COALESCE(e.passed,0),COALESCE(e.summary,''),e.findings_json,e.created_at,e.updated_at FROM agent_evaluations e JOIN agent_runs r ON r.id=e.evaluated_run_id WHERE r.agent_id=? ORDER BY e.created_at DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentEvaluation
	for rows.Next() {
		var e AgentEvaluation
		var passed int
		if err = rows.Scan(&e.ID, &e.EvaluatedRunID, &e.CriticAgentID, &e.CriticRunID, &e.Status, &e.Score, &passed, &e.Summary, &e.FindingsJSON, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Passed = passed != 0
		out = append(out, e)
	}
	return out, rows.Err()
}
