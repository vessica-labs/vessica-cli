---
name: setup-vessica
description: Set up or diagnose Vessica and its engineering harness in the current repository using the ves CLI.
---

# Setup Vessica

1. Run `ves capabilities --cwd "$PWD" --json` and parse only the JSON envelope.
2. If uninitialized, explain the proposed profile and run `ves init ... --dry-run --json`; obtain confirmation before the real command.
3. Inspect `ves doctor --json`. Never place credentials in command arguments; use `ves auth login <provider>` or documented environment references.
4. Preview harness installation with `ves harness install --dry-run --json`. After confirmation run it with `--yes --idempotency-key setup-<unique> --json`, then `ves harness audit --json`.
5. Report failed checks and exact recovery commands. Do not edit Vessica state files directly.
