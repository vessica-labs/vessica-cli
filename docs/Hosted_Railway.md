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

One Railway Postgres service hosts two deliberately separate logical stores:

- `vessica_control`, owned by `vessica_control_user`, is the workflow and control-plane authority.
- `vessica_knowledge`, owned by `vessica_knowledge_user`, is the knowledge authority and is the only database with pgvector enabled.

Each service receives its own generated credential and private-network connection URL. The stores have separate migration histories and application interfaces; there are no cross-database joins, foreign keys, shared tables, or distributed transactions. Webhook delivery IDs, jobs, and outbound tracker operations remain idempotent and recover stale leases after service restarts.

## Current replica constraint

Run exactly one control-plane replica. The process acquires a database-backed lease at startup and returns an explicit error when another replica from the same Railway deployment is active. A new deployment ID may take over the lease so rolling releases can drain the previous process without deadlocking readiness.

The deployment runs `ves control-plane migrate` as a locked pre-deploy step. API and worker processes only verify the expected schema; they do not race to apply migrations.

Postgres pools are bounded per process. Defaults are 20 open and 5 idle connections, with 30-minute maximum lifetime and 5-minute maximum idle time. Override them with `VES_DB_MAX_OPEN_CONNS`, `VES_DB_MAX_IDLE_CONNS`, `VES_DB_CONN_MAX_LIFETIME_SECONDS`, and `VES_DB_CONN_MAX_IDLE_SECONDS`; budget the total across the control plane and active workers against the database connection limit.

Railway worker sandboxes remain independently scalable. Before increasing the control-plane replica count, move preview forwarding and stream coordination out of process memory, assign distributed ownership to reconciliation and cleanup loops, and load-test claims, projections, outbox delivery, and database pool limits with concurrent Postgres writers.

## Provision

From a repository with a reachable GitHub origin:

    ves up --dry-run --json
    ves up --yes --stream jsonl

`ves up` opens provider authentication only when a valid existing session is unavailable. Linear is not part of quickstart and can be connected later with `ves integration connect linear`. Codex authentication remains owned by Codex and is provisioned to sandboxes without asking for an OpenAI API key.

New installations always create the Railway project as `vessica-control-plane`, independent of the repository being attached. This keeps Vessica infrastructure distinct from any Railway project later used to deploy the target application.

Provisioning creates only one managed Postgres service. It waits for the service variables, connects through the public bootstrap endpoint without logging the URL, creates or reconciles both fixed database roles and databases under an advisory lock, enables `vector` in `vessica_knowledge`, and then configures the application services. Repeating or resuming the operation reuses the same Railway service and logical databases.

The remote repository-orientation step also captures an immutable Railway disk
checkpoint containing `/workspace/repo`, installed dependencies, and warmed
package caches. Its name is fingerprinted by repository, commit, dependency
manifests, and worker toolchain. Runs prefer this checkpoint and fall back to the
generic toolchain checkpoint when repository metadata is absent or incompatible.
Variables, credentials, and private-network mode are always supplied at sandbox
creation and are not part of either checkpoint.

Onboarding records a durable operation journal. Provider-login interruptions,
deploy failures, Sandbox Priority Boarding, and readiness timeouts can be
continued with `ves up resume <operation-id> --yes --stream jsonl`; do not start
a second installation to compensate. `ves up status --json` reports the current
stage and recovery action.

The control plane receives only `VES_CONTROL_DATABASE_URL`. The knowledge service receives only `VES_KNOWLEDGE_DATABASE_URL`. No service receives the other store's URL, and there is no generic database variable that can silently point a process at the wrong store.

## Warm Snapshot Lifecycle

A **toolchain checkpoint** is the fingerprinted worker base: its pinned runtime,
tools, and runtime attestation are reusable across repositories. A
**repository checkpoint** is a derived, immutable snapshot containing a clean
repository checkout, prepared dependencies, and warmed package caches for one
repository, commit, dependency fingerprint, and toolchain fingerprint.

Before a warm fork is used, Vessica verifies its checkpoint attestation; a
mismatch falls back to full verification and, when necessary, the compatible
toolchain checkpoint. A fresh run from a repository checkpoint fetches only the
Git delta and refreshes dependencies only when dependency manifests changed.
After a successful delta-based run, Vessica can asynchronously fork and scrub
the sandbox to capture a newer repository checkpoint. It advances the mapped
generation only after the replacement is verified, retaining the known-good
checkpoint as the fallback until then.

