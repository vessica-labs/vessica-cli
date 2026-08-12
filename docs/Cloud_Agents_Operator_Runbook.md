# Vessica Cloud Agents Operator Runbook

This runbook covers ChatGPT Work Outlook ingestion, remote MCP, shared web
conversations, COS briefings, the daily newsletter, and durable Vessica agents.
It deliberately excludes Telegram, LinkedIn, and direct Microsoft Graph access.

## Production configuration

Run one control-plane replica and one private agent-runtime service. Configure:

- `VES_MCP_ENABLED=true`
- `VES_MCP_PUBLIC_URL=https://<canonical-control-plane-origin>`
- `VES_MCP_ALLOWED_ORIGINS=<approved HTTPS ChatGPT/Codex origins>`
- `VES_CLOUD_AGENTS_ENABLED=true`
- `VES_COS_AGENT_ID=<active COS agent ID>`
- `VES_NEWSLETTER_AGENT_ID=<active Newsletter agent ID>`
- `VES_CLOUD_AGENT_TIMEZONE=America/Los_Angeles`

The public URL must be one canonical HTTPS origin. Keep OAuth access and refresh
tokens out of variables, plugin assets, logs, and documentation. Newsletter
metadata may name a credential environment variable but never its value.

## Set up the ChatGPT Work project

1. Create a ChatGPT Work project named `Vessica COS` and restrict it to the
   intended workspace members.
2. Connect Outlook Email and Outlook Calendar with the native ChatGPT Work
   connectors. Do not add a Microsoft Graph application or copy mailbox
   credentials into Vessica.
3. Add the packaged `vessica-outlook-ingestion` skill with its account scope,
   v2 contract, and validator. This skill is a separate ChatGPT Work asset; it
   is not supplied by `ves setup codex --plugin`.
4. Enable ChatGPT developer mode if workspace policy permits it. In ChatGPT
   Plugins, create a remote MCP connection for
   `https://<canonical-control-plane-origin>/mcp`, complete interactive OAuth,
   and grant only the scopes required by the task. Vessica advertises its
   dynamic client-registration endpoint and accepts only public PKCE clients
   whose callbacks are on `https://chatgpt.com`; it does not issue or store a
   client secret. See the
   [official connection steps](https://developers.openai.com/plugins/deploy/connect-chatgpt).
5. Copy the registered `plugin_asdk_app_...` technical ID. Use
   `render-chatgpt-plugin.sh` to create the separate app-backed plugin candidate,
   then have an administrator test and publish that candidate to the workspace.
   Never invent an ID or claim that the local Codex marketplace install also
   installs the plugin in ChatGPT web.
6. Run `scheduled_write_probe` once with a stable idempotency key. Confirm one
   allowed action in Dashboard → Operations before enabling Outlook writes.
7. Submit one minimized, narrow-window test batch. Confirm its batch ID,
   receipt partition, independent committed email/calendar watermarks, action
   ledger, and resulting briefing.

Never submit raw bodies, HTML, MIME, attachments, cookies, credentials, or
source instructions. Correct rejected input in Work; never bypass validation or
edit database records.

## Weekday schedules

Create exactly two ChatGPT Work Scheduled Tasks in `America/Los_Angeles`:

- `Vessica morning Outlook ingestion` — weekdays at **6:30 AM**
  (`30 6 * * 1-5`). Scan from the last committed email/calendar watermarks and
  request the morning COS briefing.
- `Vessica afternoon Outlook ingestion` — weekdays at **4:30 PM**
  (`30 16 * * 1-5`). Scan from the last committed email/calendar watermarks and
  request the afternoon COS briefing.

Each input carries the required scheduled task/run provenance and a stable
batch identity for that execution. Only the Vessica receipt may advance each
watermark. Newsletter synthesis is separate; do not duplicate it here.

## Routine checks

Dashboard → Operations is owner-only. Review OAuth failures, MCP errors and
latency, source checkpoints older than 24 hours, rejected Outlook records,
missing morning/afternoon/newsletter artifacts, failed agent runs, budgets, and
denied actions. Follow conversation action-ledger links to the exact decision.

Prometheus values are at `/internal/dashboard/metrics`. Alert on nonzero OAuth
failures, rejected records, failed agents, missing briefings, or denied actions;
alert when MCP error rate or latency breaches the team SLO. Investigate stale
checkpoints by source instead of advancing them manually.

## Key rotation and revocation

For compromise or scheduled rotation, revoke the OAuth client or token family
first, then issue a new grant through interactive OAuth. Access and refresh
material stays hash-only at rest. Revoke provider credentials at their owner,
rotate the Railway secret reference, redeploy only dependents, and verify
readiness plus one bounded probe. Never paste a token into SQL, logs, plugin
JSON, documentation, or a conversation.

Rotate control-plane service credentials through the normal credential path,
restart dependents with the new value, then prove the old credential is denied.

## Schedule recovery

1. Preserve the last committed email and calendar watermarks.
2. Repair connector/OAuth health and confirm MCP discovery.
3. Run one manual catch-up with a bounded scan window and new stable key.
4. Accept only the committed receipt watermarks.
5. Confirm the expected canonical briefing and ledger entry.
6. Re-enable the normal schedule without replaying overlapping windows.

If a run failed after durable trigger creation, recover or retry its fenced
task. Do not create an untracked replacement.

## Source suspension and recovery

Suspend an unhealthy newsletter source with the MCP subscription-disable tool
or approved dashboard/API path. Its independent checkpoint remains while other
sources continue. Record the cause. Re-enable only after a bounded collection
proves deduplication and monotonic checkpointing.

For Outlook, pause the corresponding Work schedule instead of deleting its
checkpoint. Resume only after a successful OAuth/MCP probe and validated test
batch. Never edit checkpoint rows.

## Incident triage

1. Confirm canonical HTTPS origin and `/readyz`.
2. Check Operations and the action ledger.
3. Check OAuth discovery, consent, and revocation state.
4. Check MCP errors, latency, and denial reason.
5. Check source checkpoint age and rejection details.
6. Check orchestration task, agent run, and budget state.
7. Check the canonical knowledge artifact and citations.

Restore the narrow failed boundary. Do not add Telegram, LinkedIn, Graph, local
writable fallback state, or database repair scripts as an incident workaround.
