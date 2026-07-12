# Vessica v1 PRD — Local-First Harness Engineering CLI

**Product:** Vessica CLI  
**Canonical command:** `ves`  
**Package/repo name:** `vessica-cli`  
**Document type:** Product Requirements Document  
**Version:** v1 draft  
**Primary audience:** Engineering team building Vessica v1  
**Status:** Build-ready draft  
**Last updated:** 2026-07-08

---

## 1. Executive Summary

Vessica v1 is a local-first, developer-facing CLI for agent-driven software development. It initializes a repository with a durable harness, manages epics, planning artifacts, tickets, memory, and execution runs, and coordinates coding agents inside local Docker sandboxes. The CLI is the canonical interface for both humans and coding agents. It provides deterministic lifecycle commands, persistent state, machine-safe JSON output, event streaming, preview URLs, PR creation, and receipts.

The v1 wedge is **harness engineering for real software repositories**:

1. Create or reconcile a repo harness: `AGENTS.md`, `ARCHITECTURE.md`, `DESIGN.md`, `DEPLOY.md`, `TESTING.md`, deterministic architecture lint rules, agent packs, templates, and hooks.
2. Convert user-articulated epics into PRDs, ADRs, DesignSpecs, TestScenarios, and a dependency-aware ticket graph.
3. Run coding agents in Docker sandboxes to claim tickets, code with TDD, build, validate with browser automation, open a PR, and produce a receipt.
4. Persist all durable knowledge and artifacts in Vessica state so humans and agents can resume, coordinate, and audit work.

Vessica v1 is software-development-first. It deliberately does **not** attempt to fully implement the future general-purpose hosted agent runtime, custom hosted UIs, remote sandboxes, enterprise RBAC, or full MCP ecosystem. However, the v1 object model must be designed so those v2 capabilities can be added without a rewrite.

---

## 2. Product Thesis

Modern coding agents can write useful code, but the practical bottleneck is not raw model capability. The bottleneck is the absence of a durable operating layer around the work:

- Agents forget product context between sessions.
- Repositories lack explicit harnesses that encode architecture, design, deployment, testing, and constraints.
- Epics do not automatically become PRDs, ADRs, test plans, and dependency-aware tickets.
- Multiple agents cannot reliably coordinate work without claims, leases, waves, and state.
- Humans lack a live view of what agents are doing, what they changed, what passed, what failed, and what it cost.
- Existing coding CLIs are powerful but need a structured project memory and lifecycle controller.

Vessica v1 provides that operating layer.

---

## 3. Goals

### 3.1 Product Goals

Vessica v1 must allow a developer to:

1. Initialize a repo with Vessica state, config, harness, agent pack, and coding-tool setup.
2. Generate or reconcile a software harness for a new or existing repo.
3. Create and plan epics into durable artifacts and tickets.
4. Run coding agents in local Docker sandboxes against ready tickets.
5. Stream live run output to the terminal.
6. Preview the running application from the sandbox.
7. Open a draft PR with validated changes.
8. Produce a receipt with artifacts, logs, traces, costs, model usage, test results, validation results, and Return on Intelligence inputs.
9. Resume failed or partial runs from a known phase.
10. Allow coding agents to use `ves` directly to prime context, claim work, update memory, and close tickets.

### 3.2 Strategic Goals

Vessica v1 should establish foundations for:

- Team coordination through networked Postgres state.
- Hosted service with managed state, scheduler, run coordinator, preview broker, and UI.
- Arbitrary agent definitions with structured data and custom hosted UIs.
- Agent-to-agent tasking and human inbox.
- Remote sandbox backends.
- Richer integrations with Linear, Jira, GitHub, GitLab, MCP, and model runners.

### 3.3 Non-Goals

Vessica v1 is not:

- A general-purpose hosted agent platform.
- A replacement for GitHub, GitLab, Linear, or Jira.
- A full MCP-first tool ecosystem.
- A cloud sandbox service.
- A visual workflow builder.
- A SaaS team dashboard.
- A full enterprise governance, billing, or RBAC product.
- A proprietary coding model or coding agent.

---

## 4. Users and Personas

### 4.1 Solo Developer / Founder

A technical founder or individual developer wants to use coding agents safely against an existing repo.

Needs:

- Simple local setup.
- Repo harness generation.
- Durable memory and tickets.
- Docker sandbox execution.
- Preview URL.
- PR output.
- Transparent logs and receipts.

Default mode:

```bash
ves init --profile solo --state sqlite --sandbox docker --runner codex --repo github
```

### 4.2 Engineering Lead / Team Developer

A developer or lead wants multiple humans and agents to coordinate on the same repo.

Needs:

- Shared state through networked Postgres.
- Atomic ticket claims and leases.
- Best-efforts Linear/Jira sync.
- Shared memory and artifacts.
- Reproducible agent packs committed to the repo.

Default mode:

```bash
ves init --profile team --state postgres-url --db-url "$DATABASE_URL" --sandbox docker --runner codex --tracker linear --repo github
```

### 4.3 Coding Agent

