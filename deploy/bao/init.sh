#!/bin/sh
# deploy/bao/init.sh — run once after first `docker compose up` to initialize
# the Bao Raft instance and write credentials to .bao-init.
#
# Usage:
#   docker compose exec bao sh /etc/bao/init.sh
#
# After running this script:
#   - Unseal keys and root token are written to .bao-init in this directory
#   - Bao is unsealed and ready
#   - Copy the root token into your .env as BAO_ROOT_TOKEN

set -e

BAO_ADDR="${BAO_ADDR:-http://localhost:8200}"
OUT="/bao/data/.bao-init"

# Already initialized?
if bao status 2>/dev/null | grep -q "Initialized.*true"; then
  echo "Bao already initialized."
  if bao status 2>/dev/null | grep -q "Sealed.*true"; then
    echo "Bao is sealed. Unseal with keys from ${OUT}"
    echo "Run: bao operator unseal <key>"
  else
    echo "Bao is unsealed and ready."
  fi
  exit 0
fi

echo "Initializing Bao (1 key share, threshold 1 for compose)..."
INIT=$(bao operator init -key-shares=1 -key-threshold=1 -format=json)

UNSEAL_KEY=$(echo "$INIT" | grep -o '"unseal_keys_b64":\["[^"]*"' | cut -d'"' -f4)
ROOT_TOKEN=$(echo "$INIT" | grep -o '"root_token":"[^"]*"' | cut -d'"' -f4)

echo "$INIT" > "$OUT"
chmod 600 "$OUT"

echo ""
echo "Unsealing..."
bao operator unseal -reset 2>/dev/null || true
echo "$UNSEAL_KEY" | bao operator unseal -

echo ""
echo "═══════════════════════════════════════"
echo " Bao initialized and unsealed"
echo "═══════════════════════════════════════"
echo ""
echo " Unseal key:  $UNSEAL_KEY"
echo " Root token:  $ROOT_TOKEN"
echo ""
echo " Saved to: ${OUT}"
echo ""
echo " Add to your .env:"
echo "   BAO_ROOT_TOKEN=$ROOT_TOKEN"
echo "═══════════════════════════════════════"
