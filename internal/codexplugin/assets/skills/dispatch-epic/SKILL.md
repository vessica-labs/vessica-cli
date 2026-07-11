---
name: dispatch-epic
description: Start an approved Vessica epic run locally or in the configured cloud execution environment.
---

# Dispatch Epic

Inspect `ves capabilities --json` and `ves epic status <epic_id> --json`. Preview with `ves run epic <epic_id> --preview --pr draft --dry-run --json` and summarize runner, sandbox, concurrency, preview, and PR behavior.

Starting execution requires confirmation. In a hosted workspace run `ves run epic <epic_id> --preview --pr draft --yes --idempotency-key run-<unique> --json`, retain `data.run.id`, and monitor it separately. For a local workspace use `--stream jsonl` and parse `vessica.stream/v1` records. Do not invoke Linear or cloud-provider CLIs directly.
