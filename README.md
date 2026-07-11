# Vessica CLI (`ves`)

**Local-first harness engineering for agent-driven software development.**

Vessica turns a repository into a durable operating layer for coding agents: harness docs, pinned agent packs, epics, planning artifacts, dependency-aware tickets, Docker sandboxes, live run streams, preview URLs, draft PRs, and receipts.

The canonical interface is the `ves` CLI — the same commands humans and coding agents use.

```bash
ves init --profile solo --runner codex --repo github
ves pack install @vessica/engineering-harness
ves harness sync
ves epic add --title "Add password reset" --body-file epic.md
ves run epic <epic_id> --concurrency 3 --preview --pr draft --stream
ves receipt view <rcpt_id>
```

---

## Why Vessica

Modern coding agents can write useful code, but production use is limited by missing surrounding infrastructure:

- Agents forget product context between sessions
- Repos lack explicit harnesses for architecture, design, deploy, and testing
- Epics do not become PRDs, ADRs, test plans, and ticket graphs
- Multiple agents cannot reliably claim and coordinate work
- Humans lack a live, auditable view of what changed, what passed, and what it cost

Vessica is that operating layer. It orchestrates tools like Codex, Claude Code, Cursor, and Pi — it does not replace them.

---

## Features (v1)

| Area | What you get |
|---|---|
| **Workspace** | `ves init` with SQLite (solo) or Postgres (team) |
| **Harness** | `AGENTS.md`, architecture/design/deploy/testing/security docs, lint-arch rules |
| **Agent packs** | Pinned `@vessica/engineering-harness` committed to the repo |
| **Epics & artifacts** | PRD, ADR, DesignSpec, TestScenarios with versions and approval |
| **Tickets & waves** | Dependency graph, topological waves, atomic claims + leases |
| **Memory** | Durable markdown memory with search |
| **Runs** | Phase-addressable software epic workflow with resume |
| **Sandboxes** | Local Docker (falls back to local workdir when Docker is unavailable) |
| **Streaming** | Codex-like human stream, expandable TUI, compact events, and persisted raw JSONL |
| **Preview** | Preview URL from harness config |
| **PRs** | GitHub draft PR with receipt summary |
| **Receipts** | Cost/time/ticket/validation/PR/preview summary + ROI inputs |
| **Agent UX** | `ves prime --json` and `--json` on agent-facing commands |
| **Setup** | `ves setup codex\|claude\|cursor\|pi` managed guidance |

---

## Requirements

- **Go 1.25+** (to build from source)
- **Git**
- **Docker** (recommended for sandboxes; local fallback works without it)
- **Node.js 24+ and pnpm 11** for Node projects
- **GitHub token** (for remote clone + draft PRs)
- Optional: **Codex** CLI (or another runner); **Postgres** for team mode

---

## Install

### Build from source

```bash
git clone https://github.com/vessica-labs/vessica-cli.git
cd vessica-cli
make build          # produces ./bin/ves
make install        # copies to ~/.local/bin/ves
```

Or:

```bash
go build -o bin/ves ./cmd/ves
```

Verify:

```bash
ves version
ves --help
```

---

## Quick start

### 1. Initialize a workspace

From any Git repository:

```bash
ves init --profile solo --runner codex --repo github
```

Team / shared state:

```bash
ves init --profile team --state postgres-url --db-url "$DATABASE_URL" \
  --sandbox docker --runner codex --tracker linear --repo github
```

### 2. Authenticate GitHub

```bash
ves auth login github         # opens the GitHub CLI browser flow
# CI fallback: ves auth login github --token "$GITHUB_TOKEN"
ves auth status
```

OAuth credentials use macOS Keychain when available. File-based fallbacks are stored with mode `0600` under `~/.vessica/secrets/` and are never committed to the repo.

### 3. Install the software harness pack

```bash
ves pack install @vessica/engineering-harness
ves harness sync
ves doctor
```

