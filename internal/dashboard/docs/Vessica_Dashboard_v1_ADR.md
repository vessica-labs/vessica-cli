# Vessica Dashboard v1 ADR — Embedded Dashboard Served by the Control Plane

**Document type:** Architecture Decision Record  
**ADR ID:** ADR-003  
**Product:** Vessica Dashboard  
**Status:** Accepted  
**Date:** 2026-07-11

---

## 1. Decision Summary

Vessica will build the dashboard in the `vessica-cli` repository and ship its compiled frontend assets inside the existing Go binary.

The same HTTP dashboard application runs in two modes:

- `ves dashboard` serves a loopback-only local dashboard backed by SQLite and local sandbox services.
- `ves control-plane serve` serves the hosted dashboard backed by Postgres, Railway sandboxes, integrations, and the hosted knowledge service.

The dashboard is a presentation and transport adapter over shared Go application services. It is not implemented inside Cobra handlers, does not shell out to `ves`, and does not contain control-plane business logic.

The frontend will be a TypeScript React/Vite application using shadcn/ui components and Tailwind CSS, built during release and embedded with `go:embed`. Installed users need only the `ves` binary; Node is not a runtime dependency. Vessica-owned design tokens provide polished light and dark themes rather than shipping unmodified component defaults.

Run and agent activity uses the existing append-only `vessica.stream/v1` event sequence over authenticated Server-Sent Events with cursor-based resume. Preview, refinement, approval, rollback, retention, and cancellation actions call the same application services as the CLI.

The local dashboard exposes a guided “Move to Railway” operation backed by the same reusable Railway provisioning service as `ves railway up`.

The dashboard includes a read-oriented knowledge explorer for entities, artifacts, memories, relationships, provenance, immutable versions, work history, and retrieval explanations. Knowledge access remains mediated by the control-plane application service.

---

## 2. Context

Vessica’s primary interfaces remain coding-tool conversation, the CLI, and Linear/Jira. A web dashboard is still necessary for high-bandwidth operational tasks:

- Understanding concurrent run and sandbox state.
- Following live agent messages and structured events.
- Inspecting artifacts, evidence, receipts, branches, and PRs.
- Opening and refining live previews.
- Approving or rolling back completed work.
- Diagnosing Railway, database, knowledge, and integration health.
- Helping novice users move from local operation to hosted Vessica.

The current control plane already exposes run APIs, persisted events, SSE-style review streaming, preview forwarding, refinement, and review controls. A new separately deployed dashboard would duplicate deployment and authentication concerns while remaining tightly dependent on the control-plane API.

Local and hosted dashboards must behave consistently. This requires shared application services rather than parallel implementations in CLI and HTTP handlers.

---

## 3. Decision Drivers

- One-binary installation and upgrade.
- Identical local and hosted user experience.
- Reuse of current events, previews, refinement, and review capabilities.
- Secure browser access to operational state.
- Resumable live streaming over ordinary HTTP infrastructure.
- No provider credentials in the browser.
- One implementation of business rules across CLI, plugin, tracker, and dashboard.
- Version-matched offline documentation.
- A low-friction path from solo to hosted operation.

---

## 4. Architecture

```text
                      Browser
                         |
                 Dashboard HTTP app
                         |
                Shared application services
                 /       |        \
                /        |         \
         local adapters  domain   hosted adapters
          SQLite/Docker  rules    Postgres/Railway
                                      |
                               Knowledge HTTP API
```

The browser calls only the Vessica HTTP application. Provider and database access remains server-side.

The repository structure is:

```text
internal/app          shared application services
internal/dashboard    assets, sessions, API and SSE handlers
internal/controlplane hosted adapters and process lifecycle
web/dashboard         React, shadcn/ui, Tailwind source and compiled assets
```

---

## 5. Major Decisions

### Decision 1 — Keep the dashboard in `vessica-cli`

The dashboard is part of the control-plane product and release. It is not a separate repository or Railway service in V1.

This keeps API and UI versions aligned, preserves one deployment, and makes the full dashboard available in the local binary.

### Decision 2 — Embed compiled frontend assets

The TypeScript frontend is compiled at release time and embedded into the Go binary. Node and frontend tooling are development dependencies only.

