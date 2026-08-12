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

## Release-blocker remediation

The Task 3 security review was resolved before later task work began:

- Consent is bound to the active workspace ID and a fresh membership lookup;
  cached session roles cannot authorize consent after a role or membership
  change. Cross-workspace and stale-role tests cover both layers.
- The configured canonical HTTPS origin is mandatory while MCP is enabled.
  Authorization codes, token exchanges, refresh rotation, stored access and
  refresh grants, and `/mcp` validation all require the exact canonical
  `resource`; no request `Host` fallback exists. Token and revocation requests
  accept bounded form bodies only.
- Write idempotency is now a durable claim/finalize protocol. A unique ledger
  claim is committed before the side effect, concurrent callers elect one
  owner, completed results replay, failed work is retryable, and stale leases
  can be reclaimed. Claim material and caller keys are hashed and bounded.
- Browser origins are explicitly same-origin or allowlisted HTTPS origins.
  Malformed JSON, missing/invalid authorization, invalid stored scopes,
  hostile origins, unknown tools, SDK pre-handler schema denials, scope denials,
  and allowed calls are durably audited. Audit errors are returned rather than
  discarded.
- Outlook contacts are persisted as normalized ingestion items and deliberately
  excluded from message/event receipt source-ID partitions. Validation now ports the approved Python v2 contract's unsafe
  full-batch scan, canonical accounts, nonempty bounded typed strings, nullable
  initial watermarks, due dates, recurrence and change semantics, warnings and
  count rules. Stored source IDs are trimmed. Independent expected-previous and
  monotonic checkpoints advance transactionally; stale batches roll back.
- Refresh replay transactionally revokes its whole token family. Revoked
  clients cannot validate access or refresh credentials. MCP credential,
  trigger-claim, and action-claim prefixes are redacted everywhere.
- Agent and agent-run MCP responses now use explicit public DTOs. Runtime
  input, trigger, claim, lease, reservation, rate and resolved-knowledge fields
  are JSON-hidden on internal models as a second boundary.
- Subscription upsert/disable are explicitly destructive and idempotent. The
  scheduled probe remains exactly destructive=false, readOnly=false and
  idempotent=true.

### Additional RED/GREEN evidence

1. RED: foreign-workspace and removed-role dashboard sessions approved consent.
   GREEN: current workspace membership and role are re-read before consent.
2. RED: missing/wrong OAuth resource values and insecure public origins could
   reach grant paths. GREEN: exact-resource and canonical-origin tests now pass
   for authorize, token, refresh, access validation, and startup.
3. RED: the original check/act/append flow allowed concurrent duplicate side
   effects. GREEN: concurrent state claims have exactly one winner; failure,
   reclaim, completion and replay tests pass under the race detector.
4. RED: successful replay was initially observed as still claimed because
   redaction made stored structured JSON invalid. GREEN: JSON-preserving secret
   redaction and the explicit public trigger DTO produce a completed replay.
5. RED: contacts were validated but absent from durable items and receipts.
   GREEN: contact persistence and exact receipt-partition tests pass.
6. RED: full-batch unsafe warnings, unscoped client signals, invalid typed due
   dates, nullable initial checkpoints and stale/backward checkpoints were not
   handled with Python-contract parity. GREEN: parity/security corpus and
   transactional checkpoint tests pass.
7. RED: agent list/detail/run responses exposed input, rates, reservations,
   claims, leases and workspace internals. GREEN: explicit DTO leak tests pass.

### Final verification after remediation

- `go test ./... -count=1` — passed.
- `go test -race ./... -count=1` — passed; the final security-only adjustment
  was additionally checked with `go test -race ./internal/state ./internal/app
  ./internal/run ./internal/controlplane -count=1`.
- `go vet ./...` and `go build ./cmd/ves` — passed.
- `./scripts/lint-arch.sh` — passed with only the same two pre-existing soft
  file-length warnings.
- Dashboard API generation, 4 Vitest files / 6 tests, production build, and 2
  Playwright end-to-end tests — passed.
