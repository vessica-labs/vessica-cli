# Vessica Knowledge Layer v1 — Product Requirements Document

> **Implementation-history record.** Current hosted knowledge starts in healthy
> lexical mode without an embeddings key. Use the Operator Guide and knowledge
> follow-up register for current behavior and remaining gaps.

**Product:** Vessica Knowledge Layer  
**Document type:** Product Requirements Document  
**Version:** v1  
**Status:** Approved direction  
**Primary audience:** Vessica product and engineering  
**Last updated:** 2026-07-11

---

## 1. Executive Summary

The Vessica Knowledge Layer is a durable knowledge substrate shared by local coding agents, cloud coding sandboxes, team members, the Vessica control plane, and future non-coding agents.

It stores durable knowledge rather than conversation transcripts. Entities identify what exists, artifacts preserve authoritative work products, memories capture retrieval-optimized understanding, and relationships connect all three.

The knowledge layer is implemented in a separate repository, `vessica-knowledge-server`, and deployed as a separate cloud service. It remains a single-product experience: developers install only `ves` and the Vessica Codex plugin. In solo mode, `ves` embeds the knowledge core and stores knowledge in SQLite without a daemon, external database, embedding API key, or separate executable. In hosted mode, every caller uses the same authenticated HTTP service backed by Postgres and pgvector.

There are no backward-compatibility requirements for the current experimental Vessica state model. The implementation should choose the cleanest target architecture and migrate development workspaces explicitly rather than preserve obsolete interfaces or schemas.

---

## 2. Product Thesis

Modern agents lose continuity between sessions, machines, and execution environments. Prompt history and repository-local memory files are not a durable shared substrate.

Vessica should remember what future agents will wish they already knew:

- Authoritative requirements, designs, plans, and decisions.
- Durable facts and instructions.
- The identity of people, organizations, repositories, projects, products, and topics.
- Concise episodes describing meaningful work and outcomes.
- Provenance connecting knowledge to artifacts, tickets, runs, receipts, commits, pull requests, and external systems.

The knowledge layer augments Git, Linear, repositories, and agent runtimes. It does not replace them.

---

## 3. Goals

V1 must:

1. Provide durable knowledge shared across agents and machines.
2. Keep artifacts authoritative and memories retrieval-optimized.
3. Maintain extensible entity identity and generic relationships.
4. Preserve provenance, confidence, temporal validity, lifecycle, and immutable version history.
5. Produce useful context within a caller-specified token budget.
6. Support semantic memory retrieval in hosted mode using Postgres and pgvector.
7. Provide useful zero-key retrieval in solo mode using deterministic selection, entity/scope filters, and SQLite FTS5/BM25.
8. Expose a stable Go HTTP/JSON API and a reusable Go core.
9. Expose all developer workflows through the existing `ves` CLI.
10. Promote a solo knowledge store to hosted mode safely through `ves railway up`.
11. Ensure epics, tickets, runs, and receipts produce queryable knowledge about work performed.
12. Use one authoritative knowledge store at a time and prevent split-brain local/cloud writes.

### Success criteria

- A new developer can install `ves` and the Codex plugin and use knowledge features without an API key or cloud account.
- Multiple team members and cloud sandboxes retrieve the same hosted knowledge.
- A solo workspace can be promoted without changing object IDs or losing versions, relationships, provenance, or events.
- An agent can answer what work was attempted, completed, blocked, approved, or rolled back and cite the relevant epic, tickets, receipt, PR, commit, and artifacts.
- Active PRDs and ADRs are retrieved deterministically rather than only through semantic similarity.
- Context responses explain why each item was selected.

---

## 4. Non-Goals

V1 is not:

- A CRM, document management system, issue tracker, workflow engine, or conversation database.
- A replacement for Git, Linear, GitHub, or agent runtimes.
- A general knowledge-graph reasoning engine.
- Enterprise search across arbitrary external sources.
- Bidirectional or real-time replication between SQLite and Postgres.
- Automatic instruction creation.
- Automatic conflict resolution between memories.
- A web UI, human review queue, or approval workflow.
- A requirement to support legacy Vessica artifact or memory schemas.