A coding agent running inside Codex, Claude Code, Cursor, Pi, or another harness needs a minimal, deterministic interface.

Needs:

- Prime context.
- Discover ready tickets.
- Claim with a lease.
- Persist insights.
- Close tickets with evidence.
- Emit structured output.
- Avoid ad hoc TODO, plan, and memory files.

Expected commands:

```bash
ves prime --json
ves ticket ready --json
ves ticket claim --next --epic epic_abc123 --agent agent_coder_1 --lease 45m --json
ves memory add --stdin --json
ves ticket close tix_abc123 --agent agent_coder_1 --evidence rcpt_abc123 --json
```

---

## 5. Scope Summary

### 5.1 In Scope for v1

Vessica v1 includes:

1. CLI core and command taxonomy.
2. Local workspace initialization.
3. State backends:
   - SQLite for solo/local mode.
   - Postgres URL for team/manual shared mode.
   - Optional Postgres Docker helper if low-effort.
4. Local Docker sandbox backend.
5. Runner abstraction for at least one coding CLI, with design support for Codex, Claude, Cursor, and Pi.
6. GitHub repo integration for remote clone and PR creation.
7. Linear/Jira best-efforts tracker sync design; at least one tracker integration may be minimal or stubbed in v1 depending on build capacity.
8. Vessica as source of truth for epics, tickets, artifacts, memory, runs, receipts, and traces.
9. Harness create/audit/sync/lint/status.
10. Agent pack install/pin/update mechanics, committed to repo.
11. Epics, artifacts, tickets, waves.
12. Phase-addressable run engine.
13. Local Docker sandboxes with event streaming.
14. Preview tunneling from sandbox.
15. Draft PR creation.
16. Receipts and traces.
17. CLI setup for Codex/Claude-style agent use.
18. JSON output for all agent-facing commands.
19. Append-only event log.
20. Redaction of secrets from persisted and streamed output.

### 5.2 Out of Scope for v1

Vessica v1 excludes:

1. Hosted Vessica service.
2. Managed Postgres.
3. Remote sandbox backends such as Railway or Runloop.
4. Scheduler and cron agent runs.
5. General-purpose arbitrary agent runtime.
6. Agent-to-agent task delegation outside the software workflow.
7. Human inbox.
8. Fully custom hosted UIs.
9. Structured agent data collections for non-coding agents.
10. Enterprise RBAC, SSO, audit exports, and org administration.
11. Marketplace for agent packs or UI packages.
12. Full MCP server as a primary interface. A minimal MCP adapter may be created only if cheap and thin over CLI/core.
13. Browser-hosted dashboard.
14. Production-grade hosted credential broker.
15. Full bidirectional conflict resolution with Linear/Jira.
16. Multi-cloud remote execution.
17. Fine-grained billing.

---

## 6. Core Concepts

### 6.1 Workspace

A workspace is a Git repository initialized with Vessica.

A workspace contains:

```text
.vessica/
  config.yaml
  pack.lock
  harness.yaml
  agents/
  templates/
  workflows/
  lint-arch.*
  cache/          # ignored
  state/          # ignored for SQLite local mode
  runs/           # ignored
```

Committed files:

```text
AGENTS.md
ARCHITECTURE.md
DESIGN.md
DEPLOY.md
TESTING.md
SECURITY.md
.vessica/config.yaml
.vessica/pack.lock
.vessica/harness.yaml
.vessica/agents/
.vessica/templates/
.vessica/workflows/
.vessica/lint-arch.*
```

Ignored files:

```text
.vessica/cache/
.vessica/state/
.vessica/runs/
.vessica/sandboxes/
.vessica/secrets/
```

### 6.2 State Backend

The state backend stores:

- Epics
- Artifacts
- Artifact sets
- Tickets
- Waves
- Memories
- Runs
- Sandboxes
- Events
- Receipts
- Traces
- Auth metadata, but not raw long-lived secrets
- External tracker mappings
- Repo mappings

Backends:

| Backend | v1 Purpose | Limitations |
|---|---|---|
| SQLite | Solo/local experimentation and demo mode | Not suitable for multi-developer or remote sandbox coordination |
| Postgres URL | Team/manual shared mode | User must provision DB in v1 |
| Postgres Docker | Optional local team/dev convenience | Not required if v1 scope is tight |
| Hosted Vessica state | v2 | Out of scope for v1 |

The CLI must clearly warn users when SQLite is used with workflows that imply shared coordination.

### 6.3 Harness

The harness is the repo’s explicit operating manual for agents and deterministic constraints.

Harness files include:

- `AGENTS.md`
- `ARCHITECTURE.md`
- `DESIGN.md`
- `DEPLOY.md`
- `TESTING.md`
- `SECURITY.md`
- `.vessica/harness.yaml`
- `.vessica/lint-arch.*`
- `.vessica/templates/*`
- `.vessica/workflows/*`

Harness commands:

```bash
ves harness create
ves harness audit
ves harness sync
ves harness lint
ves harness status
```

### 6.4 Agent Pack

