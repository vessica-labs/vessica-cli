# Vessica v1 ADR — Architecture for Local-First Harness Engineering CLI

**Document type:** Architecture Decision Record  
**ADR ID:** ADR-001  
**Product:** Vessica CLI  
**Version:** v1 draft  
**Status:** Proposed / Accepted for v1 planning  
**Date:** 2026-07-08

---

## 1. Decision Summary

Vessica v1 will be implemented as a **local-first, CLI-first control plane for agent-driven software development**.

The canonical interface is the `ves` CLI. MCP, hosted UI, and future web APIs are adapters over the same core model, not the primary architecture.

Vessica v1 will:

1. Use a compiled CLI core, preferably Go.
2. Treat the CLI as the stable human and agent contract.
3. Store durable state in SQLite for solo/local mode and Postgres for team/shared mode.
4. Use local Docker as the only v1 sandbox backend.
5. Treat Vessica as the source of truth for epics, artifacts, tickets, runs, memory, and receipts.
6. Sync Linear/Jira best-efforts rather than making them authoritative.
7. Commit harness files and `.vessica` agent pack definitions to the user repo.
8. Pin agent packs for deterministic runs.
9. Execute software workflows as phase-addressable, resumable DAGs.
10. Stream all sandbox and agent output into an append-only event log.
11. Generate preview URLs, draft PRs, and receipts as first-class outputs.
12. Design the object model so arbitrary agents, structured data, custom hosted UIs, remote sandboxes, hosted coordination, schedules, and inbox can be added in v2 without replacing v1 foundations.

---

## 2. Context

Coding agents are increasingly capable, but production use is limited by poor surrounding infrastructure:

- They lack durable memory.
- They lack repo-specific architecture and design harnesses.
- They have no reliable multi-agent ticket claim/lease model.
- They do not naturally produce PRDs, ADRs, test scenarios, and receipts.
- They need safe, observable, resumable execution environments.
- Developers need to inspect, preview, and trust outputs before merging.

Vessica addresses the operating layer around coding agents. It does not compete with Codex, Claude Code, Cursor, Pi, or similar coding tools. It orchestrates them.

---

## 3. Decision Drivers

### 3.1 Developer Experience

- Install should be simple.
- Commands should be memorable and scriptable.
- Agents should be able to invoke the same commands humans use.
- The system should work locally before hosted infrastructure exists.
- Logs and progress should be visible in the terminal.

### 3.2 Agent Compatibility

- Current coding agents are good at shell/CLI usage.
- A CLI interface has lower context overhead than exposing a large MCP schema.
- `ves prime` can provide concise current context without injecting every command into the model prompt.
- JSON output makes commands safe for agent consumption.

### 3.3 Determinism

- Agent packs must be pinned.
- Harness files must be committed.
- Runs must be phase-addressable.
- State changes must be recorded.
- Events must be append-only.
- Ticket claims must be atomic and lease-based.

### 3.4 Future Hosted Product

v1 must not block:

- Managed Postgres.
- Remote sandboxes.
- Hosted run coordinator.
- Web UI.
- Custom agent UI packages.
- Structured agent data.
- Agent schedules.
- Human inbox.
- Team permissions.
- Receipts dashboard.

---

## 4. Architecture Overview

```text
Human Developer / Coding Agent
              │
              ▼
          ves CLI
              │
              ▼
       Vessica Core
              │
 ┌────────────┼─────────────┬─────────────┬──────────────┐
 ▼            ▼             ▼             ▼              ▼
State      Harness       Run Engine     Sandbox       Integrations
DB         + Packs       + Events       Docker        GitHub/Linear/etc.
              │             │             │
              ▼             ▼             ▼
         Repo Files      Receipts      Preview + PR
```

### 4.1 Core Modules

```text
cmd/ves                 CLI commands
internal/config         workspace/user config
internal/state          SQLite/Postgres persistence
internal/id             ID generation and idempotency
internal/auth           provider auth and credential injection
internal/pack           agent pack install/pin/update
internal/harness        harness create/audit/sync/lint
internal/artifact       PRD/ADR/DesignSpec/TestScenarios
internal/ticket         ticket graph, dependencies, claims, leases
internal/memory         memory storage/search
internal/run            workflow orchestration and phases
internal/sandbox        Docker sandbox lifecycle
internal/event          append-only event log and streaming
internal/runner         coding runner abstraction
internal/repo           Git/GitHub/GitLab integration
internal/tracker        Linear/Jira best-efforts sync
internal/receipt        receipt generation
internal/redaction      secret redaction
```

