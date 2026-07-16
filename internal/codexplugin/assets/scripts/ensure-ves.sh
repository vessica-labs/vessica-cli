#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
version="$(tr -d '[:space:]' <"$script_dir/cli-version.txt")"
checksum_manifest="$script_dir/cli-checksums.txt"
install_dir="${VES_BIN_DIR:-$HOME/.vessica/bin}"

ensure_railway() {
  railway_path=""
  if command -v railway >/dev/null 2>&1; then
    railway_path="$(command -v railway)"
  elif [ -x "$HOME/.railway/bin/railway" ]; then
    railway_path="$HOME/.railway/bin/railway"
  fi
  if [ -n "$railway_path" ]; then
    case "$($railway_path --version 2>/dev/null)" in railway\ 5.*) export PATH="$(dirname "$railway_path"):$PATH"; return ;; esac
    echo "Unsupported Railway CLI; Vessica requires tested major version 5" >&2
    exit 1
  fi
  railway_tmp="$(mktemp -d)"
  curl -fsSL https://railway.com/install.sh -o "$railway_tmp/install.sh"
  sh "$railway_tmp/install.sh"
  rm -rf "$railway_tmp"
  export PATH="$HOME/.railway/bin:$PATH"
  if ! command -v railway >/dev/null 2>&1; then
    echo "Railway CLI bootstrap completed but railway is not available on PATH" >&2
    exit 1
  fi
  case "$(railway --version 2>/dev/null)" in railway\ 5.*) ;; *) echo "Installed Railway CLI is not a tested major version" >&2; exit 1 ;; esac
}

run_ves() {
  ves_path="$1"
  shift
  ensure_railway
  exec "$ves_path" "$@"
}

if command -v ves >/dev/null 2>&1 && "$(command -v ves)" --json version 2>/dev/null | grep -q "\"version\":\"$version\""; then
  run_ves "$(command -v ves)" "$@"
fi
if [ -x "$install_dir/ves" ] && "$install_dir/ves" --json version 2>/dev/null | grep -q "\"version\":\"$version\""; then
  run_ves "$install_dir/ves" "$@"
fi

case "$(uname -s)-$(uname -m)" in
  Darwin-x86_64) target="darwin_amd64" ;;
  Darwin-arm64) target="darwin_arm64" ;;
  Linux-x86_64) target="linux_amd64" ;;
  Linux-aarch64|Linux-arm64) target="linux_arm64" ;;
  *) echo "Unsupported platform for the Vessica CLI bootstrap" >&2; exit 1 ;;
esac

asset="vessica-cli_${version}_${target}.tar.gz"
base="https://github.com/vessica-labs/vessica-cli/releases/download/v${version}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$base/$asset" -o "$tmp/$asset"
expected="$(awk -v name="$asset" '$2==name || $2=="./"name {print $1}' "$checksum_manifest")"
if [ -z "$expected" ]; then echo "Release checksum is missing for $asset" >&2; exit 1; fi
actual="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
if [ "$actual" != "$expected" ]; then echo "Vessica CLI checksum verification failed" >&2; exit 1; fi
tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$install_dir"
install -m 0755 "$tmp/vessica-cli_${version}_${target}/ves" "$install_dir/ves"
run_ves "$install_dir/ves" "$@"
