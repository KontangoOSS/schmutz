#!/usr/bin/env bash
# =============================================================================
# disconnect-all-lxcs.sh — Remove Ziti from all enrolled LXCs on hank
#
# Runs disconnect-ziti.sh on each LXC via SSH to hank + pct exec.
# Does NOT touch: controllers (ctrl-1/2/3), routers, caddy, ctrl-dev,
# or your workstation.
#
# Usage:
#   bash disconnect-all-lxcs.sh              # dry run (shows what would happen)
#   bash disconnect-all-lxcs.sh --execute    # actually do it
# =============================================================================
set -euo pipefail

HANK="10.11.30.27"
HANK_USER="root"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DISCONNECT_SCRIPT="$SCRIPT_DIR/disconnect-ziti.sh"
DRY_RUN=true

if [[ "${1:-}" == "--execute" ]]; then
    DRY_RUN=false
fi

# LXC VMIDs and their hostnames (from the Bao identity data)
# Update this list based on your actual Proxmox VMID assignments
declare -A LXCS=(
    # [VMID]="hostname (nickname)"
    # Fill these in from: ssh root@10.11.30.27 'pct list'
)

echo "=== Disconnect All LXCs ==="
echo "Target: hank ($HANK)"
echo "Mode: $(if $DRY_RUN; then echo 'DRY RUN'; else echo 'EXECUTE'; fi)"
echo ""

# Step 1: Get the LXC list from hank
echo "Fetching LXC list from hank..."
LXC_LIST=$(ssh -o StrictHostKeyChecking=no "$HANK_USER@$HANK" 'pct list' 2>/dev/null | tail -n +2)

if [ -z "$LXC_LIST" ]; then
    echo "ERROR: Could not get LXC list from hank"
    exit 1
fi

echo "$LXC_LIST"
echo ""

# Step 2: For each running LXC, run the disconnect script
echo "=== Processing LXCs ==="
TOTAL=0
DISCONNECTED=0

while IFS= read -r line; do
    VMID=$(echo "$line" | awk '{print $1}')
    STATUS=$(echo "$line" | awk '{print $2}')
    NAME=$(echo "$line" | awk '{print $NF}')

    # Skip stopped LXCs
    if [ "$STATUS" != "running" ]; then
        echo "  SKIP $VMID ($NAME) — $STATUS"
        continue
    fi

    # Skip infrastructure LXCs that should NOT be disconnected
    case "$NAME" in
        ctrl-dev|schmutz*|caddy*|ziti*)
            echo "  SKIP $VMID ($NAME) — infrastructure"
            continue
            ;;
    esac

    TOTAL=$((TOTAL + 1))
    echo ""
    echo "  [$TOTAL] VMID=$VMID NAME=$NAME STATUS=$STATUS"

    if $DRY_RUN; then
        echo "       Would run: pct exec $VMID -- bash -s < disconnect-ziti.sh"
    else
        echo "       Disconnecting..."
        ssh "$HANK_USER@$HANK" "pct exec $VMID -- bash -s" < "$DISCONNECT_SCRIPT" 2>&1 | sed 's/^/       /'
        DISCONNECTED=$((DISCONNECTED + 1))
        echo "       Done."
        sleep 1
    fi
done <<< "$LXC_LIST"

echo ""
echo "=== Summary ==="
echo "Total LXCs processed: $TOTAL"
if $DRY_RUN; then
    echo "Mode: DRY RUN — nothing was changed"
    echo "Run with --execute to disconnect"
else
    echo "Disconnected: $DISCONNECTED"
fi
