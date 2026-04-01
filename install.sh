#!/bin/sh
# Universal installer — downloads the right binary and runs it
# Usage: curl -sf https://your-network.example/install | sh
# Usage: curl -sf https://any.endpoint/install | sh -s -- https://any.endpoint
set -e

BASE_URL="${1:-https://join.example.net}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7*|armhf)  ARCH="arm" ;;
esac

BINARY="schmutz-join-${OS}-${ARCH}"
TMP=$(mktemp -d)
trap "rm -rf $TMP" EXIT

echo "downloading ${BINARY}..."
curl -fsSL "${BASE_URL}/download/${BINARY}" -o "${TMP}/schmutz-join"
curl -fsSL "${BASE_URL}/download/${BINARY}.sha256" -o "${TMP}/checksum"

EXPECTED=$(awk '{print $1}' "${TMP}/checksum")
ACTUAL=$(sha256sum "${TMP}/schmutz-join" 2>/dev/null | awk '{print $1}')
[ -z "$ACTUAL" ] && ACTUAL=$(shasum -a 256 "${TMP}/schmutz-join" 2>/dev/null | awk '{print $1}')

if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "error: checksum mismatch" >&2
  exit 1
fi

chmod +x "${TMP}/schmutz-join"
exec "${TMP}/schmutz-join" "$BASE_URL"
