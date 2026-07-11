---
name: operate-vessica
description: Explain, install, configure, diagnose, or operate the Vessica CLI and hosted knowledge server. Use when users ask how Vessica works, how to run a command, how solo and hosted modes differ, how to promote to Railway, how to interpret JSON or ranking output, or how to troubleshoot Vessica, Linear, harness, run, or knowledge failures.
---

# Operate Vessica

Read [references/operator-guide.md](references/operator-guide.md) before answering operational or troubleshooting questions.

Use `ves capabilities --json`, `ves doctor --json`, `ves knowledge status --json`, and read-only status commands to verify current state when the question concerns this machine. Prefer typed JSON evidence over assumptions.

Always direct users and agents through `ves`; do not instruct them to edit databases, call the knowledge API directly, or modify managed state files. Distinguish solo lexical mode from hosted semantic-hybrid mode. Preserve the single-authority rule during hosted failures.

Before mutations, show or run the dry-run form, explain the impact, obtain confirmation, then use `--yes` with an idempotency key. Keep credentials in environment variables, Keychain, Vessica credential storage, or Railway secrets.
