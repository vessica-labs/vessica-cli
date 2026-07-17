# Vessica Dashboard v1 — Product Requirements Document

> **Implementation-history record.** Use the README and Operator Guide for the
> dashboard capabilities available in the current release.

**Product:** Vessica Dashboard  
**Document type:** Product Requirements Document  
**Version:** v1  
**Status:** Approved direction  
**Primary audience:** Vessica product and engineering  
**Last updated:** 2026-07-11

---

## 1. Executive Summary

The Vessica Dashboard is the visual operating surface for local and Railway-hosted Vessica workspaces. It complements, rather than replaces, the primary interaction modes: conversational use through coding tools with the Vessica plugin, direct `ves` CLI use, and issue-driven workflows through Linear or Jira.

The dashboard shows system health, active runs, sandbox progress, agent messages and events, previews, pull requests, receipts, and integration status. It also exposes the existing preview refinement and approval controls in a coherent run workspace.

The same dashboard ships inside the `ves` Go binary and runs in two modes:

- `ves dashboard` serves a loopback-only dashboard backed by local SQLite and local sandboxes.
- The Railway control-plane process serves the dashboard against hosted Postgres, Railway sandboxes, and the hosted knowledge service.

The local dashboard also provides documentation, onboarding, diagnostics, and a guided “Move to Railway” workflow backed by the same provisioning application service as `ves railway up`.

---

## 2. Goals

V1 must:

1. Provide one dashboard experience in solo and hosted modes.
2. Show current control-plane, database, knowledge-service, integration, run, and sandbox health.
3. Stream versioned agent messages and run events with reconnect and resume support.
4. Provide fast access to live previews and associated refinement, approval, and rollback controls.
5. Show run phases, tickets, artifacts, evidence, receipts, branches, commits, and pull requests.
6. Embed version-matched documentation and novice onboarding.
7. Let a local user initiate and monitor the same promotion workflow as `ves railway up`.
8. Use shared Go application services so CLI and HTTP behavior remain equivalent.
9. Ship as part of the existing `ves` binary without requiring a second dashboard service or runtime.
10. Enforce appropriate local and hosted authentication, authorization, CSRF, and audit controls.
11. Provide a read-oriented knowledge explorer for entities, artifacts, memories, relationships, provenance, and versions.
12. Deliver a clean, beautiful, accessible interface with first-class light and dark themes.

### Success criteria

- A solo user can run `ves dashboard --open` without Docker, Node, a cloud account, or a separate server installation.
- A hosted user can see active runs and sandbox state from another machine.
- Event streaming resumes after browser refresh without losing or duplicating events.
- A user can open a preview, submit a refinement, inspect the result, and approve or roll back from one run workspace.
- A novice can understand local status and complete a verified Railway promotion from the dashboard.
- CLI and dashboard mutations produce identical state transitions, evidence, and audit records.

---

## 3. Non-Goals

V1 is not:

- A replacement for Codex, the `ves` CLI, Linear, Jira, or GitHub.
- A general visual workflow builder.
- A code editor or terminal emulator.
- A replacement for Railway’s infrastructure dashboard.
- A visual knowledge-graph reasoning canvas or full knowledge-authoring and curation workflow.
- A billing, ROI, or enterprise administration dashboard.
- A separately deployed frontend service.
- A public anonymous interface to workspace state or previews.
- A mechanism for the browser to hold Railway, GitHub, Linear, database, or embedding credentials.

---

## 4. Personas and Primary Workflows

### 4.1 Solo developer

- Opens the local dashboard after `ves init`.
- Reads setup and harness documentation.
- Inspects local readiness, runs, sandboxes, events, previews, and receipts.
- Starts a guided promotion to Railway.

### 4.2 Team developer

- Opens the hosted dashboard from any machine.
- Monitors work triggered through Codex, CLI, Linear, or Jira.
- Opens previews and reviews evidence.
- Requests refinements or approves completed work.

### 4.3 Engineering lead

- Sees concurrent epics, runs, agents, blockers, failures, and retained sandboxes.
- Opens the associated tracker issues, PRs, artifacts, receipts, and previews.
- Reviews audit history for consequential actions.

### 4.4 New user

- Learns the Vessica model through embedded documentation.
- Sees missing prerequisites and exact recovery steps.
- Moves from local solo operation to a verified hosted workspace.

---

## 5. Architecture

### 5.1 Packaging

The dashboard frontend lives in `vessica-cli/web/dashboard`, is built during release, and is embedded in the Go binary with `go:embed`.

The frontend stack is TypeScript, React, and Vite. UI components use shadcn/ui primitives and styling uses Tailwind CSS. Installed users receive compiled assets and do not need Node. Node is required only for Vessica development and release builds.