---

## 5. Major Decisions

## Decision 1 — CLI-first, not MCP-first

### Decision

The `ves` CLI is the canonical interface for humans and agents. MCP may be added as a thin adapter but will not define the core architecture.

### Rationale

- Coding agents already invoke shell commands reliably.
- CLI commands are more compact to teach through `ves prime` and setup guidance.
- Developers need the same interface that agents use.
- The CLI can be versioned, scripted, tested, and installed independently.
- MCP is useful for MCP-only hosts and external tool discovery, but Vessica is a local project operating layer.

### Consequences

Positive:

- Lower context overhead.
- Easier terminal UX.
- Easier agent skill/hook integration.
- Better local reliability.
- Clear contract for v1.

Negative:

- MCP-first environments need an adapter.
- CLI command design must be strict and machine-safe.
- Shell execution requires permission and safety controls.

### Implementation Requirements

- Every agent-facing command supports `--json`.
- `ves prime` teaches the minimum command grammar.
- `ves setup codex|claude|cursor|pi` installs managed guidance.
- Optional `ves mcp serve` can wrap a small set of core operations later.

---

## Decision 2 — Use Go for the CLI core

### Decision

Implement the Vessica CLI core in Go unless a later prototype proves a different language materially better.

### Rationale

Go is a strong fit for:

- Native single-binary distribution.
- Fast startup.
- Cross-platform CLI behavior.
- Docker, Git, subprocess, and filesystem orchestration.
- Local daemons/event streaming.
- Concurrency for runs, event streams, and agents.
- Lower operational friction than Python or Node for a trusted local developer tool.

### Consequences

Positive:

- Boring install story.
- Good performance.
- Good concurrency.
- Easier enterprise trust posture.

Negative:

- AI SDK ecosystem is richer in TypeScript/Python.
- Plugin authors may prefer TS/Python.
- Some UI tooling is less natural.

### Mitigation

Do not make Go the extension surface. Use CLI, JSON, MCP, subprocess protocols, and future SDKs for extension. Agent packs, prompts, workflows, and templates are language-neutral repo artifacts.

---

## Decision 3 — SQLite for solo mode, Postgres for shared/team mode

### Decision

Vessica v1 supports SQLite for solo/local mode and Postgres URL for shared/team mode.

### Rationale

SQLite is useful for:

- Easy local install.
- Solo developer workflows.
- Demos.
- Early testing.

Postgres is required for:

- Multiple developers.
- Multiple sandboxes.
- Reliable concurrent claims.
- Network-accessible state from Docker and future remote sandboxes.
- Hosted service compatibility.

### Consequences

Positive:

- Easy entry path.
- Clear upgrade path.
- Shared state model exists from v1.

Negative:

- Two backends increase implementation complexity.
- SQLite can create false confidence if users attempt team workflows.

### Mitigation

`ves doctor` and workflow preflight must warn when SQLite is used for multi-agent/team scenarios.

---

## Decision 4 — Local Docker only for v1 sandboxes

### Decision

Vessica v1 supports local Docker sandboxes only. Remote sandboxes are v2.

### Rationale

Local Docker is enough to validate:

- Repo clone.
- Runner execution.
- Event streaming.
- Preview tunnel.
- Ticket claiming.
- Build/validation loop.
- PR creation.

Remote sandbox backends add complexity in auth, networking, state access, previews, logs, and lifecycle management.

### Consequences

Positive:

- Faster v1.
- Easier debugging.
- Lower infra burden.
- Local-first credibility.

Negative:

- Less scalable.
- Not ideal for large teams.
- Harder to run from thin clients.
- Requires Docker.

### Mitigation

Define `sandbox` interface now:

```text
Create
Start
Exec
Stream
ExposePort
Destroy
Status
```

Add remote backends in v2 without changing run engine semantics.

---

## Decision 5 — Vessica is source of truth; trackers are mirrors

### Decision

