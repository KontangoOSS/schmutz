#!/usr/bin/env bash
# =============================================================================
# rebuild-controller.sh — Wipe and rebuild a Ziti controller from scratch
#
# Preserves: Bao, Caddy, OS config
# Destroys:  Ziti PKI, Raft database, router enrollment, tunnel identities
#
# Usage:
#   # On the INIT node (ctrl-1):
#   bash rebuild-controller.sh --init --admin-pass <password>
#
#   # On joining nodes (ctrl-2, ctrl-3) — AFTER copying root CA from ctrl-1:
#   bash rebuild-controller.sh
#
# Prerequisites:
#   - Ziti binary already installed at /opt/zstack/bin/ziti
#   - /etc/hosts has all controller entries
# =============================================================================
set -euo pipefail

ZITI_BIN="/opt/zstack/bin/ziti"
ZITI_BASE="/opt/zstack/ziti"
TRUST_DOMAIN="kontango.io"

# Auto-detect node name from hostname or existing config
if [[ -f "$ZITI_BASE"/ctrl-1/ctrl.yaml ]]; then
    NODE_NAME="ctrl-1"
elif [[ -f "$ZITI_BASE"/ctrl-2/ctrl.yaml ]]; then
    NODE_NAME="ctrl-2"
elif [[ -f "$ZITI_BASE"/ctrl-3/ctrl.yaml ]]; then
    NODE_NAME="ctrl-3"
else
    echo "ERROR: Cannot detect node name. No ctrl.yaml found."
    exit 1
fi

INIT=false
ADMIN_PASSWORD=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --init)       INIT=true; shift ;;
        --admin-pass) ADMIN_PASSWORD="$2"; shift 2 ;;
        --name)       NODE_NAME="$2"; shift 2 ;;
        *) echo "Unknown: $1"; exit 1 ;;
    esac
done

PKI_DIR="$ZITI_BASE/pki"
CTRL_DIR="$ZITI_BASE/$NODE_NAME"
INTER_DIR="$PKI_DIR/intermediate-${NODE_NAME}"

# Read IP from existing config
PUBLIC_IP=$(grep -oP 'advertiseAddress: tls:\K[^:]+' "$CTRL_DIR/ctrl.yaml" 2>/dev/null | head -1)
if [[ -z "$PUBLIC_IP" ]]; then
    PUBLIC_IP=$(hostname -I | awk '{print $1}')
fi
CTRL_ADDR="${NODE_NAME}.tango"

echo "============================================"
echo "  Ziti Controller Rebuild"
echo "============================================"
echo "  Node:     $NODE_NAME"
echo "  Address:  $CTRL_ADDR:1280"
echo "  IP:       $PUBLIC_IP"
echo "  Mode:     $(if $INIT; then echo 'INIT (new cluster)'; else echo 'JOIN (after root CA copy)'; fi)"
echo "  Bao:      $(systemctl is-active openbao 2>/dev/null || echo 'not running')"
echo "  Caddy:    $(systemctl is-active caddy 2>/dev/null || echo 'not running')"
echo "============================================"
echo ""
echo "This will DESTROY all Ziti state on this node."
echo "Bao and Caddy will NOT be affected."
echo ""
read -rp "Continue? [y/N] " confirm
[[ "$confirm" =~ ^[Yy]$ ]] || exit 0

# === Step 1: Stop Ziti services ===
echo ""
echo "[1/6] Stopping Ziti services..."
systemctl stop ziti-tunnel 2>/dev/null || true
systemctl stop ziti-router 2>/dev/null || true
systemctl stop ziti-controller 2>/dev/null || true
sleep 2
echo "  Stopped."

# === Step 2: Wipe Raft data ===
echo "[2/6] Wiping Raft database..."
rm -rf "$CTRL_DIR/raft"
mkdir -p "$CTRL_DIR/raft"
echo "  Wiped."

