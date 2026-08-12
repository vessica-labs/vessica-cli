package state

const CloudAgentTriggerRequestSQL = `
ALTER TABLE agent_run_triggers ADD COLUMN rate_snapshot_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE outlook_outbox ADD COLUMN available_at TEXT NOT NULL DEFAULT '';
`
