---
name: manage-harness
description: Install, inspect, audit, synchronize, or explain drift in a Vessica engineering harness.
---

# Manage Harness

Read status with `ves harness status --json`, drift with `ves harness audit --json`, and deterministic errors with `ves harness lint --json`. These reads need no confirmation.

For installation, preview `ves harness install --dry-run --json`; after confirmation use `ves harness install --yes --idempotency-key harness-<unique> --json`. For synchronization, explain the reported drift, preview it with `ves harness sync --dry-run --json`, then after confirmation run `ves harness sync --yes --idempotency-key harness-sync-<unique> --json`. Preserve unmanaged repository guidance and never edit `.vessica/pack.lock` by hand.
