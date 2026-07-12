#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
assets="$root/internal/dashboard/assets"
if [[ ! -f "$assets/index.html" ]]; then
  echo "dashboard assets are missing" >&2
  exit 1
fi
compressed=0
while IFS= read -r -d '' file; do
  size="$(gzip -c "$file" | wc -c | tr -d ' ')"
  compressed="$((compressed + size))"
done < <(find "$assets" -type f -print0)
limit="$((500 * 1024))"
if (( compressed > limit )); then
  echo "dashboard compressed asset budget exceeded: $compressed > $limit" >&2
  exit 1
fi
echo "dashboard compressed assets: $compressed bytes"