Vessica owns epics, tickets, artifacts, claims, waves, and run state. Linear/Jira are best-efforts mirrors.

### Rationale

Vessica requires semantics not native to Linear/Jira:

- Agent claims and leases.
- Ticket waves.
- Artifact sets.
- Run evidence.
- Receipt linkage.
- Memory references.
- Agent provenance.

Making Linear/Jira authoritative would make Vessica dependent on external data models and conflict behaviors.

### Consequences

Positive:

- Clean internal model.
- Reliable agent coordination.
- Best-efforts external visibility.

Negative:

- Teams with strict Jira/Linear governance may need deeper sync later.
- Conflict handling must be surfaced.

### Mitigation

Store external mappings and sync status:

```text
external_provider
external_id
sync_status
last_synced_at
last_error
```

---

## Decision 6 — Commit harness files and agent pack definitions

### Decision

Harness files and `.vessica` agent pack definitions are committed to the repo. Runtime state, caches, secrets, runs, and sandboxes are not.

### Rationale

Committed definitions provide:

- Reviewability.
- Determinism.
- Team sharing.
- Reproducibility.
- Git history.
- PR-based changes to agent behavior.

### Commit

```text
AGENTS.md
ARCHITECTURE.md
DESIGN.md
DEPLOY.md
TESTING.md
SECURITY.md
.vessica/config.yaml
.vessica/harness.yaml
.vessica/pack.lock
.vessica/agents/
.vessica/templates/
.vessica/workflows/
.vessica/lint-arch.*
```

### Do Not Commit

```text
.vessica/cache/
.vessica/state/
.vessica/runs/
.vessica/sandboxes/
.vessica/secrets/
```

### Consequences

Positive:

- Agent behavior becomes infra-as-code.
- Users can fork and modify packs.
- Runs are deterministic against pack lock.

Negative:

- Repo receives more files.
- Users may edit generated files incorrectly.

### Mitigation

Use managed sections, validation, `ves harness audit`, and `ves pack sync`.

---

## Decision 7 — Agent packs are pinned and never silently updated

### Decision

Vessica runs use the agent pack pinned in `.vessica/pack.lock`. Vessica does not silently pull latest pack definitions during runs.

### Rationale

Autonomous code execution must be reproducible. Silent prompt/template/hook updates create non-deterministic behavior and destroy debuggability.

### Consequences

Positive:

- Reproducible runs.
- Safer enterprise behavior.
- Pack changes can be reviewed in PRs.

Negative:

- Users must manually update packs.
- Bugs in old packs persist until updated.

### Mitigation

`ves pack update` and `ves doctor` can warn when newer pack versions are available.

---

## Decision 8 — Software workflow is a phase-addressable DAG

### Decision

The epic workflow is modeled as phases in a resumable DAG.

Phases:

```text
preflight
harness
plan
design
ticketize
code
build
validate
preview
pr
receipt
```

### Rationale

Users need both:

- A continuous low-latency flow.
- The ability to restart after failure or skip completed planning.

### Commands

```bash
ves run epic epic_abc123
ves run epic epic_abc123 --stop-after ticketize
ves run epic epic_abc123 --start-at code --reuse-artifacts approved
ves run resume run_abc123 --from validate
```

### Consequences

Positive:

- Resumability.
- Faster restarts.
- Reuse of approved artifacts.
- Cleaner debugging.

Negative:

- Requires explicit phase state.
- Requires artifact sets.
- Requires careful idempotency.

### Mitigation

Use run phase records, artifact sets, and idempotency keys.

---

## Decision 9 — Planning outputs are artifacts, not loose memories

### Decision

PRDs, ADRs, DesignSpecs, and TestScenarios are first-class artifacts. They are indexed into memory but are not merely memories.

### Rationale

Artifacts need:

- Versioning.
- Approval.
- Source run.
- Source epic.
- Downstream ticket relationships.
- Artifact set grouping.
- Diffing.
- Reuse across runs.

Memory alone is too weak for authoritative lifecycle documents.

### Consequences

Positive:

- Cleaner lifecycle.
- Reusable planning packages.
- Better audit and receipts.

Negative:

- More object types.
- More commands.

### Mitigation

Use `ves artifact ...` as the general interface and keep convenience commands minimal.

---

