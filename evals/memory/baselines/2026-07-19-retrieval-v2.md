# Memory Benchmark — Retrieval v2 Treatment

Run `20260719-v2` evaluated corrected lexical retrieval, semantic-hybrid
retrieval with `text-embedding-3-small`, context admission, and cold-chat
restoration through the installed Vessica plugin. Codex local memory was
disabled and every chat was ephemeral.

## Environment

- Vessica CLI: `0.2.45`
- Knowledge server: `0.5.4`
- Retrieval: `v2`, semantic hybrid
- Embeddings: OpenAI `text-embedding-3-small`, backlog zero, index fresh
- Reranking: disabled
- Fixed ranked-query set: 6 queries covering project paraphrases,
  near-duplicates, conflicting personas, and distractors

## Weighted result

| Dimension | Weight | v1 treatment | v2 treatment | Findings |
| --- | ---: | ---: | ---: | --- |
| Trigger decision | 20 | 20 | 20 | The negative control made no Vessica call; restoration chats selected the knowledge skill. |
| Storage quality | 30 | 23 | 27 | Atomic decisions/instructions, canonical subjects, SPO fields, entity links, confidence, importance, and the synthetic namespace were present. Source-conversation provenance and richer relationships remain follow-ups. |
| Retrieval quality | 30 | 19 | 30 | Recall@5/10, MRR@5, and NDCG@5 were 1.00 with zero distractor, stale, scope, or wrong-person violations. Context admitted relevant durable memories under 4,000 tokens. |
| Restoration quality | 20 | 15 | 20 | Feature and named-persona constraints were restored exactly; an unnamed conflicting persona produced a refusal instead of a guess. |
| **Total** | **100** | **77** | **97** | **Clears the target of 85 without model reranking.** |

The storage score remains partly manual because the ranked retrieval fixture
uses controller-created synthetic records. The unchanged base suite still
passes all 84 objective canaries.

## Ranked retrieval

| Configuration | Recall@1 | Recall@5 | Recall@10 | MRR@5 | NDCG@5 | p50 | p95 | Violations |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Corrected lexical v2 | 0.6111 | 1.00 | 1.00 | 1.00 | 1.00 | 612.9 ms | 1,053.4 ms | 0 |
| Hybrid v2, `text-embedding-3-small` | 0.6111 | 1.00 | 1.00 | 1.00 | 1.00 | 749.1 ms | 906.0 ms | 0 |

Hybrid v2 meets the one-second p95 gate. Recall@1 is lower because three
project decisions are all relevant to multi-answer queries; every required ID
is present by rank five. The runner records no reranker calls. Query-embedding
token/cost telemetry is not yet surfaced in the retrieval response, so the
result files must not be interpreted as zero embedding-provider usage.

At a 4,000-token context budget, all three relevant Alder decisions were
admitted. Unrelated artifacts were constrained by relevance and type budgets;
omissions reported `irrelevant`, `type_budget`, or `token_budget` rather than
silently crowding out durable memory.

## Cold-chat plugin treatment

| Sensor | v1 input tokens | v2 input tokens | Change | Vessica retrieval calls | Result |
| --- | ---: | ---: | ---: | ---: | --- |
| Exact project restoration | 92,298 | 63,718 | -31.0% | 1 | Restored all three Alder decisions. |
| Paraphrased project restoration | 97,224 | 73,426 | -24.5% | 1 | Restored all three Juniper decisions. |
| Named chief-of-staff persona | 132,478 | 63,760 | -51.9% | 1 | Resolved the West entity and restored only its instruction. |
| **Comparable total** | **322,000** | **200,904** | **-37.6%** | **3** | **Clears the 30% aggregate reduction target.** |

The exact and paraphrase chats each stayed below the two-retrieval-call cap.
The negative control answered directly with no Vessica command. An unnamed
sales-persona chat returned `ambiguous_subject`; after the plugin contract was
hardened, Codex refused to apply either conflicting instruction.

## Reranker decision

Luna and nano were not promoted or enabled. Hybrid retrieval already achieved
NDCG@5 of 1.00, so neither model can deliver the required improvement of 0.03.
Keeping reranking disabled also avoids sending readable candidate memory text
to a model provider and preserves deterministic sub-second retrieval.

## Cleanup

All 11 `[MEMBENCH:20260719-v2]` memories were previewed and archived as version
2 after cold-chat verification. An active lexical search for the namespace
returns an empty array. Embedding backlog remains zero and the index remains
fresh.
