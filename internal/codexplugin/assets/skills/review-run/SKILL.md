---
name: review-run
description: Review Vessica run artifacts, validation evidence, preview, pull request, and receipt before deciding its outcome.
---

# Review Run

Collect `ves run view <run_id> --json`, `ves run artifacts <run_id> --json`, and `ves run receipt <run_id> --json`. Present validation results, material changes, preview and PR links, failures, and missing evidence.

Approval and rollback require explicit confirmation. After confirmation use either `ves run approve <run_id> --merge-method squash --yes --idempotency-key approve-<unique> --json` or `ves run rollback <run_id> --yes --idempotency-key rollback-<unique> --json`. Never merge or close the PR directly.
