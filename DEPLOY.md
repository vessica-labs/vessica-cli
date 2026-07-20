# Hosted deployment

The production image is built by the root `Dockerfile`. Railway runs `ves control-plane migrate` as the pre-deploy command, then starts `ves control-plane serve`. Readiness is exposed at `/readyz` and verifies database connectivity.

The control plane currently requires one replica. The runtime database lease rejects accidental same-deployment scale-out; keep the Railway replica count at one. Rolling releases use deployment-aware lease handoff.

Worker sandboxes use a pinned Codex CLI, pnpm, Playwright, and Railway CLI toolchain. Update versions deliberately, verify upstream checksums or lockfiles, run the complete test gate, and document the change. Never replace pins with `latest` installers.

The toolchain contract fingerprint is part of the checkpoint name. Release
bootstrap scripts verify compatible CLI archives against published checksums;
missing or mismatched release metadata is a hard failure, not permission to use
an unverified binary.

The worker checkpoint also provides the common coding-agent baseline: `rg`, `fd`, `jq`, checksum-pinned `yq`, `bat`, Git/Git LFS/GitHub CLI, checksum-pinned Go and Node runtimes, Python, build tools, archive tools, and process diagnostics. Its checkpoint name is derived from the toolchain contract. Checkpoint creation verifies the complete contract as the unprivileged `vessica-agent` user, including an actual headless Chromium launch. Every worker launch performs a lightweight pinned-version and browser-asset integrity check. Use `ves toolchain verify --json` to run the full machine-readable readiness check locally.

`ves up` also derives a repository-bearing Railway checkpoint from that base.
It contains a clean remote checkout, a reviewable multi-stack preparation
contract, dependency and harness fingerprints, installed dependencies, and
warmed caches, but no variables or credentials. Warm runs boot from this
checkpoint, fetch only the remote delta, and create isolated integration and
ticket worktrees. Dependency trees are projected with copy-on-write behavior;
offline reconstruction and ordinary installation remain fallbacks, and
cross-worktree dependency symlinks are forbidden. A changed toolchain
invalidates the derived checkpoint; changed manifests refresh dependencies
before validation. A successful full run may promote only a scrubbed, clean
copy-on-write fork as the next generation. See
`docs/Repository_Checkpoint_Acceleration_Plan.md` for lifecycle and telemetry.

Engine-managed Codex calls use the checkpoint's attested MCP inventory and
disable all configured servers unless explicitly named in
`VES_CODEX_MCP_ALLOWLIST`. The override is invocation-scoped and must not mutate
the user's persistent Codex configuration.

Before production deployment, verify required secrets described in `SECURITY.md`, Postgres pool limits, public dashboard/preview origins, and the health endpoint. A successful build is not sufficient: verify the terminal Railway deployment status and a live readiness request.

Normal users do not deploy from source. `ves up` resolves released control-plane and knowledge-server images to immutable digests, provisions one PostgreSQL service, and idempotently creates `vessica_control` and `vessica_knowledge` with separate owners and URLs. pgvector is enabled only in the knowledge database. It then validates Railway Sandbox availability and waits for terminal `SUCCESS` plus `/readyz`. Hosted knowledge is valid with no `EMBEDDING_API_KEY`; it reports lexical retrieval and `embedding_state: not_configured` until the user explicitly enables embeddings.

Onboarding is journaled and resumable with `ves up resume`. New installations
use a dedicated `vessica-control-plane` Railway project rather than the target
application's repository name. Optional Linear configuration redeploys only the
control plane and targets the explicitly selected Linear project.