## OAuth Application Setup

The official Vessica Railway and Linear client IDs are compiled into the CLI, so installed releases require no OAuth configuration. Client IDs are public; client secrets are never embedded. Forks and development builds can override the defaults with `VES_RAILWAY_OAUTH_CLIENT_ID` and `VES_LINEAR_OAUTH_CLIENT_ID`.

### Railway

1. Open the Railway workspace **Settings**, choose **Developer**, then choose **New OAuth App**.
2. Name the app `Vessica CLI` and select a **Native** application. Native apps use PKCE and do not use a client secret.
3. Add the exact redirect URI `http://127.0.0.1:8765/oauth/railway/callback`.
4. Save the app and configure its client ID as the Vessica application default. Forks can instead set `VES_RAILWAY_OAUTH_CLIENT_ID`.
5. Vessica requests `openid profile email offline_access workspace:member project:member`; `offline_access` enables refresh-token rotation for the persistent control plane.

The Vessica OAuth grant remains the provisioning credential. Native preview forwarding uses a separate official Railway CLI session because the public Vessica OAuth scopes do not authorize Railway SSH keys. Authorize that session once after onboarding:

    ves railway preview-session authorize

The command starts Railway's browserless device flow inside the deployed control plane and relays its short-lived approval link. After approval, the control plane generates a dedicated Ed25519 key locally and registers only the public key through the authorized Railway CLI session. No private key is sent through a service variable.

### Linear

1. Open Linear **Settings**, choose **API**, then create a new OAuth application named `Vessica`.
2. Add the exact callback URL `http://127.0.0.1:8765/oauth/linear/callback` and enable the authorization-code/PKCE flow.
3. Configure the client ID as the Vessica application default. Forks can instead set `VES_LINEAR_OAUTH_CLIENT_ID`. The native CLI does not require the app secret.
4. Authorize Vessica as a Linear workspace administrator. Vessica requests `read`, `write`, `issues:create`, `comments:create`, and `admin`; the `admin` grant is needed to create the issue webhook after Railway assigns the control-plane URL.
5. Do not hard-code a webhook URL in the app: each installation receives a different Railway domain, and the optional Linear connection creates the webhook after deployment.

Linear authentication intentionally uses the authorizing user as the OAuth actor. Linear app actors cannot request `admin`, while Vessica needs that scope to create a different webhook for each deployed control plane. As a result, Vessica's issue, comment, and status mutations are attributed to the authorizing user in Linear.

### Hosted dashboard GitHub identity

The hosted dashboard uses GitHub's OAuth device flow for browser identity. The
public client ID of the Vessica Labs-owned OAuth app is compiled into official
releases. Forks can override it with `VES_GITHUB_OAUTH_CLIENT_ID`. No client
secret is deployed or retained. The dashboard discards the GitHub access token
after resolving the authenticated identity.

The first authenticated GitHub identity to present the expiring owner-claim URL
created during onboarding becomes the workspace owner. Owners can invite members
by GitHub username. Invitations and owner claims are single-use and expire.

The CLI continues to use the installed `gh` CLI for GitHub repository operations,
and Codex authentication remains owned by the installed `codex` CLI.

## Dashboard and preview origins

Hosted dashboard delivery is part of the control-plane process and is enabled
with `VES_DASHBOARD_ENABLED=1`. `ves up --provider railway` configures the
dashboard origin and, by default, provisions a small `vessica-preview-edge`
service with its own generated HTTPS domain:

    VES_DASHBOARD_ORIGIN=https://dashboard.example.com
    VES_PREVIEW_ORIGIN=https://vessica-preview-edge.example.railway.app

The edge forwards preview traffic to the broker over Railway's private network
using a generated service-to-service secret. Its public origin receives only
expiring, run-scoped preview capabilities; it does not expose dashboard or API
routes. Pass `--preview-origin` only when supplying an equivalent dedicated
custom HTTPS origin. Preview cookies cannot authorize dashboard API requests.
Production onboarding uses released, digest-pinned images. Source deployment is
reserved for contributor workflows and is never a quickstart fallback.

## Operate

    ves railway status
    ves railway logs --lines 200
    ves railway preview-session status
    ves railway preview-session repair-key
    ves railway preview-session smoke
    ves railway approve <run_id>
    ves railway down --yes

