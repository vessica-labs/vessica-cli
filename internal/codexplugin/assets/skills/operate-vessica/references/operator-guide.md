# Vessica Operations Reference

## First checks

```bash
ves capabilities --json
ves doctor --json
ves knowledge status --json
ves prime --for codex --json
```

Vessica exposes one operational interface: `ves`. The plugin supplies guidance only. Never access Vessica databases or the knowledge HTTP API directly.

## Modes

- Interactive: edit the current tree directly.
- Dispatch: create approved Vessica work and start a run.
- Hybrid: discover and plan interactively, then dispatch after confirmation.
- Solo knowledge: embedded SQLite plus FTS5/BM25, no API key, `retrieval_mode: lexical`.
- Hosted knowledge: workspace service plus Postgres/pgvector and embeddings, normally `retrieval_mode: semantic_hybrid`.

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
ves memory search "<query>" --json
ves memory get <id> --json
```

Context includes `ranking.version`, component weights, deterministic artifact policy, per-memory scores, artifact selection reasons, retrieval mode, index freshness, provenance, references, omissions, and token estimate.

Create facts, decisions, and episodes only when durable value exists. Create instructions only on explicit user request. Artifacts are authoritative; memories summarize or optimize retrieval and should reference sources.

## Work and runs

```bash
ves epic draft --spec-file epic.json --json
ves epic add --spec-file epic.json --yes --idempotency-key <key> --json
ves run epic <epic_id> --yes --idempotency-key <key> --json
ves run watch <run_id> --json
ves run view <run_id> --json
ves receipt view <receipt_id> --json
```

Confirm run start, resume, cancel, refinement, approval, rollback, and merge. Meaningful workflow outcomes become episode memories with epic, ticket, artifact, receipt, PR, commit, Linear, and run references.

## Railway promotion

```bash
export EMBEDDING_API_KEY='...'
ves railway up --embedding-api-key-env EMBEDDING_API_KEY --source /path/to/vessica-cli --dry-run --json
ves railway up --embedding-api-key-env EMBEDDING_API_KEY --source /path/to/vessica-cli --yes --idempotency-key <key> --json
```

The command resolves a compatible public knowledge image to an immutable digest, provisions the service and isolated Postgres, configures secrets, waits for `SUCCESS` and readiness, imports and verifies SQLite, saves a read-only recovery copy, then changes authority to hosted.

If promotion fails, local SQLite remains authoritative and the command can be retried. Do not manually flip configuration.

## Failure routing

- `not_initialized`: run `ves init` in the repository or pass `--cwd`.
- `confirmation_required`: review and repeat with `--yes` and an idempotency key.
- Harness drift: run `ves harness audit --json`, preview sync, then confirm.
- Hosted unauthorized: run `ves auth status --json`; reconnect or rerun Railway setup.
- Hosted unavailable: inspect `ves railway status --json` and `ves railway logs`; do not fall back locally.
- Linear projection failure: Vessica remains canonical; retry projection rather than duplicating work.
- `index_fresh: false`: embeddings are catching up; lexical retrieval remains usable.
- Poor retrieval: inspect score components, artifact reasons, omissions, selectors, entity hints, and token budget.

## Server details

Hosted server variables are `DATABASE_URL`, `KNOWLEDGE_API_TOKEN`, `KNOWLEDGE_EXPORT_TOKEN`, `KNOWLEDGE_WORKSPACE_ID`, and `EMBEDDING_API_KEY`. `/healthz` checks process health; `/readyz` reports database, migration, embedding worker, and index state.

The API uses `vessica.knowledge/v1`. Writes require bearer auth, idempotency, actor headers, and provenance. Export/import use a separate token. Direct API use is for Vessica services and integrations, not normal developer workflows.
