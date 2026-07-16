# Hosted deployment

The production image is built by the root `Dockerfile`. Railway runs `ves control-plane migrate` as the pre-deploy command, then starts `ves control-plane serve`. Readiness is exposed at `/readyz` and verifies database connectivity.

The control plane currently requires one replica. The runtime database lease rejects accidental same-deployment scale-out; keep the Railway replica count at one. Rolling releases use deployment-aware lease handoff.

Worker sandboxes use a pinned Codex CLI, pnpm, Playwright, and Railway CLI toolchain. Update versions deliberately, verify upstream checksums or lockfiles, run the complete test gate, and document the change. Never replace pins with `latest` installers.

The worker checkpoint also provides the common coding-agent baseline: `rg`, `fd`, `jq`, checksum-pinned `yq`, `bat`, Git/Git LFS/GitHub CLI, checksum-pinned Go and Node runtimes, Python, build tools, archive tools, and process diagnostics. Its checkpoint name is derived from the toolchain contract. Checkpoint creation and every worker launch verify the contract as the unprivileged `vessica-agent` user, including an actual headless Chromium launch. Use `ves toolchain verify --json` to run the same machine-readable readiness check locally.

Before production deployment, verify required secrets described in `SECURITY.md`, Postgres pool limits, public dashboard/preview origins, and the health endpoint. A successful build is not sufficient: verify the terminal Railway deployment status and a live readiness request.

Normal users do not deploy from source. `ves up` resolves released control-plane and knowledge-server images to immutable digests, provisions one PostgreSQL service, and idempotently creates `vessica_control` and `vessica_knowledge` with separate owners and URLs. pgvector is enabled only in the knowledge database. It then validates Railway Sandbox availability and waits for terminal `SUCCESS` plus `/readyz`. Hosted knowledge is valid with no `EMBEDDING_API_KEY`; it reports lexical retrieval and `embedding_state: not_configured` until the user explicitly enables embeddings.
