#!/usr/bin/env bash
# =============================================================================
# bootstrap-profiles.sh — TangoKore seed data
#
# Creates the foundational profiles, policies, and role definitions on a
# fresh Ziti controller. Run ONCE on the init node. Replicates via Raft.
#
# Internal network: .tango (self-signed Ziti PKI)
# External network: *.kontango.io (LE via Caddy)
# Config prefix:    kore-*
#
# Usage:
#   export ZITI_CTRL_URL=https://ctrl-1.tango:1280
#   export ZITI_ADMIN_PASS=<password>
#   bash bootstrap-profiles.sh
# =============================================================================
set -euo pipefail

PLUGIN="${PLUGIN_PATH:-/home/leonardo/git/plugins/plugins-ziti-go/build/plugins-ziti}"
CTRL="${ZITI_CTRL_URL:?Set ZITI_CTRL_URL}"
PASS="${ZITI_ADMIN_PASS:?Set ZITI_ADMIN_PASS}"

p() {
  env PLUGIN_CTRL_URL="$CTRL" PLUGIN_ADMIN_PASSWORD="$PASS" PLUGIN_SKIP_VERIFY=true \
    PLUGIN_ACTION="$1" "${@:2}" "$PLUGIN" 2>/dev/null
}

echo "============================================"
echo "  TangoKore — Bootstrap Seed Data"
echo "============================================"
echo "  Controller: $CTRL"
echo "  Network:    .tango (internal)"
echo ""

# -- Step 1: Verify connectivity ---------------------------------------------
echo "[1/6] Checking controller..."
env PLUGIN_CTRL_URL="$CTRL" PLUGIN_SKIP_VERIFY=true PLUGIN_ACTION=health "$PLUGIN" 2>/dev/null | \
  python3 -c "import sys,json; print(f'  Connected: v{json.load(sys.stdin)[\"version\"]}')" 2>/dev/null

# -- Step 2: Create kore-profiles identity ------------------------------------
echo ""
echo "[2/6] Creating kore-profiles identity..."
p identity-create PLUGIN_IDENTITY_NAME=kore-profiles PLUGIN_IDENTITY_TYPE=Service \
  > /dev/null && echo "  Created." || echo "  Already exists."

# -- Step 3: Service Policies ------------------------------------------------
echo ""
echo "[3/6] Creating service policies..."

# Bind policies (who hosts what)
declare -A BIND=(
  ["kore-quarantine-bind"]="Bind|#kore-join|#kore-quarantine-services"
  ["kore-app-bind"]="Bind|#kore-standard|#kore-app"
  ["kore-ssh-bind"]="Bind|#kore-standard|#kore-ssh"
  ["kore-infra-bind"]="Bind|#kore-infra|#kore-infra-services"
  ["kore-web-bind"]="Bind|#kore-web|#kore-web-services"
  ["kore-home-bind"]="Bind|#kore-home-router|#kore-home-services"
  ["kore-k8s-bind"]="Bind|#kore-k8s|#kore-k8s-services"
)

# Dial policies (who can reach what)
declare -A DIAL=(
  ["kore-quarantine-dial"]="Dial|#kore-admin|#kore-quarantine-services"
  ["kore-app-dial"]="Dial|#kore-admin,#kore-web-client|#kore-app"
  ["kore-ssh-dial"]="Dial|#kore-ssh-client|#kore-ssh"
  ["kore-infra-dial"]="Dial|#kore-admin|#kore-infra-services"
  ["kore-web-dial"]="Dial|#kore-web-client|#kore-web-services"
  ["kore-k8s-dial"]="Dial|#kore-admin,#kore-web-client|#kore-k8s-services"
  ["kore-home-dial"]="Dial|#kore-admin|#kore-home-services"
)

