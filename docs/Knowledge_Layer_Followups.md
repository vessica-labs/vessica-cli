# Knowledge Layer Deferred Follow-ups

These items are intentionally outside the MVP production boundary approved on 2026-07-11.

## Durable chunked import tracking

Current snapshots are checksummed and event-idempotent, so the same import can be retried safely. Import status is process-local and large snapshots are submitted as one request. A future version should persist import sessions, chunk checksums, accepted ranges, and resume cursors.

## Team identity and invitations

The MVP deploys one workspace-scoped service with separate ordinary and export/import bearer credentials. Actor-bound claims, expiring invitations, team membership, token rotation UI, and enterprise identity are deferred.

## Relationship lifecycle

Relationships are immutable assertions created once. Current workflows correct knowledge by appending a new relationship or a `supersedes` assertion. Relationship version/update endpoints should be added only when a concrete mutation workflow requires them.

## Postgres lexical ranking

Hosted retrieval uses semantic candidates plus a lexical fallback. PostgreSQL full-text ranking is deferred because semantic retrieval is required in hosted mode and SQLite already provides BM25 for zero-key solo mode.

## Broader operations

Persistent distributed rate limiting, production metrics dashboards, scheduled backup orchestration, restore drills, and a web administration UI remain future operations work.

## Hosted worker checkpoint reuse

The live MVP acceptance completed successfully, but its first Railway sandbox downloaded Playwright, Chromium, and browser system packages during bootstrap despite a configured worker checkpoint. Align checkpoint version selection with the bootstrap toolchain and verify browser executables are present before release so warm runs avoid this startup cost.
