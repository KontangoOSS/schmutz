#!/usr/bin/env bash
# =============================================================================
# discover.sh — Repo & app discovery for CI pipelines
# =============================================================================
# Gathers all metadata about the current repo/app from:
#   1. Git / Woodpecker CI environment variables
#   2. Local config/konfig.json (if present)
#   3. Konfig API (https://konfig.konoss.org)
#
# Outputs: .ci-discover.env (sourceable by subsequent steps)
#
# Usage in .woodpecker.yml:
#   - name: discover
#     image: alpine:3.20
#     commands:
#       - apk add --no-cache curl jq bash
#       - bash scripts/ci/discover.sh
#
# All output variables are prefixed with CI_APP_ to avoid collisions.
# =============================================================================

set -euo pipefail

OUTPUT_FILE="${CI_DISCOVER_OUTPUT:-.ci-discover.env}"
KONFIG_API="${KONFIG_URL:-https://konfig.konoss.org}/api/v1"

# ── Helpers ──────────────────────────────────────────────────────────────────

_set() {
  local key="$1" val="$2"
  printf '%s=%s\n' "$key" "$val" >> "$OUTPUT_FILE"
  printf '  %-28s %s\n' "$key" "$val"
}

_set_quoted() {
  local key="$1" val="$2"
  printf '%s="%s"\n' "$key" "$val" >> "$OUTPUT_FILE"
  printf '  %-28s %s\n' "$key" "$val"
}

# ── Init ─────────────────────────────────────────────────────────────────────

: > "$OUTPUT_FILE"

echo "══════════════════════════════════════════"
echo "  Discover: gathering repo metadata"
echo "══════════════════════════════════════════"

# ── 1. Git / CI context ─────────────────────────────────────────────────────

echo ""
echo "── Git context ──"

_set "CI_APP_REPO_NAME"    "${CI_REPO_NAME:-$(basename "$(git rev-parse --show-toplevel 2>/dev/null || echo unknown)")}"
_set "CI_APP_REPO_OWNER"   "${CI_REPO_OWNER:-$(git remote get-url origin 2>/dev/null | sed 's|.*[:/]\([^/]*\)/[^/]*\.git$|\1|' || echo unknown)}"
_set "CI_APP_BRANCH"       "${CI_COMMIT_BRANCH:-$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)}"
_set "CI_APP_COMMIT_SHA"   "${CI_COMMIT_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
_set "CI_APP_COMMIT_SHORT" "${CI_COMMIT_SHA:+${CI_COMMIT_SHA:0:8}}"
_set "CI_APP_COMMIT_MSG"   "${CI_COMMIT_MESSAGE:-$(git log -1 --format=%s 2>/dev/null || echo unknown)}"
_set "CI_APP_REPO_URL"     "${CI_REPO_LINK:-$(git remote get-url origin 2>/dev/null || echo unknown)}"
_set "CI_APP_EVENT"        "${CI_PIPELINE_EVENT:-manual}"
_set "CI_APP_PIPELINE_NUM" "${CI_PIPELINE_NUMBER:-0}"

APP_NAME="${CI_REPO_NAME:-$(basename "$(git rev-parse --show-toplevel 2>/dev/null || echo unknown)")}"

# ── 2. Local konfig.json ────────────────────────────────────────────────────

echo ""
echo "── Local config ──"

KONFIG_FILE=""
for candidate in config/konfig.json deploy/konfig.json konfig.json; do
  if [ -f "$candidate" ]; then
    KONFIG_FILE="$candidate"
    break
  fi
done

if [ -n "$KONFIG_FILE" ]; then
  echo "  Found: $KONFIG_FILE"

  _set "CI_APP_LOCAL_NAME"        "$(jq -r '.name // empty' "$KONFIG_FILE")"
  _set "CI_APP_LOCAL_ORG"         "$(jq -r '.org // empty' "$KONFIG_FILE")"
  _set "CI_APP_LOCAL_VERSION"     "$(jq -r '.version // empty' "$KONFIG_FILE")"
  _set "CI_APP_LOCAL_REGISTRY"    "$(jq -r '.registry // empty' "$KONFIG_FILE")"
  _set "CI_APP_LOCAL_DESCRIPTION" "$(jq -r '.description // empty' "$KONFIG_FILE")"

  # Secrets config
  _set "CI_APP_SECRETS_PROVIDER"  "$(jq -r '.secrets.provider // empty' "$KONFIG_FILE")"
  _set "CI_APP_SECRETS_PATH"     "$(jq -r '.secrets.path // empty' "$KONFIG_FILE")"

  # Deploy target
  _set "CI_APP_DEPLOY_TARGET"     "$(jq -r '.deploy.target // empty' "$KONFIG_FILE")"

  # Network config (if present)
  _set "CI_APP_DOMAIN"            "$(jq -r '.network.domain // empty' "$KONFIG_FILE")"
  _set "CI_APP_ZITI_SERVICE"      "$(jq -r '.network.ziti_service // empty' "$KONFIG_FILE")"

  # Ticketarr config
  _set "CI_APP_TICKET_PROJECT"    "$(jq -r '.ticketarr.project_key // empty' "$KONFIG_FILE")"

  # Use local name if it's a real value (not a placeholder)
  LOCAL_NAME="$(jq -r '.name // empty' "$KONFIG_FILE")"
  if [ -n "$LOCAL_NAME" ] && [ "$LOCAL_NAME" != "APPNAME" ]; then
    APP_NAME="$LOCAL_NAME"
  fi
