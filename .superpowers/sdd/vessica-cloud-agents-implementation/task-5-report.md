# Task 5 report — Control-plane web interaction, plugin packaging, and operations

## Outcome

Implemented the Task 5 web, plugin, and operator surfaces on
`codex/cloud-agents-mcp`. The control plane now supports workspace-scoped web
conversations with durable agent selection and runs; the dashboard presents
conversation/run/citation/audit evidence plus briefing and newsletter views;
the Vessica Codex plugin adds OAuth-protected remote MCP while retaining every
CLI-backed skill; and operators have owner-only health views, Prometheus
metrics, deployment configuration, and a production runbook.

No Telegram, LinkedIn, direct Microsoft Graph, model-runtime replacement,
Railway deployment, or live schedule creation was added. No push was performed.

## Delivered

- Migration v14 binds a conversation to an optional workspace-owned durable
  agent and indexes workspace recency. State/application methods list and get
  only current-workspace conversations, change agent selection only to a
  current-workspace agent, and preserve database-allocated message order.
- Dashboard APIs create/list/get conversations and send idempotent messages.
  Each web message creates or replays one durable `web_conversation` agent run,
  records a redacted action-ledger decision, and persists run/agent/ledger
  linkage in message metadata. Detail responses include current run status,
  terminal errors/output, citations, and owner action-ledger links.
- The React dashboard adds Conversations, Briefings, and Operations navigation.
  Conversations preserve the list/composer when a request fails; detail cards
  poll durable run state. Briefings and newsletters render versioned knowledge
  artifacts with freshness and citation links.
- The owner-only Operations API/UI covers OAuth failures, MCP calls/errors and
  latency, source checkpoints older than 24 hours, stale incomplete ingestion
  batches, rejected records, failed agent runs, missing morning/afternoon/daily
  artifacts, daily budget limits/reservations/spend, denied actions, and recent
  redacted action-ledger entries.
- `/internal/dashboard/metrics` retains the existing dashboard metrics and adds
  Prometheus-format OAuth, MCP, source/ingestion, rejection, failed-agent,
  missing-briefing, aggregate budget, and denied-action signals.
- The existing Codex plugin now includes `.mcp.json` with remote HTTP MCP at
  `${VES_MCP_PUBLIC_URL}/mcp`, declares it through `mcpServers`, and adds the
  `use-vessica-cloud` skill. Existing setup, epic, run, harness, knowledge,
  agent, and operator CLI skills remain. Shared Claude rendering continues to
  pass and keeps its Codex production-runner boundary.
- Release packaging asserts the MCP descriptor and cloud skill are present.
  The dashboard OpenAPI source and generated TypeScript contract were updated,
  and production assets/docs were regenerated and embedded.
- `docs/Cloud_Agents_Operator_Runbook.md` documents Railway configuration,
  ChatGPT Work project/connector/skill/OAuth setup, the exact weekday 6:30 AM
  and 4:30 PM America/Los_Angeles schedules, scheduled-write validation, key
  rotation and revocation, schedule catch-up, source suspension/recovery,
  alerts, and incident triage. It explicitly excludes Telegram, LinkedIn,
  Graph, database edits, and writable local fallbacks.

## TDD evidence

1. RED: dashboard conversation tests did not compile because conversations had
   no agent binding or web routes. GREEN: authenticated creation, selected
   agent, two durable runs, message sequences 1/2, run linkage, and a second
   workspace denial pass.
2. RED: plugin installation lacked `use-vessica-cloud`, `mcpServers`, and
   `.mcp.json`. GREEN: installation/package tests and the plugin-creator
   validator pass while existing CLI skills and Claude rendering remain intact.
3. RED: the conversation React test could not resolve a page implementation.
   GREEN: the page keeps its heading and start form visible while showing the
   structured API failure in an alert.
4. RED: no operator API or new metrics existed. GREEN: owner access returns the
   seeded OAuth/MCP denial, stale source, rejected receipt, missing artifacts,
   and ledger signals; member access is forbidden; Prometheus output exposes
   the expected counts and latency.

## Verification

- `go test ./... -count=1` — passed.
- `go test -race ./internal/state ./internal/app ./internal/run ./internal/controlplane ./internal/dashboard -count=1` — passed.
- `go vet ./...` — passed.
- `go build ./cmd/ves` — passed.
- `./scripts/lint-arch.sh` — passed with only the pre-existing 526-line soft
  warning in `internal/controlplane/agent_runtime_api.go`.
- Dashboard clean install, OpenAPI generation, 5 Vitest files / 7 tests,
  production build, and Playwright E2E — passed. The clean install reports the
  existing 8 high-severity transitive npm audit findings.
- Agent runtime 10 Vitest files / 34 tests, typecheck, and build — passed.
- Plugin-creator `validate_plugin.py` — passed against
  `internal/codexplugin/assets` (PyYAML supplied in an isolated temporary
  dependency directory because the system Python does not include it).
- Codex plugin, shared Claude plugin, and dashboard embed tests — passed.
- Dashboard asset budget — passed at 150,665 compressed bytes.
- `git diff --check` — passed.

## Operational boundary

The task documents—but does not perform—the external ChatGPT Work project and
Scheduled Task writes or live Railway enablement. Those are Task 6 actions and
require real workspace/OAuth credentials plus explicit deployment authority.

## Review corrections

The Task 5 P1/P2 review findings were corrected in a follow-up TDD pass:

- Every dashboard cookie route now rejects a session whose workspace differs
  from the active database workspace, requires a current membership, and uses
  the membership's current role instead of the session's cached role. Tests
  cover an owner cookie crossing workspaces and an owner demoted to member.
- The dashboard API accepts its existing caller-provided `Idempotency-Key`, and
  the conversation composer now retains one key across an ambiguous network
  failure. It rotates the key only after definitive success or when the user
  edits the request as a new action. A lost-response retry test proves one
  message and one durable run, with the second response replayed.
- Knowledge citation links now seed an exact search from the `citation` query
  and visibly identify the requested evidence. The React test follows a
  citation link and proves that the cited ID is queried and rendered.
- Missing briefing health now resolves the most recent expected weekday morning
  and afternoon slots in `America/Los_Angeles`, applies a 30-minute grace
  window, includes the expected local date in the signal, and treats an older
  canonical mapping as stale. Tests cover pre/post morning grace, post-afternoon
  grace, and weekend fallback.
- Action-ledger records now carry an explicit transport source. MCP call/error/
  latency and OAuth metrics count only `source=mcp`; dashboard conversation
  actions remain in the general/denied action ledger but cannot inflate MCP
  metrics. The regression test seeds both transports and proves the filter.
- Codex plugin source and release archives no longer contain a literal
  `${VES_MCP_PUBLIC_URL}` URL. `ves setup codex --plugin` validates a configured
  canonical HTTPS origin and generates a concrete, token-free `.mcp.json`; when
  absent it installs the complete CLI skill set without claiming MCP. Unsafe
  URLs fail before installation writes. A separate renderer accepts only a real
  registered `plugin_asdk_app_...` ID and emits an app-backed ChatGPT plugin
  candidate; it never invents an ID or claims the local Codex install reaches
  ChatGPT web. The runbook now follows the official OpenAI developer-mode,
  connection, test, and administrator-publication flow.

Follow-up verification passed: all Go tests, selected race suites, `go vet`,
CLI build, architecture lint (same pre-existing 526-line soft warning), 5
dashboard Vitest files / 9 tests, dashboard build, 2 Playwright tests, agent
runtime 10 files / 34 tests plus typecheck/build, source and rendered ChatGPT
plugin validation, and the 151,164-byte compressed dashboard asset budget.
