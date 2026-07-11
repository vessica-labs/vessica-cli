#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VES="${VES:-$ROOT/bin/ves}"
if [[ ! -x "$VES" ]]; then
  (cd "$ROOT" && go build -o bin/ves ./cmd/ves)
fi
WORKDIR="${TMPDIR:-/tmp}/ves-launch-smoke-$$"
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR"
cp -R "$ROOT/testdata/sample-app/." "$WORKDIR/"
cd "$WORKDIR"
git init -q
git add -A
git -c user.email=t@t.com -c user.name=t commit -qm init
"$VES" init --profile solo --runner codex --repo github --json >/dev/null
"$VES" pack install --json >/dev/null
"$VES" harness sync --json >/dev/null
echo "Reset password via email token" > epic.md
"$VES" epic add --title "Add password reset" --body-file epic.md --json >/dev/null
EPIC="$("$VES" epic list --json | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"][0]["id"])')"
VES_RUNNER_MODE=stub "$VES" run epic "$EPIC" --concurrency 2 --preview --pr draft --json > /tmp/ves-launch-run.json
python3 - <<'PY'
import json
r=json.load(open("/tmp/ves-launch-run.json"))["data"]
assert r["status"]=="completed"
assert r.get("preview_url")
assert r.get("pr_url")
assert r.get("receipt_id")
assert r.get("artifact_set_id")
assert r.get("sandbox_id")
assert r.get("sandbox_expires_at")
print("LAUNCH_OK", r["id"], r["receipt_id"])
PY
