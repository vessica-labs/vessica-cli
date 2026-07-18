# Repository Checkpoint Acceleration Plan

## Outcome

Hosted runs use Railway disk checkpoints as their primary provisioning path. A
warm run boots with the complete Vessica toolchain, a clean repository checkout,
installed dependencies, and reusable package caches. Docker images remain a
deployment artifact and cold fallback; they are not the normal run bootstrap.

## Checkpoint hierarchy

1. **Toolchain checkpoint** — the existing fingerprinted Vessica worker base.
   It contains pinned Node, Go, pnpm, Codex, Playwright/Chromium, Git/GitHub
   tools, Python, shell utilities, and the unprivileged `vessica-agent` account.
2. **Stack specialization** — repository inspection selects a Node, Go, Python,
   Rust, Ruby, Java, or generic dependency preparation contract. Extra runtime
   packages are installed only in the derived repository checkpoint.
3. **Repository checkpoint** — a clean checkout at a known commit plus installed
   dependencies and warmed language/package caches. Its immutable name includes
   the canonical repository, base commit, dependency fingerprint, and toolchain
   fingerprint.

Variables, provider credentials, model authentication, and network mode are not
checkpointed. Railway injects them for each new sandbox.

## Run path

```text
run request
  -> resolve compatible repository checkpoint
  -> create fresh Railway sandbox from checkpoint
  -> inject current variables and private-network mode
  -> verify lightweight runtime integrity and Codex authentication
  -> fetch and hard-reset to the current remote default branch
  -> refresh dependencies only if dependency manifests changed
  -> consume the one-time checkpoint marker
  -> use /workspace/repo directly as the integration checkout
  -> execute the Vessica phase graph
```

Retained-run resume and refinement do not resynchronize the repository because
the one-time checkpoint marker has already been consumed. This preserves the
run branch and uncommitted preview state.

## Implemented waves

### Wave 0 — measurable baseline

- Persist infrastructure timing events before phase execution begins.
- Include infrastructure spans and both execution and request-to-finish elapsed
  time in the final receipt.
- Measure control-plane queueing, checkpoint resolution, sandbox creation,
  checkpoint boot, runtime verification, auth verification, worker download,
  repository sync, dependency refresh, pack readiness, integration checkout,
  and total worker-process time.

### Wave 1 — immutable base

- Keep the versioned toolchain checkpoint as the reproducible cold fallback.
- Run the expensive synthetic package-script and Chromium launch smoke only
  when building the immutable checkpoint.
- Use a lightweight version/browser-asset integrity check for every warm fork.

### Wave 2 — first-install repository analysis

- Extend the existing Railway orientation sandbox to clone into
  `/workspace/repo`, fingerprint dependency manifests, detect the stack, install
  dependencies without provider/model secrets, and capture a server-side
  repository checkpoint.
- Publish the checkpoint contract into repository-scoped hosted metadata.
- Automatically rebuild missing or toolchain-incompatible snapshots during
  `ves up`; `ves up --refresh` deliberately refreshes the mapped commit and
  snapshot.

### Wave 3 — warm execution

- Prefer a compatible repository checkpoint over the generic toolchain base.
- Fetch only the Git delta and preserve installed dependency directories.
- Refresh dependencies only when the manifest fingerprint changes.
- Use the checkpoint checkout directly instead of cloning a second integration
  worktree.
- Reuse a complete committed engineering pack or the CLI's embedded release
  pack instead of cloning the pack repository on each run.

### Wave 4 — correctness and lifecycle

- Immutable names prevent an active run from observing checkpoint replacement.
- Toolchain changes invalidate all derived repository checkpoints.
- Commit and dependency changes produce new names; old checkpoints remain safe
  for running sandboxes and can be garbage-collected later.
- The checkpoint contains a one-time marker so fresh forks synchronize while
  retained runs preserve their working state.
- The generic toolchain checkpoint remains the automatic fallback when metadata
  is absent, stale, or invalid.

### Wave 5 — benchmark and optimization loop

- Run a small real hosted epic after release and snapshot refresh.
- Compare request-to-worker-ready, phase, and request-to-finish time with the
  prior run.
- Attribute remaining time to Railway boot, network/control-plane work, model
  inference, repository commands, validation, preview publication, or PR work.

## Security contract

- Clone credentials exist only during checkout and are removed before repository
  dependency commands execute.
- Git remotes stored on disk never contain tokens.
- Dependency commands run as `vessica-agent`; Railway variables and model/provider
  credentials are not inherited by those commands.
- Each run receives a new filesystem fork and cannot mutate the golden snapshot.
- `.git` metadata remains protected from coding-agent processes during execution.

## Initial service-level objectives

- Warm checkpoint resolution: under 100 ms in the control plane.
- Railway sandbox creation and checkpoint restore: under 10 seconds at p50.
- Source-only Git synchronization: under 5 seconds for typical repositories.
- Dependency preparation on an unchanged lockfile: zero install work.
- Sandbox request to worker ready: under 20 seconds at p50.
- Infrastructure overhead before the first model-backed phase: under 30 seconds
  at p50, with every larger span attributable in the receipt.

These are rollout targets, not claims about Railway's current experimental
sandbox performance. The production epic benchmark supplies the first measured
post-change result.
