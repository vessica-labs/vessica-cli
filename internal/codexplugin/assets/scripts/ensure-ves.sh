#!/usr/bin/env bash
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
version="$(tr -d '[:space:]' <"$script_dir/cli-version.txt")"
install_dir="${VES_BIN_DIR:-$HOME/.vessica/bin}"

if [ -z "$version" ] || [ "$version" = "dev" ]; then
  echo "Vessica plugin does not contain a released CLI pin" >&2
  exit 1
fi

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "A SHA-256 utility (sha256sum or shasum) is required" >&2
    exit 1
  fi
}

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

case "$(uname -s)-$(uname -m)" in
  Darwin-x86_64) target="darwin_amd64" ;;
  Darwin-arm64) target="darwin_arm64" ;;
  Linux-x86_64) target="linux_amd64" ;;
  Linux-aarch64|Linux-arm64) target="linux_arm64" ;;
  *) echo "Unsupported platform for the Vessica CLI bootstrap" >&2; exit 1 ;;
esac

asset="vessica-cli_${version}_${target}.tar.gz"
base="https://github.com/vessica-labs/vessica-cli/releases/download/v${version}"
stamp="$install_dir/.ves-${version}-${target}.sha256"

if [ -x "$install_dir/ves" ] && [ -f "$stamp" ] && "$install_dir/ves" --json version 2>/dev/null | grep -q "\"version\":\"$version\""; then
  installed_hash="$(sha256_file "$install_dir/ves")"
  recorded_hash="$(tr -d '[:space:]' <"$stamp")"
  if [ -n "$recorded_hash" ] && [ "$installed_hash" = "$recorded_hash" ]; then
    run_ves "$install_dir/ves" "$@"
  fi
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
curl -fsSL "$base/$asset" -o "$tmp/$asset"
expected="$(awk -v name="$asset" '$2==name || $2=="./"name {print $1}' "$tmp/checksums.txt")"
if [ -z "$expected" ]; then echo "Release checksum is missing for $asset" >&2; exit 1; fi
actual="$(sha256_file "$tmp/$asset")"
if [ "$actual" != "$expected" ]; then echo "Vessica CLI checksum verification failed" >&2; exit 1; fi
tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$install_dir"
install -m 0755 "$tmp/vessica-cli_${version}_${target}/ves" "$install_dir/ves"
sha256_file "$install_dir/ves" > "$stamp"
run_ves "$install_dir/ves" "$@"