---

## 5. Product Principles

### 5.1 Knowledge, not conversations

Store durable understanding and concise episodes. Raw chats, prompts, and transcripts are not knowledge objects. Conversation identifiers may appear in provenance or scope metadata.

### 5.2 Artifacts are authoritative

PRDs, ADRs, designs, specifications, plans, and reports remain authoritative. Memories may summarize or derive from them but must link to their source versions.

### 5.3 Generic core model

The service must not require domain-specific tables for customers, repositories, technologies, or contacts. Entity types, artifact types, relationship predicates, and metadata are extensible strings governed by validation, not fixed ontologies.

### 5.4 Append-only history

Knowledge is never destructively edited. Changes append events and create immutable versions. Supersession, archival, and tombstones replace destructive update and delete semantics.

### 5.5 One authority

A workspace is either local-authoritative or hosted-authoritative. Hosted outages must not silently create a writable local fork.

### 5.6 CLI-first experience

Agents and humans use `ves`. The Codex plugin supplies workflows and command guidance but contains no knowledge business logic and accesses no database or knowledge API directly.

---

## 6. Architecture and Deployment

### 6.1 Repository ownership

`vessica-knowledge-server` owns:

- Domain models and validation.
- Event envelopes and projections.
- SQLite and Postgres adapters.
- Context assembly and retrieval ranking.
- Embedding jobs and pgvector integration.
- Export/import and migration verification.
- HTTP handlers, authentication, tenancy, and authorization.
- A reusable Go module consumed by `vessica-cli`.

`vessica-cli` owns:

- CLI commands and JSON contracts.
- Local-versus-hosted routing through a `KnowledgeClient` interface.
- Railway provisioning and promotion orchestration.
- Keychain-backed credentials and workspace connection.
- Run, Linear, harness, and receipt integrations.
- Codex plugin skills.

### 6.2 Solo mode

Solo mode runs the released knowledge core in-process inside `ves`:

```text
ves command -> EmbeddedKnowledgeClient -> knowledge core -> SQLite
```

Requirements:

- No daemon, Docker, Postgres, model download, API key, or separate binary.
- Database path defaults to `.vessica/state/knowledge.db`.
- SQLite FTS5 provides lexical search and BM25 ranking.
- Deterministic artifact selection, entity relevance, scope specificity, importance, confidence, and recency participate in ranking.
- Semantic vectors are optional and absent by default.
- Context responses report `retrieval_mode: lexical` and any degraded capabilities.

V1 will not bundle a local embedding model. This avoids large downloads, native-runtime variance, licensing concerns, slow cold starts, and hardware-specific behavior. A later optional local embedding provider may implement the same interface without changing storage or CLI contracts.

### 6.3 Hosted mode

Hosted mode runs a separate knowledge service alongside the Vessica control plane:

```text
Codex/CLI -----------+
Control plane -------+-> Knowledge HTTP API -> Postgres + pgvector
Cloud sandboxes -----+
Future agents -------+
```

Requirements:

- Hosted mode requires a configured embedding provider credential owned by the knowledge service.
- Credentials are Railway/service secrets and are never returned to clients or sandboxes.
- Postgres schemas and credentials are isolated from the control plane, even when both initially share one Postgres instance.
- Callers never connect directly to the knowledge database.
- Context responses report `retrieval_mode: semantic_hybrid`, embedding model/version, and index freshness.

### 6.4 Workspace topology

The knowledge service is workspace-level, not repository-level. Multiple repositories and control-plane services may reference one knowledge workspace. Repository identity is derived from canonical external identity, such as normalized Git remote, not a machine-local path.

Additional team members join through a scoped invitation or workspace connection flow. Endpoint identifiers may be committed; bearer credentials remain in Keychain or the Vessica user credential store.

### 6.5 Hosted deployment artifact

Production releases of `vessica-knowledge-server` are distributed as versioned OCI container images built from the GitHub repository by CI and published to the Vessica container registry. The default image location is:

