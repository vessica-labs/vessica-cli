# Vessica Knowledge Layer v1 ADR — Separate Hosted Service with Embedded Solo Core

**Document type:** Architecture Decision Record  
**ADR ID:** ADR-002  
**Product:** Vessica Knowledge Layer  
**Status:** Accepted  
**Date:** 2026-07-11

---

## 1. Decision Summary

Vessica will implement the knowledge layer in a separate repository named `vessica-knowledge-server`.

The repository will publish:

1. A reusable Go knowledge core with domain, retrieval, event, migration, and storage interfaces.
2. An embedded SQLite adapter used in-process by `ves` for solo mode.
3. A Postgres/pgvector adapter and authenticated HTTP server used in hosted mode.
4. A versioned client/API package consumed by `vessica-cli` and the control plane.

Developers will install only `ves` and the Vessica coding-tool plugin. The separate service boundary is operational and architectural, not a separate user-facing workflow.

`ves` will route all knowledge operations through a `KnowledgeClient` abstraction:

- `EmbeddedClient` calls the knowledge application service in-process and persists to SQLite.
- `HTTPClient` calls the hosted knowledge service.

A workspace has exactly one writable knowledge authority. `ves railway up` performs a verified, one-way promotion from local SQLite to hosted Postgres and atomically changes the authority after successful import.

Solo V1 will not generate embeddings by default and will not require an LLM or embeddings API key. It will use deterministic artifact selection, entity/relationship filtering, scope specificity, SQLite FTS5/BM25, importance, confidence, and recency. Hosted mode requires an embedding provider credential and adds pgvector semantic ranking to the same retrieval contract.

Epics and tickets remain control-plane workplan objects. Meaningful epic, ticket, run, refinement, receipt, PR, and commit events produce idempotent episode memories and relationships in the knowledge layer.

No backward-compatibility layer will be built for the current experimental artifact and memory schemas.

For knowledge, artifact, and memory ownership, this ADR supersedes the corresponding local-state decisions in ADR-001. ADR-001 remains applicable to the CLI-first control surface, harness, run engine, tickets, sandboxes, events, and integrations except where this ADR explicitly changes them.

---

## 2. Context

Vessica needs durable knowledge that is available to:

- Local developers using Codex Desktop or a terminal.
- Cloud coding sandboxes.
- Multiple team members on different machines.
- Linear-triggered runs and the hosted control plane.
- Future non-coding agents.

Putting all knowledge logic directly inside `vessica-cli` would simplify one deployment but would couple a cross-domain, long-lived service to the coding control plane. Making knowledge a separately installed daemon or CLI would preserve separation but damage the desired one-command developer experience.

The existing Vessica code contains local artifact and memory tables, but there are no external users or compatibility obligations. Preserving those schemas would create two potential authorities and constrain the target model.

The system must also work without requiring a solo developer to obtain a second API credential. Codex authentication is suitable for Codex product use and the coding harness; it must not be repurposed as a general embeddings credential.

---

## 3. Decision Drivers

- One-install local experience.
- Shared team knowledge across machines and sandboxes.
- Clear ownership and one authoritative store.
- Generic support for coding and non-coding agents.
- Deterministic retrieval of authoritative artifacts.
- Useful zero-key solo behavior.
- Semantic retrieval and concurrency at hosted scale.
- Safe, resumable local-to-cloud promotion.
- Independent service evolution and operations.
- Agent-friendly CLI and JSON contracts.

---

## 4. Architecture

```text
Codex / Claude / Cursor
          |
          v
        ves CLI
          |
    KnowledgeClient
      /          \
     v            v
EmbeddedClient   HTTPClient -------------------+
     |                                        |
Knowledge core                     Knowledge HTTP service
     |                                        |
SQLite                             Postgres + pgvector
solo authority                     hosted authority
```

Cloud control planes, coding sandboxes, and future agents always use the HTTP API. Only a local `ves` process may use the embedded adapter.

The hosted service is workspace-level and may serve multiple repositories and control-plane services. Repository scopes use canonical external identity rather than machine-local paths.

---

## 5. Major Decisions

### Decision 1 — Separate repository and hosted service

The knowledge layer is developed in `vessica-knowledge-server` and deployed independently from the Vessica control plane.

This provides clean domain ownership, independent migrations and scaling, reuse by non-coding agents, and a stable API boundary. `ves railway up` hides the provisioning complexity from developers.

### Decision 2 — Embedded core for solo mode

The knowledge server repository exposes a reusable Go application core. `vessica-cli` pins that module and compiles the SQLite implementation into the normal `ves` binary.

Solo mode therefore requires no daemon or second executable. Local and hosted adapters share application-service contract tests to prevent semantic drift.

### Decision 3 — One authority, no normal dual-write

Configuration explicitly selects `local` or `hosted`. Promotion freezes local writes, imports and verifies a complete snapshot, flips configuration atomically, and retains SQLite only as a read-only recovery snapshot.

Hosted failures do not trigger writable local fallback. Bidirectional synchronization is deferred because it requires conflict resolution and would violate the single-authority invariant.

### Decision 4 — Append-only events with transactional projections

