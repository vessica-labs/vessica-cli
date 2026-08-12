package state

// CloudAgentControlPlaneSchemaSQL is additive control-plane state shared by
// future MCP and web adapters. Secret-bearing values are represented only by
// hashes (or, in a future key-managed implementation, encrypted blobs).
const CloudAgentControlPlaneSchemaSQL = `
CREATE TABLE IF NOT EXISTS oauth_clients (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, client_id TEXT NOT NULL,
  name TEXT NOT NULL, redirect_uris_json TEXT NOT NULL DEFAULT '[]', scopes_json TEXT NOT NULL DEFAULT '[]',
  secret_hash TEXT NOT NULL DEFAULT '', revoked_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(workspace_id,client_id), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, client_id TEXT NOT NULL, actor_id TEXT NOT NULL,
  code_hash TEXT NOT NULL, redirect_uri TEXT NOT NULL DEFAULT '', scopes_json TEXT NOT NULL DEFAULT '[]',
  expires_at TEXT NOT NULL, consumed_at TEXT, revoked_at TEXT, created_at TEXT NOT NULL,
  UNIQUE(code_hash), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(client_id) REFERENCES oauth_clients(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS oauth_access_tokens (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, client_id TEXT NOT NULL, actor_id TEXT NOT NULL,
  token_hash TEXT NOT NULL, scopes_json TEXT NOT NULL DEFAULT '[]', expires_at TEXT NOT NULL,
  revoked_at TEXT, created_at TEXT NOT NULL, UNIQUE(token_hash),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(client_id) REFERENCES oauth_clients(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, client_id TEXT NOT NULL, actor_id TEXT NOT NULL,
  material_hash TEXT NOT NULL, family_id TEXT NOT NULL, scopes_json TEXT NOT NULL DEFAULT '[]', expires_at TEXT NOT NULL,
  replaced_at TEXT, revoked_at TEXT, created_at TEXT NOT NULL, UNIQUE(material_hash),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(client_id) REFERENCES oauth_clients(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS action_ledger (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, actor_id TEXT NOT NULL, agent_id TEXT, agent_run_id TEXT,
  tool TEXT NOT NULL, policy_decision TEXT NOT NULL, redacted_arguments_json TEXT NOT NULL DEFAULT '{}',
  result_json TEXT NOT NULL DEFAULT '{}', latency_ms INTEGER NOT NULL DEFAULT 0, idempotency_key TEXT NOT NULL,
  external_ids_json TEXT NOT NULL DEFAULT '[]', created_at TEXT NOT NULL,
  UNIQUE(workspace_id,idempotency_key), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS conversations (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, actor_id TEXT NOT NULL, title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active', next_message_seq INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS conversation_messages (
  id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, workspace_id TEXT NOT NULL, sequence INTEGER NOT NULL,
  role TEXT NOT NULL, content_json TEXT NOT NULL, metadata_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL,
  UNIQUE(conversation_id,sequence), FOREIGN KEY(conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS source_checkpoints (
  workspace_id TEXT NOT NULL, source_type TEXT NOT NULL, source_id TEXT NOT NULL, checkpoint_json TEXT NOT NULL,
  updated_at TEXT NOT NULL, PRIMARY KEY(workspace_id,source_type,source_id),
  FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS newsletter_subscriptions (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, source_key TEXT NOT NULL, source_url TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', retention_days INTEGER NOT NULL DEFAULT 30,
  metadata_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(workspace_id,source_key), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS newsletter_items (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, subscription_id TEXT NOT NULL, source_item_id TEXT NOT NULL,
  normalized_json TEXT NOT NULL, published_at TEXT, retain_until TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(subscription_id,source_item_id), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(subscription_id) REFERENCES newsletter_subscriptions(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS outlook_ingestion_batches (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, idempotency_key TEXT NOT NULL, submitted_by TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'received', checkpoint_json TEXT NOT NULL DEFAULT '{}', warnings_json TEXT NOT NULL DEFAULT '[]',
  error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT,
  UNIQUE(workspace_id,idempotency_key), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS outlook_ingestion_items (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, batch_id TEXT NOT NULL, source_id TEXT NOT NULL,
  internet_message_id TEXT, conversation_id TEXT, message_at TEXT, normalized_json TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'received', error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(workspace_id,source_id), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(batch_id) REFERENCES outlook_ingestion_batches(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS outlook_ingestion_receipts (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, batch_id TEXT NOT NULL, item_id TEXT NOT NULL,
  state TEXT NOT NULL, result_json TEXT NOT NULL DEFAULT '{}', error TEXT, created_at TEXT NOT NULL,
  UNIQUE(batch_id,item_id), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(batch_id) REFERENCES outlook_ingestion_batches(id) ON DELETE CASCADE,
  FOREIGN KEY(item_id) REFERENCES outlook_ingestion_items(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS outlook_outbox (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, batch_id TEXT NOT NULL, item_id TEXT NOT NULL,
  processing_key TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
  lease_owner TEXT, lease_until TEXT, processed_at TEXT, last_error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(workspace_id,processing_key), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(batch_id) REFERENCES outlook_ingestion_batches(id) ON DELETE CASCADE,
  FOREIGN KEY(item_id) REFERENCES outlook_ingestion_items(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_task_checkpoints (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, agent_id TEXT NOT NULL, agent_run_id TEXT NOT NULL,
  checkpoint_key TEXT NOT NULL, state_json TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'saved',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(workspace_id,agent_run_id,checkpoint_key), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE,
  FOREIGN KEY(agent_run_id) REFERENCES agent_runs(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS agent_run_triggers (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, agent_id TEXT NOT NULL, idempotency_key TEXT NOT NULL,
  trigger TEXT NOT NULL, input_json TEXT NOT NULL, repository_id TEXT, parent_run_id TEXT, agent_run_id TEXT,
  state TEXT NOT NULL DEFAULT 'accepted', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  UNIQUE(workspace_id,agent_id,idempotency_key), FOREIGN KEY(workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  FOREIGN KEY(agent_id) REFERENCES agents(id) ON DELETE CASCADE,
  FOREIGN KEY(agent_run_id) REFERENCES agent_runs(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_oauth_access_token_lookup ON oauth_access_tokens(token_hash,expires_at);
CREATE INDEX IF NOT EXISTS idx_action_ledger_workspace_created ON action_ledger(workspace_id,created_at);
CREATE INDEX IF NOT EXISTS idx_conversation_messages_order ON conversation_messages(conversation_id,sequence);
CREATE INDEX IF NOT EXISTS idx_newsletter_items_retention ON newsletter_items(retain_until);
CREATE INDEX IF NOT EXISTS idx_outlook_outbox_ready ON outlook_outbox(state,lease_until,created_at);
CREATE INDEX IF NOT EXISTS idx_agent_task_checkpoints_run ON agent_task_checkpoints(agent_run_id,updated_at);
`
