#!/bin/sh
set -eu

app_id="${1:-}"
destination="${2:-}"
case "$app_id" in
  plugin_asdk_app_*) ;;
  *) echo "usage: render-chatgpt-plugin.sh <registered plugin_asdk_app ID> <new destination>" >&2; exit 2 ;;
esac
app_suffix=${app_id#plugin_asdk_app_}
case "$app_suffix" in
  ""|*[!A-Za-z0-9_-]*) echo "registered app ID contains invalid characters" >&2; exit 2 ;;
esac
if [ -z "$destination" ] || [ "$destination" = "/" ] || [ -e "$destination" ]; then
  echo "destination must be a new, explicit directory" >&2
  exit 2
fi

plugin_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mkdir -p "$destination"
cp -R "$plugin_root/." "$destination/"
rm -f "$destination/.mcp.json"
python3 - "$destination/.codex-plugin/plugin.json" "$destination/.app.json" "$app_id" <<'PY'
import json
import pathlib
import sys

manifest_path = pathlib.Path(sys.argv[1])
app_path = pathlib.Path(sys.argv[2])
app_id = sys.argv[3]
manifest = json.loads(manifest_path.read_text())
manifest.pop("mcpServers", None)
manifest["apps"] = "./.app.json"
manifest_path.write_text(json.dumps(manifest, indent=2) + "\n")
app_path.write_text(json.dumps({"apps": {"vessica": {"id": app_id, "category": "Productivity"}}}, indent=2) + "\n")
PY
