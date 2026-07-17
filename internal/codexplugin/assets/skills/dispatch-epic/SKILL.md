---
name: dispatch-epic
description: Start an approved Vessica epic run locally or in the configured cloud execution environment.
---

# Dispatch Epic

Inspect `ves capabilities --json`, `ves doctor --json`, and `ves epic status <epic_id> --json`. Preview with `ves run epic <epic_id> --preview --pr draft --dry-run --json` and summarize runner, sandbox, concurrency, preview, and PR behavior. Treat the hosted control plane as authoritative; do not create a local fallback if it is unavailable.

Starting execution requires confirmation. In a hosted workspace run `ves run epic <epic_id> --preview --pr draft --yes --idempotency-key run-<unique> --json`, retain `data.run.id`, and monitor it separately. Use local execution only inside an explicit `ves dev` workspace; pass `--stream jsonl` and parse `vessica.stream/v1` records. Do not invoke Linear or cloud-provider CLIs directly.
