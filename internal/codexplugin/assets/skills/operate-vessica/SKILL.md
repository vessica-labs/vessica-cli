---
name: operate-vessica
description: Explain, install, configure, diagnose, or operate the hosted-first Vessica CLI and knowledge server.
---

# Operate Vessica

Read [references/operator-guide.md](references/operator-guide.md) before answering operational or troubleshooting questions.

Use `ves capabilities --json`, `ves doctor --json`, `ves knowledge status --json`, and read-only workspace, Railway, preview-session, harness, run, or plugin status commands to verify current state when the question concerns this machine. Prefer typed JSON evidence over assumptions.

Always direct users and agents through `ves`; do not instruct them to edit databases, call the knowledge API directly, or modify managed state files. Hosted lexical retrieval without an embeddings key is healthy. Preserve the hosted single-authority rule during failures.

Before mutations, show or run the dry-run form, explain the impact, obtain confirmation, then use `--yes` with an idempotency key. Keep credentials in environment variables, Keychain, Vessica credential storage, or Railway secrets. Use `ves setup codex --check --json` for plugin/CLI diagnosis, `ves up --refresh` for repository orientation/checkpoint refresh, and `ves knowledge server upgrade` only for an explicitly approved knowledge-service release change.
