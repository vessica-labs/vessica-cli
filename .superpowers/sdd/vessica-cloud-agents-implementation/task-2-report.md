# Task 2 report — Durable control-plane state and application services

## Outcome

Implemented additive migration `v6` and shared state/application interfaces on
`codex/cloud-agents-mcp`. The work is intentionally limited to the persistence
and reusable application boundary; it does not add OAuth/MCP HTTP routes,
runtime behavior, dashboard UI, deployment wiring, or plugin packaging.

## Delivered

- OAuth clients, one-time authorization codes, hashed access-token lookup, and
  hash-only refresh-token material with expiry, consumption/replacement, and
  revocation state. Secret-bearing hashes are excluded from JSON. Token issuance
  verifies the client belongs to the active workspace.
- Append-only action ledger, uniquely scoped by workspace and idempotency key,
  with actor/agent/run/tool/policy/arguments/result/latency/external-ID fields.
  Arguments and results are redacted at the persistence boundary.
- Shared conversations and database-allocated ordered messages for MCP and web
  adapters.
- Newsletter subscriptions, normalized item deduplication, retention metadata,
  and independent workspace-scoped source checkpoints.
- Outlook ingestion batches, global per-workspace immutable source IDs,
  per-item receipts, independent checkpoints, and leased outbox claim/complete
  markers.
- Agent task checkpoints and idempotent agent-run triggers. The trigger record
  reserves the stable external key before creating its agent run, preventing
  duplicate run creation on retries.
- Thin reusable `internal/app` methods that expose these state capabilities to
  future MCP and web adapters without adapter-to-adapter dependencies.

## TDD evidence

1. RED: `go test ./internal/state -run '^TestOAuthCredentialsUseHashedLookupAndRedactedJSON$' -count=1`
   initially failed because OAuth types and methods did not exist.
2. GREEN: the same test passed after the migration and hash-only OAuth state
   implementation.
3. RED: state behavior suite for append-only ledger, conversation ordering,
   newsletter deduplication/checkpoints, and Outlook deduplication/outbox
   failed because the state interfaces did not exist.
4. GREEN: the state behavior suite passed after its minimal implementation.
5. RED: refresh-token revocation and idempotent trigger/checkpoint tests failed
   because those interfaces did not exist.
6. GREEN: those tests passed after the relevant state methods were added.
7. RED: a cross-workspace OAuth issuance test initially accepted a client from
   another workspace.
8. GREEN: it passed after token/code issuance began checking the workspace-owned
   client.
9. RED: ledger argument/result redaction and leased Outlook outbox/receipt
   tests initially exposed missing redaction and durable processing methods.
10. GREEN: the focused state suite passed after adding redaction and leased
    claim/complete/receipt handling.

## Verification

- `go test ./internal/state ./internal/app -count=1` — passed.
- `./scripts/lint-arch.sh` — passed; one pre-existing soft-limit warning in
  `internal/controlplane/agent_runtime_api.go` (517 lines).
- `git diff --check` — passed.
- `go test ./...` — passed.
- `go vet ./...` — passed.
- `go build ./cmd/ves` — passed.

## Concerns

The refresh-token design intentionally stores only a hash, which supports
rotation/revocation but cannot return an existing refresh credential. A future
route that must issue a replacement should generate the new plaintext outside
state, hash it before calling state, and return plaintext only once from its
authorized transport boundary. No route currently exists in this task.

## Review remediation

- Added migration v7 trigger recovery metadata and a unique durable
  `(workspace_id, trigger_id)` agent-run identity.
- Added workspace ownership gates for newsletter, Outlook, conversation,
  task-checkpoint, and trigger parent-child boundaries. Public OAuth client IDs
  are now the consistent application contract; state resolves the internal ID
  only for storage.
- Changed authorization-code consumption to a conditional update with a
  required affected-row result, so concurrent consumers have exactly one
  winner on SQLite and Postgres.
- Added leased trigger claiming and recovery of a run that committed before its
  trigger link, avoiding a duplicate run after a crash/retry.
- Replaced standalone Outlook completion with one transaction that writes the
  receipt, item state, outbox marker, and terminal batch state together.
- Expanded the application service across the Task 2 OAuth, source, Outlook,
  and task-checkpoint operations.

### Review TDD evidence

1. RED: `go test ./internal/state -run '^(TestControlPlaneChildrenRejectForeignWorkspaceParents|TestOAuthAuthorizationCodeCanBeConsumedOnceConcurrently|TestAgentRunTriggerRecoversCreatedButUnlinkedRun|TestCompleteOutlookOutboxAtomicallyWritesReceiptAndLifecycle)$' -count=1`
   initially failed because recovery and atomic Outlook interfaces were absent.
2. GREEN: the same review-focused suite passed after the ownership, conditional
   OAuth consumption, trigger recovery, and atomic Outlook transition changes.
3. GREEN: `go test ./internal/state ./internal/app -count=1`, `go test ./...`,
   `go vet ./...`, `go build ./cmd/ves`, `./scripts/lint-arch.sh`, and
   `git diff --check` passed.

## Round 2 remediation

- Persisted trigger rate snapshots alongside the original trigger, so recovery
  creates a run only from the accepted durable request rather than retry input.
- Added refresh-token validation plus Outlook claim and fenced failure/retry
  application methods. Failure atomically retains the outbox, increments its
  durable attempt record, stores the error, reschedules it, and updates item and
  batch lifecycle state.
- Added application-level OAuth, Outlook, newsletter, and source-checkpoint
  behavior tests.
- Split the former control-plane state monolith into a 499-line core and a
  dedicated agent-trigger/checkpoint state file. The pre-existing `agent.go`
  remains slightly above the soft limit because its small trigger-aware run
  additions belong beside run creation.

### Round 2 TDD evidence

1. RED: `go test ./internal/state ./internal/app -run '^(TestAgentRunTriggerRecoveryUsesOriginalDurableRequest|TestCloudAgentServiceOAuthLifecycle|TestCloudAgentServiceOutlookFailureReschedulesWork|TestCloudAgentServiceSourcesAndCheckpoints)$' -count=1` failed because the durable rate snapshot, refresh lookup, and Outlook claim/failure service methods did not exist.
2. GREEN: the same focused suite passed after persisting/reusing the original
   trigger request and adding the application and fenced retry methods.
