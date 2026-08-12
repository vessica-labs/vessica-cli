package state

// CloudAgentControlPlaneOwnershipSQL adds recovery metadata. Existing control
// tables retain their compatible primary keys; state methods enforce ownership
// predicates on every parent-child boundary so both SQLite and Postgres apply
// the same workspace authority.
const CloudAgentControlPlaneOwnershipSQL = `
ALTER TABLE agent_runs ADD COLUMN trigger_id TEXT;
ALTER TABLE agent_run_triggers ADD COLUMN claim_token TEXT;
ALTER TABLE agent_run_triggers ADD COLUMN lease_until TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_runs_workspace_trigger ON agent_runs(workspace_id,trigger_id) WHERE trigger_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_agent_run_triggers_recovery ON agent_run_triggers(workspace_id,state,lease_until);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_consume ON oauth_authorization_codes(workspace_id,code_hash,consumed_at,expires_at);
`