If only the local repository attachment is stale, use `ves workspace forget`
and then rerun `ves up`. Forgetting an attachment removes local hosted metadata
and credentials only; it does not delete this Railway project or rewrite the
repository harness and documentation.

If the CLI session is valid but its forwarding key was registered to the wrong
Railway key scope, `preview-session repair-key` rotates only the forwarding key
and registers the new fingerprint to the existing CLI user session. It does not
repeat device authorization.

Connect Linear after the hosted workspace is healthy:

    ves integration connect linear --project "Product launch" --dry-run --json
    ves integration connect linear --project "Product launch" --yes --idempotency-key connect-linear-product-launch --json
    ves integration switch-project linear --project "Next project" --dry-run --json

Project selectors accept a UUID, slug, or name. A connection or project switch
updates Linear variables and redeploys only the control plane; it does not
redeploy the knowledge service.

Approval marks the draft PR ready, squash-merges it with head-SHA protection, moves the Linear epic to Done, and destroys the run sandbox.

Hosted lifecycle commands always use the control-plane API. They never open the repository-local run database:

    ves run cancel <run_id> --yes
    ves run resume <run_id> --from validate --yes
    ves run view <run_id>
    ves run logs <run_id>
    ves epic status <epic_id>
    ves sandbox view <sandbox_id>
    ves sandbox logs <sandbox_id>

Cancel releases active job leases, persists a terminal run, and retains the Railway sandbox for recovery. Resume is idempotent and reuses that sandbox when it is still available.

## Trigger Rules

The control plane imports parent issues that:

- Belong to the configured Linear team.
- Are in the configured Todo state.
- Have the configured trigger label when one is set.

The webhook handler verifies Linear's HMAC signature and timestamp, stores the raw delivery, and responds before doing work. A periodic reconciliation also discovers matching Todo issues if a webhook was missed.

## Preview Lifetime

The run engine starts and owns the repository's harness preview inside the Railway sandbox. It waits for the configured healthcheck before browser validation and keeps the same process alive across validation repairs. After validation, the control plane starts a loopback-only `railway sandbox forward`, then places the capability-protected preview broker in front of that endpoint and healthchecks the public URL before persisting it:

    https://<preview-origin>/previews/<run_id>/?cap=<capability>

The preview edge carries that request over Railway's private network to the
control-plane broker. Readiness checks load the HTML plus same-origin stylesheets
and scripts, so a dashboard fallback or other MIME mismatch cannot be marked
ready. Only the public HTTPS URL is written to the run, receipt, draft PR,
Linear, and dashboard. A failed publication records `public_preview_failed` and
does not report a completed preview. Preview-enabled sandboxes have a 24-hour
Vessica lease. Failed runs default to four hours. Railway CLI handles ordinary
relay reconnects; Vessica restarts the CLI process with bounded backoff after
its reconnect budget is exhausted. The control plane recreates forwards and
rebuilds retained capability URLs against the configured preview origin after a
restart, then destroys expired Railway sandboxes normally.

## Secrets

Local Railway and Linear OAuth credentials are stored in macOS Keychain. On platforms without Keychain, the fallback files use mode 0600 under `~/.vessica/secrets`.

The control plane persists Railway and Linear OAuth credentials as AES-256-GCM ciphertext in Postgres and refreshes them before use. A successful `ves auth login railway|linear` in an attached workspace validates and activates the new credential through the control plane, without deleting rows or redeploying. At startup, an environment bootstrap credential replaces a stored credential only when its `updated_at` is newer; equal or older bootstrap data cannot override a rotated database credential.

The Railway CLI session and its generated forwarding key are also AES-256-GCM encrypted in Postgres. They are materialized only under the control-plane user's private home with configuration and private-key mode `0600`. The CLI rotates its refresh token under its own file lock; Vessica snapshots the updated session after Railway operations. `RAILWAY_TOKEN`, `RAILWAY_API_TOKEN`, and service-level `HOME` values are removed from every session-backed subprocess so they cannot override the device-authorized session.

The worker opens Postgres, installs the opaque Codex login file as `/home/vessica-agent/.codex/auth.json` owned by `vessica-agent` with mode `0600`, and then removes privileged credentials from the coding agent's environment. An explicit `OPENAI_API_KEY` remains supported as a headless fallback.
