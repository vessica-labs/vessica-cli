# Vessica Operator Guide

## Overview

Vessica has one user-facing control surface: `ves`. The CLI owns repository access, workplans, authentication, the engineering harness, runs, Linear synchronization, Railway provisioning, and routing to the knowledge authority. The Codex plugin teaches these workflows but contains no Vessica business logic.

## Choose a workflow

- **Interactive:** Codex edits the current working tree directly. Use Vessica only for context and durable knowledge.
- **Dispatch:** Persist an approved epic/ticket graph and start a Vessica run.
- **Hybrid:** Discover and plan interactively, then dispatch the approved implementation.

Never infer permission to dispatch from task size. Confirm harness changes, persistent work objects, run lifecycle changes, refinement prompts, approval, rollback, and merge.

## Install and initialize

```bash
ves version
ves init --profile solo --runner codex --repo github
ves pack install @vessica/engineering-harness
ves harness sync
ves setup codex --plugin
ves capabilities --json
ves doctor --json
```

Solo mode creates `.vessica/state/vessica.db` for workplans and `.vessica/state/knowledge.db` for authoritative knowledge. It requires no cloud account, model download, or embedding key.

## Agent-safe command contract

Use `--json` and parse the `vessica.cli/v1` envelope. JSON commands never prompt. A mutation without approval returns `confirmation_required`; repeat the reviewed command with `--yes` and a stable `--idempotency-key`.

```bash
ves capabilities --json
ves doctor --json
ves prime --for codex --json
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

Context responses expose the ranking version, weights, artifact selection policy, component scores, artifact reasons, provenance, and source references. Solo mode reports `lexical`; hosted mode normally reports `semantic_hybrid`.

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

## Promote to Railway

Hosted knowledge requires an embedding provider credential in an environment variable:

```bash
export EMBEDDING_API_KEY='...'
ves railway up \
  --workspace <railway-workspace> \
  --linear-team <team> \
  --source /path/to/vessica-cli \
  --embedding-api-key-env EMBEDDING_API_KEY \
  --dry-run --json

ves railway up \
  --workspace <railway-workspace> \
  --linear-team <team> \
  --source /path/to/vessica-cli \
  --embedding-api-key-env EMBEDDING_API_KEY \
  --yes --idempotency-key railway-up-<unique> --json
```

`ves railway up` deploys the compatible public knowledge-server image by immutable digest, provisions isolated Postgres, configures ordinary and export credentials, waits for `SUCCESS` and readiness, promotes knowledge, then configures the control plane to use the hosted authority.

After promotion:

```bash
ves knowledge status --json
ves railway status --json
ves knowledge context --query "recent work" --json
```

Never switch to writable local knowledge during a hosted outage. Retry the hosted operation or restore service availability.

## Diagnose common failures

### `not_initialized`

Run `ves init` in the repository root or pass `--cwd` explicitly.

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
- `docs/OPERATIONS.md`: runtime, secrets, readiness, promotion, and troubleshooting.
- `openapi.yaml`: complete authenticated HTTP contract.

Normal developers and coding agents should continue to call `ves`, not the HTTP API directly.
