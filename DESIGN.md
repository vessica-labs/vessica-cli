# Product and interface design

`ves` is the primary control surface. Commands should be safe in scripts and understandable interactively: mutating operations support dry-run/idempotency where relevant, require confirmation in non-interactive modes, emit stable JSON envelopes with `--json`, and preserve cancellation through the command context.

The local and hosted dashboards are alternate adapters over the same use cases, not separate products with different lifecycle rules. Their APIs use the schema documented in `docs/Vessica_Dashboard_v1_OpenAPI.yaml`; generated TypeScript clients and embedded assets must be reproducible from `web/dashboard`.

Human approval is an explicit boundary. Planning, coding, validation, and preview may be automated, but merging, destructive teardown, promotion, and credential-bearing infrastructure mutations require the documented confirmation or authorization path.

Errors should name the failed operation and suggest a concrete recovery action without exposing secrets or raw privileged logs. Current functionality must be described separately from roadmap or scale-out intent.
