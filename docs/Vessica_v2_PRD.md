# Vessica v2 PRD — Hosted Coordination and General Agent Runtime

> **Future-scope planning record.** Some hosted software-engineering capabilities
> described here now ship, while the general-agent runtime and several team and
> marketplace concepts do not. This document is not a current feature list.

**Product:** Vessica  
**Document type:** Product Requirements Document  
**Version:** v2 planning draft  
**Primary audience:** Product and engineering team after v1 local harness launch  
**Status:** Future-scope draft  
**Last updated:** 2026-07-08

---

## 1. Executive Summary

Vessica v2 extends the v1 local-first harness engineering CLI into a hosted coordination platform and general agent runtime.

v1 proves the software-development wedge:

- Harness engineering.
- Epics, artifacts, tickets, waves.
- Coding agents in local Docker.
- Event streaming.
- Preview.
- Draft PR.
- Receipts.

v2 adds the infrastructure required for teams and arbitrary durable agents:

1. Managed hosted state.
2. Remote sandboxes.
3. Hosted run coordinator.
4. Hosted run UI with live streaming.
5. General agent definitions.
6. Structured agent data collections.
7. Custom hosted UIs for agents.
8. Scheduled agents.
9. Agent-to-agent tasking.
10. Human inbox.
11. Team permissions.
12. Richer integrations.
13. Model/runner marketplace.
14. Receipt and Return on Intelligence dashboard.

The v2 strategic shift is from **local harness engineering CLI** to **hosted control plane for durable agent work**.

---

## 2. Product Thesis

v1 makes agent-driven software development repeatable. v2 makes durable agent work collaborative, scheduled, observable, and usable beyond software.

The key insight is that both software work and knowledge-worker agents share the same operating primitives:

```text
agent definition
task
run
sandbox
event stream
artifact
structured data
memory
human input
receipt
```

Vessica v2 generalizes the v1 software workflow into a platform for arbitrary agents while preserving the CLI as a power-user and agent interface.

---

## 3. Goals

### 3.1 Product Goals

Vessica v2 must allow users to:

1. Sign up for a hosted Vessica workspace.
2. Receive managed Postgres-backed state automatically.
3. Run workflows in remote sandboxes.
4. View live runs in a hosted UI.
5. Define and run arbitrary agents from JSON/YAML definitions.
6. Attach structured data schemas to agents.
7. Render custom UIs for agents in hosted Vessica.
8. Schedule agents with cron-like triggers.
9. Let agents create tasks for other agents.
10. Manage a human inbox for approvals, clarifications, and exceptions.
11. Track receipts, cost, quality, and Return on Intelligence.
12. Coordinate teams with permissions, roles, and audit logs.
13. Continue using the `ves` CLI locally against hosted state.

### 3.2 Strategic Goals

- Expand Vessica from software harnessing to durable agent work.
- Establish hosted Vessica as the default coordination layer for agent teams.
- Make receipts and ROI a differentiating trust layer.
- Enable a future marketplace of agent packs, skills, UI packages, and evals.
- Support both closed and open model runners.

---

## 4. Personas

### 4.1 Software Team Lead

Uses Vessica v2 to coordinate multiple developers and agents across epics.

Needs:

- Hosted state.
- Remote execution.
- Shared previews.
- PR dashboard.
- Team memory.
- Linear/Jira/GitHub sync.
- Receipts.

### 4.2 Agent Builder

Creates custom agents for knowledge work.

Needs:

- Agent definition schema.
- Skills and workflows.
- Structured data.
- Eval framework.
- Custom UI package.
- Scheduling.
- Inbox.
- Receipts.

### 4.3 Business User / Operator

Interacts with an agent through a hosted page rather than a CLI.

Needs:

- Custom UI.
- Data views.
- Approval flows.
- Inbox.
- Run history.
- Receipts.

### 4.4 Coding Agent / Non-Coding Agent

Uses Vessica APIs or CLI to:

- Pull context.
- Read/write data.
- Create tasks.
- Ask user for input.
- Emit events.
- Complete work with receipts.

### 4.5 Platform Admin

Manages:

- Org users.
- Permissions.
- Integrations.
- Secrets.
- Billing.
- Models.
- Usage limits.
- Audit logs.

---

## 5. Scope Summary

### 5.1 In Scope for v2

Vessica v2 includes:

1. Hosted Vessica service.
2. Managed state backend.
3. Team workspaces and orgs.
4. Remote sandbox backend abstraction and at least one remote backend.
5. Hosted run coordinator.
6. Hosted live run UI.
7. Preview broker for remote sandboxes.
8. General agent definition model.
9. Agent pull/create/update/list/run.
10. Structured agent data schemas and collections.
11. Custom hosted UI package metadata and rendering.
12. Scheduled/cron agents.
13. Task queue for human-created and agent-created tasks.
14. Agent-to-agent tasking.
15. Human inbox.
16. Rich receipts and ROI dashboard.
17. Model/runner abstraction expansion.
18. Better Linear/Jira/GitHub/GitLab integrations.
19. MCP adapter for selected use cases.
20. Team permissions and audit log.
21. Credential broker for hosted and remote runs.
22. Event retention and search.

### 5.2 Out of Scope for v2

Vessica v2 should not attempt:

1. A public marketplace unless core hosted agent runtime is stable.
2. Full no-code UI builder.
3. Full enterprise procurement feature set if it delays core team usage.
4. Production deployment hosting for arbitrary apps.
5. Regulated-industry compliance certifications unless required by early design partners.
6. Fine-tuned proprietary models.
7. Fully autonomous external side effects without approval policies.
8. Replacing GitHub/GitLab/Linear/Jira.
9. Supporting every sandbox/model/provider equally.
10. Complex cross-org federation.

---

## 6. v2 Product Pillars

## Pillar 1 — Hosted Coordination

### 6.1 Hosted Workspace

Users can create a hosted Vessica account and workspace.

Capabilities:

- Managed Postgres state.
- Workspace/org IDs.
- User management.
- Hosted config.
- CLI connection:

```bash
ves auth login vessica
ves workspace connect <workspace_id>
ves status
```

### 6.2 Managed State

Hosted Vessica provides state backend for:

- Epics
- Artifacts
- Tickets
- Runs
- Events
- Receipts
- Memory
- Agent definitions
- Tasks
- Schedules
- Inbox
- Structured data collections

### 6.3 Local CLI Against Hosted State

The CLI remains central.

Example:

```bash
ves init --profile hosted
ves run epic epic_abc123 --sandbox remote --stream
```

Requirements:

- Hosted state works from local CLI.
- Remote sandboxes can access hosted state.
- Local and hosted run views show the same events.

---

## Pillar 2 — Remote Sandboxes

### 6.4 Remote Sandbox Backend

v2 adds at least one remote sandbox backend.

Potential backends:

```text
runloop
railway
vessica-hosted
custom
```

Commands:

```bash
ves sandbox create --backend runloop
ves run epic epic_abc123 --sandbox runloop
ves sandbox tunnel sbx_abc123 --browser
```

### 6.5 Remote Sandbox Requirements

Remote sandboxes must:

- Pull repo from remote.
- Receive scoped credentials.
- Reach hosted state.
- Stream events.
- Expose preview.
- Support teardown.
- Support logs.
- Support configurable images.
- Support runner installation.

### 6.6 Preview Broker

Hosted Vessica must provide stable preview URLs.

Requirements:

- Authenticated preview URLs.
- Expiration policy.
- Run-level preview links.
- PR receipt links.
- Optional public sharing if explicitly enabled.

---

## Pillar 3 — Hosted Run UI

### 6.7 Live Run Page

Hosted run page shows:

- Current phase.
- Agents/workers.
- Ticket status.
- Event stream.
- Logs.
- Artifacts.
- Preview.
- PR link.
- Cost/token updates.
- Validation results.
- Receipt.

### 6.8 Event Streaming

Use the v1 event model.

Transport options:

- WebSocket.
- Server-Sent Events.
- Polling fallback.

### 6.9 Run Controls

Hosted UI supports:

- Cancel run.
- Resume run.
- Retry phase.
- Open preview.
- Open PR.
- Approve artifact set.
- Respond to inbox messages.
- Download receipt.

