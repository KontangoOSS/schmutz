#!/bin/bash
# scripts/bao-bootstrap.sh — first-boot Bao initialization for compose stack.
#
# Run once after `docker compose up -d bao`:
#   ./scripts/bao-bootstrap.sh
#
# Writes credentials to .bao-credentials (gitignored) and patches .env
# with BAO_ROOT_TOKEN and BAO_UNSEAL_KEY so subsequent restarts auto-unseal.

set -euo pipefail
cd "$(dirname "$0")/.."

BAO_URL="http://localhost:8200"

wait_for_bao() {
  echo "Waiting for Bao to start..."
  for i in $(seq 1 20); do
    if curl -sf "$BAO_URL/v1/sys/health" >/dev/null 2>&1 || \
       curl -sf "$BAO_URL/v1/sys/seal-status" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "Bao did not start in time." >&2
  exit 1
}

# Need Bao on a published port for this script to work
# Temporarily add port mapping or run inside the container
echo "Checking Bao status..."
STATUS=$(docker compose exec -T bao bao status -format=json 2>/dev/null || echo '{}')
INITIALIZED=$(echo "$STATUS" | python3 -c "import json,sys; print(json.load(sys.stdin).get('initialized', False))" 2>/dev/null || echo "false")

if [ "$INITIALIZED" = "True" ]; then
  echo "Bao already initialized."
  SEALED=$(echo "$STATUS" | python3 -c "import json,sys; print(json.load(sys.stdin).get('sealed', True))" 2>/dev/null)
  if [ "$SEALED" = "True" ]; then
    UNSEAL_KEY=$(grep "BAO_UNSEAL_KEY=" .env 2>/dev/null | cut -d= -f2)
    if [ -n "$UNSEAL_KEY" ]; then
      echo "Auto-unsealing from .env..."
      docker compose exec -T bao bao operator unseal "$UNSEAL_KEY"
    else
      echo "Bao is sealed. Run: docker compose exec bao bao operator unseal <key>"
    fi
  else
    echo "Bao is already unsealed."
  fi
  exit 0
fi

echo "Initializing Bao (Raft, 1-of-1 for compose)..."
INIT=$(docker compose exec -T bao bao operator init \
  -key-shares=1 -key-threshold=1 -format=json 2>/dev/null)

UNSEAL_KEY=$(echo "$INIT" | python3 -c "import json,sys; print(json.load(sys.stdin)['unseal_keys_b64'][0])")
ROOT_TOKEN=$(echo "$INIT" | python3 -c "import json,sys; print(json.load(sys.stdin)['root_token'])")

# Save credentials
cat > .bao-credentials << EOF
# Bao credentials — keep safe, do not commit
UNSEAL_KEY=$UNSEAL_KEY
ROOT_TOKEN=$ROOT_TOKEN
EOF
chmod 600 .bao-credentials
echo "Credentials saved to .bao-credentials"

# Unseal
echo "Unsealing..."
docker compose exec -T bao bao operator unseal "$UNSEAL_KEY" | grep "Sealed"

# Mount KV v2 at secret/
echo "Mounting KV v2 at secret/..."
docker compose exec -T bao sh -c "BAO_TOKEN=$ROOT_TOKEN bao secrets enable -path=secret -version=2 kv" 2>/dev/null || \
  echo "(secret/ already mounted)"

# Patch .env
if [ -f .env ]; then
  # Remove old values if present
  sed -i '/^BAO_ROOT_TOKEN=/d' .env
  sed -i '/^BAO_UNSEAL_KEY=/d' .env
  echo "BAO_ROOT_TOKEN=$ROOT_TOKEN" >> .env
  echo "BAO_UNSEAL_KEY=$UNSEAL_KEY" >> .env
  echo ".env updated with BAO_ROOT_TOKEN and BAO_UNSEAL_KEY"
else
  echo ""
  echo "No .env found. Add these manually:"
  echo "  BAO_ROOT_TOKEN=$ROOT_TOKEN"
  echo "  BAO_UNSEAL_KEY=$UNSEAL_KEY"
fi

echo ""
echo "════════════════════════════════════"
echo " Bao ready (Raft, durable storage)"
echo "════════════════════════════════════"
echo " Root token: $ROOT_TOKEN"
echo " Unseal key: $UNSEAL_KEY"
echo ""
echo " Next: docker compose up -d"
echo "════════════════════════════════════"
