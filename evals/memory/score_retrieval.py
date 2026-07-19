#!/usr/bin/env python3
"""Score fixed retrieval configurations from ranked memory IDs and telemetry."""

from __future__ import annotations

import argparse
import json
import math
from pathlib import Path
from statistics import mean
from typing import Any


def dcg(relevances: list[int], k: int) -> float:
    return sum(rel / math.log2(index + 2) for index, rel in enumerate(relevances[:k]))


def percentile(values: list[float], pct: float) -> float:
    if not values:
        return 0
    ordered = sorted(values)
    return ordered[math.ceil((len(ordered) - 1) * pct)]


def score_config(config: dict[str, Any]) -> dict[str, Any]:
    queries = config.get("queries", [])
    recall = {1: [], 5: [], 10: []}
    reciprocal_ranks: list[float] = []
    ndcgs: list[float] = []
    latencies: list[float] = []
    violations = {"distractor": 0, "stale": 0, "scope": 0, "wrong_person": 0}
    provider_calls = input_tokens = output_tokens = 0
    for query in queries:
        expected = set(query.get("relevant_ids", []))
        ranked = query.get("ranked_ids", [])
        for k in recall:
            recall[k].append(len(expected.intersection(ranked[:k])) / len(expected) if expected else 1.0)
        ranks = [ranked.index(item) + 1 for item in expected if item in ranked[:5]]
        reciprocal_ranks.append(1 if not expected and query.get("ambiguity") else (1 / min(ranks) if ranks else 0))
        observed = [1 if item in expected else 0 for item in ranked[:5]]
        ideal = [1] * min(len(expected), 5)
        ideal_score = dcg(ideal, 5)
        ndcgs.append(dcg(observed, 5) / ideal_score if ideal_score else 1.0)
        returned = set() if query.get("ambiguity") else set(ranked[:5])
        for kind, field in (("distractor", "distractor_ids"), ("stale", "stale_ids"), ("scope", "wrong_scope_ids"), ("wrong_person", "wrong_person_ids")):
            violations[kind] += len(returned.intersection(query.get(field, [])))
        latencies.append(float(query.get("latency_ms", 0)))
        provider_calls += int(query.get("provider_calls", 0))
        input_tokens += int(query.get("input_tokens", 0))
        output_tokens += int(query.get("output_tokens", 0))
    return {
        "name": config.get("name"),
        "queries": len(queries),
        "recall_at_1": round(mean(recall[1]), 4) if queries else 0,
        "recall_at_5": round(mean(recall[5]), 4) if queries else 0,
        "recall_at_10": round(mean(recall[10]), 4) if queries else 0,
        "mrr_at_5": round(mean(reciprocal_ranks), 4) if queries else 0,
        "ndcg_at_5": round(mean(ndcgs), 4) if queries else 0,
        "violations": violations,
        "provider_calls": provider_calls,
        "input_tokens": input_tokens,
        "output_tokens": output_tokens,
        "latency_p50_ms": round(percentile(latencies, .50), 1),
        "latency_p95_ms": round(percentile(latencies, .95), 1),
        "estimated_api_cost_usd": config.get("estimated_api_cost_usd", 0),
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("results", type=Path)
    args = parser.parse_args()
    payload = json.loads(args.results.read_text(encoding="utf-8"))
    scored = [score_config(config) for config in payload.get("configurations", [])]
    print(json.dumps({"schema": "vessica.retrieval-eval-score/v1", "configurations": scored}, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