for name in "${!BIND[@]}"; do
  IFS='|' read -r type identity_roles service_roles <<< "${BIND[$name]}"
  p service-policy-create \
    PLUGIN_POLICY_NAME="$name" PLUGIN_POLICY_TYPE="$type" \
    PLUGIN_IDENTITY_ROLES="$identity_roles" PLUGIN_SERVICE_ROLES="$service_roles" \
    > /dev/null && echo "  $name" || echo "  $name (exists)"
done

for name in "${!DIAL[@]}"; do
  IFS='|' read -r type identity_roles service_roles <<< "${DIAL[$name]}"
  p service-policy-create \
    PLUGIN_POLICY_NAME="$name" PLUGIN_POLICY_TYPE="$type" \
    PLUGIN_IDENTITY_ROLES="$identity_roles" PLUGIN_SERVICE_ROLES="$service_roles" \
    > /dev/null && echo "  $name" || echo "  $name (exists)"
done

# -- Step 4: Edge Router Policies --------------------------------------------
echo ""
echo "[4/6] Creating edge-router-policies..."

declare -A ERP=(
  ["kore-join-erp"]="#kore-join|#kore-public-edge"
  ["kore-standard-erp"]="#kore-standard|#kore-public-edge,#kore-lan"
  ["kore-infra-erp"]="#kore-infra|#all"
  ["kore-admin-erp"]="#kore-admin|#all"
  ["kore-workstation-erp"]="#kore-workstation|#all"
  ["kore-proxy-erp"]="#kore-proxy|#all"
  ["kore-k8s-erp"]="#kore-k8s|#all"
)

for name in "${!ERP[@]}"; do
  IFS='|' read -r identity_roles edge_router_roles <<< "${ERP[$name]}"
  p erp-create \
    PLUGIN_POLICY_NAME="$name" \
    PLUGIN_IDENTITY_ROLES="$identity_roles" PLUGIN_EDGE_ROUTER_ROLES="$edge_router_roles" \
    > /dev/null && echo "  $name" || echo "  $name (exists)"
done

# -- Step 5: Service Edge Router Policy --------------------------------------
echo ""
echo "[5/6] Creating service-edge-router-policy..."
p serp-create \
  PLUGIN_POLICY_NAME="kore-all-services-all-routers" \
  PLUGIN_SERVICE_ROLES="#all" PLUGIN_EDGE_ROUTER_ROLES="#all" \
  > /dev/null && echo "  kore-all-services-all-routers" || echo "  kore-all-services-all-routers (exists)"

# -- Step 6: Summary ---------------------------------------------------------
echo ""
echo "============================================"
echo "  TangoKore seed data complete"
echo "============================================"
echo ""
echo "  Profiles identity:   kore-profiles"
echo "  Service policies:    ${#BIND[@]} bind + ${#DIAL[@]} dial"
echo "  Edge router policies: ${#ERP[@]}"
echo "  Service ERP:         1"
echo ""
echo "  Profiles (create via API or ziti-dash):"
echo "    kore-join        → role: kore-join"
echo "    kore-standard    → role: kore-standard"
echo "    kore-infra       → role: kore-infra"
echo "    kore-workstation → role: kore-workstation"
echo ""
echo "  Identity roles:       kore-join, kore-standard, kore-infra,"
echo "                        kore-workstation, kore-admin, kore-proxy,"
echo "                        kore-tunnel, kore-ssh-client, kore-web-client,"
echo "                        kore-home-router, kore-k8s, kore-web"
echo ""
echo "  Service roles:        kore-quarantine-services, kore-app, kore-ssh,"
echo "                        kore-infra-services, kore-web-services,"
echo "                        kore-home-services, kore-k8s-services"
echo ""
echo "  Router roles:         kore-edge, kore-public-edge, kore-lan"
echo ""
echo "  Internal network:     .tango"
echo "  External certs:       *.kontango.io (via Caddy)"
echo ""
echo "  Next: create profiles in ziti-dash or via API"
echo "        enroll machines with: curl -sf https://join.kontango.net/install | sh"