## Decision 10 — Ticket claiming is atomic and lease-based

### Decision

Agents claim tickets atomically with explicit leases and heartbeats.

### Rationale

Multiple agents may work in parallel. Without atomic claims, duplicate work and conflicts will happen.

### Required Commands

```bash
ves ticket claim --next --epic <epic_id> --agent <agent_id> --lease 45m
ves ticket heartbeat <ticket_id> --agent <agent_id>
ves ticket release <ticket_id> --agent <agent_id> --reason <reason>
ves ticket close <ticket_id> --agent <agent_id> --evidence <receipt_id>
```

### Consequences

Positive:

- Safe multi-agent execution.
- Clear ownership.
- Recoverability after crashed agents.

Negative:

- Requires robust DB transactions.
- SQLite support is limited for higher concurrency.

### Mitigation

Require Postgres for real team/multi-agent shared scenarios. SQLite remains solo mode.

---

## Decision 11 — Event log is the universal run substrate

### Decision

All sandbox output, agent progress, tool execution, tests, validation steps, cost updates, preview status, and PR events are persisted as append-only events.

### Rationale

One event substrate powers:

- Live CLI streaming.
- Later hosted UI streaming.
- Logs.
- Receipts.
- Debugging.
- Resume.
- Audit.
- ROI analytics.

### Consequences

Positive:

- Strong observability.
- Future UI-ready.
- Better receipts.

Negative:

- More data volume.
- Requires redaction.
- Requires retention policies.

### Mitigation

Implement event types, retention config, and redaction pipeline in v1.

---

## Decision 12 — Stream model-visible output, not hidden reasoning

### Decision

Vessica streams agent progress, model-visible messages, tool output, logs, and structured events. Hidden reasoning is not part of the product contract.

### Rationale

Providers differ in what they expose. Hidden reasoning should not be treated as product data. Users need useful progress, not private chain-of-thought.

### Consequences

Positive:

- Provider-independent.
- Safer.
- Clear user expectations.

Negative:

- Some users may expect full model thought trace.

### Mitigation

Show rich progress events and tool outputs.

---

## Decision 13 — Preview is first-class

### Decision

Runs produce preview URLs from sandboxes before or alongside PR creation.

### Rationale

Preview is the trust bridge for human review. A PR diff is not enough for product work.

### Consequences

Positive:

- Faster human validation.
- Strong demo value.
- Hosted UI path.
- Preview-enabled runs start the development server at the beginning of the code phase and keep it attached to the integration checkout.

Negative:

- Requires app startup detection and port handling.
- Some repos will not be previewable without config.

### Mitigation

Use harness `DEPLOY.md` and `.vessica/harness.yaml` to define preview commands and ports. Standardize Node execution on pnpm through Corepack, normalize legacy npm/npx harness commands at runtime, resolve Vite host/port settings and Node watch mode, and defer an unhealthy base preview until the post-build validation retry.

---

## Decision 14 — Receipts are first-class outputs

### Decision

Every run produces a receipt.

### Rationale

Receipts create accountability for autonomous work:

- What was done.
- Which agents/models did it.
- What it cost.
- What passed.
- What failed.
- What changed.
- What human input was needed.
- What value was returned.

### Consequences

Positive:

- Trust.
- Debuggability.
- ROI measurement.
- Future hosted dashboard.

Negative:

- Requires consistent cost/model capture across runners.

### Mitigation

Capture what is available. Mark unavailable metrics explicitly rather than guessing.

---

## Decision 15 — General agent runtime is designed, not implemented, in v1

### Decision

v1 focuses on software harnessing. The object model reserves space for arbitrary agents, structured data, custom UI packages, tasks, schedules, and inbox, but these are out of scope for implementation.

### Rationale

The software harness is the wedge. Building the general hosted runtime first would dilute v1 and delay learning.

### Consequences

Positive:

- Focused v1.
- Future-compatible architecture.
- Stronger initial product.

Negative:

- Some schema fields remain future-facing.
- Users cannot build arbitrary scheduled agents in v1.

### Mitigation

Document v2 separately and avoid hardcoding software concepts into the core run/task/artifact/event model.

---

## 6. Data Architecture

### 6.1 Entity List

Minimum v1 entities:

```text
Workspace
Config
ProviderAuth
Pack
HarnessStatus
Epic
Artifact
ArtifactVersion
ArtifactSet
Ticket
TicketDependency
Wave
Claim
Memory
Run
RunPhase
Sandbox
Event
Receipt
Trace
ExternalMapping
```

### 6.2 ID Strategy

Use type-prefixed unique IDs.

Examples:

```text
epic_8f3k2p9q
tix_3p8x1q9d
run_5n2a8k7r
evt_7h2k9d1p
```

Rules:

- Mutable entities use generated unique IDs.
- Immutable versions may use content hashes.
- Idempotency uses explicit idempotency keys.
- IDs must be safe for CLI, URLs, filenames, and JSON.

### 6.3 State Backend Rules

- All state mutations go through Vessica core.
- Ticket claims use transactions.
- Event writes are append-only.
- Receipts are immutable after finalization, except annotations if explicitly supported.
- SQLite is marked solo.
- Postgres is required for shared/team coordination.

---

## 7. Execution Architecture

### 7.1 Run Engine

The run engine owns:

- Workflow phase graph.
- Phase transitions.
- Sandbox provisioning.
- Runner invocation.
- Event stream.
- Artifacts.
- Ticket coordination.
- Resume.
- Cancellation.
- Receipts.

### 7.2 Runner Interface

Runner abstraction:

```text
Prepare(context)
Start(task)
StreamEvents()
Cancel()
CollectResult()
```

Runner inputs:

- Repo path
- Phase
- Instructions
- Artifact context
- Ticket context
- Harness context
- Budget
- Permissions
- Environment

Runner outputs:

- Events
- File changes
- Artifacts
- Status
- Evidence
- Cost metadata if available

### 7.3 Sandbox Interface

Sandbox abstraction:

```text
Create
Start
Exec
Stream
ExposePort
Destroy
Status
```

v1 implementation:

```text
DockerSandbox
```

v2 implementations:

```text
RunloopSandbox
RailwaySandbox
HostedSandbox
CustomSandbox
```

### 7.4 Preview

Preview uses sandbox port exposure.

Inputs from harness:

```yaml
preview:
  command: pnpm run dev
  port: 3000
  healthcheck: http://localhost:3000/health
```

### 7.5 PR Creation

PR creation is a repo integration action, not a sandbox action.

Flow:

1. Sandbox pushes branch.
2. Vessica repo module opens draft PR.
3. Receipt and PR body include links.

---

## 8. Security Architecture

### 8.1 Secret Handling

- Do not commit secrets.
- Do not persist raw provider tokens in repo.
- Redact secrets from logs and events.
- Inject only scoped credentials into sandbox.
- Prefer short-lived credentials where possible.
- Never expose DB URL in streamed logs.

### 8.2 Destructive Actions

Commands such as delete/destroy/cancel require confirmation unless `--yes`.

### 8.3 Sandbox Safety

v1 Docker sandbox is not a strong security boundary against malicious code. The product should be clear: it is an isolation and reproducibility mechanism, not enterprise-grade containment.

### 8.4 Audit

Events, receipts, and traces provide auditability.

---

## 9. Configuration Architecture

Precedence:

1. CLI flags
2. Environment variables
3. Workspace config
4. User config
5. Defaults

Workspace config:

```yaml
state:
  backend: sqlite
  db_url: null

sandbox:
  backend: docker

runner:
  default: codex

repo:
  provider: github
  remote: git@github.com:org/repo.git

tracker:
  provider: linear
  mode: best_efforts

pack:
  lockfile: .vessica/pack.lock
```

---

## 10. Directory Architecture

```text
repo/
  AGENTS.md
  ARCHITECTURE.md
  DESIGN.md
  DEPLOY.md
  TESTING.md
  SECURITY.md
  .vessica/
    config.yaml
    harness.yaml
    pack.lock
    agents/
      product/
      architect/
      qa/
      design/
      coder/
      build/
      validator/
    templates/
      prd.md
      adr.md
      design-spec.md
      test-scenarios.md
    workflows/
      software_epic.yaml
    lint-arch.*
    cache/       # gitignored
    state/       # gitignored
    runs/        # gitignored
    secrets/     # gitignored
```

---

## 11. Alternatives Considered

