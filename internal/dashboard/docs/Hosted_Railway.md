# Hosted Railway Control Plane

## Architecture

The local CLI, persistent control plane, and sandbox worker are roles of the same ves binary. This preserves one workflow implementation:

    Linear webhook
      -> durable inbox
      -> leased job
      -> Railway agent sandbox
      -> ves control-plane worker
      -> plan / code / build / validate / preview / draft PR
      -> transactional outbox
      -> Linear comments, sub-issues, and status updates

Postgres is the shared source of truth. Webhook delivery IDs, jobs, and outbound tracker operations are idempotent and recover stale leases after service restarts.

## Current replica constraint

Run exactly one control-plane replica. The process acquires a database-backed lease at startup and returns an explicit error when another replica from the same Railway deployment is active. A new deployment ID may take over the lease so rolling releases can drain the previous process without deadlocking readiness.

The deployment runs `ves control-plane migrate` as a locked pre-deploy step. API and worker processes only verify the expected schema; they do not race to apply migrations.

Postgres pools are bounded per process. Defaults are 20 open and 5 idle connections, with 30-minute maximum lifetime and 5-minute maximum idle time. Override them with `VES_DB_MAX_OPEN_CONNS`, `VES_DB_MAX_IDLE_CONNS`, `VES_DB_CONN_MAX_LIFETIME_SECONDS`, and `VES_DB_CONN_MAX_IDLE_SECONDS`; budget the total across the control plane and active workers against the database connection limit.

Railway worker sandboxes remain independently scalable. Before increasing the control-plane replica count, move preview forwarding and stream coordination out of process memory, assign distributed ownership to reconciliation and cleanup loops, and load-test claims, projections, outbox delivery, and database pool limits with concurrent Postgres writers.

## Provision

From an initialized repository:

    ves auth login github
    ves auth login railway
    ves auth login linear
    ves auth login codex
    ves railway up

Running `ves auth login` with no provider performs those four logins in sequence. Railway and Linear open a PKCE browser flow and return to a loopback callback on `127.0.0.1:8765`. GitHub uses `gh auth login --web`; Codex uses the official `codex login` browser flow.

## OAuth Application Setup

The official Vessica Railway and Linear client IDs are compiled into the CLI, so installed releases require no OAuth configuration. Client IDs are public; client secrets are never embedded. Forks and development builds can override the defaults with `VES_RAILWAY_OAUTH_CLIENT_ID` and `VES_LINEAR_OAUTH_CLIENT_ID`.

### Railway

1. Open the Railway workspace **Settings**, choose **Developer**, then choose **New OAuth App**.
2. Name the app `Vessica CLI` and select a **Native** application. Native apps use PKCE and do not use a client secret.
3. Add the exact redirect URI `http://127.0.0.1:8765/oauth/railway/callback`.
4. Save the app and configure its client ID as the Vessica application default. Forks can instead set `VES_RAILWAY_OAUTH_CLIENT_ID`.
5. Vessica requests `openid profile email offline_access workspace:member project:member`; `offline_access` enables refresh-token rotation for the persistent control plane.

Railway's SSH-key endpoint does not currently accept OAuth access tokens. Vessica uses the Railway CLI's local browser-login session only to register the dedicated preview-forwarding key, while OAuth remains the credential for provisioning and the hosted control plane. If no Railway CLI session exists, run `railway login` once and retry `ves railway up`.

### Linear

1. Open Linear **Settings**, choose **API**, then create a new OAuth application named `Vessica`.
2. Add the exact callback URL `http://127.0.0.1:8765/oauth/linear/callback` and enable the authorization-code/PKCE flow.
3. Configure the client ID as the Vessica application default. Forks can instead set `VES_LINEAR_OAUTH_CLIENT_ID`. The native CLI does not require the app secret.
4. Authorize Vessica as a Linear workspace administrator. Vessica requests `read`, `write`, `issues:create`, `comments:create`, and `admin`; the `admin` grant is needed to create the issue webhook after Railway assigns the control-plane URL.
5. Do not hard-code a webhook URL in the app: each user receives a different Railway domain, and `ves railway up` creates the webhook after deployment.

