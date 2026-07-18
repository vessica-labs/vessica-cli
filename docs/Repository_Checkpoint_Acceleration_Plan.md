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
  -> inject current variables and scoped worker database route
  -> verify the checkpoint attestation and Codex authentication
  -> fetch and hard-reset to the current remote default branch
  -> refresh dependencies only if dependency manifests changed
  -> consume the one-time checkpoint marker
  -> create an isolated integration Git worktree
  -> link immutable dependencies directly from the repository snapshot
  -> execute the Vessica phase graph
```

Retained-run resume and refinement do not resynchronize the repository because
the one-time checkpoint marker has already been consumed. This preserves the
run branch and uncommitted preview state.

## Operator checklist

Before selecting a repository checkpoint for a fresh hosted run:

- [ ] Confirm its recorded default-branch commit is still an acceptable starting
  point. Fetch the current default-branch delta during sandbox setup; if the
  checkpoint cannot be brought forward cleanly, do not use it.
- [ ] Compare the recorded dependency-manifest fingerprint with the current
  manifests and lockfiles. Refresh dependencies only when the fingerprint
  changed; treat a missing, stale, or mismatched fingerprint as incompatible.
- [ ] Verify the checkpoint attestation and the worker binary before executing
  work. Accept the snapshot binary only when its recorded worker/toolchain
  fingerprint matches; otherwise perform the full runtime verification or use
  a verified worker download.
- [ ] On any attestation, worker-binary, repository-sync, or dependency
  verification failure, discard the candidate for this run and create a fresh
  sandbox from the generic toolchain checkpoint. Do not repair or mutate the
  golden repository checkpoint in place; retain the last known-good checkpoint
  until a replacement has been fully verified and atomically published.

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
- Persist a fingerprinted runtime attestation after the full checkpoint smoke
  test. Warm forks use a subsecond attestation/path check and fall back to the
  full version/browser check on any mismatch.
- Persist the empty engine-managed MCP inventory in the checkpoint so the first
  agent does not launch a discovery subprocess.

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
- Create an isolated Git worktree from the checkpoint checkout without a
  second clone. Link the snapshot's immutable `node_modules` and other prepared
  dependency trees directly into it, with package installation retained only
  as a stale/missing-snapshot fallback.
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
- After a successful delta-based run, enqueue a copy-on-write fork that removes
  run/auth state, verifies a clean root, captures the new default-branch
  checkpoint, and atomically advances repository metadata. The active run never
  waits for this proactive refresh.

### Wave 5 — benchmark and optimization loop

- Run a small real hosted epic after release and snapshot refresh.
- Compare request-to-worker-ready, phase, and request-to-finish time with the
  prior run.
- Attribute remaining time to Railway boot, network/control-plane work, model
  inference, repository commands, validation, preview publication, or PR work.

### Wave 6 — orchestration critical path

- Route short-lived workers through Railway's public Postgres proxy while
  preserving the scoped Vessica database role and database. The persistent
  control plane continues to use the private database address.
- Start preview only after integration and build, never speculatively before a
  coding agent can begin.
- Make repository-wide lint, build, test, and preview engine-owned gates; coding
  agents run only targeted checks while editing.
- Run build before tests for repositories whose rendered tests consume build
  output.
- Run lint, architecture lint, and the build lane concurrently; run tests after
  build. If any lane needs repair, rerun the complete gate set serially.
- Remove preview PID/log artifacts before PR creation and retain a requested
  Railway sandbox until the launcher publishes its preview URL.

## Production benchmark

The final benchmark used the repository checkpoint
`vessica-repo-a987fb8602-ae022121ef-c2ca74fecb-87a7ba273f` and a one-sentence
documentation epic. It completed all planning, coding, lint, architecture lint,
build, tests, browser validation, preview publication, draft PR creation, and
receipt finalization.

| Measurement | Baseline | Final | Improvement |
| --- | ---: | ---: | ---: |
| Request to terminal completion | 666.0 s | 122.7 s | 81.6% |
| Sandbox to worker ready | 154.3 s | 13.5 s | 91.3% |
| Worker database open | 134.0 s | 0.079 s | 99.9% |
| Repository synchronization | 2.10 s | 1.13 s | 46.5% |
| Integration worktree | 7 ms | 48 ms | isolation added for correctness |
| Worktree dependency materialization | implicit repair | 9.04 s | deterministic cache-backed step |
| Total worker process | 632.7 s | 120.3 s | 81.0% |

The final draft PR changed exactly one documentation file, all harness gates
passed, and the public preview returned HTTP 200. These are single-run values,
not percentile claims.

## Implemented runtime acceleration follow-through

The post-benchmark runtime pass now includes:

1. `xs` planning emits or deterministically derives one validated ticket, and
   ticketization reuses it without a second model call.
2. Coders receive a bounded current-run context packet with planning artifacts
   and version-matched CLI and receipt contracts.
3. Each ticket worktree is preconfigured and verified as one exact Git
   `safe.directory` for the unprivileged agent.
4. Coders receive focused validation guidance and leave repository-wide gates to
   the engine.
5. Railway worktrees link the immutable dependency tree from the repository
   snapshot; reflink/offline/normal installs remain compatibility fallbacks.
6. Engine-managed Codex calls consume a pre-attested MCP inventory, disable
   configured MCP servers by default, and use an explicit allowlist for tasks
   that need them.
7. Epic status now follows completed, review, failure, cancellation, approval,
   and rollback outcomes instead of remaining stale at `planned`.
8. Explicit, localized `xs` epics use deterministic lean planning artifacts and
   skip the planning model call as well as duplicate ticketization.
9. The release workflow builds amd64 and arm64 containers on native runners,
   verifies both architectures, and publishes the multi-architecture manifest
   atomically. Archive/plugin assembly runs in parallel as a draft release; the
   release becomes public only after both paths pass.

These paths emit explicit infrastructure or agent metadata so the next hosted
benchmark can measure each saving independently. VM sizing remains unchanged.

## Remaining optimization waves

1. **Per-run receipt scope:** when an epic is intentionally rerun, report only
   artifacts and tickets whose `source_run_id` matches the receipt, while
   linking prior attempts separately.
2. **Preview reuse:** reuse the validation preview in the explicit preview
   phase instead of issuing a second start request, even though the current
   second request is subsecond.
3. **Worker API boundary:** replace direct worker database access with a
   short-lived, least-privilege control-plane worker API. This removes database
   credentials from sandboxes and makes the fast route provider-independent.
4. **Checkpoint garbage collection:** retain the last known good checkpoint and
   garbage-collect older unreferenced generations after a safety window.
5. **Deeper monorepo profiles:** extend the persisted reviewable snapshot
   specification with package-level workspace roots and repository-specific
   native/database clients when evidence requires them.
6. **SLO telemetry:** publish p50/p95 timing histograms by repository, stack,
   checkpoint generation, and phase before changing VM size.

VM sizing is not the primary constraint in this benchmark. The final build took
about 5.6 seconds and tests about 0.6 seconds; the removed delays were database
network readiness, speculative preview waiting, duplicate validation, and
repair-agent work. Increase CPU or memory only when telemetry shows sustained
CPU saturation, memory pressure, or materially CPU-bound builds after these
orchestration improvements.

## Security contract

- Clone credentials exist only during checkout and are removed before repository
  dependency commands execute.
- Git remotes stored on disk never contain tokens.
- Dependency commands run as `vessica-agent`; Railway variables and model/provider
  credentials are not inherited by those commands.
- The worker database route keeps the scoped Vessica role/database credentials;
  only the Railway host and port use the authenticated public proxy. Moving to
  a short-lived worker API remains the preferred long-term boundary.
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
