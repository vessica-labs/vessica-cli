# Vessica CLI Reference

This is the public command inventory for Vessica CLI `0.2.45`. The root
`README.md` and `Vessica_Operator_Guide.md` explain the recommended workflows;
this page is the compact discovery map. Run `ves <command> --help` for flags and
`ves capabilities --json` for the versioned subset intended for coding agents.

## Global contract

All commands accept `--cwd` to select another repository and `--json` for the
`vessica.cli/v1` envelope. Long-running run streams use
`--stream=jsonl`/`vessica.stream/v1` instead.

Mutations should be previewed with `--dry-run --json`. JSON mutations that need
approval return `confirmation_required` until repeated with `--yes`; pass a
stable `--idempotency-key` when retrying a supported write. Diagnostics go to
stderr. Do not parse pretty output.

## Hosted setup and workspace

| Command | Subcommands | Purpose |
|---|---|---|
| `ves up` | `status`, `resume` | Discover or create the Railway installation, attach the repository, prepare its harness and checkpoint, or resume a journaled operation. Use `--refresh` to rebuild repository orientation and checkpoint metadata. |
| `ves workspace` | `status`, `forget` | Inspect the hosted workspace or remove only the local attachment and credentials. `forget` does not delete Railway resources or rewrite repository guidance. |
| `ves repo` | `list`, `status`, `connect`, `pr create`, `pr view` | Inspect attached repositories and GitHub pull-request integration. |
| `ves integration` | `connect linear`, `switch-project linear` | Connect optional hosted Linear projection or change its default project without rebuilding the knowledge service. |
| `ves railway` | `status`, `logs`, `approve`, `preview-session authorize`, `preview-session status`, `preview-session repair-key`, `preview-session smoke`, `down` | Operate the hosted deployment, native preview forwarding, approval, and teardown. |
| `ves dashboard` | `status` | Open the embedded dashboard or inspect the local dashboard process. |
| `ves dev` | `up`, `reset` | Explicit local SQLite/Docker development and test utilities. This is not the product onboarding path. |

## Readiness, configuration, and agents

| Command | Subcommands | Purpose |
|---|---|---|
| `ves status` | — | Show the effective workspace status. |
| `ves doctor` | — | Diagnose repository, provider, harness, runner, and hosted readiness. |
| `ves capabilities` | — | Return CLI version, schemas, agent-safe commands, available tools, authentication, and workspace attachment state. |
| `ves toolchain` | `verify` | Verify the workstation or complete pinned worker toolchain. |
| `ves config` | `list`, `get`, `set`, `unset` | Inspect or modify explicit developer-mode configuration. |
| `ves auth` | `login`, `logout`, `status` | Manage GitHub, Linear, Railway, Codex, and knowledge credentials. |
| `ves setup` | `codex`, `claude`, `cursor`, `pi`, `mcp` | Install managed repository guidance. `ves setup codex --plugin` also installs or updates the first-party Codex plugin; `--check` is read-only. |
| `ves prime` | — | Assemble workspace guidance, ready work, and bounded authoritative/durable context for a human or agent. |
| `ves completion` | `bash`, `fish`, `powershell`, `zsh` | Generate shell completion. |
| `ves version` | — | Print the CLI version. |

Codex is the current production execution backend. Claude, Cursor, Pi, and MCP
setup commands install guidance only; those runner names require simulation
mode when selected as execution backends.

## Harnesses and packs

| Command | Subcommands | Purpose |
|---|---|---|
| `ves pack` | `install`, `pull`, `sync`, `update`, `pin`, `origin get`, `origin set` | Install the default or custom Git-backed engineering pack, restore its lock, update its ref, or pin an immutable version/SHA. |
| `ves harness` | `install`, `create`, `audit`, `sync`, `lint`, `status` | Create and reconcile repository-specific guidance, explain drift, and run deterministic harness lint. |

## Product work and coordination

