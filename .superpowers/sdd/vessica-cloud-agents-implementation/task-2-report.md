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