Every accepted mutation appends a versioned knowledge event and updates query projections in one transaction. Objects are superseded, archived, or tombstoned rather than destructively edited.

This provides provenance, replayability, auditability, stable migration, and immutable history without forcing callers to consume event streams directly.

### Decision 5 — Zero-key lexical solo retrieval

Solo V1 uses SQLite FTS5/BM25 plus deterministic artifact selectors, entity relationships, scopes, importance, confidence, temporal validity, and recency. It stores no default vectors and reports lexical retrieval mode.

V1 will not bundle a local embedding model because model distribution, licensing, native dependencies, hardware variance, binary size, and cold-start cost conflict with the zero-friction install goal. It will not use Codex login tokens as embedding credentials.

Hosted mode requires an embedding provider key, generates embeddings asynchronously, stores model/version metadata, and uses pgvector for hybrid semantic ranking. Pending embeddings never prevent lexical retrieval.

### Decision 6 — Workplan objects emit knowledge

Epics and tickets remain in the control plane. The control plane emits idempotent workflow events through a durable outbox. The knowledge layer converts meaningful milestones into observed episode memories and relationships.

Required sources include epic/ticket identifiers, generated artifacts, run and receipt IDs, validation evidence, commits, pull requests, and Linear issues. Routine heartbeats and low-level execution noise do not create memory.

Automatic workflow processing may create episodes. Facts and decisions require explicit creation or provenance-backed derivation from authoritative artifact versions. Instructions always require an explicit action in V1.

### Decision 7 — One CLI and plugin experience

The public interface remains `ves`; no `agent-memory` CLI is introduced. The Codex plugin teaches `ves knowledge`, `ves entity`, `ves artifact`, `ves memory`, and `ves prime` workflows. It contains no storage, API, embedding, or Railway business logic.

---

## 6. Rejected Alternatives

### Put the knowledge layer entirely inside `vessica-cli`

Rejected because it couples a workspace-wide, multi-domain service to a coding CLI/control plane, complicates independent scaling and access, and makes future non-coding integrations depend on the CLI architecture.

### Require a locally running knowledge daemon

Rejected because it adds process lifecycle, port, installation, upgrade, and support complexity before the user receives value.

### Require local Postgres and pgvector

Rejected because it breaks the zero-friction solo experience and makes demos and debugging dependent on Docker or database installation.

### Bundle a local embedding model in V1

Rejected because it materially increases install size and operational variance for limited initial benefit. The provider abstraction remains available for a later optional implementation.

### Use Codex authentication for embeddings

Rejected because Codex product authentication is not a general embedding-service contract and would couple Vessica correctness to undocumented or unavailable token behavior.

### Dual-write SQLite and Postgres continuously

Rejected because partial failures create divergent histories, require conflict resolution, and undermine the definition of authoritative knowledge.

### Store epics and tickets as knowledge artifacts

Rejected because they are mutable workflow coordination objects. Their durable meaning is represented through authoritative planning artifacts, episode memories, source references, and relationships.

---

## 7. Consequences

### Positive

- Clean service and data ownership.
- One-binary solo experience.
- Shared hosted knowledge for teams and sandboxes.
- No solo API-key requirement.
- Semantic retrieval can scale independently in hosted mode.
- Work history becomes queryable without storing transcripts or execution noise.
- Local-to-cloud promotion has a clear safety model.
- The same core can support future agent domains.

### Negative

- `vessica-cli` must pin and coordinate releases with another Go module.
- SQLite and Postgres adapters require parity tests.
- Railway provisioning manages another service and schema.
- Solo and hosted ranking quality differ because solo has no semantic vectors.
- Promotion requires a temporary local write lock.
- Hosted operation requires an embedding provider credential.

### Mitigations

- Publish versioned API and Go modules together from the knowledge repository.
- Maintain adapter conformance and context golden tests.
- Report retrieval mode and score explanations in every context response.
- Keep lexical/entity retrieval active in hosted mode when embeddings are pending or failed.
- Make promotion resumable and verify counts, hashes, relationships, and event watermarks before authority changes.

---

## 8. Implementation Constraints

1. No legacy-schema compatibility layer or dual authority.
2. Stable IDs are created before persistence and preserved during promotion.
3. All writes require idempotency, actor, and provenance.
4. Local and hosted modes expose equivalent domain behavior and response contracts.
5. Context assembly is server/core behavior, not plugin prompt logic.
6. Hosted callers never receive database or embedding-provider credentials.
7. Workflow memory creation is driven by durable events, not best-effort prompt instructions.
8. `ves prime` becomes a knowledge-context client.
9. The current Vessica artifact and memory tables are replaced or migrated during development rather than supported indefinitely.

---

## 9. Validation

The decision is validated when:

- A fresh solo install retrieves useful context with no network service or API key.
- SQLite and Postgres pass the same domain and retrieval conformance suite.
- Promotion preserves all IDs, versions, hashes, events, and relationships.
- Hosted semantic retrieval improves ranking without changing API behavior.
- Multiple machines and cloud sandboxes see the same hosted knowledge.
- Completed and failed work produces appropriately linked episodes.
- Agents answer work-history questions with citations to artifacts and evidence.
- Hosted outages never produce an implicit writable local fork.