---

## Pillar 4 — General Agent Runtime

### 6.10 Agent Definition

Commands:

```bash
ves agent create -f agent.json
ves agent create < agent.json
ves agent pull <git-url>
ves agent list
ves agent view <agent_id>
ves agent update <agent_id> -f agent.json
ves agent delete <agent_id>
ves agent run <agent_id> --prompt "..."
ves agent run <agent_id> --input-file input.json --stream
```

Agent definition includes:

- Name
- Description
- System prompt
- Model
- Skills
- Workflow DAG
- Token/cost/time budget
- Permissions
- Eval framework
- Receipt schema
- Structured data schema
- UI package
- Tasking policy
- Inbox policy

### 6.11 Agent Definition Schema

Example:

```json
{
  "kind": "vessica.agent.v1",
  "name": "weekly-market-map",
  "description": "Creates a weekly market map and asks for input when confidence is low.",
  "system": "You are a strategy research agent...",
  "model": {
    "provider": "openai",
    "name": "gpt-5.5",
    "reasoning_effort": "high"
  },
  "budget": {
    "max_tokens": 200000,
    "max_usd": 25,
    "max_wall_time_minutes": 60
  },
  "permissions": {
    "tools": ["web.search", "memory.search", "artifact.write", "inbox.ask"],
    "can_task_agents": ["agent_researcher", "agent_editor"],
    "requires_approval_for": ["external_send", "repo_write", "payment"]
  },
  "skills": [
    {
      "ref": "git+https://github.com/vessica-ai/skills.git//market-research",
      "version": "v0.2.1"
    }
  ],
  "workflow": {
    "type": "dag",
    "ref": "./workflows/main.yaml"
  },
  "data": {
    "schema": {
      "type": "json-schema",
      "ref": "./schema/data.schema.json"
    },
    "collections": []
  },
  "eval": {
    "ref": "./evals/eval.yaml"
  },
  "receipt": {
    "include": ["tokens", "cost", "elapsed_time", "artifacts", "data_changes", "evals", "human_inputs"]
  },
  "ui": {
    "mode": "none|cards|custom",
    "cards": [],
    "package": {
      "type": "local|git|artifact|pnpm",
      "ref": "./ui"
    },
    "data_scopes": []
  }
}
```

### 6.12 Agent Runs

Agent runs use the same run/event/receipt substrate as software runs.

Commands:

```bash
ves agent run agent_abc123 --prompt "Research competitors" --stream
ves run view run_abc123
ves receipt view rcpt_abc123
```

---

## Pillar 5 — Structured Agent Data

### 6.13 Data Model

Agents may define structured collections.

Examples:

- Leads
- Accounts
- Recommendations
- Research items
- Candidate profiles
- Risks
- Experiments
- Decisions
- Approval requests
- Portfolio holdings

Commands:

```bash
ves data schema apply --agent <agent_id> -f schema.json
ves data schema view --agent <agent_id>
ves data list --agent <agent_id> --collection recommendations
ves data get <record_id>
ves data query --agent <agent_id> --collection recommendations --where status=open
ves data put --agent <agent_id> --collection recommendations -f record.json
ves data delete <record_id>
```

### 6.14 Requirements

Structured data must:

- Be schema-validated.
- Be permissioned.
- Be queryable by agents and UIs.
- Emit events on change.
- Be included in receipts where relevant.
- Support versioning/audit.

### 6.15 Relationship to Memory

Structured data is not memory.

```text
memory      = semantic recall and natural-language knowledge
artifact    = authored documents
data        = structured records
```

---

## Pillar 6 — Custom Hosted UIs

### 6.16 UI Model

Agents may declare custom UI packages.

The UI definition may reference:

- Local path in agent pack.
- Git repo.
- Published package.
- Hosted artifact.

Example:

```json
"ui": {
  "mode": "custom",
  "package": {
    "type": "git",
    "url": "https://github.com/example/portfolio-agent-ui.git",
    "ref": "v0.2.0",
    "path": "."
  },
  "runtime": "react",
  "build": {
    "command": "pnpm install --frozen-lockfile && pnpm run build",
    "output_dir": "dist"
  },
  "entry": "dist/index.html",
  "routes": [
    {
      "path": "/",
      "title": "Portfolio Review"
    }
  ],
  "data_scopes": [
    "agent.collections.holdings:read",
    "agent.collections.recommendations:read",
    "agent.collections.recommendations:write",
    "agent.runs:read",
    "agent.receipts:read"
  ]
}
```

### 6.17 UI Runtime Requirements

Custom UIs:

- Render inside hosted Vessica.
- Use sandboxed execution, likely iframe or equivalent.
- Receive scoped capability token.
- Read/write through Vessica APIs.
- Never receive direct database credentials.
- Can access only declared data scopes.
- Can show run history, receipts, structured records, and inbox actions if permitted.

### 6.18 Declarative Cards

For simpler agents, v2 may support `ui.mode = cards`.

Cards include:

- Summary
- Table
- Chart
- Decision
- Approval
- Timeline
- Receipt
- Inbox

Cards are not a substitute for custom UI packages.

---

## Pillar 7 — Tasks, Schedules, and Agent-to-Agent Work

### 6.19 Task Queue

Commands:

```bash
ves task add --agent <agent_id> --prompt "..."
ves task list
ves task view <task_id>
ves task cancel <task_id>
```

Tasks include:

- Target agent
- Prompt/input
- Parent run
- Created by actor
- Budget
- Deadline
- Status
- Required output schema
- Receipt requirement

### 6.20 Agent-to-Agent Tasking

Agents may create tasks for other agents only if permitted.

Requirements:

- Parent run ID is required.
- Budget is required.
- Tasking policy is enforced.
- Delegated task emits its own receipt.
- Parent receipt links to delegated receipts.

### 6.21 Schedules

Commands:

```bash
ves schedule create --agent <agent_id> --cron "0 9 * * MON" --prompt "..."
ves schedule list
ves schedule view <schedule_id>
ves schedule pause <schedule_id>
ves schedule resume <schedule_id>
ves schedule delete <schedule_id>
```

Requirements:

- Hosted scheduler creates tasks/runs.
- Local daemon may support dev-only schedules.
- Schedules have budgets and permissions.
- Missed schedules have explicit policy.
- Schedule runs produce receipts.

---

## Pillar 8 — Human Inbox

### 6.22 Inbox Commands

```bash
ves inbox list
ves inbox view <message_id>
ves inbox respond <message_id>
ves inbox dismiss <message_id>
```

### 6.23 Inbox Message Types

```text
clarification_needed
approval_required
exception
validation_failed
budget_exceeded
handoff
review_requested
external_action_approval
```

### 6.24 Hosted Inbox

Hosted UI must show:

- Messages by user.
- Source agent/run/task.
- Required response type.
- Due date.
- Impact of response.
- Suggested options.
- Audit trail.

### 6.25 Agent Ask Pattern

Agents ask through Vessica, not ad hoc messages.

Example:

```bash
ves inbox ask \
  --user user_abc123 \
  --from-run run_abc123 \
  --question "Should I optimize for speed or cost?" \
  --options "speed,cost,balance"
```

---

## Pillar 9 — Permissions, Governance, and Audit

### 6.26 Org and Workspace Model

v2 adds:

- Organizations.
- Workspaces.
- Users.
- Teams.
- Roles.
- Agent owners.
- Data scopes.
- Tool permissions.
- Budget permissions.

### 6.27 Roles

Initial roles:

```text
owner
admin
developer
agent_builder
operator
viewer
```

### 6.28 Approval Policies

Agents require approval for configured actions:

- External send.
- Repository write.
- PR merge.
- Payment.
- Production deployment.
- High-cost run.
- Sensitive data access.
- Public sharing.

### 6.29 Audit

Audit events include:

- User login.
- Agent create/update/delete.
- Permission change.
- Data schema change.
- Secret change.
- Run start/cancel/resume.
- Inbox response.
- External action.
- PR creation.

---

## Pillar 10 — Receipts and Return on Intelligence

### 6.30 Hosted Receipt Dashboard

