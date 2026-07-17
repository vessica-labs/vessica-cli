# Knowledge layer follow-ups

This is a current gap register, not a description of the original MVP boundary.
Items shipped since that boundary are recorded so older PRDs and ADRs are not
mistaken for the present product state.

## Shipped since the MVP boundary

- Hosted Postgres lexical retrieval is the healthy zero-key default. Optional
  user-funded embeddings add semantic-hybrid retrieval and an asynchronous,
  observable backfill.
- The embedded dashboard supports GitHub-based owner claims, expiring member
  invitations, workspace roles, repository switching, knowledge exploration,
  and workspace health.
- The knowledge service has readiness reporting for database, migrations,
  embedding workers, and index freshness. Vessica keeps the hosted service as
  the sole writable authority during outages.
- One Railway Postgres service contains separately owned control and knowledge
  databases with distinct credentials, URLs, migrations, and access boundaries.

## Durable chunked import tracking

Snapshots are checksummed and event-idempotent, so the same import can be
retried safely. Large imports still need durable sessions, chunk checksums,
accepted ranges, and resume cursors before import can be treated as a fully
restartable background workflow.

## Identity and access depth

GitHub-based owner/member access and expiring invitations ship today. Deferred
work includes enterprise identity providers, richer role policy, service
accounts, token-rotation administration, and organization-level audit/export
controls.

## Relationship lifecycle

Relationships remain immutable assertions. Corrections append a new relationship
or a `supersedes` assertion. Add relationship update/version endpoints only when
a concrete mutation workflow requires them.

## Retrieval quality and curation

The current service provides explainable lexical or semantic-hybrid ranking,
deterministic artifact selection, provenance, and index freshness. Remaining
work includes duplicate/contradiction review, stale-memory workflows, retrieval
regression evaluation, and operator-facing curation queues.

## Broader operations

Persistent distributed rate limiting, production metrics dashboards, scheduled
backup orchestration, restore drills, and broader administration remain future
operations work. The current embedded dashboard is a product workspace UI, not
a complete infrastructure administration console.
