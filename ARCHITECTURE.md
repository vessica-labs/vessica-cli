# Vessica CLI architecture

Vessica is a local-first Go application with an optional hosted control plane. The same domain and use-case code serves the CLI, dashboard, and hosted HTTP adapters; transport handlers must not reimplement lifecycle rules.

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

- Event sequence allocation and ticket claims are database-atomic.
- A state mutation is not successful if its required audit/event write failed.
- Hosted processes verify the schema but never migrate it. `ves control-plane migrate` is the sole hosted migration role and holds a Postgres advisory lock.
- Database pool sizes are explicit and bounded through `VES_DB_*` settings.
- Request and command contexts propagate into database, runner, sandbox, and network calls.

## Hosted topology and current scale constraint

The hosted deployment currently supports exactly one control-plane replica. A database lease rejects a second replica from the same Railway deployment. Different deployment IDs may briefly overlap only for a controlled rolling-deployment handoff; the prior process detects lease loss and shuts down.

Railway worker sandboxes are horizontally scalable and separate from the singleton control plane. Agent processes run as an unprivileged user and receive an allowlisted environment rather than the control plane's environment.

All repository-controlled execution—agent tools, build, validation, and preview—runs across that unprivileged boundary in hosted workers. Git metadata is not writable by the agent, and privileged orchestration Git commands disable repository hooks.

Before enabling multiple control-plane replicas, replace process-local preview/stream coordination, audit every singleton loop, introduce distributed ownership for scheduled work, and test all claims and projections under concurrent Postgres writers.

## Maintainability constraints

Go source files have a hard limit of 800 lines and a soft warning at 500. Split by cohesive responsibility, not arbitrary numeric chunks. New behavior belongs in a shared use-case service when more than one adapter needs it. Errors at state and external-system boundaries must be wrapped with operation context and propagated unless the behavior is explicitly best-effort and documented.

Architectural decisions that change these invariants require an ADR in `docs/` and corresponding updates to this file, `SECURITY.md`, and the engineering harness.
