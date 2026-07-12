---
name: create-epic
description: Convert a conversation into a validated Vessica epic intent for the design pipeline, with a ticket graph only when the user explicitly asks to pre-plan one.
---

# Create Epic

By default, capture only the outcome the user wants as an epic with `title` and `body`. Do not invent implementation subtasks, a PRD, an ADR, validation tests, or a ticket graph during conversational epic creation. Those are outputs of `ves run epic` and its design pipeline.

Write a temporary JSON spec containing `title`, `body`, and an empty `tickets` array. Run `ves epic draft --spec-file <path> --json` and parse `data.valid`. Present the proposed epic intent to the user and explain that Vessica will produce the PRD, ADR, ticket decomposition, and validation plan when the epic is run.

Persistence requires confirmation. After confirmation, run `ves epic add --spec-file <path> --yes --idempotency-key epic-<unique> --json`. In a hosted workspace this creates the canonical Vessica record and publishes the epic to Linear. Use returned identifiers and never call Linear directly. Starting the design/build run is a separate confirmation boundary handled by `dispatch-epic`.

Only include `tickets` when the user explicitly asks for a manually pre-planned or imported ticket graph. In that opt-in flow, every ticket must have a unique `key`, `title`, optional `type` and `body`, and `depends_on` keys; present the full graph before asking for persistence confirmation.

On `invalid_spec`, repair missing titles, unknown dependency keys, duplicates, or cycles and draft again. On `hosted_publish_failed`, retry with `ves epic publish <local_epic_id> --yes --idempotency-key <same-key> --json`. On `confirmation_required`, stop and request confirmation.