This preserves a single executable while allowing a component-based interface appropriate for concurrent streams, filtering, preview workspaces, and long-running operations.

### Decision 3 — Share application services across transports

Cobra commands, dashboard HTTP handlers, tracker webhooks, and plugin-driven CLI workflows use the same application services for runs, sandboxes, events, previews, review, provisioning, and status.

Transport adapters handle input, output, sessions, and protocol concerns only. They do not implement state transitions or provider orchestration.

### Decision 4 — Use SSE for run and operation streams

Run events and provisioning progress use authenticated SSE with ordered sequence IDs, `Last-Event-ID`, explicit cursors, and a terminal result record.

SSE fits the primarily server-to-browser flow, works through standard HTTP infrastructure, and extends the existing persisted event model. WebSockets remain supported by proxied preview applications but are not the dashboard event transport in V1.

### Decision 5 — Reuse preview and review capabilities behind APIs

The current preview broker, refinement prompt, and approval/rollback behavior become reusable application capabilities. The dashboard composes them into a run workspace.

Preview content receives isolated, expiring authorization and must not share general dashboard authority. A preview-specific origin is preferred in hosted production.

### Decision 6 — Different local and hosted authentication boundaries

Local mode binds to loopback, uses a short-lived launch-token exchange, establishes an HTTP-only session, and enforces CSRF for mutations.

Hosted mode uses authenticated user sessions, workspace membership, roles, secure cookies, CSRF protection, and audit records. The service API bearer token is not a browser credential. Signed review links remain narrow, expiring capabilities rather than dashboard sessions.

### Decision 7 — Share Railway provisioning logic

Railway provisioning is extracted from CLI handlers into a reusable application service. Both `ves railway up` and the local dashboard’s “Move to Railway” operation call it directly.

The dashboard does not spawn the CLI or call Railway from browser JavaScript. Promotion is a durable, idempotent, resumable operation with an SSE progress stream and explicit confirmation.

### Decision 8 — Embed version-matched documentation

The release contains offline documentation matching the binary. The dashboard may link to current online documentation but remains usable without it.

### Decision 9 — Use shadcn/ui, Tailwind, and a Vessica design system

The frontend uses shadcn/ui primitives and Tailwind CSS. shadcn/ui is a source-level component foundation, not the final visual identity. Vessica defines semantic color, typography, spacing, radius, elevation, status, code, timeline, and focus tokens through Tailwind-compatible CSS variables.

Light, dark, and system themes are first-class and must preserve hierarchy, contrast, status meaning, charts, code, logs, previews, and event timelines. Theme selection is persisted and applied before initial render to avoid a flash of the wrong theme.

The design system targets WCAG 2.2 AA, keyboard operation, screen readers, responsive layouts, reduced motion, deliberate empty/loading/error states, and automated accessibility and visual-regression coverage.

### Decision 10 — Include read-oriented knowledge exploration in V1

The dashboard exposes entities, artifacts, memories, relationships, provenance, scopes, confidence, lifecycle, embedding/index state, immutable versions, work-history links, and retrieval explanations.

The browser calls control-plane knowledge application services rather than the knowledge server directly. This preserves workspace authorization, prevents knowledge bearer-token exposure, and gives local embedded and hosted HTTP knowledge clients one dashboard contract.

Full browser authoring, bulk curation, conflict resolution, ontology management, and graph-canvas visualization are deferred. Explicit CLI and agent workflows remain the primary knowledge mutation path in V1.

---

## 6. Rejected Alternatives

### Separate dashboard repository and Railway service

Rejected for V1 because the frontend is tightly coupled to control-plane APIs and must also run locally. A second service adds deployment, authentication, CORS, compatibility, and operational complexity without creating a useful domain boundary.

### Server-render every dashboard screen in Go

Rejected as the primary V1 approach because concurrent event streams, filtering, preview composition, reconnect state, and long-running provisioning benefit from a small client application. Go remains responsible for security and business behavior.

### Electron or another desktop application

Rejected because Codex Desktop and the browser already provide the interaction surfaces. A desktop shell would duplicate installation, update, and security work.

### Dashboard handlers execute `ves` subprocesses

Rejected because subprocess orchestration duplicates parsing and lifecycle concerns, weakens type safety, and makes cancellation, idempotency, and testing less reliable than shared application services.