An agent pack is a committed set of prompts, skills, hooks, templates, workflow DAGs, and deterministic scripts used by Vessica.

Commands:

```bash
ves pack install <pack-ref>
ves pack pull <git-url>
ves pack sync
ves pack update
ves pack pin <version-or-sha>
ves pack origin get
ves pack origin set <git-url>
```

v1 default pack:

```text
@vessica/software-harness
```

Agent packs must be pinned through `.vessica/pack.lock`. Vessica must not silently pull latest agent definitions during runs.

### 6.5 Epic

An epic is user-articulated product intent.

Example:

```bash
ves epic add --title "Add password reset flow" --body-file epic.md
```

An epic may produce:

- PRD
- ADR(s)
- DesignSpec
- TestScenarios
- Ticket graph
- Wave plan
- Risk notes

### 6.6 Artifact

An artifact is a durable authored document or structured lifecycle object.

Artifact types in v1:

- `prd`
- `adr`
- `design-spec`
- `test-scenarios`
- `risk`
- `note`

Artifacts are indexed into memory but are not merely memories. They have versioning, status, source runs, source epics, approval status, and downstream relationships.

### 6.7 Artifact Set

An artifact set captures a coherent planning/design package for an epic.

Example:

```text
aset_abc123
  prd_...
  adr_...
  design_...
  test_...
  ticket_graph_version_...
```

Artifact sets allow execution to restart from coding without regenerating planning artifacts.

### 6.8 Ticket

A ticket is a unit of implementation work.

Tickets must support:

- Type: feature, bug, refactor, test, docs, chore
- Dependencies
- Wave assignment
- Status
- Claiming
- Leases
- Heartbeats
- Evidence-based closure
- External tracker mapping

Ticket states:

```text
draft
ready
claimed
in_progress
blocked
review
closed
failed
cancelled
```

### 6.9 Wave

A wave is a topological layer of the ticket dependency graph. Tickets in the same wave may be implemented in parallel.

### 6.10 Run

A run is an execution of a workflow.

Examples:

```bash
ves run epic epic_abc123
ves run epic epic_abc123 --stop-after ticketize
ves run epic epic_abc123 --start-at code --reuse-artifacts approved
ves run resume run_abc123
```

A run owns:

- Run ID
- Phase status
- Sandboxes
- Worker agents
- Event stream
- Logs
- Artifacts created
- Tickets claimed/closed
- Receipts
- Preview URL
- PR URL

### 6.11 Sandbox

A sandbox is an isolated execution environment, v1 backed by local Docker.

Sandbox capabilities:

- Clone repo from remote.
- Run configured coding CLI.
- Reach Vessica state backend.
- Stream events to Vessica.
- Expose preview port through tunnel.
- Support shell/log inspection.
- Destroy cleanly.

### 6.12 Memory

Memory is durable semantic knowledge. It is separate from artifacts and structured state.

Memory supports:

- Markdown content
- Frontmatter metadata
- Semantic search
- Permissions
- Source tracking
- Compaction
- Import/export

### 6.13 Receipt

A receipt is a durable summary of work performed.

Receipt fields include:

- Run ID
- Epic/ticket IDs
- Agents used
- Models used
- Token counts
- Cost
- Sandbox/runtime cost
- Elapsed time
- Files changed
- Tests run
- Build result
- Validation result
- Human interventions
- PR URL
- Preview URL
- Artifacts created
- Event/trace links
- Return on Intelligence inputs

### 6.14 Event

An event is an append-only structured record emitted during a run.

Event types include:

- `run.started`
- `run.phase.started`
- `run.phase.completed`
- `agent.message`
- `agent.progress`
- `tool.exec`
- `tool.result`
- `ticket.claimed`
- `ticket.closed`
- `test.output`
- `build.output`
- `validation.step`
- `sandbox.stdout`
- `sandbox.stderr`
- `cost.update`
- `preview.ready`
- `repo.pr.created`
- `error`
- `warning`

The event log powers live CLI streaming, later hosted UI streaming, logs, receipts, debugging, and audit.

---

## 7. CLI Requirements

### 7.1 General CLI Behavior

Every command must:

1. Return nonzero exit codes on failure.
2. Support `--json` where agent usage is likely.
3. Avoid color/control characters with `--json` or `--no-color`.
4. Be deterministic and scriptable.
5. Validate input before mutating state.
6. Emit structured errors.
7. Support `--cwd` where practical.
8. Respect config precedence:
   - explicit CLI flags
   - environment variables
   - workspace config
   - user config
   - defaults
9. Never print secrets.
10. Provide useful help text.

Common flags:

```bash
--json
--no-color
--quiet
--verbose
--debug
--cwd <path>
--config <path>
--yes
--dry-run
--idempotency-key <key>
```

### 7.2 Workspace Commands

```bash
ves init [--profile solo|team] \
         [--state sqlite|postgres-url|postgres-docker] \
         [--db-url <url>] \
         [--sandbox docker] \
         [--tracker linear|jira] \
         [--repo github|gitlab] \
         [--runner codex|claude|cursor|pi]

ves status
ves doctor
ves config list
ves config get <key>
ves config set <key> <value>
ves config unset <key>
```

