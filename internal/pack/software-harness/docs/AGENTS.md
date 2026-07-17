# AGENTS.md

This repository is managed with Vessica (`ves`).

## Agent rules

1. Prefer `ves` for memory, tickets, and context.
2. Do not invent ad hoc TODO/plan files when Vessica state exists.
3. Follow ARCHITECTURE.md, DESIGN.md, TESTING.md, DEPLOY.md, SECURITY.md.
4. Respect the current execution mode.
5. In a hosted workspace, Vessica is the state authority. Do not create a writable local fallback when the hosted service is unavailable.
6. Parse `vessica.cli/v1` JSON and `vessica.stream/v1` JSONL for automation; do not scrape human output.

## Engine-managed runs

When invoked by `ves run epic`, Vessica owns ticket lifecycle and run state.

Do not run lifecycle commands from inside the coding task:
- `ves ticket claim`
- `ves ticket close`
- `ves ticket heartbeat`
- `ves ticket release`
- `ves memory add`

Implement the requested change, run relevant local checks, and return a concise evidence summary. The Vessica engine will commit, merge, close tickets, create receipts, and update memory/state after the runner returns.

## Standalone/manual agents

Only when operating outside `ves run epic` and the user explicitly asks for
manual ticket management, use Vessica ticket lifecycle commands.

## Useful commands

```bash
ves prime --json
ves ticket ready --json
ves ticket claim --next --epic <epic_id> --agent <agent_id> --lease 45m --json
ves memory add --stdin --json
ves ticket close <ticket_id> --agent <agent_id> --evidence <receipt_id> --json
```
