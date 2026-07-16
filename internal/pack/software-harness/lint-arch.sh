#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
soft_limit="${VES_ARCH_SOFT_LINE_LIMIT:-500}"
hard_limit="${VES_ARCH_HARD_LINE_LIMIT:-800}"
failures=0
warnings=0

while IFS= read -r -d '' file; do
  lines="$(wc -l < "$file" | tr -d ' ')"
  relative="${file#"$root"/}"
  if (( lines > hard_limit )); then
    printf 'arch-lint: error: %s has %d lines (hard limit %d)\n' "$relative" "$lines" "$hard_limit" >&2
    failures=$((failures + 1))
  elif (( lines > soft_limit )); then
    printf 'arch-lint: warning: %s has %d lines (soft limit %d)\n' "$relative" "$lines" "$soft_limit" >&2
    warnings=$((warnings + 1))
  fi
done < <(find "$root" -type f -name '*.go' \
  -not -path '*/vendor/*' \
  -not -path '*/.git/*' \
  -not -path '*/.vessica/*' \
  -print0)

if (( failures > 0 )); then
  printf 'arch-lint: failed with %d oversized Go file(s)\n' "$failures" >&2
  exit 1
fi
printf 'arch-lint: ok (%d soft-limit warning(s))\n' "$warnings"