Dashboard shows:

- Runs.
- Costs.
- Token usage.
- Runtime usage.
- Tickets completed.
- Artifacts produced.
- Validation pass rate.
- Human interventions.
- Estimated human time avoided.
- Return on Intelligence.

### 6.31 Receipt Requirements

Receipts must support:

- Software runs.
- Arbitrary agent runs.
- Delegated tasks.
- Scheduled runs.
- Human approvals.
- Data changes.
- UI interactions where relevant.

### 6.32 ROI Calculation

Initial formula:

```text
Return on Intelligence = Estimated Value / (Human Labor Cost + Token Cost + Runtime Cost)
```

v2 must allow users to configure:

- Human hourly rate.
- Estimated time avoided.
- Business value.
- Cost attribution by workspace/project/agent.

---

## Pillar 11 — Integrations

### 6.33 Repo Integrations

Expand:

- GitHub
- GitLab

Capabilities:

- PR creation.
- PR updates.
- Branch status.
- Checks.
- Comments.
- Artifact links.
- Receipt links.

### 6.34 Tracker Integrations

Expand:

- Linear
- Jira

Capabilities:

- Push Vessica tickets.
- Pull external status where configured.
- Map fields.
- Show sync conflicts.
- Comment with receipts.
- Preserve Vessica as source of truth.

### 6.35 Model and Runner Integrations

Support:

- Codex
- Claude
- Cursor
- Pi
- Open-source model runners
- Custom runners

Capabilities:

- Provider config.
- Budgeting.
- Token/cost capture where available.
- Runner health checks.
- Capability metadata.

### 6.36 MCP

v2 may include:

```bash
ves mcp serve
ves mcp install claude
ves mcp tools
```

Use MCP for:

- MCP-only hosts.
- Tool discovery.
- External applications.
- Selected Vessica actions.

Do not replace CLI/core contract.

---

## 7. Hosted Architecture Requirements

### 7.1 Services

v2 hosted architecture includes:

```text
API service
Auth service
State database
Event ingestion service
Run coordinator
Scheduler
Credential broker
Sandbox manager
Preview broker
Artifact store
Receipt service
Web UI
Worker queue
```

### 7.2 Event Streaming

The hosted UI consumes the same event log as CLI.

Requirements:

- Low-latency streaming.
- Durable persistence.
- Replay.
- Filtering by run, phase, agent, ticket, event type.
- Redaction before display.

### 7.3 Credential Broker

Credential broker issues scoped credentials for:

- Repo access.
- Tracker access.
- Model providers.
- Sandboxes.
- Agent tools.

Requirements:

- Short-lived tokens.
- Auditable issuance.
- Least privilege.
- Revocation.
- No raw secrets in logs.

### 7.4 Artifact Store

Stores:

- Large artifacts.
- Trace bundles.
- UI bundles.
- Screenshots.
- Validation recordings.
- Run logs beyond DB limits.

---

## 8. v2 CLI Additions

New or expanded commands:

```bash
# Hosted
ves auth login vessica
ves workspace list
ves workspace create
ves workspace connect <workspace_id>
ves workspace invite <email>
ves workspace members

# Remote sandboxes
ves sandbox create --backend runloop|railway|vessica
ves run epic <epic_id> --sandbox remote

# General agents
ves agent create -f agent.json
ves agent pull <git-url>
ves agent list
ves agent view <agent_id>
ves agent update <agent_id> -f agent.json
ves agent delete <agent_id>
ves agent run <agent_id> --prompt "..." --stream

# Structured data
ves data schema apply --agent <agent_id> -f schema.json
ves data schema view --agent <agent_id>
ves data query --agent <agent_id> --collection <name>
ves data put --agent <agent_id> --collection <name> -f record.json

# Tasks and schedules
ves task add --agent <agent_id> --prompt "..."
ves task list
ves task view <task_id>
ves task cancel <task_id>
ves schedule create --agent <agent_id> --cron "0 9 * * MON" --prompt "..."
ves schedule pause <schedule_id>
ves schedule resume <schedule_id>

# Inbox
ves inbox list
ves inbox view <message_id>
ves inbox respond <message_id>

# UI
ves ui build --agent <agent_id>
ves ui preview --agent <agent_id>
ves ui publish --agent <agent_id>

# Admin/governance
ves org members
ves org roles
ves policy list
ves policy set
ves usage summary
```