else
  echo "  No konfig.json found (checked config/, deploy/, root)"
fi

# ── 3. Konfig API ───────────────────────────────────────────────────────────

echo ""
echo "── Konfig API ──"

API_JSON=""
if curl -sk --connect-timeout 5 "$KONFIG_API/applications?page_size=1" >/dev/null 2>&1; then
  API_JSON=$(curl -sk "$KONFIG_API/applications?page_size=200" \
    | jq -r --arg name "$APP_NAME" \
      '.data[] | select(.name | ascii_downcase == ($name | ascii_downcase))' 2>/dev/null || true)
fi

if [ -n "$API_JSON" ]; then
  echo "  Found '$APP_NAME' in Konfig API"

  _set "CI_APP_UUID"              "$(echo "$API_JSON" | jq -r '.uuid // empty')"
  _set "CI_APP_VMID"              "$(echo "$API_JSON" | jq -r '.vmid // empty')"
  _set "CI_APP_CATEGORY"          "$(echo "$API_JSON" | jq -r '.category // empty')"
  _set "CI_APP_STATUS"            "$(echo "$API_JSON" | jq -r '.status // empty')"
  _set "CI_APP_DEFAULT_PORT"      "$(echo "$API_JSON" | jq -r '.default_port // empty')"
  _set "CI_APP_DOCKER_IMAGE"      "$(echo "$API_JSON" | jq -r '.docker_image // empty')"

  # Suggested tier sizing
  _set "CI_APP_CORES"             "$(echo "$API_JSON" | jq -r \
    '(.configurations[]? | select(.tier == "suggested") | .instance_size.cores) // 2')"
  _set "CI_APP_MEMORY"            "$(echo "$API_JSON" | jq -r \
    '(.configurations[]? | select(.tier == "suggested") | .instance_size.memory_mb) // 2048')"
  _set "CI_APP_DISK"              "$(echo "$API_JSON" | jq -r \
    '(.configurations[]? | select(.tier == "suggested") | .instance_size.disk_gb) // 20')"

  # Links
  _set "CI_APP_REPO_LINK"         "$(echo "$API_JSON" | jq -r \
    '(.links[]? | select(.link_type == "repository") | .path) // empty')"
  _set "CI_APP_BOARD_LINK"        "$(echo "$API_JSON" | jq -r \
    '(.links[]? | select(.link_type == "board") | .path) // empty')"
  _set "CI_APP_DOCS_LINK"         "$(echo "$API_JSON" | jq -r \
    '(.links[]? | select(.link_type == "documentation") | .path) // empty')"

  # All configurations as JSON (for advanced use)
  _set_quoted "CI_APP_CONFIGURATIONS" "$(echo "$API_JSON" | jq -c '.configurations // []')"
else
  echo "  App '$APP_NAME' not found in Konfig API (or API unreachable)"
  _set "CI_APP_UUID"     ""
  _set "CI_APP_VMID"     ""
  _set "CI_APP_CORES"    "2"
  _set "CI_APP_MEMORY"   "2048"
  _set "CI_APP_DISK"     "20"
fi

# ── 4. Derived values ───────────────────────────────────────────────────────

echo ""
echo "── Derived ──"

# Container hostname (sanitized: lowercase, alphanumeric + hyphens, max 63 chars)
HOSTNAME=$(echo "$APP_NAME" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9-]/-/g' | cut -c1-63)
_set "CI_APP_HOSTNAME"      "$HOSTNAME"

# Registry image tag
REGISTRY="$(grep '^CI_APP_LOCAL_REGISTRY=' "$OUTPUT_FILE" 2>/dev/null | cut -d= -f2 || true)"
if [ -n "$REGISTRY" ] && [ "$REGISTRY" != "APPORG" ]; then
  BRANCH_VAL="$(grep '^CI_APP_BRANCH=' "$OUTPUT_FILE" | cut -d= -f2)"
  if [ "$BRANCH_VAL" = "main" ]; then
    _set "CI_APP_IMAGE_TAG" "${REGISTRY}:latest"
  else
    _set "CI_APP_IMAGE_TAG" "${REGISTRY}:${BRANCH_VAL}"
  fi
fi

# Phase secrets path (from local config or convention)
SECRETS_PATH="$(grep '^CI_APP_SECRETS_PATH=' "$OUTPUT_FILE" 2>/dev/null | cut -d= -f2 || true)"
if [ -z "$SECRETS_PATH" ] || [ "$SECRETS_PATH" = "/APPNAME/" ]; then
  SECRETS_PATH="/${APP_NAME}/"
  _set "CI_APP_SECRETS_PATH" "$SECRETS_PATH"
fi

# ── Summary ──────────────────────────────────────────────────────────────────

echo ""
echo "══════════════════════════════════════════"
echo "  Discovery complete"
echo "  Output: $OUTPUT_FILE"
echo "  Variables: $(wc -l < "$OUTPUT_FILE") exported"
echo "══════════════════════════════════════════"
