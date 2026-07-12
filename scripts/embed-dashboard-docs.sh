#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="$repo_root/internal/dashboard/docs"
mkdir -p "$target"
rm -f "$target"/*.md
cp "$repo_root"/docs/*.md "$target"/
