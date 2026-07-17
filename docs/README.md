# Vessica documentation map

Use this page to distinguish current operating guidance from historical product
and architecture records.

## Current user and operator guidance

- [`../README.md`](../README.md): product overview, quickstart, capabilities,
  command map, integrations, security, and current release status.
- [`Vessica_Operator_Guide.md`](Vessica_Operator_Guide.md): safe CLI workflows,
  hosted setup, knowledge, Linear, recovery, and troubleshooting.
- [`Hosted_Railway.md`](Hosted_Railway.md): hosted topology, provisioning,
  OAuth, operations, previews, and secrets.
- [`Vessica_stream_v1.md`](Vessica_stream_v1.md): stable JSONL streaming
  protocol and resume semantics.
- [`Knowledge_Layer_Followups.md`](Knowledge_Layer_Followups.md): current shipped
  state and remaining knowledge/operations gaps.

Repository contributors should also read the root `ARCHITECTURE.md`,
`SECURITY.md`, `TESTING.md`, `DEPLOY.md`, `DESIGN.md`, and `AGENTS.md`.

## Decision records still relevant to the current design

- [`Vessica_Hosted_First_Quickstart_ADR.md`](Vessica_Hosted_First_Quickstart_ADR.md)
- [`Vessica_Single_Postgres_Logical_Databases_ADR.md`](Vessica_Single_Postgres_Logical_Databases_ADR.md)
- [`Vessica_Dashboard_v1_ADR.md`](Vessica_Dashboard_v1_ADR.md)
- [`Vessica_Knowledge_Layer_v1_ADR.md`](Vessica_Knowledge_Layer_v1_ADR.md)

These records explain decisions, and some are explicitly superseded in part.
When an example or capability statement differs from the current guides above,
follow the current guide.

## Historical requirements and planning records

The following documents preserve the intent and implementation history of the
product. They are not command references or promises that every described
future capability ships today:

- `Vessica_v1_PRD.md` and `Vessica_v1_ADR.md`
- `Vessica_v2_PRD.md`
- `Vessica_Dashboard_v1_PRD.md`
- `Vessica_Knowledge_Layer_v1_PRD.md`

The v1 documents predate the hosted-first product decision. The v2 PRD includes
future general-agent platform ideas beyond the current software-engineering
control plane.

## Product and marketing input

- [`Marketing_Site_Update_Memo.md`](Marketing_Site_Update_Memo.md): current
  feature/benefit framing, reliability story, site narrative, proof points, and
  claim guardrails for the next marketing-site brief.
