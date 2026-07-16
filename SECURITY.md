# Security model

Vessica is currently open source and single tenant, but the hosted control plane is still a networked privileged service. Single tenant means there is one trust domain; it does not make unauthenticated endpoints, leaked credentials, or arbitrary code execution acceptable.

## Trust boundaries

- Dashboard/API requests are untrusted and must pass authentication, authorization, CSRF/origin checks where applicable, body limits, and structured validation.
- Linear webhooks require signature verification before persistence or job creation.
- Agent-generated code and output are untrusted. Execute them only in a sandbox and redact output before returning it through APIs or logs.
- Postgres, Railway, GitHub, Linear, OpenAI/Codex, and the knowledge service are separate credential boundaries.

## Secret isolation

The control plane may hold infrastructure credentials. A runner subprocess receives only the explicit allowlist implemented in `internal/runner`, plus deliberately supplied runner variables. It must not inherit database, Railway, GitHub, Linear, control-plane, webhook, encryption, or knowledge-service secrets.

Hosted agent commands run as the unprivileged `vessica-agent` user with a dedicated home directory. Codex authentication is written there with mode `0600`. The control-plane image itself runs as a non-root user. Never pass secrets in command-line arguments or persist them in run artifacts.

Repository build, validation, and preview commands use the same unprivileged boundary but do not receive the model credential. Working-tree content is writable by the isolated user; `.git` metadata remains privileged and orchestration Git commands disable repository hooks. This prevents generated hooks or local Git configuration from executing later with worker authority.

## Operational requirements

- Configure a strong `VES_CONTROL_PLANE_API_TOKEN`, webhook secret, credential-encryption key, worker-download token, and OAuth credentials through Railway secrets.
- Keep the control-plane service private except for intentional public HTTP routes; keep Postgres private.
- Rotate credentials after suspected exposure. Open-source history is permanent, so revoke first and clean history second.
- Keep dependencies and build actions pinned, run vulnerability scanning in CI, and review lockfile changes.
- Do not enable multiple control-plane replicas until the scale-out work listed in `ARCHITECTURE.md` is complete.

Report vulnerabilities privately to the maintainers rather than opening a public issue containing exploit details or credentials.