The dashboard must define Vessica-owned design tokens through Tailwind CSS variables rather than accepting shadcn/ui defaults as the finished visual design. The component system must support cohesive light and dark themes, responsive layouts, strong typography, restrained motion, clear status semantics, and accessible interaction states.

The Go dashboard package owns static-asset serving, browser sessions, API handlers, SSE streams, and security headers.

### 5.2 Runtime modes

```text
Local:
Browser -> ves dashboard -> shared application services -> SQLite/local Docker

Hosted:
Browser -> control-plane HTTP server -> shared application services
        -> Postgres/Railway/knowledge service/integrations
```

The dashboard must not shell out to `ves`. Cobra commands and HTTP handlers call the same application services.

### 5.3 Shared application services

V1 requires explicit services for:

- System status and diagnostics.
- Runs and phases.
- Sandboxes and retention.
- Events and streams.
- Preview brokering.
- Refinement and review actions.
- Integrations.
- Railway provisioning and promotion.
- Knowledge-service status.
- Knowledge browsing and retrieval explanations.
- Documentation metadata.

Application services enforce validation, idempotency, authorization, state transitions, evidence creation, and audit behavior. Transport adapters do not duplicate business logic.

---

## 6. Information Architecture

### 6.1 Overview

The overview shows:

- Local or hosted mode and workspace identity.
- CLI/control-plane and dashboard versions.
- Database, migrations, job queue, and outbox readiness.
- Knowledge-service version, health, index freshness, and retrieval mode.
- Railway control-plane and sandbox deployment status.
- GitHub, Linear, or Jira integration health.
- Active, retained, failed, expired, and blocked sandboxes.
- Running, queued, completed, failed, cancelled, and review-ready runs.
- Actionable warnings with recovery instructions.

### 6.2 Runs

The run list supports status, epic, repository, runner, phase, sandbox, and time filtering.

Each run page shows:

- Epic, tracker issue, tickets, dependencies, and current wave.
- Runner, model, reasoning configuration, and sandbox.
- Current and completed phases.
- Agent messages and structured activity.
- Artifacts and validation evidence.
- Preview, branch, commit, PR, and receipt.
- Failures, blockers, cancellation, and retry information.
- Refinement and review actions allowed by current state and authorization.

### 6.3 Sandboxes

The sandbox view shows:

- Backend and Railway deployment identity.
- Attached run, branch, status, and current agent activity.
- Created, last-accessed, retained-until, and expiry times.
- Preview readiness and URL.
- Destruction or retention controls where authorized.
- Clear recovery information when a preview forward or sandbox is unavailable.

### 6.4 Documentation and onboarding

Version-matched documentation is embedded with the dashboard and covers:

- Installation and first repository setup.
- Codex plugin use.
- Harnesses, epics, tickets, runs, previews, and receipts.
- Linear/Jira workflows.
- Solo knowledge behavior.
- Railway promotion and cloud sandboxes.
- Diagnostics and common recovery procedures.

The dashboard may link to newer online documentation but must retain usable offline documentation matching the installed binary.

### 6.5 Knowledge explorer

The dashboard provides a read-oriented interface to the authoritative knowledge workspace in both solo and hosted modes. It calls the knowledge layer through the control-plane application service; the browser never receives a knowledge-service bearer token or database credential.

The knowledge explorer includes:

- Global search across entities, artifacts, and memories.
- Filters for object type, subtype, scope, lifecycle, confidence source, importance, provenance, entity, and time.
- Entity list and detail views with canonical identity, aliases, external references, metadata, scopes, versions, and related objects.
- Artifact list and detail views with artifact type, lifecycle, active version, immutable version history, content hash, provenance, rendered Markdown, source references, and related memories/entities.
- Memory list and detail views with memory type, subject/predicate/object, content, importance, confidence, confidence source, temporal validity, embedding state, provenance, versions, and related artifacts/entities/episodes.
- Relationship views on each object showing typed incoming and outgoing links.
- Work-history navigation from episode memories to epics, tickets, runs, receipts, commits, PRs, and Linear/Jira issues.
- Retrieval explanation views showing retrieval mode, scope/entity matches, ranking components, semantic or lexical contribution, and source citations.
- Direct links from run, ticket, artifact, receipt, and system pages into the relevant knowledge objects.

V1 knowledge views are read-oriented. Existing explicit CLI and agent workflows remain the primary mutation path. Full browser-based authoring, bulk curation, conflict resolution, ontology management, and graph-canvas visualization remain future work.

Solo mode reports lexical retrieval and `embedding_state: not_configured`. Hosted mode reports hybrid semantic retrieval, embedding model/version, pending or failed embedding state, and index freshness without exposing provider credentials.

### 6.6 Visual design system

The dashboard must feel like a polished product rather than an infrastructure console assembled from default components.

Requirements:

- Use shadcn/ui primitives as accessible building blocks and Tailwind CSS for layout, typography, color, state, and responsive behavior.
- Define a Vessica theme with semantic tokens for surfaces, text, borders, focus, status, code, charts, events, and preview controls.
- Support light, dark, and system theme preferences, persist the user choice, and avoid theme flashes during startup.
- Meet WCAG 2.2 AA contrast and keyboard requirements.
- Provide visible focus, skip navigation, semantic landmarks, accessible dialogs, labelled controls, and screen-reader status announcements.
- Use restrained animation that respects `prefers-reduced-motion` and never obscures operational state.
- Use consistent density and hierarchy for tables, cards, timelines, detail panels, Markdown, code, logs, and event streams.
- Provide deliberate empty, loading, reconnecting, degraded, permission-denied, and failure states.
- Remain usable at narrow mobile, tablet, laptop, desktop, and wide operational-monitoring widths.
- Enforce component, accessibility, responsive, visual-regression, and theme tests in CI.

---

## 7. Live Events

The dashboard uses the append-only event sequence and `vessica.stream/v1` protocol.

The HTTP application exposes an authenticated SSE endpoint:

```text
GET /api/v1/runs/{run_id}/stream
Last-Event-ID: <sequence>
```

Requirements:

- Events remain ordered by run sequence.
- The server accepts `Last-Event-ID` and an explicit `after` cursor.
- The browser persists the greatest acknowledged sequence per run.
- Reconnects do not duplicate rendered activity.
- Terminal run state produces a final result record.
- Compact events are streamed by default; expanded raw detail is fetched separately.
- Prompts, secrets, credentials, and sensitive raw output are redacted before serving.
- Raw logs require an additional permission and are never embedded in list responses.
- Streaming applies backpressure and bounds client buffers.

SSE is the V1 transport because run activity is primarily server-to-browser, supports standard HTTP infrastructure, and reconnects naturally. WebSockets remain available to proxied application previews where required by the previewed application.

---

## 8. Preview, Refinement, and Review

### 8.1 Preview access

Every live preview has authenticated open, embed, and pop-out actions. Preview routes preserve root-relative assets and WebSocket upgrades.

Preview content must be isolated from general dashboard authority. The preferred production design uses a preview-specific origin or an equivalently isolated signed route with restrictive cookies, CSP, iframe sandboxing, and expiry.

### 8.2 Refinement

Authorized users can submit a refinement prompt to a retained, non-active sandbox.

The action:

1. Shows the target run, branch, sandbox, and impact.
2. Requires explicit confirmation.
3. Uses an idempotency key.
4. Records the request as a run event and audit action.
5. Streams resulting agent activity.
6. Reports changed files, commit, push status, checks, and preview refresh.
7. Projects the refinement to Linear/Jira and the knowledge layer through durable outboxes.

### 8.3 Review

The run workspace supports:

- Request changes.
- Approve and merge.
- Roll back and close the PR.
- Extend preview lifetime.

Actions are state-gated, authorized, confirmed, idempotent, and audited. The dashboard reuses the same review application service as CLI and tracker-driven review links.

---

## 9. Local-to-Railway Experience

The local dashboard presents “Move to Railway” only when the workspace is local-authoritative.

The browser calls a loopback API backed by the same `RailwayProvisioningService` used by `ves railway up`. It must not invoke Railway directly and the Go server should not spawn the CLI as a subprocess.

The guided flow:

1. Explains the services, database, credentials, and state that will be created or moved.
2. Runs read-only prerequisite checks.
3. Collects explicit confirmation and required provider choices.
4. Starts Railway authentication when necessary.
5. Provisions the control plane, knowledge service, Postgres, variables, secrets, and endpoints.
6. Deploys pinned release images and waits for terminal deployment success and readiness.
7. Promotes control-plane and knowledge state using verified, resumable migrations.
8. Exercises hosted health, API, event, and representative context checks.
9. Atomically changes workspace authority.
10. Opens the hosted dashboard and retains the local recovery snapshot.

Provisioning progress is represented as a durable operation with its own SSE stream. Browser refresh must reconnect to the existing operation instead of starting another deployment.

If promotion fails, the local workspace remains authoritative and the dashboard provides a resumable retry with exact failure information.

---

## 10. Authentication and Security

### 10.1 Local mode

- Bind to `127.0.0.1` by default.
- Select an available port or honor an explicit port.
- Generate a short-lived launch token.
- Exchange the launch token for an HTTP-only, secure-when-applicable, same-site session cookie.
- Require CSRF protection for mutations.
- Reject non-loopback binding unless explicitly requested with a warning and authentication configuration.
- Never expose provider credentials, database URLs, or stored secrets to the browser.

### 10.2 Hosted mode

