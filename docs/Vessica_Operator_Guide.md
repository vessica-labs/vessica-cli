# Vessica Operator Guide

## Overview

Vessica has one user-facing control surface: `ves`. The CLI owns repository access, workplans, authentication, the engineering harness, runs, Linear synchronization, Railway provisioning, and routing to the knowledge authority. The Codex plugin teaches these workflows but contains no Vessica business logic.

## Choose a workflow

- **Interactive:** Codex edits the current working tree directly. Use Vessica only for context and durable knowledge.
- **Dispatch:** Persist an approved epic/ticket graph and start a Vessica run.
- **Hybrid:** Discover and plan interactively, then dispatch the approved implementation.

Never infer permission to dispatch from task size. Confirm harness changes, persistent work objects, run lifecycle changes, refinement prompts, approval, rollback, and merge.

## Install and set up hosted Vessica

Before initializing or dispatching an epic, the repository must have a reachable
GitHub `origin`. Vessica uses it for isolated sandbox clones, branch pushes, and
pull requests:

```bash
git remote get-url origin
# If origin is missing:
git remote add origin git@github.com:your-org/your-repository.git
git push -u origin "$(git branch --show-current)"
git ls-remote origin
```

```bash
ves up --dry-run --json
ves up --yes --stream jsonl
ves workspace status --json
ves repo list --json
```

`ves up` authenticates Railway and GitHub when necessary, discovers or creates the one Vessica installation in the selected Railway workspace, attaches the repository, creates a missing engineering harness, and maps the pushed commit in a read-only cloud sandbox. Hosted lexical retrieval is healthy without an embeddings key. Local-only development is available separately through `ves dev up`.

Onboarding is durable and safe to resume. If a provider login, Railway deploy,
Sandbox Priority Boarding step, or readiness check interrupts setup, keep the
operation ID and continue the same operation:

```bash
ves up status --json
ves up resume <operation-id> --yes --stream jsonl
```

New installations use a dedicated Railway project named
`vessica-control-plane`, independent of the application repository. A successful
receipt means deployment reached terminal `SUCCESS`, `/readyz` passed, the
repository attachment was persisted, and the sandbox/toolchain check completed.

The hosted installation uses one Railway Postgres service with two isolated databases: `vessica_control` for workflow state and `vessica_knowledge` for durable knowledge. They use different roles, URLs, and migration histories; pgvector exists only in the knowledge database.

## Agent-safe command contract

Use `--json` and parse the `vessica.cli/v1` envelope. JSON commands never prompt. A mutation without approval returns `confirmation_required`; repeat the reviewed command with `--yes` and a stable `--idempotency-key`.

```bash
ves capabilities --json
ves doctor --json
ves prime --for codex --json
ves toolchain verify --profile workstation --json
```

Use `--dry-run --json` before mutations. Do not scrape human output or echo secret values into arguments.

## Knowledge

```bash
ves knowledge status --json
ves knowledge context --query "authentication decisions" --token-budget 4000 --json
ves entity resolve "OAuth" --json
ves artifact list --status active --json
ves memory search "previous migration" --json
```

Knowledge objects:

- **Entities** identify repositories, projects, people, products, technologies, and topics.
- **Artifacts** are authoritative PRDs, ADRs, designs, specifications, and plans.
- **Memories** are retrieval-oriented instructions, facts, decisions, and episodes.
- **Relationships** are immutable assertions connecting knowledge and external references.

Context responses expose the ranking version, weights, artifact selection policy, component scores, artifact reasons, provenance, and source references. Hosted workspaces report `lexical` until the owner explicitly enables embeddings; during and after backfill they report `semantic_hybrid` with backlog status.

Create durable knowledge only after confirmation:

```bash
ves memory add --type decision --title "Storage decision" --body "..." --dry-run --json
ves memory add --type decision --title "Storage decision" --body "..." --yes --idempotency-key decision-<unique> --json

ves artifact create --type adr --title "ADR: Storage" --body-file ADR.md --dry-run --json
ves artifact create --type adr --title "ADR: Storage" --body-file ADR.md --yes --idempotency-key adr-<unique> --json
ves artifact activate <artifact_id> --yes --idempotency-key activate-<unique> --json
```

Create an `instruction` only when the user explicitly requests durable behavioral guidance.

## Epics and dispatch

Validate conversation-derived work without persistence:

```bash
ves epic draft --spec-file epic.json --json
```

After approval:

```bash
ves epic add --spec-file epic.json --yes --idempotency-key epic-<unique> --json
ves run epic <epic_id> --dry-run --json
ves run epic <epic_id> --yes --idempotency-key run-<unique> --json
```

Monitor and recover:

```bash
ves run view <run_id> --json
ves run watch <run_id> --json
ves run resume <run_id> --yes --idempotency-key resume-<unique> --json
```

Review evidence before approval:

```bash
ves receipt view <receipt_id> --json
ves run approve <run_id> --dry-run --json
ves run approve <run_id> --yes --idempotency-key approve-<unique> --json
ves run rollback <run_id> --yes --idempotency-key rollback-<unique> --json
```

Meaningful epic, run, ticket, blocker, follow-up, refinement, receipt, PR, and commit events become queryable episode memories. Heartbeats and polling noise do not.

## Optional semantic retrieval

No embeddings credential is used during quickstart. To opt in later, place the provider key in a local environment variable and name that variable to Vessica:

```bash
export OPENAI_API_KEY='...'
ves knowledge embeddings enable --provider openai --api-key-env OPENAI_API_KEY --yes
ves knowledge embeddings status --json
```

The CLI sends the secret directly to Railway, waits for deployment success and readiness, and starts an idempotent backfill. Changing provider or model re-embeds current versions; rotating only the key does not. `ves knowledge embeddings disable --yes` returns to lexical retrieval while retaining unused vectors.

Keep the Railway control-plane service at one replica. This release intentionally rejects a second replica from the same deployment with a database-backed singleton lease. Worker sandboxes remain independently scalable; control-plane scale-out is deferred until preview coordination, scheduled loops, and all remaining process-local ownership are distributed.

Authorize the official Railway CLI session used for native preview forwarding once after onboarding:

```bash
ves railway preview-session authorize
ves railway preview-session status
ves railway preview-session repair-key
ves railway preview-session smoke
```

The first command prints a short-lived Railway device-authorization link. The control plane then generates and registers its forwarding key, encrypts the CLI session and key in Postgres, and restores them automatically after deployments. Repeat authorization only when Railway revokes the grant or refresh validation fails. If only the forwarding key was registered to the wrong Railway key scope, use `repair-key`; it rotates the key without repeating device authorization.

Optional Linear setup is separate:

```bash
ves integration connect linear --project "Product launch" --dry-run --json
ves integration connect linear --project "Product launch" --yes --idempotency-key connect-linear-product-launch --json
```

The project selector accepts a Linear project UUID, slug, or name and becomes the default project for Vessica-created parent issues and sub-issues. Connecting Linear updates only Linear service variables and redeploys only the control plane; it does not reconcile or redeploy the knowledge service. Change the default later with:

```bash
ves integration switch-project linear --project "Next project" --dry-run --json
ves integration switch-project linear --project "Next project" --yes --idempotency-key switch-linear-next-project --json
```

Never switch to writable local knowledge during a hosted outage. Retry the hosted operation or restore service availability.

## Diagnose common failures

### `repository_required`

Run `ves up` in a pushed Git repository root or pass `--cwd` explicitly.

### `confirmation_required`

Review the dry run and repeat with `--yes` plus an idempotency key.

### Hosted unauthorized or unavailable

Run:

```bash
ves auth status --json
ves knowledge status --json
ves railway status --json
ves railway logs --lines 200
```

Do not edit `.vessica/config.yaml` to force local mode.

If the local attachment is partial or stale and cannot reach the hosted control
plane, clear only the local hosted attachment and credentials:

```bash
ves workspace forget --dry-run --json
ves workspace forget --yes --idempotency-key forget-stale-attachment --json
```

This recovery command does not delete Railway resources, the engineering
harness, repository documentation, or unmanaged `AGENTS.md` content. Run
`ves up` again to rediscover or reattach the repository.

### Toolchain mismatch

```bash
ves toolchain verify --profile workstation --json
ves toolchain verify --profile worker --json
```

The workstation profile covers tools needed to operate hosted Vessica. The
worker profile verifies the full pinned coding environment and launches
Playwright Chromium. Hosted checkpoints are named from the contract fingerprint,
so a toolchain change creates a new checkpoint instead of silently mutating an
existing one.

Each attached repository can also have a derived repository checkpoint. Run
`ves up --refresh --yes` after a material toolchain or dependency change to
refresh the mapped commit and repository snapshot immediately. Normal source-only
changes do not require a refresh: each sandbox fetches the remote Git delta while
retaining warmed dependencies. Run receipts include `infrastructure` spans and
`wall_elapsed` so operators can separate provisioning time from model and phase
execution.

For ticket worktrees, the worker reports `git_worktree_trust`,
`worktree_dependencies`, and `coder_context_packet` infrastructure stages.
`worktree_dependencies.mode` is `reflink`, `offline_install`, or `install`, and
`cache_hit` identifies paths that avoided a networked install. Engine-managed
Codex events also report `mcp_policy`, `mcp_discovery`, and
`mcp_disabled_count`. MCP servers are
disabled by default for these runs; set the worker variable
`VES_CODEX_MCP_ALLOWLIST` to a comma-separated list only when a repository task
requires a specific integration.

An `xs` planning bundle can carry its single implementation ticket, allowing the
ticketize phase to finish with `fast_path: true` and no second planning call.
Terminal epic status mirrors the run: draft PRs become `in_review`, successful
non-review runs and approved merges become `completed`, and failed, cancelled,
or rolled-back runs no longer remain stale at `planned`.

### Linear projection failure

Vessica remains canonical. Inspect Railway outbox/log status and retry synchronization; do not recreate local epics to compensate.

### Harness drift

```bash
ves harness audit --json
ves harness sync --dry-run --json
ves harness sync --yes --idempotency-key harness-<unique> --json
```

### Retrieval quality

Inspect `ranking`, per-memory `explanation`, `artifact_explanations`, `retrieval_mode`, `index_fresh`, and `omissions`. Increase the token budget or provide entity hints and deterministic artifact selectors before changing stored knowledge.

## Service/API help

The hosted implementation and operational details live in the public `vessica-knowledge-server` repository:

- `README.md`: server overview and development.
- `docs/OPERATIONS.md`: runtime, secrets, readiness, embeddings, and troubleshooting.
- `openapi.yaml`: complete authenticated HTTP contract.

Normal developers and coding agents should continue to call `ves`, not the HTTP API directly.
