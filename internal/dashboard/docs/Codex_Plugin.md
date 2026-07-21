# Vessica Codex Plugin

The first-party Vessica plugin gives Codex a conversational operating layer over
the `ves` Go CLI. It does not connect directly to Postgres, the knowledge HTTP
API, Railway, Linear, or an MCP server. The CLI remains the only product
implementation and the hosted control plane remains authoritative.

## Install or update

From an installed release:

```bash
ves setup codex --plugin
ves setup codex --check --json
```

The first command updates the Vessica-managed section of repository
`AGENTS.md`, writes the plugin source to the personal marketplace, and installs
or updates the plugin assets. The read-only check reports whether `ves`, Codex,
and the plugin manifest are present.

The released plugin archive also contains `scripts/ensure-ves.sh`. This is the
plugin-only bootstrap path: it reads the CLI pin packaged with the plugin,
downloads the matching platform archive to `~/.vessica/bin`, verifies it against
the release `checksums.txt`, and then executes that managed binary. It does not
trust an unrelated same-version `ves` elsewhere on `PATH`.

For a source checkout, `make install` builds the version declared in `VERSION`,
refreshes the plugin source and cachebuster, reinstalls it in Codex, and verifies
that the cached plugin and CLI versions match. Use `make install-cli` only when
you intentionally do not want to refresh the plugin.

## Skill routing

| Skill | Use it for |
|---|---|
| `work-with-vessica` | Choose direct interactive coding, Vessica dispatch, or a hybrid workflow. Task size alone never authorizes dispatch. |
| `setup-vessica` | Preview and confirm `ves up`, bootstrap the compatible CLI, resume onboarding, refresh repository orientation, or recover a stale local attachment. |
| `create-epic` | Convert a conversation into a validated epic intent, then persist it only after confirmation. Ticket graphs are opt-in. |
| `dispatch-epic` | Inspect readiness, preview run options, confirm, start an epic run, and retain the run ID. |
| `monitor-run` | Read persisted run truth and resume `vessica.stream/v1` consumption from the last event sequence. |
| `refine-run` | Apply a confirmed prompt to a retained, inactive run sandbox through `ves run prompt`. |
| `review-run` | Collect the run, planning artifacts, receipt, validation, preview, and PR evidence before confirmed approval or rollback. |
| `manage-harness` | Inspect, install, audit, lint, or synchronize managed repository guidance while preserving unmanaged content. |
| `use-knowledge` | Resolve entities; retrieve, version, or diagnose artifacts and memories; and manage optional retrieval features. |
| `use-agents` | Discover the active registry; build or edit durable agents; invoke them; and resume `arun_` streams from the greatest sequence. |
| `operate-vessica` | Explain commands, diagnose setup/hosted state, operate Railway preview forwarding, and maintain the knowledge service. |

## Confirmation and authority boundaries

Read-only discovery may run immediately. Before a mutation, the plugin previews
the exact action with `--dry-run --json`, explains its effect, and obtains user
confirmation. The confirmed call uses `--yes` and a stable idempotency key.

Separate confirmations apply to:

- creating or updating durable epics, tickets, artifacts, memories, or harnesses;
- starting, resuming, cancelling, or refining a run;
- approving a merge or rolling back a run;
- enabling embeddings or model reranking;
- forgetting a local attachment or deleting hosted Railway resources.

Inside an engine-managed `ves run epic`, the engine owns ticket claims,
heartbeats, release/close, integration, repository-wide gates, receipts, and
workflow-memory updates. A coding agent edits its assigned worktree, performs
focused checks, and returns evidence; it must not manually drive Vessica
lifecycle commands.

## Setup and recovery

The plugin begins setup with the managed bootstrap:

```bash
scripts/ensure-ves.sh up --cwd "$PWD" --dry-run --json
```

After confirmation it repeats the command with `--yes --stream jsonl`. `ves up`
creates or discovers the Railway installation, attaches the current repository,
installs a missing harness, produces a repository map, and builds a verified
multi-stack repository checkpoint. If setup is interrupted, the plugin resumes
the same operation with `ves up resume`; it does not start a competing
installation.

Use `ves up --refresh` when the attached repository needs a fresh map and
checkpoint. Use `ves workspace forget` only when local attachment metadata or
credentials are stale. `forget` must not delete Railway resources or rewrite
the harness, documentation, or unmanaged `AGENTS.md` content.

Quickstart never requests an embeddings key or Linear project. Lexical hosted
knowledge is healthy immediately; both features are explicit post-setup options.

## Knowledge restoration

For a named project, person, or account, the plugin resolves the entity first
and passes the exact ID to one focused `ves memory retrieve` or
`ves knowledge context` call. It inspects ranking explanations, provenance,
index freshness, omissions, and reranker metadata.

`ambiguity: "ambiguous_subject"` is a hard stop. The plugin does not apply the
top candidate merely because it ranks first; it asks the user to identify the
subject. Empty retrieval remains an honest absence and does not trigger a fanout
of near-synonym searches.

Artifacts are authoritative. Memories optimize restoration and may contain
facts, decisions, explicit instructions, or work episodes. The plugin creates
an instruction only when the user explicitly asks for durable behavior. Hosted
API failures never cause a silent writable-local fallback.

## Runs and review

The plugin uses persisted records for status and evidence:

```bash
ves run view <run_id> --json
ves run watch <run_id> --jsonl --after-seq <last-seq>
ves run artifacts <run_id> --json
ves run receipt <run_id> --json
```

Monitoring surfaces phase progress, active agents, typed failures, preview and
PR links, and the last consumed sequence. Refinement uses `ves run prompt`; it
does not enter the sandbox or bypass receipts. Review presents missing evidence
as well as passing gates. Approval and rollback always remain explicit user
decisions and are performed through `ves`, never by calling GitHub directly.

General-agent runs follow the same persisted-stream rule. The plugin discovers
agents with `ves agent registry --json`, invokes one with `ves agent run ...
--stream=jsonl`, retains the greatest sequence, and reconnects with `ves run
watch <arun_id> --jsonl --after-seq <sequence>`. Agent definitions, schedules,
budgets, attempts, child runs, and critic results remain control-plane state.

## Security and current boundaries

- Parse `vessica.cli/v1` JSON and `vessica.stream/v1` JSONL; never scrape human output.
- Keep provider credentials in Codex/Vessica auth storage, Keychain, environment variables, or Railway secrets. Never echo key values into commands.
- Codex is the production runner. Claude, Cursor, Pi, and MCP setup targets install guidance but are not production execution adapters.
- Engine-managed Codex runs disable configured MCP servers by default and enable only `VES_CODEX_MCP_ALLOWLIST` entries.
- The plugin is a guidance/bootstrap package. It does not broaden the user's authorization to mutate Vessica, GitHub, Linear, or Railway.

## Troubleshooting

```bash
ves setup codex --check --json
ves capabilities --json
ves doctor --json
ves knowledge status --json
ves railway status --json
```

If plugin bootstrap reports a compatibility or checksum failure, update or
reinstall the released plugin rather than bypassing verification. If the plugin
source is current but Codex still shows an older cachebuster, rerun the normal
install/update path. If hosted state is unavailable, restore service or resume
the typed operation; do not create local product state as a fallback.
