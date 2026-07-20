---
name: monitor-run
description: Monitor a Vessica run, resume event consumption, and summarize progress or blockers.
---

# Monitor Run

Run `ves run view <run_id> --json`, then `ves run watch <run_id> --jsonl --after-seq <last_seq>`. Parse JSONL records with schema `vessica.stream/v1`; save the greatest event sequence so monitoring can resume after interruption. Use `ves run logs <run_id> --jsonl` for persisted replay and `--detail <event_id>` or `--agent-output` only when the user needs deeper evidence.

Monitoring is read-only and needs no confirmation. Summarize persisted run status, current phase, completed work, active agents, typed failures, preview and PR links, and the last sequence. A success-shaped agent message never overrides a failed run record. If disconnected, rerun watch from the saved sequence. Do not scrape pretty output or expose raw agent output by default.
