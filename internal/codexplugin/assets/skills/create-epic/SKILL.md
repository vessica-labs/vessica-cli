---
name: create-epic
description: Convert a conversation into a validated Vessica epic and dependency-aware ticket graph.
---

# Create Epic

Write a temporary JSON spec with `title`, `body`, and `tickets`; every ticket has a unique `key`, `title`, optional `type` and `body`, and `depends_on` keys. Run `ves epic draft --spec-file <path> --json` and parse `data.valid`.

Present the epic, tickets, and dependencies to the user. Creation requires confirmation. Then run `ves epic add --spec-file <path> --yes --idempotency-key epic-<unique> --json`. In a hosted workspace this creates the local record, publishes the hosted graph, and returns real Linear identifiers. Use the returned IDs and never call Linear directly.

On `invalid_spec`, repair missing titles, unknown dependency keys, duplicates, or cycles and draft again. On `hosted_publish_failed`, retry with `ves epic publish <local_epic_id> --yes --idempotency-key <same-key> --json`. On `confirmation_required`, stop and request confirmation.
