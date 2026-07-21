package state

// AgentSchemaSQL is deliberately additive. General-agent lifecycle state is
// separate from the coding-run schema so neither subsystem can accidentally
// claim or mutate the other's work.
const AgentSchemaSQL = `
CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, name TEXT NOT NULL,
  name_key TEXT NOT NULL, purpose TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'active',
  current_version INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(workspace_id,name_key),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_versions (
  agent_id TEXT NOT NULL, version INTEGER NOT NULL, definition_json TEXT NOT NULL,
  provenance_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL,
  PRIMARY KEY(agent_id,version), FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_build_operations (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, agent_id TEXT, kind TEXT NOT NULL,
  description TEXT NOT NULL, review INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL,
  warnings_json TEXT NOT NULL DEFAULT '[]', usage_json TEXT NOT NULL DEFAULT '{}',
  error TEXT, created_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS agent_drafts (
  id TEXT PRIMARY KEY, operation_id TEXT NOT NULL UNIQUE, workspace_id TEXT NOT NULL,
  agent_id TEXT, definition_json TEXT NOT NULL, warnings_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'pending', created_at TEXT NOT NULL, activated_at TEXT,
  FOREIGN KEY(operation_id) REFERENCES agent_build_operations(id) ON DELETE CASCADE,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS agent_schedules (
  agent_id TEXT PRIMARY KEY, enabled INTEGER NOT NULL DEFAULT 0, cron TEXT NOT NULL,
  timezone TEXT NOT NULL, next_due_at TEXT, last_due_at TEXT, updated_at TEXT NOT NULL,
  FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_budget_policies (
  agent_id TEXT PRIMARY KEY, daily_limit_microusd INTEGER NOT NULL, timezone TEXT NOT NULL,
  updated_at TEXT NOT NULL, FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_budget_periods (
  agent_id TEXT NOT NULL, period_start TEXT NOT NULL, period_end TEXT NOT NULL,
  limit_microusd INTEGER NOT NULL, reserved_microusd INTEGER NOT NULL DEFAULT 0,
  spent_microusd INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL,
  PRIMARY KEY(agent_id,period_start), FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_runs (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, agent_id TEXT NOT NULL,
  definition_version INTEGER NOT NULL, trigger TEXT NOT NULL, input_json TEXT NOT NULL,
  originating_repository_id TEXT, parent_run_id TEXT, root_run_id TEXT NOT NULL,
  nesting_depth INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL,
  budget_period_start TEXT NOT NULL, reservation_microusd INTEGER NOT NULL DEFAULT 0,
  rate_snapshot_json TEXT NOT NULL DEFAULT '{}', resolved_knowledge_json TEXT NOT NULL DEFAULT '[]',
  output_json TEXT, terminal_error TEXT, cancel_requested_at TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, started_at TEXT, finished_at TEXT,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(agent_id,definition_version) REFERENCES agent_versions(agent_id,version),
  FOREIGN KEY(parent_run_id) REFERENCES agent_runs(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS agent_budget_ledger (
  id TEXT PRIMARY KEY, agent_id TEXT NOT NULL, run_id TEXT, period_start TEXT NOT NULL,
  kind TEXT NOT NULL, amount_microusd INTEGER NOT NULL, usage_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE,
  FOREIGN KEY(run_id) REFERENCES agent_runs(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS agent_run_attempts (
  id TEXT PRIMARY KEY, run_id TEXT NOT NULL, attempt_number INTEGER NOT NULL,
  worker_id TEXT NOT NULL, fence_token TEXT NOT NULL UNIQUE, status TEXT NOT NULL,
  lease_until TEXT NOT NULL, heartbeat_at TEXT NOT NULL, usage_json TEXT NOT NULL DEFAULT '{}',
  error TEXT, started_at TEXT NOT NULL, finished_at TEXT, UNIQUE(run_id,attempt_number),
  FOREIGN KEY(run_id) REFERENCES agent_runs(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_run_events (
  id TEXT PRIMARY KEY, run_id TEXT NOT NULL, attempt_id TEXT NOT NULL, seq INTEGER NOT NULL,
  attempt_ordinal INTEGER NOT NULL, type TEXT NOT NULL, payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL, UNIQUE(run_id,seq), UNIQUE(attempt_id,attempt_ordinal),
  FOREIGN KEY(run_id) REFERENCES agent_runs(id) ON DELETE CASCADE,
  FOREIGN KEY(attempt_id) REFERENCES agent_run_attempts(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_runtime_tasks (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, kind TEXT NOT NULL, subject_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued', available_at TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 3, lease_owner TEXT, lease_until TEXT, fence_token TEXT,
  payload_json TEXT NOT NULL DEFAULT '{}', last_error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(kind,subject_id), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_tool_calls (
  id TEXT PRIMARY KEY, attempt_id TEXT NOT NULL, logical_ordinal INTEGER NOT NULL,
  tool_id TEXT NOT NULL, argument_hash TEXT NOT NULL, status TEXT NOT NULL,
  result_json TEXT, error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(attempt_id,logical_ordinal), FOREIGN KEY(attempt_id) REFERENCES agent_run_attempts(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_evaluations (
  id TEXT PRIMARY KEY, evaluated_run_id TEXT NOT NULL UNIQUE, critic_agent_id TEXT NOT NULL,
  critic_run_id TEXT, status TEXT NOT NULL, score REAL, passed INTEGER,
  summary TEXT, findings_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  FOREIGN KEY(evaluated_run_id) REFERENCES agent_runs(id) ON DELETE CASCADE,
  FOREIGN KEY(critic_agent_id) REFERENCES agents(id), FOREIGN KEY(critic_run_id) REFERENCES agent_runs(id)
);
CREATE TABLE IF NOT EXISTS agent_eval_stats (
  agent_id TEXT PRIMARY KEY, evaluation_count INTEGER NOT NULL DEFAULT 0,
  cumulative_score REAL NOT NULL DEFAULT 0, mean_score REAL NOT NULL DEFAULT 0,
  pass_count INTEGER NOT NULL DEFAULT 0, pass_rate REAL NOT NULL DEFAULT 0, updated_at TEXT NOT NULL,
  FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_agents_workspace ON agents(workspace_id,state,name_key);
CREATE INDEX IF NOT EXISTS idx_agent_runs_workspace ON agent_runs(workspace_id,created_at);
CREATE INDEX IF NOT EXISTS idx_agent_runs_agent_status ON agent_runs(agent_id,status,created_at);
CREATE INDEX IF NOT EXISTS idx_agent_events_run_seq ON agent_run_events(run_id,seq);
CREATE INDEX IF NOT EXISTS idx_agent_tasks_ready ON agent_runtime_tasks(status,available_at,lease_until);
CREATE INDEX IF NOT EXISTS idx_agent_schedules_due ON agent_schedules(enabled,next_due_at);
`
