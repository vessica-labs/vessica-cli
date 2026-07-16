# ADR: One Railway Postgres Service with Two Logical Databases

## Status

Accepted.

## Context

Vessica has two durable stores with different ownership and lifecycle rules: workflow/control-plane state and the knowledge graph. Provisioning a separate Railway Postgres service for each store adds cost and onboarding latency without requiring physical-server isolation at the current single-tenant scale.

## Decision

A new Railway installation provisions one managed Postgres service and initializes:

- `vessica_control`, owned by `vessica_control_user`.
- `vessica_knowledge`, owned by `vessica_knowledge_user`.

The roles use independently generated passwords and cannot connect to the other application's database. The control plane receives `VES_CONTROL_DATABASE_URL`; the knowledge service receives `VES_KNOWLEDGE_DATABASE_URL`. No generic database URL is configured. URLs and passwords are stored only in the OS credential store and Railway service variables.

The control plane retains its `schema_migrations` history. The knowledge store owns `knowledge_schema_migrations`. pgvector is installed only in `vessica_knowledge` during infrastructure bootstrap and is verified, not installed, by knowledge-server startup.

Provisioning holds a Postgres advisory lock, checks roles and databases before creation, reconciles ownership and grants, and uses `CREATE EXTENSION IF NOT EXISTS` in the knowledge database. It is safe to repeat after partial failure.

## Consequences

The two stores share a Railway service lifecycle and physical capacity, but not schemas, credentials, connections, migrations, queries, foreign keys, or transactions. Application storage interfaces remain independent. A failure requiring physical database replacement affects both stores, which is acceptable for the current installation model.

There is no migration or upgrade path from the former two-service topology. Existing development or hosted installations are destroyed and recreated.
