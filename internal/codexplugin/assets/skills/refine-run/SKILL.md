---
name: refine-run
description: Apply an approved refinement prompt to a retained Vessica run sandbox.
---

# Refine Run

Inspect `ves run view <run_id> --json` and ensure the run is not active. Put substantial prompt text in a temporary file. Preview with `ves run prompt <run_id> --file <path> --dry-run --json`.

Refinement changes code and requires confirmation. After confirmation run `ves run prompt <run_id> --file <path> --yes --idempotency-key prompt-<unique> --json`. Report changed files, commit, push status, checks, and preview URL from the JSON response. Do not enter the sandbox or bypass Vessica receipts.
