#!/usr/bin/env python3
"""Score objective parts of a Vessica memory sensor run.

The results file is intentionally simple: {"observations": [{...}]}. Each
observation may be captured from a cold Codex chat or filled in by a reviewer.
Semantic memory structure remains a human-scored dimension documented in the
runbook; this script scores only observable trigger, confirmation, and answer
canaries.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def load(path: Path) -> Any:
    with path.open(encoding="utf-8") as handle:
        return json.load(handle)


def contains(text: str, needle: str) -> bool:
    return needle.casefold() in text.casefold()


def score_sensor(sensor: dict[str, Any], observed: dict[str, Any]) -> dict[str, Any]:
    expected = sensor["expected"]
    memory_use = expected["memory_use"]
    write = expected["write"]
    plugin_invoked = bool(observed.get("plugin_invoked"))
    proposed_write = bool(observed.get("proposed_write"))
    confirmed_before_write = bool(observed.get("confirmed_before_write"))
    wrote_memory = bool(observed.get("wrote_memory"))
    answer = str(observed.get("answer", ""))

    checks: list[dict[str, Any]] = []

    if sensor["phase"] in {"trigger", "retrieval", "retrieval_after_update"}:
        checks.append(
            {
                "name": "codex_local_memory_isolated",
                "pass": bool(observed.get("codex_local_memory_disabled")),
            }
        )

    if memory_use == "required":
        checks.append({"name": "trigger", "pass": plugin_invoked})
    elif memory_use == "forbidden":
        checks.append({"name": "trigger", "pass": not plugin_invoked})

    if write == "required_after_confirmation":
        checks.extend(
            [
                {"name": "write_proposed", "pass": proposed_write},
                {
                    "name": "confirmation_gate",
                    "pass": (not wrote_memory) or confirmed_before_write,
                },
            ]
        )
    elif write == "forbidden":
        checks.append({"name": "no_write", "pass": not proposed_write and not wrote_memory})

    for needle in expected.get("must_restore", []):
        checks.append(
            {"name": f"restore:{needle}", "pass": contains(answer, needle)}
        )
    for needle in expected.get("must_not_restore", []):
        checks.append(
            {"name": f"reject:{needle}", "pass": not contains(answer, needle)}
        )

    earned = sum(1 for check in checks if check["pass"])
    possible = len(checks)
    return {
        "id": sensor["id"],
        "track": sensor["track"],
        "phase": sensor["phase"],
        "earned": earned,
        "possible": possible,
        "checks": checks,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("results", type=Path)
    parser.add_argument(
        "--scenarios",
        type=Path,
        default=Path(__file__).with_name("scenarios.json"),
    )
    parser.add_argument(
        "--suite",
        choices=["base", "retrieval_v2", "all"],
        default="base",
        help="base preserves the original 18-sensor, 84-canary benchmark",
    )
    args = parser.parse_args()

    scenarios = load(args.scenarios)
    results = load(args.results)
    by_id = {item["id"]: item for item in results.get("observations", [])}

    suite_ids = set()
    if args.suite == "all":
        for ids in scenarios.get("suites", {}).values():
            suite_ids.update(ids)
    else:
        suite_ids.update(scenarios.get("suites", {}).get(args.suite, []))
    selected = [sensor for sensor in scenarios["sensors"] if not suite_ids or sensor["id"] in suite_ids]

    scored = []
    missing = []
    for sensor in selected:
        observed = by_id.get(sensor["id"])
        if observed is None:
            missing.append(sensor["id"])
            continue
        scored.append(score_sensor(sensor, observed))

    earned = sum(item["earned"] for item in scored)
    possible = sum(item["possible"] for item in scored)
    total_sensors = len(selected)
    output = {
        "schema": "vessica.memory-eval-score/v1",
        "suite": args.suite,
        "objective_score_observed": round(100 * earned / possible, 1) if possible else 0,
        "coverage_pct": round(100 * len(scored) / total_sensors, 1),
        "earned": earned,
        "possible": possible,
        "missing": missing,
        "sensors": scored,
    }
    print(json.dumps(output, indent=2))
    return 0 if not missing and earned == possible else 1


if __name__ == "__main__":
    raise SystemExit(main())
