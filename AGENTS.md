# Repository guidance for agents

Read `ARCHITECTURE.md`, `SECURITY.md`, and `TESTING.md` before changing runtime behavior.

Keep transport code in `internal/cli`, `internal/dashboard`, and `internal/controlplane` thin. Put reusable lifecycle behavior in `internal/app`, concurrency guarantees in `internal/state`, and workflow-phase behavior in `internal/run`. Do not introduce a dependency from an inner package back to a transport package.

Preserve user changes in a dirty worktree. Never expose tokens, database URLs, OAuth payloads, Codex credentials, Railway credentials, or raw agent output in logs or tests. Hosted agent subprocesses must continue to use the environment allowlist and unprivileged runner user.

Run `./scripts/lint-arch.sh` while editing Go files. A file above 500 lines should prompt a cohesive split; a file above 800 lines cannot merge. Run the focused package tests first, then the checks in `TESTING.md`.

Treat `README.md`, `docs/Vessica_Operator_Guide.md`, and
`docs/Hosted_Railway.md` as the current user documentation. PRDs and versioned
ADRs under `docs/` are historical decision records unless their status says
otherwise; do not copy old local-first or future-scope claims into current
guidance. Update `docs/README.md` when the current/historical split changes.

The dashboard source is `web/dashboard`. Do not hand-edit generated files under `internal/dashboard/assets`; rebuild them and verify reproducibility with `./scripts/check-dashboard-assets.sh`.

The embedded dashboard documentation under `internal/dashboard/docs` is also
generated. Edit the matching source under `docs/`, then run
`./scripts/embed-dashboard-docs.sh`.

When Vessica invokes an engine-managed run, do not claim, close, heartbeat, or release tickets manually. The engine owns that lifecycle.
