---
name: use-agents
description: Discover, create, edit, invoke, and monitor durable Vessica general agents through the ves CLI.
---

# Use Vessica Agents

Use this skill when the user wants a persistent cloud agent, asks which agents
are available, or wants to invoke or monitor a general agent. These agents are
distinct from Vessica coding runs: their definitions are workspace-wide and
their executions use `arun_` IDs.

## Discover before invoking

Read the active registry and select by purpose, not name alone:

```bash
ves agent registry --json
```

If no existing agent fits, describe the proposed definition and obtain the
user's confirmation before creating it. Owners can create or edit agents;
owners and members can invoke and view them.

## Create and edit

Prefer the natural-language builder unless the user supplied a complete
`vessica.agent/v1` document:

```bash
ves agent create --description "<requested behavior>" --review --json
ves agent draft view <draft_id> --json
ves agent draft approve <draft_id> --yes --idempotency-key <stable-key> --json
```

Use `ves agent create --file <path>` for a structured definition. Use
`ves agent update <name-or-id> --description "..." --review` for an AI-assisted
edit. Never silently enable tools, increase a budget, or change a heartbeat.

## Invoke and reconnect

Start an ad-hoc run and consume the JSONL stream incrementally:

```bash
ves agent run <name-or-id> --prompt "<task>" --stream=jsonl
```

Parse `vessica.stream/v1` records. Retain the greatest event sequence seen. If
the stream disconnects, reconnect with:

```bash
ves run watch <arun_id> --jsonl --after-seq <greatest-seq>
```

Use `ves run view`, `ves run logs`, and `ves run cancel` for `arun_` IDs. Report
attempt boundaries, child-run IDs, typed tool failures, usage, terminal state,
and critic results. Persisted run state is authoritative; never infer success
from conversational text alone.

## Security and boundaries

- Use only `ves`; do not call the control plane, Railway, OpenAI, or the
  knowledge server directly.
- Do not print credentials or unrestricted tool output.
- An inactive runtime requires `ves auth login openai --env OPENAI_API_KEY`.
- Creating agents, changing definitions, schedules, or budgets, and cancelling
  runs are mutations that require explicit user authority.
