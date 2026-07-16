#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
exec "$root/internal/pack/software-harness/lint-arch.sh" "$@"
