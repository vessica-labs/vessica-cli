# Vessica Memory Benchmark

This eval measures the full memory loop from a Codex conversation through the
Vessica plugin. It is not only a search benchmark. It separately measures:

1. whether the plugin decides to use memory;
2. whether it proposes a well-structured durable record;
3. whether Vessica retrieves the right active records; and
4. whether Codex restores them into a useful, calibrated answer.

The two tracks are feature discovery and a synthetic chief-of-staff workflow
covering scheduling preferences and sales-account context.

`scenarios.json` keeps the original 18 sensors in the `base` suite and adds a
`retrieval_v2` suite for paraphrase-only recall, near-duplicate projects,
conflicting personas, lifecycle/temporal distractors, wrong-scope semantic
neighbors, constrained context, embedding failure, and ambiguous subjects.

## Why the sensors use cold chats

Trigger behavior cannot be measured fairly in the conversation that designed
the benchmark: that conversation already mentions Vessica memory. Run each
trigger or retrieval sensor in a new Codex chat with the Vessica plugin enabled.
The originating chat remains the controller: it previews writes, records memory
IDs, runs direct `ves` probes, and scores the cold-chat transcripts.

Cold chats must have outbound access to the configured hosted knowledge service.
A filesystem read-only sandbox that also blocks DNS/network access is not a
valid hosted-retrieval treatment; record it separately as a harness failure.
When automating cold chats with `codex exec`, pass `--disable memories` so the
separate Codex local-memory feature cannot restore facts from controller session
files before the Vessica plugin runs. `--ephemeral` alone does not provide this
isolation.

## Safety and isolation

- Prefix every test title with `[MEMBENCH:<run-id>]` and include the run ID in
  the content. Never use unmarked synthetic data.
- Run `ves knowledge status --json` before the first sensor.
- Run every proposed `ves memory add|update|supersede|archive` with
  `--dry-run --json` first. The controller must show the exact effect and obtain
  confirmation before repeating it with `--yes` and an idempotency key.
- The chief-of-staff persona is synthetic. Its scheduling instructions must not
  be presented as the real user's preferences.
- Calendar events and CRM stage, value, owner, and next activity remain in
  their structured systems. Memory may hold stable preferences, rationale, and
  relationship context, but must not silently become a second CRM/calendar.
- Archive all benchmark memories after a run unless the user explicitly elects
  to retain them. Record every created and superseded memory ID before cleanup.
- Do not enable embeddings merely to improve a baseline. Establish lexical
  performance first, then compare semantic-hybrid mode as a separate treatment.

## Run protocol

Choose a run ID such as `20260719-a`. Save these observations for every sensor:

```json
{
  "id": "PF-TP-01",
  "plugin_invoked": true,
  "commands": ["ves knowledge status --json", "ves knowledge context ..."],
  "proposed_write": false,
  "confirmed_before_write": false,
  "wrote_memory": false,
  "retrieved_memory_ids": ["mem_..."],
  "answer": "...",
  "notes": "..."
}
```

Run in this order:

1. **Preflight and negative controls:** `PF-TN-01`, `COS-TN-01`, then record
   `ves knowledge status --json`, `ves memory list --json`, and a nonsense-token
   search. This establishes false-positive and contamination baselines.
2. **Cold missing-memory controls:** run `PF-TP-01` and `COS-RX-01` before
   seeding. Codex should invoke memory but clearly report that the synthetic
   facts are unavailable.
3. **Storage proposals:** run `PF-WR-01`, `PF-WR-02`, `COS-WR-01`, and
   `COS-WR-02`. A passing plugin proposes retrieval-optimized records and stops
   for confirmation. The controller dry-runs and, after confirmation, creates
   them with unique idempotency keys.
4. **Retrieval:** run `PF-RX-01`, `PF-RP-01`, and `COS-RX-01` in separate cold
   chats. Capture both Vessica JSON and the final Codex answer.
5. **Non-write controls:** run `PF-NW-01` and `COS-NW-01`. Verify the memory
   count and active IDs are unchanged.
6. **Versioning:** run `PF-UP-01` and `COS-UP-01`; confirm the dry-run; apply the
   updates; then run `PF-RU-01` and `COS-RU-01` cold.
7. **Cleanup:** archive the namespaced records after preview and confirmation,
   then rerun one exact query to verify archived data is not restored as active.

## Scoring

Use four equally visible dimensions. Keep the sub-scores separate; a good final
answer must not hide a broken plugin trigger or a poor stored record.

| Dimension | Weight | What passes |
| --- | ---: | --- |
| Trigger decision | 20 | Required sensors invoke the Vessica knowledge skill; forbidden sensors do not; optional read-only sensors never write. |
| Storage quality | 30 | Correct type, one durable idea per record, concise context, explicit synthetic/run namespace, confidence source, no transcript dump, preview plus confirmation, and update/supersede instead of contradictory active copies. |
| Retrieval quality | 30 | Correct active memory appears high enough to fit the budget, paraphrases work, distractors and archived/superseded versions are rejected, explanations are present, and unrelated active artifacts do not crowd memories out. |
| Restoration quality | 20 | The answer applies every current constraint, separates memory from calendar/CRM truth, states unknown live facts, cites returned IDs/provenance when useful, and does not invent precision. |

Hard failures cap the total score at 49: writing without confirmation, leaking
synthetic guidance into real behavior, silently falling back to local storage,
or presenting stale/transient/structured-system data as current durable truth.

The objective canaries can be scored with:

```bash
python3 evals/memory/score.py path/to/results.json
python3 evals/memory/score.py path/to/results.json --suite retrieval_v2
```

The base command remains the original 84-canary gate. Use `--suite all` for a
complete run. Record the five fixed configurations as lexical v1, lexical v2,
hybrid v2 with `text-embedding-3-small`, hybrid plus Luna, and hybrid plus nano.
For ranked-ID telemetry, run:

```bash
python3 evals/memory/score_retrieval.py path/to/retrieval-results.json
```

Promotion requires Recall@5 at least 0.95, MRR@5 at least 0.90, zero stale,
scope, archived, or wrong-person restorations, at least one relevant memory in
a 4,000-token context when one exists, hybrid p95 at most one second, and no
more than two Vessica retrieval calls per exact or paraphrase cold chat.
Conditional reranking additionally requires NDCG@5 improvement of at least
0.03, no Recall@5 regression, p95 overhead no greater than 2.5 seconds, and an
invocation rate no greater than 25 percent.

Review storage structure and answer usefulness manually using the weighted
rubric. Report latency, retrieval mode, token budget, memory IDs, rank/score
explanations, omissions, and the exact Codex/`ves` versions with every run.

## Improvement loop

Classify every miss at the earliest failed layer:

- **No plugin invocation:** adjust skill description, trigger guidance, or the
  Codex integration. Do not tune retrieval for a call that never happened.
- **Bad proposed record:** improve extraction/typing, atomicity, source links,
  temporal metadata, or confirmation UX.
- **Correct record, bad result set:** tune scope/entity hints, lexical query
  formation, ranking, active-version filtering, artifact admission, or budgets.
- **Correct result, bad answer:** improve context rendering and restoration
  instructions rather than storage/ranking.

Change one layer at a time and rerun the same sensor IDs. Keep lexical and
semantic-hybrid results as separate benchmark series.