### Alternative A — MCP-first

Rejected for v1.

Reason:

- Too much schema/context overhead for the core use case.
- Less natural for human terminal users.
- Vessica is a local project operating layer, not primarily an external tool discovery surface.

MCP remains a future adapter.

### Alternative B — TypeScript CLI

Rejected as default for v1 core.

Reason:

- Easier AI SDK access, but weaker native install and trusted local orchestration posture.
- Better as an extension/UI layer later.

### Alternative C — Python CLI

Rejected.

Reason:

- Packaging and environment fragility.
- Slower startup.
- Dependency issues.

Python remains useful for future eval scripts and plugins.

### Alternative D — Remote sandbox v1

Rejected.

Reason:

- Adds network, auth, preview, lifecycle, and hosted coordination complexity before local loop is proven.

### Alternative E — Linear/Jira source of truth

Rejected.

Reason:

- External trackers do not natively model Vessica claims, leases, waves, artifacts, receipts, and memory.

---

## 12. Out of Scope for v1 Architecture

- Hosted service.
- Hosted run coordinator.
- Remote sandbox backends.
- Custom hosted UI rendering.
- General arbitrary agent runtime.
- Structured agent data collections.
- Scheduler/cron.
- Inbox.
- Enterprise RBAC/SSO.
- Marketplace.
- Full MCP tool ecosystem.
- Production deployment automation.
- Strong sandbox security guarantees.
- Full Linear/Jira bidirectional conflict resolution.

---

## 13. Risks and Mitigations

### Risk: v1 scope becomes too broad

Mitigation:

- Prioritize local software harness loop.
- Defer general agent runtime.
- Defer hosted UI.
- Defer remote sandboxes.

### Risk: Runner integration variability

Mitigation:

- Define runner abstraction.
- Start with one runner fully.
- Treat others as config/setup targets until proven.

### Risk: Agents produce noisy or low-quality artifacts

Mitigation:

- Use templates.
- Use artifact approval.
- Use evals.
- Use receipts and validation.

### Risk: Docker preview fails for many repos

Mitigation:

- Require harness preview config.
- Surface clear errors.
- Make preview optional.

### Risk: Event logs leak secrets

Mitigation:

- Redaction pipeline before persistence and streaming.
- Avoid raw env dumps.
- Secret pattern detection.

### Risk: SQLite misused for team workflows

Mitigation:

- Warnings.
- Doctor checks.
- Require Postgres for shared mode.

---

## 14. Acceptance Criteria for Architecture

The architecture is successful when:

1. A solo developer can run the full loop locally with SQLite and Docker.
2. A team can point Vessica at Postgres and coordinate through shared state.
3. A coding agent can use the CLI without MCP.
4. Runs are visible live through streamed events.
5. Planning can be run separately from coding.
6. Failed runs can resume from phase boundaries.
7. A run produces preview, PR, and receipt.
8. The object model can support v2 arbitrary agents without replacing the core.

---

## 15. Implementation Order

Recommended order:

1. CLI skeleton and config.
2. State schema and repositories.
3. IDs, idempotency, transactions.
4. Epics, artifacts, tickets, claims.
5. Memory and prime.
6. Pack and harness.
7. Docker sandbox.
8. Event log and streaming.
9. Run engine.
10. Runner integration.
11. Planning workflow.
12. Coding/build/validation workflow.
13. Preview.
14. GitHub PR.
15. Receipts.
16. Setup for coding agents.
17. Tracker sync.
18. Hardening.

---

## 16. Future Compatibility Notes

v1 should reserve but not fully implement:

```json
{
  "agent": {
    "definition": {},
    "workflow": {},
    "data": {},
    "ui": {},
    "eval": {},
    "schedule": {},
    "inbox": {}
  }
}
```

This ensures v2 can add general agents without rethinking the run/event/artifact/memory model.

---

## 17. Final Decision

Proceed with a Go-based, CLI-first, local-first Vessica v1 focused on software harness engineering, backed by SQLite/Postgres state, local Docker sandboxes, committed agent packs/harness files, phase-addressable runs, append-only event streaming, preview, PR creation, and receipts. Defer hosted, remote, arbitrary-agent, custom-UI, schedule, inbox, and enterprise capabilities to v2.
