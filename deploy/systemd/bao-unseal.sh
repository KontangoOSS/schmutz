#!/usr/bin/env bash
# /usr/local/sbin/bao-unseal.sh — invoked by bao-unseal.service at boot.
# Reads unseal keys from /etc/kontango/bao-unseal.keys (one per line) and
# applies them to the local Bao at 127.0.0.1:8200.
#
# v1 trade-off: the keys file lives on disk alongside Bao data. Anyone with
# root on this controller has both. Acceptable for v1 to bootstrap. Hardening
# to KMS-based auto-unseal or manual unseal is a v2/v3 item.

set -euo pipefail

KEYS_FILE="/etc/kontango/bao-unseal.keys"
BAO_ADDR="http://127.0.0.1:8200"

[[ -f "$KEYS_FILE" ]] || { echo "no keys file at $KEYS_FILE"; exit 1; }

# Wait up to 30s for bao API to be reachable
for i in $(seq 1 30); do
  if curl -sf -o /dev/null "${BAO_ADDR}/v1/sys/health?sealedcode=200" 2>/dev/null; then
    break
  fi
  sleep 1
done

# Check seal status. /sys/health returns 503 when sealed (or 200 with our query
# arg above). Use /sys/seal-status for precise state.
SEAL_STATUS="$(curl -sf "${BAO_ADDR}/v1/sys/seal-status" || true)"
SEALED="$(echo "$SEAL_STATUS" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("sealed", True))' 2>/dev/null || echo true)"

if [[ "$SEALED" != "True" ]]; then
  echo "bao already unsealed"
  exit 0
fi

# Apply each unseal key
while IFS= read -r KEY; do
  [[ -z "$KEY" ]] && continue
  RESP="$(curl -sf -X POST -H 'Content-Type: application/json' \
    -d "{\"key\":\"$KEY\"}" "${BAO_ADDR}/v1/sys/unseal" || true)"
  PROGRESS="$(echo "$RESP" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("progress",0), d.get("threshold",0), d.get("sealed",True))' 2>/dev/null || echo "?")"
  echo "  unseal step: $PROGRESS"
done < "$KEYS_FILE"

# Final check
SEAL_STATUS="$(curl -sf "${BAO_ADDR}/v1/sys/seal-status")"
SEALED="$(echo "$SEAL_STATUS" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("sealed", True))')"

if [[ "$SEALED" == "True" ]]; then
  echo "ERROR: still sealed after applying keys" >&2
  exit 1
fi

echo "bao unsealed"
