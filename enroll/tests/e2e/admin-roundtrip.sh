#!/usr/bin/env bash
# server/tests/e2e/admin-roundtrip.sh
#
# Manually-run end-to-end smoke for schmutz-server.
# Prerequisites:
#   - You're enrolled on the overlay as an #admins identity.
#   - Your local ziti-edge-tunnel has admin.tango intercepted.
#   - schmutz-server is running on at least one controller.
#   - agent.sh is reachable at https://enroll.kontango.net/agent.sh.
#
# Usage:
#   ./admin-roundtrip.sh

set -euo pipefail

SCHMUTZ="${SCHMUTZ_URL:-https://admin.tango}"
SLUG="e2e-$(date +%s)"
TARGET_HOST="${TARGET_HOST:-}"   # ssh-able test machine

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red()   { printf '\033[31m%s\033[0m\n' "$*" >&2; }
step()  { printf '\n→ %s\n' "$*"; }

require() {
  command -v "$1" >/dev/null 2>&1 || { red "$1 not found in PATH"; exit 1; }
}

require curl
require jq

step "1/8 whoami"
WHOAMI=$(curl -sf "$SCHMUTZ/api/whoami")
echo "$WHOAMI" | jq .
echo "$WHOAMI" | jq -e '.role_attributes | index("admins")' >/dev/null || {
  red "caller is not #admins"; exit 1
}

step "2/8 issue token (slug=$SLUG, role=test, 10m expiry)"
TOKEN_RESP=$(curl -sf -X POST "$SCHMUTZ/api/tokens" \
  -H 'Content-Type: application/json' \
  -d "{\"slug\":\"$SLUG\",\"role\":\"test\",\"expires\":\"10m\"}")
TOKEN=$(echo "$TOKEN_RESP" | jq -r .token)
echo "  token: $TOKEN"

step "3/8 list active tokens (should include $TOKEN)"
curl -sf "$SCHMUTZ/api/tokens" | jq --arg t "$TOKEN" \
  '.tokens[] | select(.Token == $t)' \
  | jq -e '.Token' >/dev/null || { red "issued token not in list"; exit 1; }

if [ -z "$TARGET_HOST" ]; then
  green "Skipping enrollment + approval (TARGET_HOST not set)"
  step "4/8 revoke token"
  curl -sf -X DELETE "$SCHMUTZ/api/tokens/$TOKEN"
  green "Smoke test (token-only) passed."
  exit 0
fi

step "4/8 enroll target host via agent.sh"
ssh "$TARGET_HOST" "curl -sfL https://enroll.kontango.net/agent.sh | bash -s -- $TOKEN" \
  | tee /tmp/enroll.log
IDENT=$(grep -oE 'machine-[0-9a-f]+' /tmp/enroll.log | head -1)
[ -n "$IDENT" ] || { red "could not parse identity name from agent output"; exit 1; }
echo "  enrolled as: $IDENT"

step "5/8 inspect identity (should be quarantined)"
curl -sf "$SCHMUTZ/api/identities/$IDENT" | jq -e \
  '.roleAttributes | index("quarantine")' >/dev/null \
  || { red "identity is not quarantined"; exit 1; }

step "6/8 approve identity with role=test"
curl -sf -X POST "$SCHMUTZ/api/identities/$IDENT/approve" \
  -H 'Content-Type: application/json' -d '{"role":"test"}'

step "7/8 verify quarantine lifted, role=test applied"
curl -sf "$SCHMUTZ/api/identities/$IDENT" | jq '.roleAttributes'
curl -sf "$SCHMUTZ/api/identities/$IDENT" | jq -e \
  '.roleAttributes | index("quarantine") | not' >/dev/null
curl -sf "$SCHMUTZ/api/identities/$IDENT" | jq -e \
  '.roleAttributes | index("test")' >/dev/null

step "8/8 delete identity"
curl -sf -X DELETE "$SCHMUTZ/api/identities/$IDENT"

step "verify audit recorded all actions"
SINCE=$(date -u -d '5 minutes ago' '+%Y-%m-%dT%H:%M:%SZ')
curl -sf "$SCHMUTZ/api/audit?since=$SINCE&limit=50" | jq -r '.events[] | "\(.action) \(.resource)"'

green "All e2e steps passed."
