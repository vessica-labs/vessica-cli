#!/usr/bin/env python3
"""Run a fixed Vessica retrieval fixture through the public ves command."""

from __future__ import annotations

import argparse
import json
import subprocess
import time
from pathlib import Path
from typing import Any


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--fixture", type=Path, required=True)
    parser.add_argument("--ves", default="ves")
    parser.add_argument("--configuration", required=True)
    parser.add_argument("--rerank", choices=["auto", "never", "always"], default="never")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    fixture = json.loads(args.fixture.read_text(encoding="utf-8"))
    subprocess.run([args.ves, "knowledge", "status", "--json"], check=True, text=True, capture_output=True)
    observations: list[dict[str, Any]] = []
    for query in fixture["queries"]:
        command = [args.ves, "memory", "retrieve", query["query"], "--limit", "20", "--rerank", args.rerank, "--json"]
        for entity_id in query.get("entity_ids", []):
            command.extend(["--entity", entity_id])
        started = time.monotonic()
        completed = subprocess.run(command, check=True, text=True, capture_output=True)
        latency_ms = round((time.monotonic() - started) * 1000, 1)
        envelope = json.loads(completed.stdout)
        data = envelope["data"]
        if query.get("expect_ambiguity") and data.get("ambiguity") != query["expect_ambiguity"]:
            raise RuntimeError(f"{query['id']}: expected {query['expect_ambiguity']}, got {data.get('ambiguity')}")
        observation = {key: value for key, value in query.items() if key != "query"}
        observation.update(
            {
                "ranked_ids": [item["memory"]["id"] for item in data["results"]],
                "latency_ms": latency_ms,
                "retrieval_mode": data["retrieval_mode"],
                "ranking_version": data["ranking"]["version"],
                "index_fresh": data["index_fresh"],
                "ambiguity": data.get("ambiguity", ""),
                "provider_calls": 1 if data["rerank"]["applied"] else 0,
                "input_tokens": data["rerank"].get("input_tokens", 0),
                "output_tokens": data["rerank"].get("output_tokens", 0),
            }
        )
        observations.append(observation)
    rendered = json.dumps(
        {
            "schema": "vessica.retrieval-eval-results/v1",
            "run_id": fixture["run_id"],
            "configurations": [{"name": args.configuration, "queries": observations}],
        },
        indent=2,
    ) + "\n"
    if args.output:
        args.output.write_text(rendered, encoding="utf-8")
    else:
        print(rendered, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