- Dashboard asset budget — passed at 148157 compressed bytes.
- `git diff --check` — passed.

The attempted generic `make lint` command has no repository target; the
authoritative `./scripts/lint-arch.sh` gate above passed. No push was performed.

## Round 2 release-blocker remediation

- Added explicit workspace-ID filters at the state boundary for agent and
  agent-run list/get, plus scoped application methods used by every MCP agent
  read tool. Two-workspace transport tests prove foreign agents and runs cannot
  be enumerated, filtered by agent ID, or fetched by direct ID.
- Added operation-specific conversation idempotency. The hashed MCP action key,
  optional conversation creation, sequence increment, message insert, and
  replay mapping commit in one transaction. A deterministic test performs the
  real domain mutation, simulates failed audit finalization and lease expiry,
  reclaims the audit, and proves replay returns the original IDs with one
  conversation and one message. The same recovery path is covered by the
  disposable PostgreSQL integration test.
- Verified the remaining write primitives at their durable boundaries: agent
  triggers replay one run, subscription upsert/disable preserve one record,
  Outlook batch/item/outbox keys replay existing records, finalized Outlook
  batches are safely repeatable, and the scheduled probe has no domain effect
  beyond its already-atomic ledger claim.
- Added transactional Outlook checkpoint reservations before batch creation.
  A stale or concurrently reserved expected checkpoint rejects before any
  batch, item, or outbox record exists. Finalization verifies the reservation,
  advances both checkpoints and batch state atomically, releases reservations,
  and safely recognizes a completed recovery.
- Contact updates remain durable Outlook ingestion items/outbox work, but never
  enter the Task 1 receipt partition. The server now validates that accepted,
  deduplicated, and rejected IDs are mutually exclusive and exactly partition
  submitted messages/events, with committed watermarks identical to the batch.
- Expanded the shared unsafe-instruction corpus to the Task 1 forms for
  ignore/disregard instructions and reveal/exfiltrate secrets or credentials.
- Translated official-SDK pre-handler schema failures at the transport boundary
  into the same stable typed `invalid_arguments` structured content returned by
  handler-level errors, while preserving the durable denial audit.
- Token and revocation endpoints reject credential/grant query parameters and
  consume only `PostForm` values after bounded form parsing. Refresh-token
  revocation now transactionally revokes its whole refresh family and every
  access token in that family; an endpoint-level test proves both are invalid.
- Hostile origins remain rejected before the request body is read. Their audit
  is intentionally attributed to `mcp_transport`: reading an untrusted
  cross-origin body merely to improve tool attribution would weaken the early
  origin boundary.

### Round 2 RED/GREEN evidence

1. RED: state compilation failed because no workspace-scoped agent/read methods
   existed. GREEN: state and MCP two-workspace enumeration/direct-ID tests pass.
2. RED: conversation recovery had no domain action record. GREEN: simulated
   finalize-failure/expired-lease recovery and hashed-key join tests pass with
   one actual side effect.
3. RED: stale Outlook checkpoints were detected only after item/outbox writes.
   GREEN: reservation tests reject before all three durable record types.
4. RED: contact IDs appeared in successful receipts. GREEN: receipt validation
   and transport tests persist contacts while partitioning only messages/events.
5. RED: SDK schema rejection was audited but lacked structured content. GREEN:
   the raw JSON-RPC response now contains typed `invalid_arguments` content.
6. RED: token/revoke query values were accepted through `r.Form`, and refresh
   revoke affected one token only. GREEN: query-only and family-wide endpoint
   revocation tests pass.

### Round 2 verification

- Focused state, application, control-plane, dashboard, CLI, and redaction
  suites — passed.
- Disposable PostgreSQL `TestPostgresHostedSchema`, including MCP conversation
  recovery — passed.
