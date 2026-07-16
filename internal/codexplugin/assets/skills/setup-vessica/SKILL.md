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

Never request an embeddings API key during quickstart. Never place provider credentials in arguments or edit Vessica state files directly.
