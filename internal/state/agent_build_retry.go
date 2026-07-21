package state

import (
	"context"
	"database/sql"
	"time"
)

func (db *DB) FailAgentRuntimeTask(ctx context.Context, subjectID, fence, errorText string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attempts, maxAttempts int
	if err = tx.QueryRowContext(ctx, db.Rebind(`SELECT attempts,max_attempts FROM agent_runtime_tasks WHERE subject_id=? AND fence_token=? AND status='running'`), subjectID, fence).Scan(&attempts, &maxAttempts); err == sql.ErrNoRows {
		return ErrAgentFenceLost
	}
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if attempts < maxAttempts {
		delay := 5 * time.Second
		if attempts > 1 {
			delay = 30 * time.Second
		}
		_, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status='queued',available_at=?,lease_owner=NULL,lease_until=NULL,fence_token=NULL,last_error=?,updated_at=? WHERE subject_id=? AND fence_token=?`), FormatTime(now.Add(delay)), errorText, FormatTime(now), subjectID, fence)
		if err == nil {
			_, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_build_operations SET status='queued',error=?,updated_at=? WHERE id=?`), errorText, FormatTime(now), subjectID)
		}
	} else {
		_, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_runtime_tasks SET status='failed',last_error=?,lease_owner=NULL,lease_until=NULL,updated_at=? WHERE subject_id=? AND fence_token=?`), errorText, FormatTime(now), subjectID, fence)
		if err == nil {
			_, err = tx.ExecContext(ctx, db.Rebind(`UPDATE agent_build_operations SET status='failed',error=?,updated_at=? WHERE id=?`), errorText, FormatTime(now), subjectID)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
