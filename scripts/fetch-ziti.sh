#!/usr/bin/env bash
# Pre-release hook: fetch the upstream `ziti` CLI binary for each architecture
# we ship and unpack it into dist/vendored-ziti/ so GoReleaser's archive and
# docker stages can pick it up.
#
# Idempotent — won't re-download if the file is already present and non-empty.
# Driven by ZITI_VERSION env var (set in .goreleaser.yaml). Bump that variable
# to roll the bundled Ziti CLI version.

set -euo pipefail

: "${ZITI_VERSION:?ZITI_VERSION must be set (e.g. 2.0.0-pre11)}"

OUT_DIR="build/vendored-ziti"
mkdir -p "$OUT_DIR"

for arch in amd64 arm64; do
  out="$OUT_DIR/ziti-linux-$arch"
  if [ -s "$out" ]; then
    echo "fetch-ziti: $out already present (skipping)"
    continue
  fi
  url="https://github.com/openziti/ziti/releases/download/v${ZITI_VERSION}/ziti-linux-${arch}-${ZITI_VERSION}.tar.gz"
  tmp="$(mktemp -d)"
  echo "fetch-ziti: downloading $url"
  curl -fsSL -o "$tmp/archive.tar.gz" "$url"
  tar -xzf "$tmp/archive.tar.gz" -C "$tmp" ziti
  mv "$tmp/ziti" "$out"
  chmod 0755 "$out"
  rm -rf "$tmp"
  echo "fetch-ziti: wrote $out"
done

echo "fetch-ziti: done"