- `go test ./... -count=1` and the repository race gate — passed.
- `go vet ./...`, `go build ./cmd/ves`, `./scripts/lint-arch.sh`, dashboard API
  generation/unit/build/E2E, generated-asset reproducibility, asset budget, and
  `git diff --check` — passed.

Dashboard installation still reports the same 8 high-severity transitive npm
audit findings described above. No push was performed.

## Round 3 durability remediation

- Action claims now persist a SHA-256 argument fingerprint independently from
  their redacted audit payload. Claim identity is therefore bound to workspace,
  actor, tool, hashed idempotency key, and canonical arguments. Completed,
  failed, and expired claims reject changed arguments with the stable,
  non-retryable `idempotency_conflict` envelope before any handler executes.
  Conflict attempts also append a separate durable denied audit record.
- Real agent-trigger and subscription-upsert/disable recovery tests commit the
  domain mutation, simulate failed action-ledger finalization, expire and
  reacquire the claim, and prove identical arguments replay one durable result
  while changed arguments cannot mutate state. The agent and subscription
  recovery paths also run against disposable PostgreSQL.
- Outlook item insertion and outbox enqueue are now one state transaction and
  one application operation. Processing-key collision tests inject the enqueue
  failure and prove the preceding item insert rolls back, so committed accepted
  items always have deliverable outbox work. Deduplication also verifies that a
  previously committed item has its durable outbox record.
- Contact updates retain the normalized `contact:<email>` identity while their
  ingestion source ID adds a stable canonical content/evidence hash. Identical
  observations deduplicate; changed evidence or content creates and enqueues a
  new durable observation without adding contacts to receipt source-ID
  partitions.
- Outlook checkpoint reservations now store only a hashed claim token with a
  bounded lease. Same-batch resume rotates the fence; competing batches remain
  blocked. Finalize and release require the current unexpired fence, both
  checkpoint rows are released transactionally, stale owners cannot advance or
  release checkpoints, and a crashed batch can reclaim its reservation and
  deduplicate already committed item/outbox work.

### Round 3 RED/GREEN evidence

1. RED: an expired or failed action claim compared only its idempotency key, so
   changed arguments could reacquire and execute. GREEN: state and MCP tests now
   receive `idempotency_conflict`, record the denial, and preserve exactly one
   actual agent run or subscription mutation after finalize failure.
2. RED: item persistence and outbox enqueue were separate commits. GREEN: an
   injected processing-key conflict rolls back the new item on SQLite and
   PostgreSQL; committed item and outbox counts remain equal.
3. RED: every update for one contact used `contact:<email>` as the source ID, so
   changed evidence was discarded as a duplicate. GREEN: canonical observation
   hashing deduplicates identical maps regardless of key order/case while
   persisting and enqueueing changed observations.
4. RED: checkpoint reservations had no lease or fence, so an abandoned batch
   could block ingestion indefinitely and any same-batch caller could finalize.
   GREEN: crash/reclaim tests rotate the hashed fence, reject the stale owner,
   replay existing item/outbox work once, and finalize only through the current
   lease owner.

### Round 3 verification

- `go test ./... -count=1` — passed.
- `go test -race ./... -count=1` — passed; after the final conflict-audit
  adjustment, `go test -race ./internal/state ./internal/app ./internal/run
  ./internal/controlplane -count=1` also passed.
- Disposable PostgreSQL `TestPostgresHostedSchema` — passed with agent and
  subscription finalize-failure/reacquire, atomic Outlook rollback, and
  checkpoint fence-reclaim coverage.
- `go vet ./...`, `go build ./cmd/ves`, `./scripts/lint-arch.sh`, and
  `git diff --check` — passed. Architecture lint reports one pre-existing soft
  file-length warning for `internal/controlplane/agent_runtime_api.go`.
- Dashboard clean install/API generation, 4 Vitest files / 6 tests, production
  build, 2 Playwright E2E tests, and the 148157-byte compressed asset budget —
  passed.

The dashboard clean install still reports the same 8 high-severity transitive
npm audit findings. This task did not modify dashboard dependencies. No push
was performed.