| Command | Subcommands | Purpose |
|---|---|---|
| `ves epic` | `draft`, `add`, `list`, `view`, `update`, `delete`, `plan`, `status` | Validate intent without persistence; create, inspect, edit, or delete canonical work; run planning through ticketization; and inspect lifecycle status. |
| `ves ticket` | `add`, `list`, `ready`, `view`, `update`, `delete`, `claim`, `heartbeat`, `release`, `close`, `block`, `unblock` | Manage dependency-aware manual work, claims, leases, evidence, and blockers. Engine-managed runs own these lifecycle calls themselves. |
| `ves wave` | `list`, `view`, `status` | Inspect dependency-ready execution waves. |
| `ves artifact` | `add`/`create`, `list`, `view`/`get`, `update`, `activate`, `supersede` | Create immutable authoritative artifacts, version their content, and change active authority. |

Conversation-derived epics should normally contain only `title`, `body`, and an
empty ticket list. `ves run epic` produces planning artifacts and ticket
decomposition unless the user explicitly supplies a pre-planned graph.

## Runs, evidence, and sandboxes

| Command | Subcommands | Purpose |
|---|---|---|
| `ves run` | `epic`, `ticket`, `list`, `view`, `logs`, `watch`, `resume`, `cancel`, `artifacts`, `receipt`, `preview`, `prompt`, `approve`, `rollback` | Execute phase-addressable work; stream/replay it; inspect persisted truth and evidence; refine retained work; publish previews; merge or reject the result. |
| `ves sandbox` | `list`, `view`, `logs`, `shell`, `tunnel`, `prompt`, `retain`, `destroy`, `gc` | Inspect and operate explicit local or retained Railway environments. |
| `ves receipt` | `list`, `view` | Inspect final delivery evidence. |
| `ves trace` | `list`, `view` | Inspect diagnostic traces. |

The run phase graph is `preflight → harness → plan → design → ticketize → code
→ build → validate → preview → pr → receipt`. `--start-at`, `--stop-after`, and
`run resume --from` address phases. Engine-managed gates own repository-wide
build, lint, architecture, test, preview, and receipt truth.

## Durable knowledge

| Command | Subcommands | Purpose |
|---|---|---|
| `ves knowledge` | `status`, `context`, `embeddings status`, `embeddings enable`, `embeddings disable`, `reranking status`, `reranking enable`, `reranking disable`, `server upgrade` | Inspect retrieval/index health, assemble bounded mixed context, manage optional semantic features, or upgrade only the hosted knowledge service. |
| `ves entity` | `create`, `resolve`, `search` | Create or resolve canonical identities used to constrain retrieval. |
| `ves memory` | `add`, `list`, `view`/`get`, `update`, `supersede`, `archive`, `search`, `retrieve` | Version durable facts, decisions, instructions, and episodes; inspect administratively; or use retrieval v2 for restoration. |
| `ves artifact` | `add`/`create`, `list`, `view`/`get`, `update`, `activate`, `supersede` | Manage authoritative PRDs, ADRs, designs, specifications, and plans. |
| `ves prime` | — | Combine repository guidance and selected active knowledge into bounded agent context. |

Hosted knowledge starts in healthy lexical mode with no embeddings key.
`ves memory retrieve` is the restoration path: it supports entity constraints,
weighted lexical/semantic ranking, explanations, index state, and an
`ambiguous_subject` stop. `ves memory search` remains a broad administrative
lexical operation. Conditional model reranking is separately configured and is
disabled by default.

## External tracker compatibility

`ves tracker connect|status|sync|push` remains available for explicit
developer-mode best-effort projection. `ves tracker pull` is a public command
that returns `unsupported`: Vessica is the work authority and does not import
tracker state through that legacy path. Hosted Linear users should use
`ves integration connect linear` instead.

## Internal service roles

The binary also contains hidden `control-plane` and migration/worker roles used
by the released Railway deployment. They are deployment internals, not normal
user or plugin commands. Follow `Hosted_Railway.md` and `DEPLOY.md` rather than
invoking hidden roles by hand.
