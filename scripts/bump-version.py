#!/usr/bin/env python3
import pathlib
import re
import sys


def main() -> int:
    path = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "VERSION")
    current = path.read_text(encoding="utf-8").strip() if path.exists() else "0.1.0"
    match = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)", current)
    if not match:
        raise SystemExit(f"invalid semver in {path}: {current!r}")
    major, minor, patch = map(int, match.groups())
    next_version = f"{major}.{minor}.{patch + 1}"
    path.write_text(next_version + "\n", encoding="utf-8")
    print(next_version)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
