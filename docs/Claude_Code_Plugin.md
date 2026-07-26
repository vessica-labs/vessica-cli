# Vessica Claude Code Plugin

The first-party Vessica plugin gives Claude Code the same conversational
operating layer over the `ves` Go CLI as the Codex plugin. It does not connect
directly to Postgres, the knowledge HTTP API, Railway, Linear, or an MCP server.
The CLI remains the product implementation and the hosted control plane remains
authoritative.

The plugin lets Claude Code operate Vessica. It does not make Claude a Vessica
execution runner: production `ves run epic` workflows continue to use Codex.

## Install or update

From an installed Vessica release, run this in an attached repository:

```bash
ves setup claude --plugin
ves setup claude --check --json
```

The first command updates the Vessica-managed section of `CLAUDE.md`, renders a
versioned local marketplace under `~/.vessica/claude-marketplace`, and registers
or updates `vessica@vessica` when the `claude` executable is available. If
Claude Code is not installed yet, the command still prepares the marketplace
and returns the exact registration command. The read-only check reports the
CLI, manifest, marketplace, Claude availability, and installed-plugin state.

For a source checkout, `make install` builds the version declared in `VERSION`,
refreshes both first-party plugins, and validates the Claude marketplace. Use
`make install-cli` only when you intentionally do not want to update plugins.

The release artifact `vessica-claude-plugin_<version>.tar.gz` is a
self-contained marketplace. After extracting it, manual installation is:

```bash
claude plugin marketplace add /absolute/path/to/extracted-marketplace --scope user
claude plugin install vessica@vessica --scope user
```

## Included skills

Claude can select these skills automatically from their descriptions, or the
user can invoke them through the `/vessica:<skill>` namespace:

| Skill | Use it for |
|---|---|
| `work-with-vessica` | Choose direct interactive coding, Vessica dispatch, or a hybrid workflow. |
| `setup-vessica` | Preview and confirm hosted setup, refresh orientation, or recover a stale local attachment. |
| `create-epic` | Validate an epic intent and persist it only after confirmation. |
| `dispatch-epic` | Inspect readiness, preview options, confirm, and start a Codex-backed Vessica run. |
| `monitor-run` | Read persisted run truth and resume JSONL event consumption. |
| `refine-run` | Apply a confirmed prompt to a retained inactive run sandbox. |
| `review-run` | Review artifacts, validation, previews, pull requests, and receipts before approval or rollback. |
| `manage-harness` | Inspect, install, audit, lint, or synchronize the engineering harness. |
| `use-knowledge` | Resolve entities and retrieve or version authoritative artifacts and durable memories. |
| `use-agents` | Discover, create, invoke, and monitor durable Vessica general agents. |
| `operate-vessica` | Explain and diagnose the CLI, hosted workspace, Railway forwarding, and knowledge service. |
| `vessica-outlook-ingestion` | Read Outlook through Claude Desktop's authorized connector, classify response needs and contacts, validate a minimized batch, and hand it to Vessica without sending or modifying mail. |

The Claude bundle is rendered from the same workflow source as the Codex
plugin. Provider-specific rendering adjusts the client diagnostics,
local-memory boundary, `ves prime --for claude`, and the
`${CLAUDE_PLUGIN_ROOT}` path used to reach the bundled bootstrap, and adds an
explicit guard against selecting Claude as the Vessica execution runner.

The Outlook skill requires the Outlook or Microsoft 365 connector to be
authorized and exposed in the current Claude Desktop session. It never moves
mailbox credentials or direct Outlook access into Vessica, and it remains a
read-only ingestion workflow.

## Safety and authority boundaries

- Read-only discovery may run immediately.
- Mutations are previewed with `--dry-run --json`, explained, and confirmed.
- Confirmed retryable writes use `--yes` and a stable idempotency key.
- The plugin parses `vessica.cli/v1` JSON and `vessica.stream/v1` JSONL instead
  of scraping human output.
- Tokens and key values never belong in generated command arguments.
- Hosted failures never create a writable local fallback.
- During `ves run epic`, the engine owns ticket lifecycle, integration,
  repository-wide gates, receipts, and workflow memories.

The bundled `scripts/ensure-ves.sh` installs the matching released CLI under
`~/.vessica/bin` and validates it against the published `checksums.txt`. Skills
reach it through `${CLAUDE_PLUGIN_ROOT}`, which remains valid after Claude copies
the plugin into its versioned cache.

## Troubleshooting

```bash
ves setup claude --check --json
claude plugin marketplace list --json
claude plugin list --json
claude plugin validate ~/.vessica/claude-marketplace --strict
ves capabilities --json
ves doctor --json
```

Restart Claude Code or run `/reload-plugins` after updating the plugin. If the
bootstrap reports a version or checksum failure, reinstall through the normal
Vessica path rather than bypassing verification. If hosted state is unavailable,
restore or resume the typed Vessica operation; do not create alternate local
product state.