# === Step 3: Wipe old identity files ===
echo "[3/6] Removing old identity files..."
rm -f "$ZITI_BASE"/*.json
rm -f "$ZITI_BASE"/router/*.json
echo "  Cleaned."

# === Step 4: Regenerate PKI ===
echo "[4/6] Regenerating PKI..."

if $INIT; then
    echo "  Creating NEW root CA (trust domain: spiffe://$TRUST_DOMAIN)..."
    rm -rf "$PKI_DIR"
    mkdir -p "$PKI_DIR"

    "$ZITI_BIN" pki create ca \
        --pki-root "$PKI_DIR" \
        --ca-name "root-ca" \
        --ca-file "root-ca" \
        --trust-domain "$TRUST_DOMAIN" \
        --pki-organization "Kontango" \
        --pki-organizational-unit "Infrastructure" \
        --pki-country "US"
else
    if [[ ! -f "$PKI_DIR/root-ca/certs/root-ca.cert" ]]; then
        echo "  ERROR: Root CA not found. Copy from init node first:"
        echo "    scp -r root@<ctrl-1>:$PKI_DIR/root-ca/ $PKI_DIR/root-ca/"
        exit 1
    fi
    echo "  Using existing root CA."
    # Remove old intermediates for this node
    rm -rf "$INTER_DIR"
fi

# Create intermediate CA
echo "  Creating intermediate CA for $NODE_NAME..."
"$ZITI_BIN" pki create intermediate \
    --pki-root "$PKI_DIR" \
    --ca-name "root-ca" \
    --intermediate-name "intermediate-${NODE_NAME}" \
    --intermediate-file "intermediate-${NODE_NAME}" \
    --pki-organization "Kontango" \
    --pki-organizational-unit "Infrastructure"

# Server cert
echo "  Creating server certificate..."
"$ZITI_BIN" pki create server \
    --pki-root "$PKI_DIR" \
    --ca-name "intermediate-${NODE_NAME}" \
    --server-name "${NODE_NAME}" \
    --server-file "server" \
    --dns "${CTRL_ADDR},localhost" \
    --ip "${PUBLIC_IP},127.0.0.1" \
    --spiffe-id "spiffe://${TRUST_DOMAIN}/controller/${NODE_NAME}"

# Client cert
echo "  Creating client certificate..."
"$ZITI_BIN" pki create client \
    --pki-root "$PKI_DIR" \
    --ca-name "intermediate-${NODE_NAME}" \
    --client-name "${NODE_NAME}" \
    --client-file "client" \
    --spiffe-id "spiffe://${TRUST_DOMAIN}/controller/${NODE_NAME}"

# CA bundle
echo "  Building CA bundle..."
cat "$PKI_DIR/root-ca/certs/root-ca.cert" > "$PKI_DIR/ca-bundle.pem"
for f in "$PKI_DIR"/intermediate-*/certs/intermediate-*.cert; do
    [[ -f "$f" ]] && cat "$f" >> "$PKI_DIR/ca-bundle.pem"
done

# Chain files
cat "$INTER_DIR/certs/server.cert" "$INTER_DIR/certs/intermediate-${NODE_NAME}.cert" "$PKI_DIR/root-ca/certs/root-ca.cert" \
    > "$INTER_DIR/certs/server.chain.pem"
cat "$INTER_DIR/certs/client.cert" "$INTER_DIR/certs/intermediate-${NODE_NAME}.cert" "$PKI_DIR/root-ca/certs/root-ca.cert" \
    > "$INTER_DIR/certs/client.chain.pem"

echo "  PKI complete."

# === Step 5: Initialize or prepare ===
if $INIT; then
    if [[ -z "$ADMIN_PASSWORD" ]]; then
        ADMIN_PASSWORD=$(openssl rand -base64 32 | tr -d '/+=' | head -c 32)
        echo ""
        echo "  *** Generated admin password: $ADMIN_PASSWORD ***"
        echo "  *** SAVE THIS ***"
        echo ""
    fi

    echo "[5/6] Initializing controller database..."
    "$ZITI_BIN" controller edge init "$CTRL_DIR/ctrl.yaml" -u admin -p "$ADMIN_PASSWORD"
    echo "  Database initialized."
else
    echo "[5/6] Skipping init (will join cluster from leader)..."
fi

# === Step 6: Start controller ===
echo "[6/6] Starting ziti-controller..."
systemctl start ziti-controller

echo "  Waiting for startup..."
for i in $(seq 1 15); do
    if systemctl is-active --quiet ziti-controller; then
        break
    fi
    sleep 2
done

if systemctl is-active --quiet ziti-controller; then
    echo ""
    echo "============================================"
    echo "  Controller $NODE_NAME is RUNNING"
    echo "============================================"
    echo ""
    if $INIT; then
        echo "NEXT:"
        echo "  1. Save the admin password to Bao:"
        echo "     bao kv put secret/networking/openziti/app admin_password=<password>"
        echo ""
        echo "  2. Copy root CA to ctrl-2 and ctrl-3:"
        echo "     scp -r $PKI_DIR/root-ca/ root@ctrl-2.tango:$PKI_DIR/root-ca/"
        echo "     scp -r $PKI_DIR/root-ca/ root@ctrl-3.tango:$PKI_DIR/root-ca/"
        echo ""
        echo "  3. Run rebuild-controller.sh on ctrl-2 and ctrl-3 (without --init)"
        echo ""
        echo "  4. Join them from THIS node:"
        echo "     ziti agent cluster add tls:ctrl-2.tango:1280"
        echo "     ziti agent cluster add tls:ctrl-3.tango:1280"
    else
        echo "NEXT: From the init node, run:"
        echo "  ziti agent cluster add tls:$CTRL_ADDR:1280"
    fi
else
    echo "ERROR: Controller failed to start"
    journalctl -u ziti-controller -n 20 --no-pager 2>/dev/null
    exit 1
fi