Acceptance criteria:

- `ves init --profile solo` creates a usable local workspace with SQLite and Docker defaults.
- `ves init --profile team --state postgres-url --db-url ...` validates DB connectivity.
- `ves doctor` identifies missing runner, Docker, auth, repo remote, harness, pack lock, and state backend issues.
- Config can be read and updated safely.

### 7.3 Auth Commands

```bash
ves auth login github|gitlab|linear|jira
ves auth logout github|gitlab|linear|jira
ves auth status
```

Acceptance criteria:

- GitHub auth supports remote clone and PR creation in v1 if GitHub integration is included.
- Auth metadata is visible through `ves auth status`.
- Long-lived secrets are not committed or logged.
- Sandbox access uses scoped, run-specific injection where feasible.

### 7.4 Setup Commands

```bash
ves setup claude
ves setup codex
ves setup cursor
ves setup pi
ves setup mcp
```

Acceptance criteria:

- Setup commands install managed guidance for coding agents.
- Managed sections are clearly delimited and safely updatable.
- Setup teaches agents to use:
  - `ves prime`
  - `ves ticket ready`
  - `ves ticket claim`
  - `ves memory add`
  - `ves ticket close`
- Setup does not overwrite unrelated user content.

### 7.5 Pack Commands

```bash
ves pack install <pack-ref>
ves pack pull <git-url>
ves pack sync
ves pack update
ves pack pin <version-or-sha>
ves pack origin get
ves pack origin set <git-url>
```

Acceptance criteria:

- Default software-harness pack can be installed.
- Pack content is committed to repo.
- Pack lock records origin, ref, commit, install time.
- Runs use locked pack version.
- `pack update` is explicit.

### 7.6 Harness Commands

```bash
ves harness create
ves harness audit
ves harness sync
ves harness lint
ves harness status
```

Acceptance criteria:

- `create` generates initial harness for new or brownfield repo.
- `audit` detects missing, stale, or materially drifted harness files.
- `sync` updates harness docs and deterministic lint-arch rules to reflect repo reality.
- `lint` runs deterministic architecture constraints and returns structured results.
- `status` shows last sync, drift status, pack version, and missing files.

### 7.7 Epic Commands

```bash
ves epic list
ves epic add
ves epic view <epic_id>
ves epic update <epic_id>
ves epic delete <epic_id>
ves epic plan <epic_id>
ves epic status <epic_id>
```

Acceptance criteria:

- Epics are Vessica source-of-truth objects.
- Epics can be created from stdin, text flags, or files.
- `epic plan` is equivalent to `ves run epic <epic_id> --stop-after ticketize`.
- Epics can map to Linear/Jira issues best-efforts.

### 7.8 Artifact Commands

```bash
ves artifact list [--epic <epic_id>] [--type prd|adr|design-spec|test-scenarios]
ves artifact view <artifact_id>
ves artifact add --type <type>
ves artifact update <artifact_id>
ves artifact approve <artifact_id>
ves artifact diff <artifact_id>
```

Acceptance criteria:

- Planning agents can create PRD, ADR, DesignSpec, and TestScenarios artifacts.
- Artifacts support versions and status.
- Artifacts are indexed into memory.
- Approved artifact sets can be reused for coding starts.

### 7.9 Ticket and Wave Commands

```bash
ves ticket list [--epic <epic_id>]
ves ticket ready [--epic <epic_id>]
ves ticket view <ticket_id>
ves ticket add
ves ticket update <ticket_id>
ves ticket delete <ticket_id>
ves ticket claim <ticket_id> --agent <agent_id> --lease 45m
ves ticket claim --next --epic <epic_id> --agent <agent_id> --lease 45m
ves ticket heartbeat <ticket_id> --agent <agent_id>
ves ticket release <ticket_id> --agent <agent_id> --reason <reason>
ves ticket close <ticket_id> --agent <agent_id> --evidence <receipt_id>
ves ticket block <ticket_id> --by <ticket_id>
ves ticket unblock <ticket_id> --by <ticket_id>

ves wave list --epic <epic_id>
ves wave view <wave_id>
ves wave status <wave_id>
```

Acceptance criteria:

- `ticket ready` returns only dependency-unblocked tickets.
- `claim` is atomic.
- Claims have leases.
- Agents must heartbeat or release.
- Expired claims return to ready or interrupted state according to policy.
- Ticket closure requires evidence.
- Waves are derived from dependency graph.

### 7.10 Run Commands

