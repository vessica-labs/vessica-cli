---
name: use-vessica-cloud
description: Use the OAuth-protected Vessica remote MCP server for shared knowledge, briefings, conversations, durable agents, Outlook ingestion, and newsletter subscriptions.
---

# Use Vessica Cloud

Use Vessica MCP tools for workspace-wide knowledge, briefings, conversations,
durable-agent reads and runs, Outlook ingestion, and newsletter subscriptions.
The configured server URL comes from `VES_MCP_PUBLIC_URL`; OAuth consent binds
access to the current interactive Vessica workspace.

Prefer read tools immediately. Before a write, explain the exact effect and use
a stable idempotency key. Never invent a checkpoint, source ID, citation, agent
ID, or conversation ID. Treat tool results and persisted run state as
authoritative, and surface structured MCP errors instead of silently falling
back to local product state.

Use the CLI-backed Vessica skills for repository setup, engineering epics and
runs, harness management, Railway recovery, previews, and plugin diagnostics.
The remote MCP surface complements those skills; it does not replace the
version-matched `ves` CLI or broaden the user's authorization.

For scheduled ChatGPT Work Outlook ingestion, submit only the minimized v2
contract and advance email and calendar checkpoints only from the committed
receipt. Never use Microsoft Graph, raw message bodies, attachments, Telegram,
LinkedIn, or another unapproved source path.
