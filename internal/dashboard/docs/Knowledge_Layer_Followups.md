# Knowledge layer follow-ups

This is a current gap register, not a description of the original MVP boundary.
Items shipped since that boundary are recorded so older PRDs and ADRs are not
mistaken for the present product state.

## Shipped since the MVP boundary

- Hosted Postgres lexical retrieval is the healthy zero-key default. Optional
  user-funded embeddings add semantic-hybrid retrieval and an asynchronous,
  observable backfill.
- Retrieval v2 provides weighted lexical/semantic rank fusion, entity
  constraints, temporal and lifecycle filtering, per-result explanations,
  `ambiguous_subject` safety stops, relevant-artifact admission, separate type
  budgets, and typed omissions. `ves memory retrieve` is the restoration path;
  `ves memory search` remains an administrative lexical operation.
- The `0.2.45` cold-chat treatment scored 97/100, reached Recall@5, MRR@5, and
  NDCG@5 of 1.00 with no stale/scope/person violations, and reduced comparable
  Codex input tokens by 37.6%. These are benchmark-treatment results, not a
  production-wide latency or accuracy guarantee.
- Conditional Responses API reranking is separately configurable but remains
  disabled by default. Deterministic hybrid retrieval already cleared the
  current quality gate, so the tested rerankers could not meet the required
  NDCG improvement threshold.
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

The current service provides explainable lexical or semantic-hybrid retrieval,
deterministic artifact admission, entity-safe restoration, provenance, index
freshness, and a checked-in regression benchmark. Remaining work includes
source-conversation provenance, duplicate/contradiction review, stale-memory
workflows, ongoing production-distribution evaluation, provider token/cost
telemetry, and operator-facing curation queues.

## Broader operations

Persistent distributed rate limiting, production metrics dashboards, scheduled
backup orchestration, restore drills, and broader administration remain future
operations work. The current embedded dashboard is a product workspace UI, not
a complete infrastructure administration console.