```text
ghcr.io/vessica-labs/vessica-knowledge-server:<version>
```

Normal hosted installation does not clone or build the GitHub repository. `ves railway up` must:

1. Resolve the knowledge-server version compatible with the installed `ves` CLI.
2. Resolve the release tag to an immutable image digest.
3. Locate the workspace-level knowledge service or create a Railway service from that image.
4. Provision or reuse Postgres, enable pgvector, and configure isolated database credentials.
5. Configure workspace/service authentication, the embedding provider credential, migrations, and health checks as Railway secrets and variables.
6. Deploy the pinned image and wait for Railway deployment `SUCCESS` and knowledge-service readiness.
7. Perform the verified SQLite-to-Postgres promotion described below.

The installed configuration records the semantic version and immutable digest:

```yaml
knowledge:
  mode: hosted
  workspace_id: kwsp_...
  service_id: ...
  endpoint: https://...
  version: 0.1.0
  image: ghcr.io/vessica-labs/vessica-knowledge-server@sha256:...
```

The release image should be publicly pullable for the normal open-source installation path so developers do not need to grant Railway access to GitHub source or private registry credentials.

Development and pre-release testing may override the artifact explicitly:

```bash
ves railway up --knowledge-image ghcr.io/vessica-labs/vessica-knowledge-server:dev
ves railway up --knowledge-source /path/to/vessica-knowledge-server
```

Source upload is a development-only path. It must never be selected implicitly for production provisioning.

---

## 7. Core Objects

### 7.1 Entities

Entities represent things that exist, including people, organizations, repositories, projects, products, topics, technologies, events, and locations.

Each entity has:

- Stable canonical ID.
- Extensible type.
- Display name and aliases.
- Metadata.
- External references.
- Scope and lifecycle state.
- Immutable versions and provenance.

### 7.2 Artifacts

Artifacts are authoritative work products such as PRDs, ADRs, specifications, designs, proposals, research reports, implementation plans, and test plans.

Each artifact has:

- Stable ID and artifact type.
- Scope.
- Immutable version number.
- Lifecycle: `draft`, `active`, `superseded`, or `archived`.
- Markdown content and content hash.
- Provenance, source reference, author/actor, and metadata.

Retrieval by identity, type, lifecycle, and version is deterministic.

### 7.3 Memories

V1 memory types are:

- `instruction`: explicit guidance for future behavior.
- `fact`: a durable observation about an entity or scope.
- `decision`: a choice and its rationale.
- `episode`: a concise summary of meaningful activity and outcome.

Each memory has:

- Stable ID and immutable version.
- Scope.
- Optional subject, predicate, and object.
- Title and content.
- Importance and confidence.
- Confidence source: `human_confirmed`, `agent_inferred`, `imported`, `external_system`, or `observed`.
- Provenance and source references.
- Valid-from and valid-until timestamps.
- Lifecycle state and metadata.
- Embedding state: `not_configured`, `pending`, `ready`, or `failed`.

Agents may create facts, decisions, and episodes. Instruction creation must remain explicit in V1.

### 7.4 Relationships

Relationships connect any two entities, artifacts, memories, or external references. Predicates are extensible strings such as `derived_from`, `references`, `implements`, `constrains`, `about`, `produced_by`, and `supersedes`.

Relationships are versioned and scoped and carry provenance and confidence.

### 7.5 Scopes

Scopes form an explicit hierarchy:

```text
global -> organization -> workspace -> project -> repository -> user/agent/task
```

Context requests supply applicable scope IDs. More specific instructions may override broader instructions only when they share a declared semantic key; otherwise both are returned with scope information.

---

## 8. Workflow Knowledge Production

Epics, tickets, runs, sandboxes, receipts, pull requests, and commits remain control-plane work objects. They are not duplicated as first-class knowledge objects. Their meaningful lifecycle events produce knowledge.

### 8.1 Event integration

The control plane publishes durable workflow events to the knowledge ingestion API through an idempotent outbox. Solo runs call the same knowledge application service in-process.

Each event includes:

- Stable event and idempotency IDs.
- Workspace, repository, epic, ticket, run, and actor IDs as applicable.
- Event type and timestamp.
- Summary and structured source references.
- Links to relevant artifacts, receipt, PR, commit, preview, and external ticket.

### 8.2 Required episodes

V1 creates or updates episode memories for meaningful milestones:

- Epic accepted, planned, completed, failed, or cancelled.
- Run completed, failed, approved, merged, rolled back, or cancelled.
- Ticket completed with evidence.
- Material blocker or newly discovered follow-up work.
- Human refinement applied to a retained sandbox.

Routine lease heartbeats, polling, and low-level command events must not create memories.

### 8.3 Work-history relationships

Episodes must reference the applicable:

- Epic and tickets through external source references.
- Generated PRD, ADR, design, implementation plan, and test plan artifacts.
- Run receipt and validation evidence.
- Git repository, branch, commit, and pull request entities/references.
- Linear parent issue and subissues.

This allows questions such as:

- What did we change in authentication last month?
- Why was this architecture selected?
- Which run implemented this feature?
- What failed during the previous migration attempt?
- Which receipt and PR prove the work was completed?

Workflow events may produce observed episodes automatically. Facts and decisions are created only when explicitly emitted by an agent/human or derived from a versioned authoritative artifact with provenance.

---

## 9. Event and Projection Model

The append-only `knowledge_events` stream is the authoritative mutation history. Each event contains:

- Event ID, workspace ID, aggregate type, and aggregate ID.
- Aggregate version and event type.
- Actor and provenance.
- Idempotency key.
- Structured payload and occurrence timestamp.

Transactional projections support queries:

- Entities, aliases, and external references.
- Artifacts and artifact versions.
- Memories, memory versions, and embeddings.
- Relationships.
- Scopes and scope ancestry.
- Background jobs and outbox state.

Every accepted write appends one event and updates its projection in the same database transaction. Replaying the event stream must reproduce the projections.

---

## 10. Retrieval and Context Assembly

### 10.1 Deterministic artifact retrieval

Artifacts are selected by identity, type, lifecycle, version, scope, and entity relationship. Active artifacts are never dependent solely on embedding similarity.

### 10.2 Memory retrieval

Ranking inputs are:

- Semantic similarity when embeddings are available.
- SQLite FTS5/BM25 or Postgres full-text relevance.
- Entity relevance.
- Scope specificity.
- Importance.
- Confidence and confidence source.
- Temporal validity and recency.

The ranking implementation is versioned. Each result includes a retrieval explanation and component scores sufficient for debugging.

### 10.3 Context assembly

Context order is:

1. Active artifacts.
2. Instructions.
3. Relevant entities.
4. Decisions.
5. Facts.
6. Episodes.

The request includes query text, applicable scopes, entity hints, deterministic artifact selectors, and a token budget. The response includes selected content, provenance, source references, ranking explanations, retrieval mode, omissions, and token estimates.

`ves prime` uses this API rather than maintaining separate memory-selection logic.

---

## 11. Embedding Behavior

### Solo mode

- No embedding credential is requested.
- Memories are immediately searchable through FTS5, entities, relationships, scopes, and deterministic metadata.
- `embedding_state` is `not_configured`.
- Retrieval remains fully functional and reports lexical mode.
- The system must not attempt to repurpose Codex login tokens or coding-model sessions as an embeddings API.

### Hosted mode

- An embedding provider credential is required during provisioning.
- Knowledge writes commit before embedding generation.
- An idempotent background job generates the embedding asynchronously.
- Pending or failed embeddings do not hide knowledge from lexical/entity retrieval.
- Embeddings record provider, model, dimensions, content hash, and generation time.
- Content changes enqueue a new embedding for the new immutable version.

Provider and model are configurable implementation details behind a stable interface.

---

## 12. API Requirements

The service exposes authenticated HTTP/JSON endpoints for:

```text
POST /v1/context

POST /v1/entities
GET  /v1/entities/{id}
GET  /v1/entities:resolve

POST /v1/artifacts
GET  /v1/artifacts/{id}
POST /v1/artifacts/{id}:version
POST /v1/artifacts/{id}:activate
POST /v1/artifacts/{id}:supersede

POST /v1/memories
GET  /v1/memories/{id}
POST /v1/memories/{id}:supersede
POST /v1/memories/{id}:archive

POST /v1/relationships
POST /v1/workflow-events

POST /v1/exports
POST /v1/imports
GET  /v1/imports/{id}
```

Requirements:

- Every write requires an idempotency key, actor, and provenance.
- IDs are generated before persistence and remain stable across migration.
- API errors are typed and machine-readable.
- Authentication uses scoped bearer tokens in V1.
- Authorization is enforced by workspace and scope in the service, not by callers.
- Export/import formats are versioned, ordered, checksummed, and resumable.

---

## 13. CLI Requirements

The knowledge layer does not ship a separate `agent-memory` CLI. `ves` exposes:

```bash
ves knowledge status
ves knowledge context
ves knowledge promote
ves knowledge export
ves knowledge import

ves entity create|resolve|search
ves artifact create|get|list|activate|supersede
ves memory add|get|list|search|supersede|archive
```

All agent-facing commands support versioned `--json`, typed errors, `--dry-run` where relevant, confirmations for mutations, and idempotency keys.

The Codex plugin teaches these workflows and always executes `ves`. It does not contain direct API, database, embedding, or Railway logic.

---

## 14. Local-to-Hosted Promotion

`ves railway up` provisions or locates the workspace-level knowledge service and performs a one-way promotion:

1. Acquire an exclusive local knowledge write lock.
2. Resolve and deploy the compatible, digest-pinned knowledge-server release image; provision the Postgres schema, pgvector, credentials, and embedding provider configuration.
3. Export a versioned snapshot with object counts, content hashes, relationship counts, event high-watermark, and checksums.
4. Import idempotently while preserving IDs and versions.
5. Validate counts, hashes, event watermark, and representative context requests.
6. Atomically set `knowledge.mode: hosted`, workspace ID, and endpoint.
7. Release the lock and retain a timestamped read-only SQLite recovery snapshot.

If any step fails, SQLite remains authoritative and the operation can resume. Once promoted, writes never silently fall back to SQLite. A hosted snapshot may be exported for debugging, but V1 does not support switching back to a divergent writable local store.

---

## 15. Security and Operations

- Tokens are scoped to workspace, actor, and permitted operations.
- User credentials are stored in Keychain or the Vessica credential store.
- Service credentials and embedding API keys are Railway secrets.
- Logs and events must redact credentials and sensitive content according to configured policy.
- Health reports distinguish API, database, migration, embedding worker, and index readiness.
- Backups include the event stream and all projection/version tables.
- Export authorization is separate from ordinary read permission.
- Rate limits and maximum artifact/memory sizes are enforced server-side.

---

## 16. Acceptance Scenarios

1. A new solo user installs `ves`, initializes a repository, creates artifacts and memories, and retrieves useful context without an API key or network service.
2. A solo context request returns active artifacts and relevant lexical memories with `retrieval_mode: lexical`.
3. `ves railway up` migrates the store, preserves IDs and hashes, and switches authority only after verification.
4. Two developers and a cloud sandbox retrieve the same hosted context.
5. Hosted writes are immediately available lexically while embeddings are pending, then become semantically ranked when ready.
6. A completed epic produces an episode linked to its artifacts, tickets, run, receipt, commit, PR, and Linear issues.
7. Replaying the same workflow event or import chunk does not duplicate knowledge.
8. A hosted outage fails writes clearly and does not create a local fork.
9. Replaying the event log reconstructs equivalent projections.
10. An agent can answer a work-history question and cite authoritative artifacts and activity evidence.

---

## 17. Future Work

- Optional bundled or user-selected local embedding providers.
- Automatic memory extraction and promotion workflows.
- Conflict detection and human review queues.
- Knowledge-graph reasoning.
- Multi-modal artifacts.
- Real-time replication and offline writes.
- Web administration and knowledge inspection UI.
- Enterprise identity, policy, retention, and audit features.