```bash
ves run epic <epic_id> [--runner codex|claude|cursor|pi] [--sandbox docker] \
  [--concurrency 3] [--preview] [--pr draft|ready|none] \
  [--stream[=pretty|ui|events|jsonl|raw|off]]

ves run epic <epic_id> --stop-after ticketize
ves run epic <epic_id> --start-at code --reuse-artifacts approved
ves run ticket <ticket_id>
ves run list
ves run view <run_id>
ves run logs <run_id>
ves run logs <run_id> --agent-output
ves run logs <run_id> --detail <event_id>
ves run logs <run_id> --raw
ves run watch <run_id>
ves run watch <run_id> --ui
ves run watch <run_id> --jsonl
ves run watch <run_id> --jsonl --after-seq <seq>
ves run resume <run_id>
ves run resume <run_id> --from <phase>
ves run cancel <run_id>
ves run artifacts <run_id>
ves run preview <run_id> --browser
ves run receipt <run_id>
```

Acceptance criteria:

- Full epic run can execute continuously from harness check through PR and receipt.
- Runs are phase-addressable.
- Runs can resume from failed phase.
- Human runs default to `--stream=pretty`, showing agent prose and concise activity summaries without dumping prompts, command output, or file contents.
- `--stream=ui` provides an expandable terminal view; `events`, `jsonl`, `raw`, and `off` provide compact lifecycle events, a versioned machine protocol, Codex JSONL, and silence respectively.
- The interactive UI fills the available terminal height, follows appended events by default, pauses following when the user navigates upward, and resumes at the bottom or on `End`.
- `--stream=jsonl` is the supported Codex skill/tool integration mode: stdout contains only `vessica.stream/v1` event and result records, while diagnostics remain on stderr.
- `run watch --jsonl --after-seq` resumes the same protocol after a disconnected consumer's last observed sequence.
- Raw runner records are persisted under `.vessica/runs/<run_id>/agent.jsonl` regardless of live stream mode.
- `run logs --detail` expands one normalized event and `run logs --raw` replays the persisted runner transcript.
- `run watch` can attach from another terminal.
- `run preview` opens sandbox preview.
- `--pr draft` opens draft PR.
- Run output is recorded as events.

### 7.11 Sandbox Commands

```bash
ves sandbox list
ves sandbox view <sandbox_id>
ves sandbox logs <sandbox_id>
ves sandbox shell <sandbox_id>
ves sandbox tunnel <sandbox_id> --browser
ves sandbox destroy <sandbox_id>
ves sandbox retain <sandbox_id> --for 7d
ves sandbox gc [--dry-run] [--older-than 24h]
```

Acceptance criteria:

- Local Docker sandbox can be created, inspected, tunneled, and destroyed.
- Sandboxes clone from remote repo rather than copying local by default.
- Dirty local changes are not silently included.
- Local preview tunnel works for supported app types.
- Sandbox output streams to run event log.
- Successful previews retain their sandbox for 24 hours and refresh that lease on preview access.
- Failed sandboxes retain for four hours; explicit retention is capped at seven days.
- Resuming a run removes superseded sandboxes only after the replacement is healthy.
- Docker sandboxes self-expire and auto-remove even when the CLI is not invoked again.

### 7.12 Repo and Tracker Commands

```bash
ves repo connect github|gitlab
ves repo status
ves repo pr create --run <run_id>
ves repo pr view --run <run_id>

ves tracker connect linear|jira
ves tracker sync
ves tracker status
ves tracker push
ves tracker pull
```

Acceptance criteria:

- GitHub PR creation works for successful runs if configured.
- Vessica remains source of truth.
- Tracker sync is best-efforts.
- Sync failures do not corrupt Vessica state.
- Conflicts are visible.

### 7.13 Memory Commands

```bash
ves memory list
ves memory view <memory_id>
ves memory add
ves memory update <memory_id>
ves memory delete <memory_id>
ves memory search "<query>"
ves memory compact
ves memory grant <memory_id> --subject user:<id>|org:<id>|public --perm read|write|admin
ves memory revoke <memory_id> --subject user:<id>|org:<id>|public
ves memory visibility <memory_id> private|org|public
```

Acceptance criteria:

- Memories support markdown with frontmatter.
- Semantic search works against memory and indexed artifacts.
- Permissions are stored, even if v1 enforcement is basic.
- Agents can add memories from stdin.

### 7.14 Prime Commands

```bash
ves prime
ves prime --json
ves prime --for claude|codex|cursor|pi
ves prime --epic <epic_id>
ves prime --ticket <ticket_id>
ves prime --minimal
```

Acceptance criteria:

- Prime returns concise operational context for humans or agents.
- Agent mode includes command guidance and current ready work.
- Prime avoids dumping full docs unless requested.
- Prime includes harness status, relevant artifacts, active epics/tickets, memory highlights, and rules.

### 7.15 Receipts and Traces

```bash
ves receipt list
ves receipt view <receipt_id>
ves trace list
ves trace view <trace_id>
```

Acceptance criteria:

- Every run emits a receipt.
- Ticket closures may reference receipts.
- Receipts link to artifacts, events, PR, preview, and validation results.
- Trace viewing supports debugging and audit.

---

## 8. Software Epic Workflow

### 8.1 Default Full Flow

Command:

```bash
ves run epic epic_abc123 --runner codex --sandbox docker --concurrency 3 --preview --pr draft --stream
```

Workflow phases:

1. `preflight`
2. `harness`
3. `plan`
4. `design`
5. `ticketize`
6. `code`
7. `build`
8. `validate`
9. `preview`
10. `pr`
11. `receipt`

### 8.2 Preflight

The run engine must verify:

- Git remote exists and is reachable.
- Working tree state is understood.
- Vessica state backend is reachable.
- Docker is available.
- Runner is configured.
- Auth is available for clone and PR if needed.
- Pack is installed and pinned.
- Harness exists or can be created/synced.
- Required ports can be allocated.
- Required secrets are available through safe injection.

### 8.3 Repo Handling

Default behavior:

- Pull repo from remote into sandbox.
- Do not copy local working directory.
- Create integration branch:

```text
vessica/epic_<id>/run_<id>
```

Optional future behavior:

- Include dirty local changes only with explicit flag.
- Use local worktree mode only with explicit flag.

### 8.4 Harness Phase

If harness has not run, is missing, or is materially drifted:

- Run `ves harness audit`.
- Depending on policy, run `ves harness sync` or fail and ask user.

v1 default policy:

- If harness is missing, run sync/create.
- If drift is material, run sync.
- If sync changes harness files, commit those changes into the run branch.

### 8.5 Planning and Design Phases

The run invokes:

- Product agent → PRD
- Architect agent → ADR(s)
- QA agent → TestScenarios
- Design agent → DesignSpec
- Planner agent → ticket graph and waves

All generated documents must be:

- Saved as artifacts.
- Indexed into memory.
- Grouped into an artifact set.
- Associated with the epic and run.
- Versioned.

### 8.6 Coding Phase

The run starts N coding workers.

Default:

```text
N = 3
```

Each worker:

1. Calls `ves ticket claim --next`.
2. Receives one ready ticket.
3. Works in an isolated branch or worktree.
4. Writes failing tests first where appropriate.
5. Implements the ticket.
6. Runs relevant tests.
7. Persists durable findings with `ves memory add`.
8. Closes the ticket only with evidence.
9. Claims the next ready ticket.

Workers may wait if no ticket is available until the current wave completes.

### 8.7 Integration

The run engine merges completed ticket branches/worktrees into the integration branch wave by wave.

Requirements:

- Merge only after ticket evidence passes.
- Run relevant tests after merges.
- Resolve conflicts through build/coding agent if possible.
- Emit events for merge attempts, failures, and resolutions.

### 8.8 Build Phase

The build agent runs:

1. Lint
2. Deterministic `lint-arch`
3. Tests
4. Build

The build agent attempts to fix errors and warnings until:

- All are green, or
- Retry/budget limits are exhausted.

### 8.9 Validation Phase

The validation agent uses browser automation, such as Playwright, to execute TestScenarios.

Requirements:

- Each validation step emits an event.
- The validator can attempt fixes.
- A repeated failure on the same step is retried up to three times.
- If unresolved, create bug tickets under the same epic, not new epics.

Example:

```bash
ves ticket add \
  --type bug \
  --epic epic_abc123 \
  --discovered-from run_abc123 \
  --test-step step_abc123
```

### 8.10 Preview Phase

The run exposes a preview URL from the sandbox.

Commands:

```bash
ves run epic epic_abc123 --preview
ves run epic epic_abc123 --open-preview
ves run preview run_abc123 --browser
ves sandbox tunnel sbx_abc123 --browser
```

Requirements:

- Preview works before PR creation.
- `--preview` retains the sandbox preview; `--open-preview` implies preview and opens the URL as soon as it becomes healthy.
- Preview starts when the integration sandbox enters `code`, before coding workers begin.
- Vite binds to the sandbox network interface and provides browser HMR; plain Node entry points restart in watch mode and reflect changes on the next request.
- pnpm is the canonical v1 package manager for Node projects; generated harness commands and sandbox dependency installation use pnpm with Corepack.
- Legacy generated npm/npx harness commands are normalized to pnpm at runtime.
- The server watches the integration checkout and observes coherent ticket changes after each branch merge.
- An initially unstartable preview is deferred and retried during validation/preview rather than blocking coding.
- Preview remains available until sandbox is destroyed or run retention expires.
- Preview URL is included in receipt.

### 8.11 PR Phase

If configured:

```bash
--pr draft
```

The run opens a draft PR.

Requirements:

- PR title references epic and run.
- PR body includes:
  - Summary
  - Artifacts
  - Tickets completed
  - Tests/build/validation
  - Preview URL
  - Receipt link or inline summary
  - Known unresolved bug tickets, if any

### 8.12 Receipt Phase

The final receipt includes:

- Status
- PR URL
- Preview URL
- Epic
- Artifact set
- Tickets completed
- Bug tickets created
- Validation results
- Build results
- Token/model usage where available
- Runtime cost where available
- Human input events
- Event/trace references
- Return on Intelligence fields

---

## 9. Phase-Addressable and Resumable Runs

### 9.1 Start and Stop Phases

Commands:

```bash
ves run epic epic_abc123 --stop-after ticketize
ves run epic epic_abc123 --start-at code --reuse-artifacts approved
ves run epic epic_abc123 --start-at validate
```