Linear authentication intentionally uses the authorizing user as the OAuth actor. Linear app actors cannot request `admin`, while Vessica needs that scope to create a different webhook for each deployed control plane. As a result, Vessica's issue, comment, and status mutations are attributed to the authorizing user in Linear.

### Hosted dashboard GitHub identity

The hosted dashboard uses GitHub's OAuth device flow for browser identity. The
public client ID of the Vessica Labs-owned OAuth app is compiled into official
releases. Forks can override it with `VES_GITHUB_OAUTH_CLIENT_ID`. No client
secret is deployed or retained. The dashboard discards the GitHub access token
after resolving the authenticated identity.

The first authenticated GitHub identity to present the expiring owner-claim URL
created during promotion becomes the workspace owner. Owners can invite members
by GitHub username. Invitations and owner claims are single-use and expire.

The CLI continues to use the installed `gh` CLI for GitHub repository operations,
and Codex authentication remains owned by the installed `codex` CLI.

## Dashboard and preview origins

Hosted dashboard delivery is part of the control-plane process and is enabled
with `VES_DASHBOARD_ENABLED=1`. Configure two HTTPS origins:

    VES_DASHBOARD_ORIGIN=https://dashboard.example.com
    VES_PREVIEW_ORIGIN=https://preview.example.com

The preview origin routes to the same service but receives only expiring,
run-scoped preview capabilities. Preview cookies cannot authorize dashboard API
requests. The guided local workflow provisions and verifies these values:

    ves railway up --preview-origin https://preview.example.com

The local dashboard can run the same durable promotion workflow from its Hosting
screen. It snapshots local state first, streams resumable operation progress,
keeps local authority on failure, and returns an expiring owner-claim URL only
after the hosted state and knowledge service verify successfully.

Useful selectors:

    ves railway up \
      --workspace <railway-workspace-id> \
      --linear-team <team-name-or-id> \
      --todo-state Todo \
      --wip-state "In Progress" \
      --done-state Done \
      --trigger-label Vessica

For source development, add --source /path/to/vessica-cli. Released builds should set --image to the published control-plane image.

## Operate

    ves railway status
    ves railway logs --lines 200
    ves railway approve <run_id>
    ves railway down --yes

Approval marks the draft PR ready, squash-merges it with head-SHA protection, moves the Linear epic to Done, and destroys the run sandbox.

## Trigger Rules

The control plane imports parent issues that:

- Belong to the configured Linear team.
- Are in the configured Todo state.
- Have the configured trigger label when one is set.

The webhook handler verifies Linear's HMAC signature and timestamp, stores the raw delivery, and responds before doing work. A periodic reconciliation also discovers matching Todo issues if a webhook was missed.

## Preview Lifetime

The worker starts the repository's harness preview inside the Railway sandbox. The control plane forwards the sandbox port through a dedicated Railway SSH identity and proxies it at:

    https://<control-plane>/previews/<run_id>/

Preview-enabled sandboxes have a 24-hour Vessica lease. Failed runs default to four hours. The control plane restores forwards after a service restart and destroys expired Railway sandboxes even while a forwarding heartbeat is active.

## Secrets

Local Railway and Linear OAuth credentials are stored in macOS Keychain. On platforms without Keychain, the fallback files use mode 0600 under `~/.vessica/secrets`. The dedicated Railway forwarding key is generated under `~/.ssh` and its public key is registered with the selected Railway workspace.

The control plane persists Railway and Linear OAuth credentials as AES-256-GCM ciphertext in Postgres and refreshes them before use. Railway service variables bootstrap the encrypted store; sandbox commands receive only a current access token. The control plane passes service-variable references when creating a sandbox. The worker opens Postgres, installs the opaque Codex login file, stores GitHub credentials, and then removes database, GitHub, Linear, Railway, control-plane, SSH, and Codex-bootstrap secrets from the coding agent's environment. An explicit `OPENAI_API_KEY` remains supported as a headless fallback.
