# Task 3 report — OAuth 2.1 and remote MCP gateway

## Outcome

Implemented the feature-flagged remote MCP control-plane surface on
`codex/cloud-agents-mcp`. The gateway uses
`github.com/modelcontextprotocol/go-sdk v1.7.0` Streamable HTTP in stateless
JSON-response mode, reuses interactive dashboard identity for consent, and
issues only dedicated hash-at-rest MCP credentials. Existing REST, CLI,
dashboard, runtime, deployment, and plugin behavior remains unchanged when
`VES_MCP_ENABLED` is not explicitly `true`.

## Delivered

- OAuth authorization-server and protected-resource metadata, authorization
  code with mandatory S256 PKCE, refresh rotation, and revocation. Authorization
  consent requires the existing dashboard session; approval additionally
  enforces its CSRF token and configured same-origin check. Service credentials
  cannot impersonate interactive consent.
- Dedicated `vmc_`, `vma_`, and `vmr_` credential material. Authorization codes,
  access credentials, and refresh credentials are stored only as SHA-256 hashes;
  long-lived plaintext is returned once at the token boundary and excluded from
  state JSON.
- Official-SDK `POST /mcp` with a 1 MiB request limit, stateless transport,
  OAuth protected-resource challenges, typed input/output schemas, structured
  error envelopes, and tool scope metadata.
- Exact scopes: `knowledge:read`, `knowledge:write`, `agents:read`, `agents:run`,
  `conversations:write`, and `sources:manage`.
- Read tools for knowledge search/get, latest briefing, agents and agent runs,
  conversation history, and subscriptions. The handlers delegate to existing
  Vessica application/knowledge/agent services.
- Write tools for Outlook ingestion, durable agent runs, conversation messages,
  subscription upsert/disable, and the non-sensitive scheduled-write probe.
  Writes require stable idempotency keys and replay the durable ledger result.
- The scheduled-write probe is annotated exactly as non-destructive,
  non-read-only, and idempotent; its only durable effect is its action-ledger
  record.
- Every authorized tool decision and every identifiable unauthorized tool call
  enters the workspace-scoped action ledger. Arguments and results pass through
  the existing persistence-boundary redactor; bearer matching was corrected so
  no credential suffix survives redaction.
- Strict Outlook ingestion v2 validation for minimized records, exact fields,
  provenance, offsets and DST-safe timestamp ordering, independent watermarks,
  global source IDs, recurrence/change rules, normalized participants,
  evidence references, findings/signals/contact updates, recognized Outlook
  connector hosts, count consistency, and prohibited source-derived content.
  Items/outbox entries are durable before both checkpoints and batch state are
  finalized atomically.
- Deployment composition through `VES_MCP_ENABLED` and `VES_MCP_PUBLIC_URL`.
  OAuth/MCP paths are reserved from the dashboard fallback only while the
  surface is enabled.

## TDD evidence

1. RED: the initial focused control-plane test did not compile because the MCP
   server fields and OAuth/MCP route types did not exist.
   GREEN: discovery, S256 exchange, hashed token storage, feature flagging, and
   bounded Streamable HTTP initialization passed after the minimal routes.
2. RED: the redaction test retained the suffix of
   `Authorization: Bearer secret-value`.
   GREEN: matching bearer credentials before generic authorization fields made
   the shared redaction and MCP ledger tests pass.
3. RED: the MCP catalog test listed only the initial probe tools instead of the
   required 15-tool surface.
   GREEN: the complete typed read/write catalog, scope metadata, annotations,
   and application delegates passed official-SDK list/call tests.
4. RED: end-to-end tool tests initially failed on missing structured helpers
   and receipt decoding, then exposed replay-shape mistakes.
   GREEN: agent-run, conversation, subscription, Outlook submission, read calls,
   and idempotent replay all passed with a single durable side effect.
5. RED: dashboard consent tests failed because no external identity adapter
   existed.
   GREEN: the narrow dashboard adapter passed session, CSRF, origin, and
   service-identity rejection tests.
6. RED: strict Outlook contract tests accepted a record missing `direction`.
   GREEN: exact record shape, evidence, confidence, participant, timestamp,
   connector, and unsafe-string validation passed valid and invalid fixtures.
7. RED: atomic Outlook finalization did not exist, allowing separate checkpoint
   writes.
   GREEN: a state transaction now commits both checkpoints and queued batch
   lifecycle together after all items are durable.

## Verification

- `go test ./...` — passed.
- `go test -race ./internal/state ./internal/app ./internal/run ./internal/controlplane -count=1` — passed.
- `go vet ./...` — passed.
- `go build ./cmd/ves` — passed.
- `./scripts/lint-arch.sh` — passed; only the two pre-existing soft warnings
  remain (`internal/state/agent.go` and
  `internal/controlplane/agent_runtime_api.go`).
- `(cd web/dashboard && npm ci && npm run generate:api && npm test && npm run build)` — passed (4 files, 6 tests); npm reported 8 high-severity
  dependency audit findings without failing the existing gate.
- `./scripts/check-dashboard-assets.sh` — passed (148157 compressed bytes).
- `git diff --check` — passed.

## Concerns

- OAuth clients are deliberately pre-registered through the existing
  workspace-scoped application/state interface. Dynamic client registration was
  not part of the task brief and is not exposed publicly; deployment onboarding
  must provision the approved client IDs, redirect URIs, and scopes.
- The dashboard dependency gate continues to report 8 high-severity npm audit
  findings. This task did not change dashboard dependencies, and remediation is
  outside the OAuth/MCP scope.

No push was performed.
