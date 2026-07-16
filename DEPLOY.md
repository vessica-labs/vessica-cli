# Hosted deployment

The production image is built by the root `Dockerfile`. Railway runs `ves control-plane migrate` as the pre-deploy command, then starts `ves control-plane serve`. Readiness is exposed at `/readyz` and verifies database connectivity.

The control plane currently requires one replica. The runtime database lease rejects accidental same-deployment scale-out; keep the Railway replica count at one. Rolling releases use deployment-aware lease handoff.

Worker sandboxes use a pinned Codex CLI, pnpm, Playwright, and Railway CLI toolchain. Update versions deliberately, verify upstream checksums or lockfiles, run the complete test gate, and document the change. Never replace pins with `latest` installers.

Before production deployment, verify required secrets described in `SECURITY.md`, Postgres pool limits, public dashboard/preview origins, and the health endpoint. A successful build is not sufficient: verify the terminal Railway deployment status and a live readiness request.
