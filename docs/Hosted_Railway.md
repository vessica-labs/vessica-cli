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

GitHub and OpenAI do not require Vessica OAuth applications. GitHub authentication is owned by the installed `gh` CLI, and Codex authentication is owned by the installed `codex` CLI.

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
