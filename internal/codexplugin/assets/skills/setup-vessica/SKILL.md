---
name: setup-vessica
description: Set up or diagnose Vessica and its engineering harness in the current repository using the ves CLI.
---

# Setup hosted Vessica

1. Use `../../scripts/ensure-ves.sh up --cwd "$PWD" --dry-run --json` so plugin-only installs bootstrap the compatible CLI and verify its release checksum.
2. Parse the `vessica.cli/v1` plan. Explain the selected Railway workspace, resources, repository scan, harness action, and healthy zero-key lexical retrieval.
3. Obtain one confirmation, then run `../../scripts/ensure-ves.sh up --cwd "$PWD" --yes --stream jsonl`.
4. If setup returns a resumable operation, use `ves up resume <operation-id> --yes --stream jsonl`. For `sandbox_feature_disabled`, present the Railway Priority Boarding action before resuming.
5. Report the final receipt: workspace, repository, dashboard, retrieval mode, harness result, repository-map artifact, and sandbox verification.

If the local hosted attachment is stale or partial, preview `ves workspace forget --dry-run --json` and obtain confirmation before running `ves workspace forget --yes --idempotency-key forget-stale-attachment --json`. This forgets only local hosted attachment metadata and credentials; it must not delete Railway resources or rewrite the harness, documentation, or unmanaged `AGENTS.md` content.

Never request an embeddings API key or Linear configuration during quickstart. Never place provider credentials in arguments or edit Vessica state files directly.
