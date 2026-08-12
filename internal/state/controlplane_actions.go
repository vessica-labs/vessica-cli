package state

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
)

func actionClaimHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ClaimActionExecution durably records an allowed invocation before its side
// effect and elects one lease owner. Failed or expired claims may be retried;
// completed claims replay their recorded result.
func (db *DB) ClaimActionExecution(ctx context.Context, in ActionLedgerInput, lease time.Duration) (*ActionExecutionClaim, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if in.ActorID == "" || in.Tool == "" || in.IdempotencyKey == "" || len(in.IdempotencyKey) > 128 {
		return nil, fmt.Errorf("bounded action actor, tool, and idempotency key are required")
	}
	if lease <= 0 {
		lease = time.Minute
	}
	claimToken := id.New("mcpclaim")
	claimHash := actionClaimHash(claimToken)
	now := Now()
	leaseUntil := FormatTime(time.Now().Add(lease))
	result, err := db.Exec(ctx, `INSERT INTO action_ledger(id,workspace_id,actor_id,agent_id,agent_run_id,tool,policy_decision,redacted_arguments_json,result_json,latency_ms,idempotency_key,external_ids_json,created_at,execution_state,claim_token_hash,lease_until,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'claimed',?,?,?) ON CONFLICT(workspace_id,idempotency_key) DO NOTHING`, id.New("act"), ws.ID, in.ActorID, nullStr(in.AgentID), nullStr(in.AgentRunID), in.Tool, "allowed", redaction.Redact(defaultJSON(in.RedactedArgumentsJSON, "{}")), "{}", 0, in.IdempotencyKey, defaultJSON(in.ExternalIDsJSON, "[]"), now, claimHash, leaseUntil, now)
	if err != nil {
		return nil, err
	}
	changed, _ := result.RowsAffected()
	if changed == 1 {
		ledger, getErr := db.GetActionLedgerByKey(ctx, in.IdempotencyKey)
		return &ActionExecutionClaim{Ledger: ledger, ClaimToken: claimToken, Acquired: true}, getErr
	}
	ledger, err := db.GetActionLedgerByKey(ctx, in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if ledger.ExecutionState == "completed" {
		return &ActionExecutionClaim{Ledger: ledger, Replay: true}, nil
	}
	result, err = db.Exec(ctx, `UPDATE action_ledger SET execution_state='claimed',claim_token_hash=?,lease_until=?,updated_at=?,result_json='{}' WHERE id=? AND workspace_id=? AND (execution_state='failed' OR (execution_state='claimed' AND lease_until<?))`, claimHash, leaseUntil, now, ledger.ID, ws.ID, now)
	if err != nil {
		return nil, err
	}
	changed, _ = result.RowsAffected()
	if changed == 1 {
		ledger, err = db.GetActionLedgerByKey(ctx, in.IdempotencyKey)
		return &ActionExecutionClaim{Ledger: ledger, ClaimToken: claimToken, Acquired: true}, err
	}
	ledger, err = db.GetActionLedgerByKey(ctx, in.IdempotencyKey)
	return &ActionExecutionClaim{Ledger: ledger}, err
}

func (db *DB) CompleteActionExecution(ctx context.Context, ledgerID, claimToken, resultJSON string, latencyMS int64) error {
	return db.finishActionExecution(ctx, ledgerID, claimToken, "completed", resultJSON, latencyMS)
}

func (db *DB) FailActionExecution(ctx context.Context, ledgerID, claimToken, resultJSON string) error {
	return db.finishActionExecution(ctx, ledgerID, claimToken, "failed", resultJSON, 0)
}

func (db *DB) finishActionExecution(ctx context.Context, ledgerID, claimToken, stateName, resultJSON string, latencyMS int64) error {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	result, err := db.Exec(ctx, `UPDATE action_ledger SET execution_state=?,result_json=?,latency_ms=?,claim_token_hash='',lease_until='',updated_at=? WHERE id=? AND workspace_id=? AND execution_state='claimed' AND claim_token_hash=?`, stateName, redaction.Redact(defaultJSON(resultJSON, "{}")), latencyMS, Now(), ledgerID, ws.ID, actionClaimHash(claimToken))
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return fmt.Errorf("action execution claim is stale or invalid")
	}
	return nil
}

func defaultJSON(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func scanActionLedger(row interface{ Scan(...any) error }) (*ActionLedger, error) {
	var v ActionLedger
	var agent, run sql.NullString
	err := row.Scan(&v.ID, &v.WorkspaceID, &v.ActorID, &agent, &run, &v.Tool, &v.PolicyDecision, &v.RedactedArgumentsJSON, &v.ResultJSON, &v.LatencyMS, &v.IdempotencyKey, &v.ExternalIDsJSON, &v.CreatedAt, &v.ExecutionState, &v.ClaimTokenHash, &v.LeaseUntil, &v.UpdatedAt)
	v.AgentID, v.AgentRunID = agent.String, run.String
	return &v, err
}
