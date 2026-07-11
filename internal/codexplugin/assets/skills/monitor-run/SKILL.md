---
name: monitor-run
description: Monitor a Vessica run, resume event consumption, and summarize progress or blockers.
---

# Monitor Run

Run `ves run view <run_id> --json`, then `ves run watch <run_id> --jsonl --after-seq <last_seq>`. Parse JSONL records with schema `vessica.stream/v1`; save the greatest event sequence so monitoring can resume after interruption.

Monitoring is read-only and needs no confirmation. Summarize current phase, completed work, active agents, failures, preview and PR links, and the last sequence. If disconnected, rerun watch from the saved sequence. Do not scrape pretty output.
