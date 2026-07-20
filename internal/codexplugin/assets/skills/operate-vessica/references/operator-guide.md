# Vessica Operations Reference

## First checks

```bash
ves capabilities --json
ves doctor --json
ves knowledge status --json
ves prime --for codex --json
ves toolchain verify --profile workstation --json
ves setup codex --check --json
```

Vessica exposes one operational interface: `ves`. The plugin supplies guidance only. Never access Vessica databases or the knowledge HTTP API directly.

## Work modes

- Interactive: edit the current tree directly.
- Dispatch: create approved Vessica work and start a run.
- Hybrid: discover and plan interactively, then dispatch after confirmation.
- Hosted knowledge starts with Postgres lexical retrieval and no embeddings key.
- Semantic-hybrid retrieval is an optional user-funded upgrade.

Never infer dispatch permission from task size. Never create a writable local fallback during a hosted outage.

## JSON safety contract

Parse the `vessica.cli/v1` envelope. Read-only commands do not require confirmation. Preview mutations with `--dry-run --json`. A JSON mutation without approval returns `confirmation_required`; repeat it with `--yes --idempotency-key <stable-key> --json`.

Do not put tokens or embedding keys in agent-generated arguments. Use environment-variable references and Vessica authentication commands.

## Knowledge commands

```bash
ves knowledge context --query "<task>" --token-budget 4000 --json
ves entity resolve "<name>" --json
ves artifact list --status active --json
ves artifact get <id> --json
ves memory retrieve "<query>" --limit 5 --rerank auto --json
ves memory search "<query>" --json
ves memory get <id> --json
```

`memory retrieve` is the restoration path. Resolve named people, projects, or accounts first and pass exact entity IDs. Inspect ranking explanations, `ambiguity`, index freshness, and reranker metadata. `ambiguous_subject` is a hard stop: ask the user to identify the subject rather than applying the top-ranked candidate. Empty results are absence, not permission for broad synonym fanout. `memory search` is for administrative lexical inspection.

Context includes `ranking.version`, component weights, deterministic artifact policy, per-memory scores, artifact selection reasons, retrieval mode, index freshness, provenance, references, typed omissions, and token estimate. Artifacts and memories have separate type budgets so unrelated authority does not crowd out relevant durable context.

Create facts, decisions, and episodes only when durable value exists. Create instructions only on explicit user request. Artifacts are authoritative; memories summarize or optimize retrieval and should reference sources.

## Work and runs

```bash
ves epic draft --spec-file epic.json --json
ves epic add --spec-file epic.json --yes --idempotency-key <key> --json
ves run epic <epic_id> --yes --idempotency-key <key> --json
ves run watch <run_id> --jsonl --after-seq <last-seq>
ves run view <run_id> --json
ves run artifacts <run_id> --json
ves run receipt <run_id> --json
```

Confirm run start, resume, cancel, refinement, approval, rollback, and merge. Use persisted run state and receipts as terminal truth even when the last agent message sounds successful. `ves run prompt` refines an inactive retained sandbox; `ves run approve` and `ves run rollback` remain separate human decisions. Meaningful workflow outcomes become episode memories with epic, ticket, artifact, receipt, PR, commit, Linear, and run references.

## Hosted setup

```bash
ves up --dry-run --json
ves up --yes --stream jsonl
ves up resume <operation-id> --yes --stream jsonl
```

The command resolves compatible release images to immutable digests, provisions the control plane and lexical knowledge service, prepares the runner checkpoint, attaches the repository, creates a starting harness when absent, persists a repository map, and captures a verified multi-stack repository checkpoint with dependencies and caches but no credentials. Runs restore compatible checkpoints, refresh changed manifests before validation, and promote a scrubbed generation only after successful gates.

One Railway Postgres service contains two isolated logical databases. The control plane uses `vessica_control`; the knowledge service uses `vessica_knowledge`, which alone has pgvector. Their credentials, URLs, migration histories, and application access remain separate.

Connect Linear separately with `ves integration connect linear --project <id-slug-or-name> --dry-run --json`, then confirm with `--yes` and an idempotency key. Change the default with `ves integration switch-project linear --project <id-slug-or-name>` using the same dry-run and confirmation flow. These commands redeploy only the control plane.

There is no local fallback. Resume the same hosted onboarding operation after correcting a typed failure.

If only the local attachment is stale, preview and confirm `ves workspace forget`. It removes local hosted attachment metadata and credentials without deleting Railway resources or rewriting the repository harness and documentation; rerun `ves up` to rediscover or reattach.

If the attachment is healthy but its repository map/checkpoint is stale, preview and confirm `ves up --refresh` instead. Do not forget the workspace for ordinary source or dependency changes.

Authorize and diagnose the dedicated Railway CLI forwarding session with:

```bash
ves railway preview-session authorize
ves railway preview-session status
ves railway preview-session repair-key
ves railway preview-session smoke
```

The preview edge exposes only expiring run-scoped preview capabilities and has a separate origin from the dashboard/API. Keep the control-plane service at one replica; worker sandboxes scale separately.

Manage optional knowledge behavior after setup:

```bash
ves knowledge embeddings status --json
ves knowledge reranking status --json
ves knowledge server upgrade --dry-run --json
```

Lexical retrieval is healthy without embeddings. Semantic retrieval is explicit and user-funded. Model reranking remains disabled by default because retrieval v2 cleared the current quality gate without it. Enabling embeddings, reranking, or upgrading the knowledge service requires impact disclosure and confirmation.

## Failure routing

- `not_initialized`: run `ves up` in the repository or pass `--cwd`.
- `confirmation_required`: review and repeat with `--yes` and an idempotency key.
- Harness drift: run `ves harness audit --json`, preview sync, then confirm.
- Hosted unauthorized: run `ves auth status --json`; reconnect or rerun Railway setup.
- Hosted unavailable: inspect `ves railway status --json` and `ves railway logs`; do not fall back locally.
- Toolchain mismatch: run `ves toolchain verify --profile workstation --json`; use the worker profile only when the full coding-agent environment, including Playwright Chromium, is expected locally.
- Plugin mismatch: run `ves setup codex --check --json`; reinstall through the normal released or `make install` path instead of bypassing checksum/version checks.
- Stale repository orientation: preview `ves up --refresh --dry-run --json`; do not delete a healthy hosted attachment.
- Linear projection failure: Vessica remains canonical; retry projection rather than duplicating work.
- `index_fresh: false`: embeddings are catching up; lexical retrieval remains usable.
- `ambiguous_subject`: resolve the named entity or ask the user; never restore the first candidate by rank alone.
- Poor retrieval: inspect score components, artifact reasons, omissions, selectors, entity hints, and token budget.

## Server details

The control plane uses only `VES_CONTROL_DATABASE_URL`; the knowledge server uses only `VES_KNOWLEDGE_DATABASE_URL`. Knowledge server variables include `KNOWLEDGE_API_TOKEN`, `KNOWLEDGE_EXPORT_TOKEN`, and `KNOWLEDGE_WORKSPACE_ID`; `EMBEDDING_API_KEY` is optional. `/healthz` checks process health; `/readyz` reports database, migration, embedding worker, and index state.

The API uses `vessica.knowledge/v1`. Writes require bearer auth, idempotency, actor headers, and provenance. Export/import use a separate token. Direct API use is for Vessica services and integrations, not normal developer workflows.
