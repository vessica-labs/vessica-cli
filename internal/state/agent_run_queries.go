package state

import "context"

func (db *DB) ListAgentRunEvents(ctx context.Context, runID string, after int64) ([]AgentRunEvent, error) {
	rows, err := db.Query(ctx, `SELECT id,run_id,attempt_id,seq,attempt_ordinal,type,payload_json,created_at FROM agent_run_events WHERE run_id=? AND seq>? ORDER BY seq`, runID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentRunEvent
	for rows.Next() {
		var event AgentRunEvent
		if err = rows.Scan(&event.ID, &event.RunID, &event.AttemptID, &event.Seq, &event.AttemptOrdinal, &event.Type, &event.PayloadJSON, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (db *DB) ListAgentRunAttempts(ctx context.Context, runID string) ([]AgentRunAttempt, error) {
	rows, err := db.Query(ctx, `SELECT id,run_id,attempt_number,worker_id,status,lease_until,heartbeat_at,usage_json,COALESCE(error,''),started_at,COALESCE(finished_at,'') FROM agent_run_attempts WHERE run_id=? ORDER BY attempt_number`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentRunAttempt
	for rows.Next() {
		var attempt AgentRunAttempt
		if err = rows.Scan(&attempt.ID, &attempt.RunID, &attempt.AttemptNumber, &attempt.WorkerID, &attempt.Status, &attempt.LeaseUntil, &attempt.HeartbeatAt, &attempt.UsageJSON, &attempt.Error, &attempt.StartedAt, &attempt.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, rows.Err()
}
