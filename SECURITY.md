# Security model

Vessica is currently open source and single tenant, but the hosted control plane is still a networked privileged service. Single tenant means there is one trust domain; it does not make unauthenticated endpoints, leaked credentials, or arbitrary code execution acceptable.

## Trust boundaries

- Dashboard/API requests are untrusted and must pass authentication, authorization, CSRF/origin checks where applicable, body limits, and structured validation.
- Linear webhooks require signature verification before persistence or job creation.
- Agent-generated code and output are untrusted. Execute them only in a sandbox and redact output before returning it through APIs or logs.
- Postgres, Railway, GitHub, Linear, OpenAI/Codex, and the knowledge service are separate credential boundaries.
- Onboarding journals, repository attachment files, harness artifacts, and receipts contain identifiers and sanitized diagnostics only. Provider tokens remain in the OS credential store or Railway secrets.

## Secret isolation

The control plane may hold infrastructure credentials. A runner subprocess receives only the explicit allowlist implemented in `internal/runner`, plus deliberately supplied runner variables. It must not inherit database, Railway, GitHub, Linear, control-plane, webhook, encryption, preview-edge, or knowledge-service secrets. The official Railway CLI forwarding session and its generated SSH key are encrypted in Postgres and materialized only in the control-plane user's private home with mode `0600`; they never enter a worker sandbox or service variable. The dedicated preview edge authenticates to the broker with a generated secret, overwrites any caller-supplied authentication header, and exposes no dashboard or API route.

Hosted agent commands run as the unprivileged `vessica-agent` user with a dedicated home directory. Codex authentication is written there with mode `0600`. Engine-managed Codex invocations disable configured MCP servers unless explicitly named in `VES_CODEX_MCP_ALLOWLIST`; overrides are invocation-scoped and do not rewrite persistent Codex configuration. The control-plane image itself runs as a non-root user. Never pass secrets in command-line arguments or persist them in run artifacts.

Repository build, validation, and preview commands use the same unprivileged boundary but do not receive the model credential. Working-tree content is writable by the isolated user; `.git` metadata remains privileged and orchestration Git commands disable repository hooks. Git trust is granted only for the exact generated worktree path and verified as the isolated user; `safe.directory=*` is never used. This prevents generated hooks or local Git configuration from executing later with worker authority.

Runtime attestations and cached worker binaries are fingerprinted or SHA-256 verified before execution. A proactive repository checkpoint is captured only from a copy-on-write fork after run directories and agent authentication are removed and the checkpoint root passes a clean Git status check. Provider credentials and Railway variables are never written into the snapshot contract.

## Operational requirements

- Configure a strong `VES_CONTROL_PLANE_API_TOKEN`, webhook secret, credential-encryption key, worker-download token, and OAuth credentials through Railway secrets.
- Keep the generated control-plane and knowledge database roles distinct. Only the control-plane service receives `VES_CONTROL_DATABASE_URL`; only the knowledge service receives `VES_KNOWLEDGE_DATABASE_URL`. The orchestration worker receives the control URL long enough to open durable state and removes it before starting the coding agent. The knowledge URL never enters a sandbox. Neither URL belongs in repository configuration, logs, or receipts.
- Keep the control-plane service private except for intentional public HTTP routes; keep Postgres private.
- Authorize native Railway preview forwarding only through `ves railway preview-session authorize`. Treat that device-authorized CLI session as a privileged workspace credential, and revoke it after suspected exposure.
- Rotate credentials after suspected exposure. Open-source history is permanent, so revoke first and clean history second.
- Embeddings are opt-in. `ves knowledge embeddings enable` accepts only an environment-variable reference, writes the value directly to Railway, and never includes it in command output or repository configuration.
- Keep dependencies and build actions pinned, run vulnerability scanning in CI, and review lockfile changes.
- Do not enable multiple control-plane replicas until the scale-out work listed in `ARCHITECTURE.md` is complete.

Report vulnerabilities privately to the maintainers rather than opening a public issue containing exploit details or credentials.
