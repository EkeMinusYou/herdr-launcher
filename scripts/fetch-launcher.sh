#!/usr/bin/env bash
# Put the prebuilt launcher for this plugin version into bin/. herdr runs this as the plugin's build step
# on macOS and Linux (scripts/fetch-launcher.ps1 on Windows). The binary is named launcher.exe everywhere
# so that one manifest command works on every platform.
#
# Modes, in order:
#   1. LAUNCHER_FROM_SOURCE=1 is set               -> go build (self-rebuilding, like a linked checkout)
#   2. matching binary already installed           -> exit 0
#   3. download the release asset and verify it against SHA256SUMS
set -euo pipefail

cd "$(dirname "$0")/.."
version="$(sed -n 's/^version = "\(.*\)"$/\1/p' herdr-plugin.toml | head -1)"
[ -n "$version" ] || { echo "no version in herdr-plugin.toml" >&2; exit 1; }
mkdir -p bin

if [ -n "${LAUNCHER_FROM_SOURCE:-}" ]; then
  go build -o bin/launcher.exe .
  rm -f bin/launcher.version
  echo "built launcher from source"
  exit 0
fi

if [ -x bin/launcher.exe ] && [ "$(cat bin/launcher.version 2>/dev/null || true)" = "$version" ]; then
  echo "launcher $version already installed"
  exit 0
fi

case "$(uname -s)/$(uname -m)" in
  Darwin/arm64)              target=darwin_arm64 ;;
  Darwin/x86_64)             target=darwin_amd64 ;;
  Linux/x86_64)              target=linux_amd64 ;;
  Linux/aarch64|Linux/arm64) target=linux_arm64 ;;
  *) echo "no prebuilt launcher for $(uname -s)/$(uname -m); set LAUNCHER_FROM_SOURCE=1 to build it with Go" >&2; exit 1 ;;
esac

asset="launcher_$target"
base="https://github.com/EkeMinusYou/herdr-launcher/releases/download/v$version"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$2" "$1"
  else
    echo "need curl or wget" >&2
    return 1
  fi
}

echo "downloading $base/$asset"
fetch "$base/$asset" "$tmp/$asset"
fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS"
expected="$(awk -v a="$asset" '$2 == a {print $1}' "$tmp/SHA256SUMS")"
[ -n "$expected" ] || { echo "$asset is not listed in $base/SHA256SUMS" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
fi
[ "$actual" = "$expected" ] || { echo "sha256 mismatch for $asset: expected $expected, got $actual" >&2; exit 1; }

chmod +x "$tmp/$asset"
mv "$tmp/$asset" bin/launcher.exe
echo "$version" > bin/launcher.version
echo "installed launcher $version ($target)"
