# Memory Benchmark — Seeded Baseline and First Treatment

Run `20260719-a` exercised 18 cold-chat sensors across project-feature
discovery and a synthetic chief-of-staff workflow. The objective scorer passed
all 84 trigger, confirmation, isolation, and answer-canary checks. That 100%
canary result is not the overall quality score: storage structure, retrieval
efficiency, and answer calibration were reviewed separately.

## Environment

- `ves`: `0.2.44` plus the local changes in this run
- Codex CLI: `0.144.1`
- Knowledge: hosted, lexical, index fresh, no embedding model
- Workspace: `ws_tbg0i54y6n`
- Repository scope: `scope_o24uarwyqduvhf2v`
- Cold-chat isolation: `codex exec --disable memories`

## Weighted result

| Dimension | Weight | Seeded baseline | First treatment | Findings |
| --- | ---: | ---: | ---: | --- |
| Trigger decision | 20 | 20 | 20 | Required retrieval invoked Vessica; both negative controls and both non-write controls stayed clean. |
| Storage quality | 30 | 23 | 23 | Atomic types and confirmation were good. Generic titles, default importance, repo-only scope, and absent entity/relationship metadata reduced the score. |
| Retrieval quality | 30 | 14 | 19 | Direct-query guidance and punctuation normalization reduced exact/paraphrase work. Context assembly and persona disambiguation remain weak. |
| Restoration quality | 20 | 14 | 15 | Current rules and system-of-record boundaries were restored, but one chief-of-staff answer invented a 30-minute meeting duration. |
| **Total** | **100** | **71** | **77** | **Useful and safe enough for guided use, but not yet reliable enough for autonomous chief-of-staff actions.** |

These are reviewer scores under the rubric in `evals/memory/README.md`, not a
statistically powered product score. The run is a reproducible first benchmark
and diagnostic, not a population estimate.

## Trigger and storage behavior

- Generic Go and generic calendar/task questions did not invoke memory.
- Explicit cross-session restoration did invoke `vessica:use-knowledge` and did
  not write.
- Disposable feature brainstorming and a one-day calendar change were not
  stored.
- Project Juniper was split into three decisions instead of a transcript dump.
  Two titles omitted the project name and all three defaulted to importance
  `0.5`, which weakens retrieval and inspection.
- The chief-of-staff write separated stable scheduling guidance from durable
  account/champion context, assigned importance `0.8`, and excluded live CRM
  stage/value/activity and calendar availability.
- Updates reused the original IDs and advanced versions. Retrieval suppressed
  the stale 36-hour link lifetime and 10:30 AM scheduling floor.

No plugin-authored record created an entity, relationship, semantic subject
key, or source-conversation link. Every record used repository scope. That is
adequate for a single product project, but it cannot safely distinguish two
people or businesses with similar scheduling and pipeline memories.

## Retrieval measurements

| Sensor | Seeded baseline | First treatment | Outcome |
| --- | --- | --- | --- |
| Exact Project Alder restore | 130,653 input tokens | 92,298 input tokens | Correct audience, 12-hour lifetime, invited-reviewer rule, and v1 boundary. |
| Paraphrased `clinic-coordinator` restore | 111,880 input tokens, four searches | 97,224 input tokens, two searches | Hyphen normalization and canonical follow-up recovered the right access rule. |
| Scoped chief-of-staff restore | 162,146 input tokens | 132,478 input tokens | Correct Keystone/Jordan and current scheduling rules; live CRM/calendar facts remained unknown. |
| Broad chief-of-staff synthesis with a second persona | Not comparable | 226,552 input tokens | Eventually disambiguated, but required extra searches and exposed missing person/account scope. |

Latency was noisy enough in one run that token and tool-call reductions are the
more useful directional signals. The paraphrase treatment halved search calls
from four to two and reduced input tokens by about 13%.

`ves knowledge context` remains a separate failure: at a 4,000-token budget it
admitted 28 unrelated active artifacts, consumed an estimated 3,960 tokens,
and admitted zero memories. Query relevance must gate artifact admission before
ranking or embeddings can improve this path.

## First treatment

The treatment deliberately changed retrieval integration rather than model
behavior broadly:

1. `ves prime` now works for hosted attachments without dereferencing a nil
   local state database. Hosted epic/ticket selectors return a clear unsupported
   error instead of panicking.
2. Empty memory list/search JSON is now `[]`, not `null`.
3. The bundled knowledge skill forbids Vessica requests from reading Codex local
   memories/session logs and uses a bounded query protocol instead of synonym
   fan-out.
4. Memory search normalizes hyphens, underscores, and dash variants before
   lexical retrieval.
5. Hosted memory state transitions map persisted states to the server's action
   routes (`archived` to `/archive`, `superseded` to `/supersede`). A parity test
   now covers archival in local and hosted modes.

The fifth item was discovered during benchmark cleanup: all dry-runs passed,
but real archive calls returned HTTP 404 because the CLI requested
`/archived`. After the fix, all nine synthetic records archived successfully.

## Prioritized follow-ups

1. Add relevance-aware context admission so unrelated authoritative artifacts
   cannot consume the whole memory budget.
2. Add first-class person, organization, account, and calendar/CRM scopes plus
   relationships. Require a scoped subject before applying chief-of-staff
   instructions autonomously.
3. Render a stronger restoration contract: never infer duration, availability,
   stage, value, owner, or next activity; fetch them from their source systems.
4. Improve extraction defaults: canonical subject in every title, explicit
   importance rationale, temporal fields where appropriate, source-conversation
   provenance, and semantic subject/predicate/object keys.
5. Add rank/score explanations and enforce a small search-call budget in the
   cold-chat harness.
6. Run the same suite in semantic-hybrid mode as a separate series only after
   context admission and scope isolation are fixed.

## Cleanup verification

The run created nine namespaced records. All nine are archived. Active memory
count returned from 35 to the pre-run 26, and the active list contains zero
`[MEMBENCH:20260719-a]` records. The archive responses advanced each record to
a terminal archived version, preserving the benchmark evidence in version
history without allowing it to affect later retrieval.
