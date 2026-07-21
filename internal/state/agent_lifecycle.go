package state

import (
	"context"
	"fmt"
)

func (db *DB) SetAgentState(ctx context.Context, agentID, state string) error {
	if state != "active" && state != "paused" && state != "archived" {
		return fmt.Errorf("invalid agent state")
	}
	current, err := db.GetAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if current.State == "archived" && state != "archived" {
		return fmt.Errorf("archived agents cannot be resumed")
	}
	res, err := db.Exec(ctx, `UPDATE agents SET state=?,updated_at=? WHERE id=?`, state, Now(), agentID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent not found")
	}
	return nil
}
