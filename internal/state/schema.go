package state

// SchemaSQL is the shared DDL for SQLite (Postgres adaptations applied at migrate time).
const SchemaSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workspaces (
  id TEXT PRIMARY KEY,
  root_path TEXT NOT NULL,
  profile TEXT NOT NULL DEFAULT 'solo',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS provider_auth (
  provider TEXT PRIMARY KEY,
  account TEXT,
  meta_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS hosted_credentials (
  provider TEXT PRIMARY KEY,
  encrypted_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS packs (
  id TEXT PRIMARY KEY,
  ref TEXT NOT NULL,
  origin TEXT,
  version TEXT,
  commit_sha TEXT,
  installed_at TEXT NOT NULL,
  pinned INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS harness_status (
  workspace_id TEXT PRIMARY KEY,
  last_sync_at TEXT,
  drift_status TEXT NOT NULL DEFAULT 'unknown',
  pack_version TEXT,
  missing_files_json TEXT NOT NULL DEFAULT '[]',
  updated_at TEXT NOT NULL,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS epics (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'draft',
  external_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  epic_id TEXT,
  artifact_set_id TEXT,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft',
  version INTEGER NOT NULL DEFAULT 1,
  body TEXT NOT NULL DEFAULT '',
  frontmatter_json TEXT NOT NULL DEFAULT '{}',
  source_run_id TEXT,
  created_by_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY (epic_id) REFERENCES epics(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS artifact_versions (
  id TEXT PRIMARY KEY,
  artifact_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  body TEXT NOT NULL,
  content_hash TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(artifact_id, version)
);

CREATE TABLE IF NOT EXISTS artifact_sets (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  epic_id TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft',
  artifact_ids_json TEXT NOT NULL DEFAULT '[]',
  source_run_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY (epic_id) REFERENCES epics(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS tickets (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  epic_id TEXT NOT NULL,
  source_run_id TEXT,
  wave_id TEXT,
  type TEXT NOT NULL DEFAULT 'feature',
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'draft',
  evidence_receipt_id TEXT,
  discovered_from_run_id TEXT,
  test_step TEXT,
  external_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY (epic_id) REFERENCES epics(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS ticket_dependencies (
  ticket_id TEXT NOT NULL,
  depends_on TEXT NOT NULL,
  PRIMARY KEY (ticket_id, depends_on),
  FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
  FOREIGN KEY (depends_on) REFERENCES tickets(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS waves (
  id TEXT PRIMARY KEY,
  epic_id TEXT NOT NULL,
  source_run_id TEXT,
  index_n INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (epic_id) REFERENCES epics(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS claims (
  id TEXT PRIMARY KEY,
  ticket_id TEXT NOT NULL UNIQUE,
  agent_id TEXT NOT NULL,
  lease_until TEXT NOT NULL,
  heartbeat_at TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TEXT NOT NULL,
  FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  epic_id TEXT,
  ticket_id TEXT,
  workflow TEXT NOT NULL DEFAULT 'software_epic',
  status TEXT NOT NULL DEFAULT 'pending',
  current_phase TEXT,
  start_phase TEXT,
  stop_after TEXT,
  concurrency INTEGER NOT NULL DEFAULT 3,
  runner TEXT,
	model TEXT,
	reasoning_effort TEXT,
  sandbox_backend TEXT,
  preview INTEGER NOT NULL DEFAULT 0,
  pr_mode TEXT NOT NULL DEFAULT 'none',
  preview_url TEXT,
  pr_url TEXT,
  receipt_id TEXT,
  artifact_set_id TEXT,
  error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY (epic_id) REFERENCES epics(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS run_phases (
  run_id TEXT NOT NULL,
  phase TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  started_at TEXT,
  finished_at TEXT,
  error TEXT,
  PRIMARY KEY (run_id, phase),
  FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS sandboxes (
  id TEXT PRIMARY KEY,
  run_id TEXT,
  workspace_id TEXT NOT NULL,
  backend TEXT NOT NULL DEFAULT 'docker',
  container_id TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  branch TEXT,
  preview_port INTEGER,
  preview_url TEXT,
  meta_json TEXT NOT NULL DEFAULT '{}',
	last_accessed_at TEXT,
	expires_at TEXT,
	retained_until TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  destroyed_at TEXT,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS events (
  id TEXT PRIMARY KEY,
  run_id TEXT,
  sandbox_id TEXT,
  seq INTEGER NOT NULL,
  type TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE,
  FOREIGN KEY (sandbox_id) REFERENCES sandboxes(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS run_evidence (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  phase TEXT NOT NULL,
  kind TEXT NOT NULL,
  ticket_id TEXT,
  status TEXT NOT NULL,
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS receipts (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  epic_id TEXT,
  status TEXT NOT NULL,
  body_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS traces (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS external_mappings (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  local_id TEXT NOT NULL,
  external_id TEXT NOT NULL,
  meta_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
	  sync_status TEXT NOT NULL DEFAULT 'pending',
	  external_version TEXT,
	  last_synced_at TEXT,
	  last_error TEXT,
  UNIQUE(provider, entity_type, local_id),
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS tracker_integrations (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  config_json TEXT NOT NULL DEFAULT '{}',
  webhook_id TEXT,
  secret_ref TEXT,
  last_synced_at TEXT,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(workspace_id, provider),
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id TEXT PRIMARY KEY,
  integration_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  delivery_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TEXT NOT NULL,
  processed_at TEXT,
  UNIQUE(provider, delivery_id),
  FOREIGN KEY (integration_id) REFERENCES tracker_integrations(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  payload_json TEXT NOT NULL DEFAULT '{}',
  run_id TEXT,
  attempts INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 5,
  lease_owner TEXT,
  lease_until TEXT,
  available_at TEXT NOT NULL,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS outbox_messages (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  integration_id TEXT NOT NULL,
  operation TEXT NOT NULL,
  idempotency_key TEXT NOT NULL UNIQUE,
  payload_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  available_at TEXT NOT NULL,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY (integration_id) REFERENCES tracker_integrations(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS control_plane_deployments (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  project_id TEXT NOT NULL,
  environment_id TEXT NOT NULL,
  service_id TEXT NOT NULL,
  postgres_service_id TEXT,
  public_url TEXT,
  version TEXT,
  status TEXT NOT NULL DEFAULT 'provisioning',
  meta_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(workspace_id, provider),
  FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
  key TEXT PRIMARY KEY,
  result_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_epics_workspace ON epics(workspace_id);
CREATE INDEX IF NOT EXISTS idx_tickets_epic ON tickets(epic_id);
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);
CREATE INDEX IF NOT EXISTS idx_artifacts_epic ON artifacts(epic_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_run_seq ON events(run_id, seq);
CREATE INDEX IF NOT EXISTS idx_run_evidence_run ON run_evidence(run_id);
CREATE INDEX IF NOT EXISTS idx_runs_workspace ON runs(workspace_id);
CREATE INDEX IF NOT EXISTS idx_claims_lease ON claims(lease_until);
CREATE UNIQUE INDEX IF NOT EXISTS idx_external_mappings_external ON external_mappings(provider, entity_type, external_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_status ON webhook_deliveries(status, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_ready ON jobs(status, available_at, lease_until);
CREATE INDEX IF NOT EXISTS idx_outbox_ready ON outbox_messages(status, available_at);
`

const SchemaFTSSQLite = `
CREATE VIRTUAL TABLE IF NOT EXISTS artifact_fts USING fts5(
  id UNINDEXED,
  title,
  body
);
`
