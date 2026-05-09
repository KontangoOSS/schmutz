#!/usr/bin/env bash
# =============================================================================
# disconnect-ziti.sh — Remove Ziti identity from a machine
#
# Handles both standard (ziti-edge-tunnel) and tango (tango-tunnel) installs.
# Stops the tunnel, removes identity files, leaves binaries in place.
#
# Usage:
#   bash disconnect-ziti.sh
#   ssh root@<ip> 'bash -s' < disconnect-ziti.sh
#   pct exec <vmid> -- bash -s < disconnect-ziti.sh
# =============================================================================
set -euo pipefail

echo "=== Ziti Disconnect ==="
echo "Host: $(hostname)"
echo "Date: $(date -Iseconds)"
echo ""

STOPPED=0

# Stop tango-tunnel (Schmutz-enrolled machines)
if systemctl is-active --quiet "tango-tunnel" 2>/dev/null; then
    echo "Stopping tango-tunnel..."
    systemctl stop tango-tunnel
    systemctl disable tango-tunnel
    STOPPED=$((STOPPED + 1))
    echo "  stopped + disabled."
fi

# Stop ziti-edge-tunnel (standard install)
if systemctl is-active --quiet "ziti-edge-tunnel" 2>/dev/null; then
    echo "Stopping ziti-edge-tunnel..."
    systemctl stop ziti-edge-tunnel
    systemctl disable ziti-edge-tunnel
    STOPPED=$((STOPPED + 1))
    echo "  stopped + disabled."
fi

if [ "$STOPPED" -eq 0 ]; then
    # Check for running processes anyway
    if pgrep -f "ziti.*tunnel" > /dev/null 2>&1; then
        echo "Killing ziti tunnel processes..."
        pkill -f "ziti.*tunnel" 2>/dev/null || true
        sleep 1
        echo "  killed."
    else
        echo "No Ziti tunnel service or process found."
    fi
fi

# Remove identity files from all known locations
REMOVED=0
for dir in /opt/tango /opt/ziti-client/identities /opt/ziti/identities /opt/ziti-identity /etc/ziti; do
    if [ -d "$dir" ]; then
        for f in "$dir"/*.json "$dir"/identity.json; do
            [ -f "$f" ] || continue
            BASENAME=$(basename "$f")
            # Keep config.json
            [ "$BASENAME" = "config.json" ] && continue
            echo "  removing: $f"
            rm -f "$f"
            REMOVED=$((REMOVED + 1))
        done
    fi
done

# Remove .bak, .disabled, .old files
find /opt/tango /opt/ziti-client /opt/ziti /opt/ziti-identity -name "*.bak" -o -name "*.disabled" -o -name "*.old" 2>/dev/null | while read -r f; do
    echo "  removing: $f"
    rm -f "$f"
done

# Clear cached state
rm -rf /tmp/ziti-edge-tunnel-* 2>/dev/null
rm -rf /var/run/ziti-edge-tunnel* 2>/dev/null

echo ""
echo "Removed $REMOVED identity files."
echo ""
echo "=== Done ==="
echo "Machine is disconnected from Ziti."
echo "To re-enroll: curl -sf https://join.kontango.net/install | sh"