Supported phases:

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

### 9.2 Resume

Commands:

```bash
ves run resume run_abc123
ves run resume run_abc123 --from validate
```

Requirements:

- Resume uses prior run state and artifact set.
- Resume does not regenerate approved artifacts unless explicitly requested.
- Resume records a continuation event.
- Failed phases preserve logs and partial outputs.

---

## 10. Event Streaming Requirements

### 10.1 Event Log

All sandbox and agent output must stream back into an append-only event log.

Events must include:

- Stable event ID.
- Run ID.
- Sandbox ID, if applicable.
- Phase.
- Agent ID, if applicable.
- Ticket ID, if applicable.
- Type.
- Timestamp.
- Level.
- Payload.

Example:

```json
{
  "event_id": "evt_7h2k9d1p",
  "run_id": "run_abc123",
  "sandbox_id": "sbx_def456",
  "phase": "code",
  "agent_id": "agent_coder_2",
  "ticket_id": "tix_91k3m2q",
  "type": "agent.progress",
  "level": "info",
  "timestamp": "2026-07-08T19:42:11Z",
  "payload": {
    "message": "Adding failing test for password reset token expiration."
  }
}
```

### 10.2 CLI Streaming

Commands:

```bash
ves run epic epic_abc123 --stream
ves run watch run_abc123
ves run watch run_abc123 --jsonl
```

Stream modes:

```bash
--stream=pretty  # concise agent messages and collapsed activity (default)
--stream=ui      # interactive, expandable activity
--stream=events  # compact normalized lifecycle events
--stream=jsonl   # versioned machine-consumable events and terminal result
--stream=raw     # raw Codex JSONL records
--stream=off     # no live output
```

The product contract is to persist model-visible messages, agent progress, tool output, test output, and structured events. Human-facing modes summarize these records by default and allow explicit expansion or raw replay. Hidden reasoning is not part of the product contract.

### 10.3 Redaction

The event pipeline must redact:

- API keys
- Tokens
- Passwords
- Connection strings
- Private keys
- `.env` values
- Credential broker outputs

Redaction must occur before persistence and before streaming to the user.

---

## 11. Memory and Artifact Frontmatter

Memory/artifact markdown must support frontmatter.

Example artifact:

```yaml
---
id: prd_4h7m1x2a
type: prd
title: Password reset PRD
workspace_id: ws_8f3k2p9q
repo: github.com/org/repo
epic_id: epic_8f3k2p9q
artifact_set_id: aset_abc123
owners:
  users: ["user_local"]
  orgs: []
permissions:
  user: rw
  org: none
  public: none
status: draft
version: 1
supersedes: null
created_by:
  actor_type: agent
  actor_id: agent_product_1
  model: claude-sonnet
created_at: 2026-07-08T18:00:00Z
updated_at: 2026-07-08T18:00:00Z
source_run_id: run_5n2a8k7r
tags: ["auth", "prd"]
---
```

Example memory:

```yaml
---
id: mem_6r8d1v3x
type: insight
scope: repo
owners:
  users: ["user_local"]
permissions:
  user: rw
  org: none
  public: none
source: chat
importance: high
created_at: 2026-07-08T18:00:00Z
---
```

---

## 12. ID Requirements

IDs must be type-prefixed.

Examples:

```text
ws_8f3k2p9q
epic_8f3k2p9q
prd_4h7m1x2a
adr_9d1p6q7r
test_2k8z5v1n
design_7b4m9c2e
aset_1r9d3k7m
tix_3p8x1q9d
wave_9q3m1z8p
run_5n2a8k7r
sbx_9q1m4z2c
mem_6r8d1v3x
rcpt_1x4q8m9p
trace_9k3v2a7b
evt_7h2k9d1p
```

Requirements:

- Mutable entities use generated unique IDs such as UUIDv7/ULID/base32.
- Immutable artifact content may also have content hashes.
- Idempotency is handled with explicit idempotency keys, not by relying only on deterministic short hashes.
- Parallel agent creation must not conflict.

---

## 13. Data Model: Minimum v1 Entities

Minimum entities:

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

---

## 14. Security and Permissions

### 14.1 Principles

- Vessica state is authoritative.
- Secrets must not be committed.
- Secrets must not be logged.
- Sandboxes receive only scoped credentials.
- Agent commands should be least-privilege where possible.
- State-changing commands should support dry-run.
- Destructive commands require explicit confirmation unless `--yes` is provided.
- All events should be redacted.

### 14.2 v1 Permission Scope

v1 stores permissions for memories and artifacts but does not need full enterprise enforcement.

v1 must enforce at least:

- Local user owns workspace.
- Public visibility does not publish externally.
- Sandbox cannot access credentials not passed to it.
- Agent claims must identify agent ID.
- Runs must record actors for audit.

### 14.3 Future Permission Compatibility

v1 metadata must support future hosted enforcement:

- User owner
- Org owner
- Public visibility
- Read/write/admin permissions
- Actor type: human, agent, system
- Source run
- Source task
- External provider mapping