- Use user authentication and HTTP-only browser sessions.
- Enforce workspace membership and role-based action permissions.
- Require CSRF protection and secure cookies.
- Record consequential actions in an audit stream.
- Do not use the control-plane service bearer token as a browser credential.
- Signed review capabilities may grant narrow, expiring run actions but never general dashboard access.

### 10.3 Preview security

- Preview authorization is scoped to workspace, run, sandbox, action, and expiry.
- Preview cookies must not grant control-plane API access.
- Embedded previews use restrictive iframe sandbox and content-security policy settings.
- Cross-origin messaging accepts only known origins, source windows, scopes, and message schemas.

---

## 11. API Surface

The browser talks only to the Vessica control-plane HTTP application:

```text
GET  /api/v1/system
GET  /api/v1/integrations

GET  /api/v1/runs
GET  /api/v1/runs/{id}
GET  /api/v1/runs/{id}/stream
GET  /api/v1/runs/{id}/events/{event_id}
POST /api/v1/runs/{id}/refinements
POST /api/v1/runs/{id}/approve
POST /api/v1/runs/{id}/rollback
POST /api/v1/runs/{id}/cancel

GET  /api/v1/sandboxes
GET  /api/v1/sandboxes/{id}
POST /api/v1/sandboxes/{id}/retain
POST /api/v1/sandboxes/{id}/destroy

GET  /api/v1/knowledge/status
GET  /api/v1/knowledge/search
GET  /api/v1/knowledge/entities
GET  /api/v1/knowledge/entities/{id}
GET  /api/v1/knowledge/artifacts
GET  /api/v1/knowledge/artifacts/{id}
GET  /api/v1/knowledge/memories
GET  /api/v1/knowledge/memories/{id}
GET  /api/v1/knowledge/relationships
POST /api/v1/knowledge/context:explain

POST /api/v1/hosting/promotions
GET  /api/v1/hosting/promotions/{id}
GET  /api/v1/hosting/promotions/{id}/stream
```

All responses use versioned machine-readable contracts. List endpoints are paginated. Mutations require idempotency, authorization, confirmation metadata where applicable, and structured audit context.

The control plane aggregates Railway, knowledge, database, and integration status. The browser never calls those providers directly.

---

## 12. Commands and Discovery

```bash
ves dashboard
ves dashboard --open
ves dashboard --port <port>
ves dashboard status --json
```

`ves dashboard --open` starts the local server if necessary and opens an authenticated local URL. Repeated invocations reuse the compatible local dashboard process or safely replace an incompatible one.

After hosted promotion, `ves dashboard --open` opens the hosted dashboard unless the user explicitly requests the local recovery view.

The Codex plugin may open or inspect the dashboard only by invoking `ves` commands.

---

## 13. Observability and Operations

- Health checks distinguish HTTP, database, migrations, worker loop, event stream, preview broker, Railway, integrations, and knowledge service.
- Frontend and backend versions are reported together.
- Server logs include request/operation IDs but redact credentials and sensitive prompts.
- SSE connection count, lag, reconnects, and dropped clients are measurable.
- Provisioning operations and review actions have durable audit records.
- Dashboard asset releases are tied to the containing `ves` version.

---

## 14. Acceptance Scenarios

1. A solo user starts the dashboard and views local readiness, runs, events, previews, and documentation without cloud dependencies.
2. A hosted user on another machine sees the same current run and sandbox state.
3. A disconnected event stream resumes from `Last-Event-ID` without duplicate activity.
4. A completed run opens its live preview and presents refinement, approval, rollback, PR, artifacts, evidence, and receipt.
5. A refinement is confirmed once, creates one durable action, streams activity, refreshes the preview, and appears in tracker and knowledge history.
6. An unauthorized user cannot read workspace state, open previews, or perform actions.
7. Arbitrary preview content cannot access dashboard sessions or control-plane credentials.
8. A local user starts Railway promotion, refreshes the browser, and reconnects to the same operation.
9. Failed promotion leaves local authority unchanged and provides a resumable retry.
10. Successful promotion opens the hosted dashboard with equivalent runs, events, artifacts, memories, and configuration.
11. A user can search for a repository or topic, inspect its entities, open an active artifact and its version history, inspect derived memories, and follow provenance back to work evidence.
12. Solo knowledge views clearly show lexical retrieval without presenting missing embeddings as an error.
13. Light and dark themes pass accessibility and visual-regression checks across operational and knowledge views.

---

## 15. Future Work

- Browser-based knowledge authoring, curation, conflict resolution, ontology management, and graph-canvas visualization.
- Human inbox and approval queues.
- Cost, token, receipt, and Return on Intelligence analytics.
- Organization administration and policy management.
- Mobile-optimized operational views.
- Notifications and subscriptions.
- Custom agent UI packages.
- Separately deployed frontend if release cadence or ownership later requires it.
