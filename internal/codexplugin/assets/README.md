# Vessica for Codex

This plugin teaches Codex to operate Vessica through the version-matched `ves`
CLI. It contains no database, Railway, Linear, knowledge-service, or MCP
implementation. The hosted control plane and `ves` remain authoritative.

## Included workflows

- Choose interactive coding, Vessica dispatch, or a hybrid workflow.
- Set up or recover a hosted repository attachment and engineering harness.
- Validate and, after confirmation, create an epic intent.
- Preview, confirm, dispatch, monitor, resume, refine, review, approve, or roll
  back Vessica runs.
- Resolve entities and retrieve/version authoritative artifacts and durable
  memories with provenance and ambiguity safeguards.
- Diagnose hosted state, Railway preview forwarding, knowledge retrieval, and
  plugin/CLI compatibility.

The `skills/` directory contains the exact workflow contracts. Operational
questions route through `operate-vessica`, whose reference documents the
versioned CLI/JSON safety contract and hosted recovery rules.

## CLI bootstrap

`scripts/ensure-ves.sh` reads `scripts/cli-version.txt`, installs the matching
release under `~/.vessica/bin`, and validates the archive against the published
`checksums.txt` before invoking it. Do not bypass a missing or mismatched
checksum and do not substitute an unrelated `ves` from `PATH`.

## Safety contract

- Read-only inspection may run without confirmation.
- Preview mutations with `--dry-run --json`; after confirmation use `--yes` and
  a stable `--idempotency-key`.
- Parse `vessica.cli/v1` JSON and `vessica.stream/v1` JSONL, not pretty output.
- Never place tokens or key values in generated command arguments.
- Never create writable local fallback state for an unavailable hosted
  workspace.
- During `ves run epic`, the engine owns ticket lifecycle, integration, gates,
  receipts, and workflow-memory updates.

Hosted knowledge is healthy in lexical mode without an embeddings key. For
restoration, use one focused `ves memory retrieve` call, constrain named
subjects with resolved entity IDs, and stop on `ambiguous_subject` rather than
guessing. Model reranking is disabled by default.