---

## 15. Integrations

### 15.1 Runners

v1 runner abstraction should support:

```text
codex
claude
cursor
pi
custom
```

v1 implementation may fully support one or two runners first, but config and command design must not hardcode one.

Runner contract:

```text
Input:
  repo path
  instructions
  phase
  ticket/artifact context
  tools
  budget
  permissions

Output:
  events
  file changes
  command outputs
  artifacts
  status
  evidence
```

### 15.2 Repo Providers

v1 target:

- GitHub first.
- GitLab designed but may be deferred.

### 15.3 Tracker Providers

v1 target:

- Vessica is source of truth.
- Linear/Jira best-efforts.
- At minimum, support mapping and push status if full sync is deferred.

### 15.4 MCP

v1 may include a minimal MCP adapter only if it is thin over the CLI/core.

MCP is not the canonical contract.

Canonical contract:

```text
ves CLI + internal core APIs
```

---

## 16. Success Metrics

### 16.1 Activation Metrics

- Time from `ves init` to first successful `ves prime`.
- Time from empty/brownfield repo to harness status green.
- Time from `ves epic add` to generated artifact set.
- Time from `ves run epic` to preview URL.
- Time from `ves run epic` to draft PR.

### 16.2 Quality Metrics

- Percentage of runs reaching build green.
- Percentage of runs passing validation.
- Number of unresolved validation bug tickets per run.
- Number of human interventions per run.
- Number of failed ticket claims or lease conflicts.
- Harness drift rate over time.

### 16.3 Agent Productivity Metrics

- Tickets completed per run.
- Average cost per ticket.
- Average elapsed time per ticket.
- Token spend per phase.
- Rework rate.
- Return on Intelligence inputs:
  - estimated human time avoided
  - token/runtime cost
  - acceptance result
  - quality/eval result

### 16.4 Reliability Metrics

- Run resume success rate.
- Event stream completeness.
- Sandbox teardown success.
- PR creation success.
- Tracker sync success/failure rate.

---

## 17. MVP Milestones

### Milestone 1 — CLI Skeleton and State

- `ves init`
- Config
- SQLite/Postgres state
- IDs
- Entity schema
- `ves status`
- `ves doctor`

### Milestone 2 — Memory, Epics, Tickets

- Memory CRUD/search
- Epic CRUD
- Artifact CRUD
- Ticket CRUD
- Dependencies/waves
- Claims/leases
- `ves prime --json`

### Milestone 3 — Harness and Pack

- Default software pack
- Pack install/pin
- Harness create/audit/sync/lint/status
- Committed `.vessica` files
- Setup for at least one coding runner

### Milestone 4 — Docker Sandbox and Run Engine

- Local Docker sandbox
- Remote clone
- Run records
- Event log
- Live streaming
- Phase execution
- Resume basics

### Milestone 5 — Full Epic Workflow

- Planning agents create artifacts
- Ticket graph generation
- Concurrent coding workers
- Build phase
- Validation phase
- Bug ticket creation
- Receipts

### Milestone 6 — Preview and PR

- Sandbox preview tunnel
- GitHub draft PR creation
- PR body includes receipt summary
- Final receipt and trace views

---

## 18. Out-of-Scope Details for v1

Explicitly defer:

- Hosted run coordinator.
- Hosted UI.
- Custom agent UI rendering.
- Agent data collections.
- Cron/scheduled agents.
- Arbitrary non-coding agent runtime.
- Agent inbox.
- Remote sandboxes.
- Enterprise org permissions.
- Marketplace.
- Billing.
- Advanced MCP ecosystem.
- Real-time collaborative web dashboard.
- Full bidirectional Linear/Jira conflict resolution.
- Cross-repo multi-agent orchestration.
- Automatic production deployment.

---

## 19. Open Implementation Decisions

1. Which runner is first-class in the first build: Codex, Claude, or Pi?
2. Does v1 implement both SQLite and Postgres from day one, or SQLite first with Postgres immediately after?
3. How much of Linear/Jira sync is required for v1 launch?
4. Which web framework preview conventions are supported initially?
5. How should sandbox images be built and cached?
6. How should Vessica estimate human time avoided for Return on Intelligence?
7. How much of model/token usage can be captured uniformly across runners?

---

## 20. v1 Launch Definition

Vessica v1 is launchable when a developer can:

```bash
ves init --profile solo --runner codex --repo github
ves auth login github
ves pack install @vessica/software-harness
ves harness sync
ves epic add --title "Add password reset" --body-file epic.md
ves run epic epic_abc123 --concurrency 3 --preview --pr draft --stream
ves run preview run_abc123 --browser
ves receipt view rcpt_abc123
```

And the system:

- Generates or updates the repo harness.
- Produces PRD/ADR/DesignSpec/TestScenarios.
- Creates a dependency-aware ticket graph.
- Coordinates coding agents through claims and leases.
- Builds and validates the work.
- Opens a draft PR.
- Provides a preview URL.
- Emits a complete receipt.
- Allows failed runs to resume.
