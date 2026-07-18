# Vessica CLI architecture

Vessica is a hosted-first Go application. `ves up` attaches repositories to one durable control plane per Railway workspace; local SQLite and Docker adapters exist only for explicit development and testing. The same domain and use-case code serves the CLI, dashboard, and hosted HTTP adapters; transport handlers must not reimplement lifecycle rules.

## Package boundaries

- `cmd/ves` is the composition root and contains no business logic.
- `internal/cli`, `internal/dashboard`, and `internal/controlplane` are adapters. They parse input, authorize requests, invoke use cases, and render output.
- `internal/app` owns use cases shared across adapters, including run and sandbox lifecycle operations.
- `internal/run` orchestrates the durable workflow phases. Phase implementations are split by responsibility; the engine coordinates them.
- `internal/state` is the persistence boundary. All concurrent claims, event sequences, leases, and state transitions must be atomic at this layer.
- `internal/runner`, `internal/sandbox`, `internal/tracker`, `internal/repo`, and `internal/knowledgegateway` are infrastructure adapters.
- `web/dashboard` is the React dashboard. Generated assets under `internal/dashboard/assets` must be reproducible from it.

Dependencies point inward: adapters may depend on use cases and infrastructure interfaces; use cases must not depend on CLI, dashboard, or control-plane packages. State must not call transport or workflow packages.

## Durable-state invariants

Postgres or SQLite is the system of record for workflow state. Knowledge artifacts and memories are authoritative in the configured knowledge service. Process memory is only for disposable coordination such as active streams and preview connections.

Hosted work is scoped by workspace and repository. Runs carry `repository_id`; launchers resolve the repository remote from durable state rather than a service-wide environment variable. A Vessica workspace may contain many repositories, while repository-owned epics, runs, artifacts, mappings, and jobs must never cross that boundary.

- Event sequence allocation and ticket claims are database-atomic.
- A state mutation is not successful if its required audit/event write failed.
- Hosted processes verify the schema but never migrate it. `ves control-plane migrate` is the sole hosted migration role and holds a Postgres advisory lock.
- Database pool sizes are explicit and bounded through `VES_DB_*` settings.
- Request and command contexts propagate into database, runner, sandbox, and network calls.

## Hosted topology and current scale constraint

Each Railway installation has one managed Postgres service and two logical databases. `vessica_control` is owned by `vessica_control_user`; `vessica_knowledge` is owned by `vessica_knowledge_user`. The services receive separate credentials and URLs, maintain independent migration histories, and never query across the database boundary. pgvector is installed only in `vessica_knowledge`.

The hosted deployment currently supports exactly one control-plane replica. A database lease rejects a second replica from the same Railway deployment. Different deployment IDs may briefly overlap only for a controlled rolling-deployment handoff; the prior process detects lease loss and shuts down.

Hosted public previews use loopback-only Railway sandbox forwards owned by the singleton control plane and exposed through run-scoped broker capabilities. A dedicated public preview-edge service carries preview requests to the broker over Railway's private network with a service-to-service secret; dashboard and API routes remain on the separate control-plane origin. The official Railway CLI session and generated forwarding identity are encrypted in Postgres, materialized into the control-plane user's private home after restart, and never passed to workers. CLI refresh-token rotation relies on its file lock, so this ownership must be redesigned before multi-replica operation.

Railway worker sandboxes are horizontally scalable and separate from the singleton control plane. Agent processes run as an unprivileged user and receive an allowlisted environment rather than the control plane's environment.

All repository-controlled execution—agent tools, build, validation, and preview—runs across that unprivileged boundary in hosted workers. Git metadata is not writable by the agent, privileged orchestration Git commands disable repository hooks, and each generated worktree is registered as a single exact `safe.directory` for the agent before Codex starts. Wildcard trust is forbidden.

Repository checkpoints are the warm source of toolchains, the checkout, and dependencies. Ticket worktrees project a baked `node_modules` tree with a copy-on-write reflink when the filesystem supports it, then try an offline package-manager reconstruction, and only then use the ordinary install contract. Each result is emitted as an infrastructure stage so receipts distinguish projection, offline reconstruction, and network fallback.

The planning call may return a validated single ticket for `xs` work. The durable ticketization phase reuses that result instead of making a second model call; larger work retains dependency-aware ticketization. Coding agents receive a bounded, version-matched context packet containing current-run planning artifacts, relevant CLI/receipt contracts, and focused validation guidance. The engine—not the coding agent—owns repository-wide build, lint, test, preview, and receipt gates.

Engine-managed Codex calls use a minimal MCP profile. Enabled MCP servers are discovered once per worker identity and disabled per invocation unless named in `VES_CODEX_MCP_ALLOWLIST`; this policy changes agent startup configuration without mutating the user's Codex configuration.

Epic lifecycle state follows terminal run truth: planning produces `planned`, successful draft-PR runs produce `in_review`, fully terminal runs or approved merges produce `completed`, and failure, cancellation, or rollback produce their corresponding terminal epic state.

Before enabling multiple control-plane replicas, replace process-local preview/stream coordination, audit every singleton loop, introduce distributed ownership for scheduled work, and test all claims and projections under concurrent Postgres writers.

## Maintainability constraints

Go source files have a hard limit of 800 lines and a soft warning at 500. Split by cohesive responsibility, not arbitrary numeric chunks. New behavior belongs in a shared use-case service when more than one adapter needs it. Errors at state and external-system boundaries must be wrapped with operation context and propagated unless the behavior is explicitly best-effort and documented.

Architectural decisions that change these invariants require an ADR in `docs/` and corresponding updates to this file, `SECURITY.md`, and the engineering harness.