The default pack comes from [vessica-labs/engineering-harness](https://github.com/vessica-labs/engineering-harness) and is pinned to its resolved commit. To use and evolve your own harness, fork that repository and install the fork:

```bash
ves pack pull https://github.com/your-org/engineering-harness.git#main
```

`ves pack sync` restores the locked commit, while `ves pack update` refreshes the configured tag or branch.

### 4. Create an epic and run it

```bash
cat > epic.md <<'EOF'
Add a password reset flow via emailed one-time token.
EOF

ves epic add --title "Add password reset" --body-file epic.md --json
ves run epic <epic_id> --concurrency 3 --preview --pr draft --stream
```

### 5. Inspect outputs

```bash
ves run preview <run_id> --browser
ves receipt view <rcpt_id>
ves run logs <run_id>
ves run logs <run_id> --agent-output
ves prime --json
```

### Smoke test

```bash
./scripts/launch-smoke.sh
```

---

## Software epic workflow

`ves run epic` executes a phase-addressable DAG:

```text
preflight → harness → plan → design → ticketize
        → code → build → validate → preview → pr → receipt
```

Useful controls:

```bash
# Plan only (through ticket graph)
ves epic plan <epic_id>
# equivalent:
ves run epic <epic_id> --stop-after ticketize

# Resume a failed or partial run
ves run resume <run_id>
ves run resume <run_id> --from code

# Start coding from approved artifacts
ves run epic <epic_id> --start-at code --reuse-artifacts approved
```

### Run output

Human runs default to a concise Codex-like stream. Prompts, command output, and file contents stay collapsed while agent messages and short activity summaries remain visible.

```bash
ves run epic <epic_id> --stream=pretty  # default, non-interactive
ves run epic <epic_id> --stream=ui      # expandable terminal UI
ves run epic <epic_id> --stream=events  # compact lifecycle events
ves run epic <epic_id> --stream=jsonl   # versioned machine stream
ves run epic <epic_id> --stream=raw     # raw Codex JSONL
ves run epic <epic_id> --stream=off     # no live stream
```

Bare `--stream` remains an alias for `--stream=pretty`; `--events-only` and `--no-stream` remain compatibility aliases. The space form, such as `--stream ui`, is also accepted.

Every runner invocation is persisted to `.vessica/runs/<run_id>/agent.jsonl`, independent of the selected live mode. Inspect it through the CLI:

```bash
ves run logs <run_id>                  # concise replay with event IDs
ves run logs <run_id> --detail <evt_id>
ves run logs <run_id> --raw
ves run watch <run_id> --ui
```

The interactive UI fills the available terminal height and follows new events automatically. Moving up pauses following so earlier rows can be inspected; moving back to the bottom or pressing `End` resumes the live tail.

Codex skills and other tool consumers should use `--stream=jsonl`. It writes only `vessica.stream/v1` JSONL records to stdout, keeps diagnostics on stderr, and ends with a `kind: "result"` record. A disconnected consumer can resume with `ves run watch <run_id> --jsonl --after-seq <seq>`. See [the streaming protocol](docs/Vessica_stream_v1.md).

### Live preview

Preview retention is explicit:

```bash
ves run epic <epic_id> --preview
ves run epic <epic_id> --open-preview  # implies --preview and opens when ready
ves run preview <run_id> --browser     # open or restart a run preview later
```

For preview-enabled runs, Vessica now starts the configured development server as soon as the integration sandbox enters the code phase. Vite runs with Docker-reachable host and port settings; plain Node entry points run in watch mode. The server watches the integration checkout and updates after each ticket branch is merged, so the preview represents the coherent integration branch. Vite provides browser HMR; plain Node servers restart automatically and reflect changes on the next browser request. If the base branch cannot start yet, Vessica emits `preview.deferred` and retries during validation/preview instead of blocking coding.

---

## Hosted dark factory

Start locally, then move the same repository to a persistent Railway control plane:

    ves auth login railway
    ves auth login linear
    ves auth login codex
    ves railway up
    ves railway status
    ves railway logs

The railway up command creates a user-owned Railway project, Postgres service, persistent control plane, Linear webhook, dedicated SSH identity, and reusable Railway worker checkpoint. A Linear issue entering the configured Todo state becomes a Vessica epic; artifacts are posted as comments, implementation tickets become sub-issues, and the completed run publishes a hosted preview and draft PR.

Railway and Linear use browser OAuth with PKCE and rotating refresh tokens. The hosted control plane encrypts those credentials in Postgres and resolves short-lived access tokens only when it calls Linear or starts a Railway sandbox. GitHub delegates browser login to `gh`; Codex delegates it to `codex login`. API keys and project tokens remain optional headless/CI fallbacks.

See [Hosted Railway](docs/Hosted_Railway.md) for lifecycle and security details.

---

## Commands

| Command | Purpose |
|---|---|
| `ves init` | Create workspace config + state |
| `ves status` / `ves doctor` | Workspace health |
| `ves config …` | Get/set workspace config |
| `ves auth …` | Provider login/logout/status |
| `ves setup <runner>` | Install managed agent guidance |
| `ves pack …` | Install/pin/update agent packs |
| `ves harness …` | Create/audit/sync/lint/status |
| `ves epic …` | Epic CRUD + plan |
| `ves artifact …` | Planning artifacts |
| `ves ticket …` / `ves wave …` | Tickets, claims, leases, waves |
| `ves memory …` | Durable memory |
| `ves prime` | Concise context for humans/agents |
| `ves run …` | Execute, watch, resume, preview |
| `ves sandbox …` | Inspect/destroy sandboxes |
| `ves repo …` / `ves tracker …` | GitHub + Linear/Jira (best-efforts) |
| `ves receipt …` / `ves trace …` | Receipts and traces |

Global flags:

```bash
--json                 # machine-safe JSON envelope {ok, data, error}
--cwd <path>           # operate on another workspace
--yes                  # skip destructive confirmations
--idempotency-key <k>  # safe retries for mutating commands
```

---

## Agent usage

Coding agents should prefer `ves` over ad hoc TODO/plan/memory files.

```bash
ves prime --json
ves ticket ready --json
ves ticket claim --next --epic <epic_id> --agent <agent_id> --lease 45m --json
ves memory add --stdin --json
ves ticket close <ticket_id> --agent <agent_id> --evidence <receipt_id> --json
```

Install managed guidance into the repo:

```bash
ves setup codex
ves setup claude
ves setup cursor
ves setup pi
```

---

## Configuration

Precedence: **CLI flags → env → workspace `.vessica/config.yaml` → user config → defaults**.

Example workspace config:

```yaml
state:
  backend: sqlite          # sqlite | postgres-url
  db_url: null
sandbox:
  backend: docker
runner:
  default: codex
  model: gpt-5.6-terra
  reasoning_effort: high
repo:
  provider: github
  remote: git@github.com:org/repo.git
tracker:
  provider: none           # linear | jira | none
  mode: best_efforts
pack:
  lockfile: .vessica/pack.lock
```

### Sandbox retention

Successful preview sandboxes are retained for 24 hours. Failed-run sandboxes are retained for four hours. Opening a preview or tunnel refreshes the 24-hour lease, and a resumed run removes older sandboxes after its replacement is healthy.

New Docker sandboxes carry Vessica ownership labels, run with Docker auto-remove enabled, and include an internal expiry watchdog. This lets Docker remove an expired container even when `ves` is not run again.

```bash
ves sandbox list
ves sandbox gc --dry-run
ves sandbox gc
ves sandbox retain sbx_abc123 --for 7d
ves sandbox destroy sbx_abc123 --yes
ves sandbox destroy --run run_abc123 --yes
```

Explicit retention is capped at seven days. Vessica only garbage-collects sandboxes recorded in the current workspace; new Docker sandboxes are additionally labeled for ownership and recovery.

Useful env vars:

| Variable | Meaning |
|---|---|
| `VES_STATE_BACKEND` | `sqlite` / `postgres-url` |
| `VES_DB_URL` | Postgres URL |
| `VES_RUNNER` | Default runner |
| `VES_REPO_REMOTE` | Git remote override |
| `GITHUB_TOKEN` / `VES_GITHUB_TOKEN` | GitHub auth |
| `VES_RUNNER_MODE=stub` / `VES_SIMULATION=1` | Run deterministic simulation mode for demos/smoke tests |

---

## Repository layout after init

**Committed**

```text
AGENTS.md
ARCHITECTURE.md
DESIGN.md
DEPLOY.md
TESTING.md
SECURITY.md
.vessica/
  config.yaml
  pack.lock
  harness.yaml
  agents/
  templates/
  workflows/
  lint-arch.sh
```

**Ignored**

```text
.vessica/cache/
.vessica/state/
.vessica/runs/
.vessica/sandboxes/
.vessica/secrets/
```

---

## Architecture

```text
Human / Coding Agent
        │
        ▼
     ves CLI
        │
        ▼
   Vessica Core
   ┌────┼────────────┬─────────────┬──────────────┐
   ▼    ▼            ▼             ▼              ▼
 State Harness    Run Engine    Sandbox      Integrations
 DB    + Packs    + Events      Docker       GitHub / trackers
                     │
                     ▼
              Receipts + Preview + PR
```

Implemented in Go as a single binary. See [docs/Vessica_v1_ADR.md](docs/Vessica_v1_ADR.md) for decisions.

---

## Development

```bash
make build
make test
./scripts/launch-smoke.sh
```

Project layout:

```text
cmd/ves/                 # CLI entrypoint
internal/
  cli/                   # cobra commands
  config/ state/ id/     # workspace + persistence
  pack/ harness/         # packs + harness
  ticket/ artifact/ memory/
  run/ sandbox/ runner/  # execution
  repo/ tracker/ auth/
  receipt/ event/ redaction/ prime/
internal/pack/software-harness/ # offline fallback snapshot
testdata/sample-app/     # fixture for smoke tests
docs/                    # PRD + ADR
```

---

## v1 scope / non-goals

**In scope:** local-first CLI, SQLite/Postgres, Docker sandboxes, Codex-first runner, GitHub PRs, harness/packs, epics→tickets→runs→receipts.

**Out of scope for v1:** hosted Vessica service, remote sandboxes, scheduler/cron, general arbitrary agent runtime, human inbox, custom hosted UIs, marketplace, enterprise RBAC/SSO, full MCP ecosystem.

Roadmap notes live in [docs/Vessica_v2_PRD.md](docs/Vessica_v2_PRD.md).

---

## Documentation

- [Vessica v1 PRD](docs/Vessica_v1_PRD.md) — product requirements and launch definition
- [Vessica v1 ADR](docs/Vessica_v1_ADR.md) — architecture decisions
- [Vessica v2 PRD](docs/Vessica_v2_PRD.md) — future hosted / general-agent direction

---

## License

License TBD — this repository is under active early development.

---

## Status

**v0.1.0** — build-ready v1 implementation matching the local-first launch loop in the v1 PRD. APIs and pack formats may still evolve before a stable 1.0 tag.
