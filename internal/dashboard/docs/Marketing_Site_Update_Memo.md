# Memo: Vessica marketing-site update input

**Date:** 2026-07-16
**Product baseline:** Vessica CLI 0.2.6
**Purpose:** Source material for a concise marketing-site brief. This memo
describes capabilities available today and keeps roadmap ideas separate.

## Recommended positioning

Vessica is a hosted engineering control plane for coding agents. It turns a Git
repository and a product intent into a durable, inspectable workflow for
planning, implementation, validation, preview, pull request, human approval,
and evidence.

The central benefit is not simply that an agent can write code. Vessica makes
agent-driven engineering repeatable: teams retain their operating instructions,
work graph, context, execution history, review surface, and delivery evidence
across sessions and repositories.

## Primary customer promise

**Move from intent to a reviewable, evidence-backed change without losing
control of the engineering process.**

Supporting promise: Vessica automates the workflow around coding agents while
keeping the human approval boundary explicit.

## Features and customer benefits

| Feature available today | Customer benefit |
|---|---|
| Hosted-first `ves up` onboarding | A team can attach an existing GitHub repository to a durable Railway-hosted workspace through one resumable setup flow instead of assembling a control plane by hand. |
| Multi-repository workspace | One Vessica installation can coordinate several repositories while keeping epics, runs, artifacts, jobs, and knowledge repository-scoped. |
| Versioned, forkable engineering harness | Teams own their agent roles, workflow rules, templates, architecture constraints, and validation practice instead of accepting a vendor-fixed prompt stack. |
| Epic-to-delivery workflow | Product intent becomes planning artifacts, dependency-aware tickets, concurrent implementation, integrated validation, preview, draft PR, and receipt in one phase-addressable run. |
| Durable coordination | Atomic claims, leases, heartbeats, dependencies, waves, and resumable phases reduce collisions and make interrupted work recoverable. |
| Isolated Railway workers | Coding and repository commands run as an unprivileged user with an allowlisted environment and protected Git metadata, limiting exposure of control-plane credentials and privileged hooks. |
| Pinned worker toolchain | A fingerprinted contract for Codex, Node, Go, pnpm, Playwright, GitHub CLI, and common agent utilities makes cloud runs more reproducible and diagnoses missing tools before work begins. |
| Live previews and retained sandboxes | Reviewers can evaluate the running change, request focused refinements, and retain the environment long enough to resolve feedback without restarting the entire workflow. |
| Draft PR and explicit approval | Vessica can automate through validation and review preparation while merge remains a deliberate, head-SHA-protected human decision. |
| Embedded dashboard and streams | Developers and leads can follow live agent messages, phases, sandboxes, evidence, knowledge, access, and repositories through a browser, TUI, human logs, or stable JSONL. |
| Receipts and persisted result truth | The final outcome includes artifacts, validation evidence, commits, preview and PR references, and persisted failure state—not merely success-shaped terminal output. |
| Durable knowledge with provenance | Vessica retrieves active artifacts, decisions, facts, instructions, and prior work with source references and ranking explanations, reducing repeated discovery and context loss. |
| Zero-key lexical retrieval | Useful hosted knowledge works immediately without requiring an embeddings API key; semantic-hybrid retrieval is an optional, reversible upgrade funded by the user. |
| Focused Linear integration | Teams can select a Linear project by ID, slug, or name, project Vessica-created work there, and switch the default project without rebuilding the workspace or knowledge service. |
| Recovery-oriented operations | Stage-based onboarding, durable resume, typed errors, attachment-only forget, locked migrations, and readiness verification make partial setup and deployment failures recoverable. |

## Reliability and bug-fix story for the site

The 0.2 line materially improves the trust story. The marketing site should
describe these as product qualities, not as a changelog dump:

- **Onboarding that finishes or tells you exactly how to resume.** Setup now
  survives interrupted authentication, partial Railway provisioning, Sandbox
  enablement, deploy failures, and readiness delays through a durable operation
  journal.
- **Infrastructure separated from the application.** New installations use a
  dedicated `vessica-control-plane` Railway project instead of borrowing the
  target repository's name or deployment context.
- **Safe local recovery.** `ves workspace forget` removes a stale local hosted
  attachment and credentials without deleting cloud resources or rewriting the
  harness and repository documentation.
- **Verified installation paths.** Plugin bootstrap verifies compatible release
  archives and checksums; local installation refreshes and verifies the CLI and
  Codex plugin together.
- **Truthful run outcomes.** Human output and the dashboard preserve useful agent
  messages, but a persisted failed run is reported as failed across terminal,
  JSON, dashboard, and receipt surfaces.
- **Hardened hosted execution.** Database coordination is atomic, migrations are
  locked and separated from service startup, pools are bounded, vulnerable
  dependencies were upgraded, and duplicate control-plane replicas are rejected
  until scale-out guarantees are complete.

## Suggested site narrative

1. **Hero:** “A control plane for coding agents that carries work from intent to
   review.” Emphasize durable workflow and human control rather than generic AI
   code generation.
2. **Problem:** Agent work loses context, collides under parallelism, and becomes
   hard to inspect once it leaves a chat.
3. **How it works:** Attach a repository, describe an epic, let Vessica plan and
   execute in isolated workers, inspect the live preview and evidence, then
   approve the pull request.
4. **Differentiators:** Team-owned harness, durable knowledge, evidence/receipts,
   resumable orchestration, and one CLI plus embedded dashboard.
5. **Trust section:** Least-privilege workers, secret isolation, protected Git
   metadata, immutable toolchain contract, explicit merge approval, and
   persisted result truth.
6. **Integrations:** GitHub, Codex, Railway, and optional project-focused Linear.
7. **Call to action:** Install `ves`, run `ves up`, and attach an existing GitHub
   repository.

## Proof points suitable for concise copy

- One hosted workspace can attach multiple repositories.
- One command begins resumable hosted onboarding.
- The dashboard is embedded in the binary; users do not deploy a separate UI.
- Hosted lexical knowledge works without an embeddings key.
- Worker readiness includes a real headless Chromium launch, not only executable
  presence checks.
- Every run can retain events, raw agent output, planning artifacts, validation
  evidence, preview and PR references, and a receipt.
- Harness versions are pinned to immutable Git commit SHAs and can come from a
  team's own fork.

## Claims to avoid or qualify

- Do not call the product 1.0 or promise stable pre-1.0 command compatibility.
- Do not imply that Claude, Cursor, or Pi are production execution backends;
  Codex is the current production runner.
- Do not claim support for every Git or issue provider. GitHub is the complete
  repository/PR path; Linear is optional and Vessica remains canonical.
- Do not advertise horizontally scaled control-plane replicas. The current
  control plane intentionally runs as one replica; worker sandboxes scale
  separately.
- Do not present semantic retrieval as required or bundled. It is optional,
  user-funded, and enabled after quickstart.
- Do not market the future general-agent runtime, schedules, marketplace, ROI
  analytics, or enterprise identity as available today.
- Do not describe local SQLite/Docker mode as onboarding. It is an explicit
  contributor and test utility under `ves dev`.

## Recommended brief deliverables

The marketing-site brief should produce a revised hero, a three-step workflow,
five or six benefit-led capability blocks, a trust/reliability section, an
integrations row, current-boundary language, and a quickstart call to action. It
should use the README and operator guide as the source of truth for every
availability claim.