---

## 9. Success Metrics

### 9.1 Hosted Adoption

- Number of hosted workspaces.
- Percentage of v1 users connecting hosted state.
- Time from signup to first hosted run.
- Number of active weekly agents/runs.

### 9.2 Team Coordination

- Number of users per workspace.
- Number of parallel agents per workspace.
- Ticket claim conflicts.
- Runs completed with multiple agents.
- Team memory usage.

### 9.3 General Agent Runtime

- Number of custom agents created.
- Number of scheduled agents.
- Number of agent-to-agent tasks.
- Number of structured data collections.
- Number of custom UI packages rendered.

### 9.4 Trust and Quality

- Validation pass rate.
- Human approval rate.
- Inbox response time.
- Run failure/resume success.
- Receipt views.
- ROI dashboard usage.

### 9.5 Commercial Metrics

- Hosted conversion rate.
- Usage by model/runtime spend.
- Cost per completed task.
- Return on Intelligence by workspace.
- Retention by active scheduled agents.

---

## 10. v2 Milestones

### Milestone 1 — Hosted State and Workspace

- Account login.
- Hosted workspace.
- Managed Postgres.
- CLI connects to hosted.
- Event ingestion to hosted.

### Milestone 2 — Remote Sandboxes

- One remote sandbox backend.
- Remote run support.
- Preview broker.
- Hosted logs.

### Milestone 3 — Hosted Run UI

- Live run page.
- Event stream.
- Artifacts.
- Preview.
- Receipt view.
- Run controls.

### Milestone 4 — General Agent Definitions

- Agent create/pull/list/view/run.
- Agent schema validation.
- Generic run workflow.
- Skills/workflow/eval refs.

### Milestone 5 — Structured Data

- Agent data schemas.
- Collections.
- Query/read/write APIs.
- CLI data commands.
- Permissions.

### Milestone 6 — Tasks, Schedules, Inbox

- Task queue.
- Agent-to-agent tasking.
- Scheduler.
- Human inbox.
- Hosted UI for inbox.

### Milestone 7 — Custom UIs

- UI package refs.
- Build/publish.
- Hosted rendering.
- Scoped data API.
- Declarative card fallback.

### Milestone 8 — Governance and ROI

- Team roles.
- Policies.
- Audit.
- Usage dashboard.
- ROI dashboard.

---

## 11. Out-of-Scope Details for v2

Defer beyond v2 unless demanded by users:

- Public marketplace for agent packs.
- Complex drag-and-drop workflow builder.
- Full enterprise compliance certifications.
- Production app hosting.
- Autonomous external payments.
- Autonomous PR merge/deploy to production without approval.
- Fine-tuned model hosting.
- Cross-org agent federation.
- Complex data warehouse product.
- Replacing BI dashboards.

---

## 12. v2 Launch Definition

Vessica v2 is launchable when a user can:

```bash
ves auth login vessica
ves workspace create "Acme AI Engineering"
ves init --profile hosted
ves run epic epic_abc123 --sandbox remote --preview --pr draft --stream
```

And also:

```bash
ves agent pull https://github.com/acme/weekly-market-agent.git
ves agent run agent_abc123 --prompt "Prepare this week's market update" --stream
ves schedule create --agent agent_abc123 --cron "0 9 * * MON" --prompt "Weekly market update"
ves inbox list
```

The hosted UI must show:

- Live software runs.
- Live general agent runs.
- Agent data.
- Inbox.
- Preview.
- Receipts.
- Cost and ROI summaries.

---

## 13. Strategic Product Arc

v1:

```text
Local-first harness engineering CLI for software agents.
```

v2:

```text
Hosted coordination platform for durable agent work.
```

The bridge is the shared object model:

```text
agent → task → run → events → artifacts/data/memory → receipt → human review
```

Vessica wins if it becomes the control plane that makes agent work durable, coordinated, observable, auditable, and economically legible.