### Browser calls Railway, Linear, GitHub, or the knowledge service directly

Rejected because it exposes credentials, fragments authorization and auditing, and creates provider-specific frontend behavior.

### WebSockets for all dashboard events

Rejected for V1 because the dashboard stream is primarily one-way and SSE provides simpler reconnect, cursor, proxy, and observability behavior. WebSockets remain necessary for some proxied previews.

### Use unmodified shadcn/ui defaults as the product design

Rejected because a component library does not provide a distinctive, coherent product identity or complete operational-state semantics. Vessica owns the theme tokens, compositions, density, and interaction patterns while retaining accessible shadcn/ui primitives.

### Let the browser call the knowledge server directly

Rejected because it exposes another credential and authorization boundary, complicates local/hosted parity, and bypasses control-plane workspace policy and audit behavior.

### Use the control-plane API token for browser login

Rejected because it grants excessive service-level authority, is difficult to revoke per user, and does not support workspace roles or safe browser sessions.

---

## 7. Consequences

### Positive

- One binary and one hosted service deliver the dashboard.
- Local and hosted modes share the same interface.
- Existing events, preview forwarding, refinement, and review work are reusable.
- CLI and dashboard behavior converge on one application layer.
- Releases keep API and frontend versions aligned.
- New users receive documentation and migration guidance without another install.

### Negative

- The repository gains a TypeScript build toolchain.
- The repository owns a design-token system, shadcn/ui component source, theme behavior, and visual-regression baselines.
- The Go binary grows by the size of compiled assets and documentation.
- Shared application-service extraction is required before feature expansion.
- Browser session and CSRF infrastructure must be added.
- Preview isolation is more complex than ordinary API authorization.
- The control plane serves both API and frontend traffic.

### Mitigations

- Keep frontend dependencies small and enforce asset budgets.
- Treat shadcn/ui components as reviewed source, keep Vessica customizations intentional, and avoid uncontrolled component generation or duplication.
- Build assets reproducibly in CI and test that embedded assets match the release.
- Use strict API contracts and generated or shared TypeScript types.
- Separate dashboard, API, preview, and signed-review authorization scopes.
- Measure SSE connections, lag, reconnects, and dashboard request load.
- Preserve the option to extract the frontend deployment later without changing APIs.

---

## 8. Implementation Constraints

1. The dashboard contains no control-plane business logic.
2. CLI and HTTP mutations call the same application services.
3. The browser never receives provider, database, or embedding credentials.
4. Local servers bind to loopback unless explicitly and safely configured otherwise.
5. Hosted dashboard access requires user identity and workspace authorization.
6. All mutations are state-gated, authorized, idempotent, confirmed where consequential, and audited.
7. Event streams are resumable from persisted sequence IDs.
8. Raw logs require separate authorization and redaction.
9. Preview content cannot inherit dashboard session authority.
10. Railway promotion is durable and resumes rather than duplicates after reconnect.
11. Installed users do not require Node or a separate dashboard executable.
12. The knowledge service remains a separate domain service and does not serve the dashboard.
13. Knowledge explorer APIs are read-oriented in V1 and are mediated by the control plane.
14. All dashboard screens support accessible light and dark themes using shared semantic tokens.

---

## 9. Validation

The decision is validated when:

- `ves dashboard --open` works from a fresh solo install.
- The same frontend operates against local and hosted application adapters.
- CLI and dashboard contract tests produce equivalent mutations and evidence.
- SSE reconnect tests prove no event loss or duplicate rendering.
- Multiple concurrent hosted users can monitor runs without affecting execution.
- Preview security tests prevent access to dashboard sessions and credentials.
- Refinement and review actions produce the same events, evidence, tracker projections, and knowledge episodes as CLI actions.
- Railway promotion survives browser refresh and process restart.
- Successful promotion redirects to a healthy hosted dashboard with equivalent state.
- Frontend asset, API compatibility, accessibility, and responsive-layout checks pass in release CI.
- Entity, artifact, and memory views preserve provenance, versions, relationships, and retrieval explanations in local and hosted modes.
- Light/dark theme and visual-regression tests cover overview, run, sandbox, preview, documentation, and knowledge screens.
