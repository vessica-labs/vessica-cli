---
name: work-with-vessica
description: Choose between direct interactive coding, Vessica dark-factory dispatch, or a hybrid workflow.
---

# Work With Vessica

For substantial coding requests, determine the user's intended mode: interactive edits here, dispatch through Vessica, or hybrid planning followed by dispatch. Never infer dispatch merely from task size.

Use `ves capabilities --json` and `ves prime --for codex --json` for context; the prime response includes selected authoritative artifacts, instructions, decisions, facts, episodes, and provenance. In interactive mode, edit normally and do not call Vessica lifecycle commands. In dispatch mode, use the create-epic and dispatch-epic workflows. In hybrid mode, inspect and draft locally, then request confirmation before persisting or starting work.
