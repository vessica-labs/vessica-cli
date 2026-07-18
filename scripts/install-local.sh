#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <ves-binary> <install-directory>" >&2
  exit 2
fi

source_binary="$1"
install_dir="$2"
installed_binary="$install_dir/ves"

if [ ! -x "$source_binary" ]; then
  echo "Vessica binary is missing or not executable: $source_binary" >&2
  exit 1
fi

mkdir -p "$install_dir"
install -m 0755 "$source_binary" "$installed_binary"

built_version="$($source_binary version --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["version"])')"
installed_version="$($installed_binary version --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["version"])')"
if [ "$installed_version" != "$built_version" ]; then
  echo "Installed CLI version $installed_version does not match built version $built_version" >&2
  exit 1
fi

plugin_workspace="$(mktemp -d)"
trap 'rm -rf "$plugin_workspace"' EXIT
"$installed_binary" --cwd "$plugin_workspace" setup codex --plugin --json >/dev/null

if ! command -v codex >/dev/null 2>&1; then
  echo "Installed ves $installed_version and refreshed the plugin source; Codex is not on PATH, so its plugin cache was not refreshed." >&2
  exit 0
fi

if codex plugin list --json | python3 -c 'import json,sys; raise SystemExit(0 if any(p.get("pluginId") == "vessica@personal" for p in json.load(sys.stdin).get("installed", [])) else 1)'; then
  codex plugin remove vessica@personal --json >/dev/null
fi
codex plugin add vessica@personal --json >/dev/null

plugin_version="$(python3 -c 'import json,pathlib; print(json.loads((pathlib.Path.home()/"plugins/vessica/.codex-plugin/plugin.json").read_text())["version"])')"
cached_version="$(codex plugin list --json | python3 -c 'import json,sys; print(next(p["version"] for p in json.load(sys.stdin)["installed"] if p.get("pluginId") == "vessica@personal"))')"
if [ "$cached_version" != "$plugin_version" ]; then
  echo "Codex cached plugin $cached_version instead of $plugin_version" >&2
  exit 1
fi

printf 'Installed ves %s and Codex plugin %s\n' "$installed_version" "$cached_version"
