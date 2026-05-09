package enroll

// installScriptV2 is the self-contained enrollment script.
// It collects machine data, POSTs to SSE, extracts identity, installs ziti, starts tunnel.
// No binary download needed — pure shell.
const installScriptV2 = `#!/bin/sh
set -e
BASE_URL="%s"
SESSION="%s"
ZITI_VERSION="%s"
BAO_VERSION="%s"
# After the tunnel is up, try the overlay URL for agent/caddy downloads.
# Falls back to BASE_URL if the overlay is not yet reachable.
OVERLAY_URL="${BASE_URL}"

# --- Parse arguments ---
MODE="service"
for arg in "$@"; do
  case "$arg" in
    --mode=*) MODE="${arg#*=}" ;;
    --mode) shift; MODE="$1" ;;
  esac
done
if [ "$MODE" != "portal" ] && [ "$MODE" != "service" ]; then
  echo "schmutz: invalid mode '$MODE' (use 'portal' or 'service')" >&2
  exit 1
fi

# --- Collect machine data ---
HOSTNAME=$(hostname)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in x86_64|amd64) ARCH="amd64";; aarch64|arm64) ARCH="arm64";; armv7*|armhf) ARCH="arm";; esac
KERNEL=$(uname -r)
TIMEZONE=$(cat /etc/timezone 2>/dev/null || echo "")
MACHINE_ID=$(cat /etc/machine-id 2>/dev/null || echo "")
CPU_INFO=$(grep -m1 "model name" /proc/cpuinfo 2>/dev/null | cut -d: -f2 | xargs || sysctl -n machdep.cpu.brand_string 2>/dev/null || echo "")
CPU_CORES=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo "0")
MEM_KB=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' || echo "0")
MEM_MB=$((MEM_KB / 1024))

# OS version
if [ -f /etc/os-release ]; then
  OS_VERSION=$(grep PRETTY_NAME /etc/os-release | cut -d= -f2 | tr -d '"')
elif [ "$OS" = "darwin" ]; then
  OS_VERSION="macOS $(sw_vers -productVersion 2>/dev/null)"
else
  OS_VERSION="$OS/$ARCH"
fi

# Network fingerprinting - all interfaces
MACS=""
if command -v ip >/dev/null 2>&1; then
  MACS=$(ip -o link 2>/dev/null | grep "link/ether" | awk '{print $NF}' | while read m; do printf "\"$m\","; done | sed 's/,$//')
elif command -v ifconfig >/dev/null 2>&1; then
  MACS=$(ifconfig 2>/dev/null | grep -i ether | awk '{print $2}' | while read m; do printf "\"$m\","; done | sed 's/,$//')
fi
[ -z "$MACS" ] && MACS=""

# DNS servers
DNS_SERVERS=""
if [ -f /etc/resolv.conf ]; then
  DNS_SERVERS=$(grep "^nameserver" /etc/resolv.conf | awk '{print $2}' | while read d; do printf "\"$d\","; done | sed 's/,$//')
fi
[ -z "$DNS_SERVERS" ] && DNS_SERVERS=""

# Network gateway
GATEWAY=""
if command -v ip >/dev/null 2>&1; then
  GATEWAY=$(ip route 2>/dev/null | grep default | awk '{print $3}' | head -1)
fi

# Hardware fingerprinting
SERIAL=$(cat /sys/class/dmi/id/product_serial 2>/dev/null || dmidecode 2>/dev/null | grep Serial | head -1 | cut -d: -f2 | xargs || echo "")
PRODUCT_NAME=$(cat /sys/class/dmi/id/product_name 2>/dev/null || dmidecode 2>/dev/null | grep "Product Name" | cut -d: -f2 | xargs || echo "")
BIOS_VERSION=$(cat /sys/class/dmi/id/bios_version 2>/dev/null || dmidecode 2>/dev/null | grep "BIOS Version" | cut -d: -f2 | xargs || echo "")

# Disk serials
DISK_SERIALS=""
if command -v lsblk >/dev/null 2>&1; then
  DISK_SERIALS=$(lsblk -d -o SERIAL 2>/dev/null | tail -n +2 | while read s; do [ -n "$s" ] && printf "\"$s\","; done | sed 's/,$//')
elif command -v diskutil >/dev/null 2>&1; then
  DISK_SERIALS=$(diskutil info / 2>/dev/null | grep "Device Identifier" | awk '{print $NF}' | while read d; do printf "\"$d\","; done | sed 's/,$//')
fi
[ -z "$DISK_SERIALS" ] && DISK_SERIALS=""

# Boot ID
BOOT_ID=$(cat /proc/sys/kernel/random/boot_id 2>/dev/null || echo "")

# Uptime in seconds
UPTIME=$(cat /proc/uptime 2>/dev/null | awk '{print int($1)}' || sysctl -n kern.boottime 2>/dev/null | awk '{print $4}' | tr -d ',' || echo "0")

# System locale
LOCALE=$(locale | grep LANG | cut -d= -f2 | cut -d. -f1 || echo "")

# Open ports (netstat or ss)
OPEN_PORTS=""
if command -v ss >/dev/null 2>&1; then
  OPEN_PORTS=$(ss -tuln 2>/dev/null | grep LISTEN | awk '{print $4}' | cut -d: -f2 | sort -u | while read p; do [ -n "$p" ] && printf "$p,"; done | sed 's/,$//')
elif command -v netstat >/dev/null 2>&1; then
  OPEN_PORTS=$(netstat -tuln 2>/dev/null | grep LISTEN | awk '{print $4}' | cut -d: -f2 | sort -u | while read p; do [ -n "$p" ] && printf "$p,"; done | sed 's/,$//')
fi
[ -z "$OPEN_PORTS" ] && OPEN_PORTS=""

# Installed packages count
PKG_COUNT=0
if command -v dpkg >/dev/null 2>&1; then
  PKG_COUNT=$(dpkg -l 2>/dev/null | grep "^ii" | wc -l)
elif command -v rpm >/dev/null 2>&1; then
  PKG_COUNT=$(rpm -qa 2>/dev/null | wc -l)
elif command -v brew >/dev/null 2>&1; then
  PKG_COUNT=$(brew list 2>/dev/null | wc -l)
fi

# Hardware hash (same as Go client: sha256 of MACs + serial + machine_id, first 16 hex)
HW_HASH=$(printf "%%s%%s%%s" "$MACS" "$SERIAL" "$MACHINE_ID" | sha256sum 2>/dev/null | cut -c1-16 || echo "")

# SSH host keys
SSH_KEYS=""
for f in /etc/ssh/ssh_host_*_key.pub; do
  [ -f "$f" ] && SSH_KEYS="$SSH_KEYS\"$(cat "$f" | tr -d '\n')\","
done
SSH_KEYS=$(echo "$SSH_KEYS" | sed 's/,$//')

# --- Display ULA before collection ---
# The ULA explains all the system information that will be collected
# User must explicitly approve (Y/y) or pass -y flag to proceed

ULA_URL="https://raw.githubusercontent.com/KontangoOSS/TangoKore/main/docs/ULA.txt"
AUTO_APPROVE=0

# Check for -y flag in script arguments
for arg in "$@"; do
  if [ "$arg" = "-y" ] || [ "$arg" = "--yes" ]; then
    AUTO_APPROVE=1
    break
  fi
done

if [ $AUTO_APPROVE -eq 0 ]; then
  echo ""
  echo "schmutz: displaying User Agreement for data collection..."
  echo ""

  # Fetch ULA from GitHub or use fallback
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$ULA_URL" 2>/dev/null || cat << 'ULATEXT'
[ULA not available, displaying summary]

KONTANGO PLATFORM - DATA COLLECTION NOTICE

The following system information will be collected:
- Device ID, hostname, serial number, boot ID
- OS, kernel, architecture, timezone, locale
- CPU model, cores, RAM, disk serials, hardware hash
- All MAC addresses, DNS servers, gateway, open ports
- SSH host keys, package count, uptime
- Network connection info and TLS fingerprint

Data is stored encrypted in Bao and transmitted over TLS/Ziti.
Retention: Indefinite while enrolled, 24h in audit streams.
Access: Controller administrators and authenticated Ziti requests only.

By proceeding, you acknowledge and consent to this data collection.
ULATEXT
  else
    cat << 'ULATEXT'
[ULA not available]

KONTANGO PLATFORM - DATA COLLECTION NOTICE

System information will be collected for device verification.
By proceeding, you consent to data collection as described in the documentation.
ULATEXT
  fi

  echo ""
  echo "Do you approve? (Y/n or -y to auto-acknowledge)"
  read -r RESPONSE < /dev/stdin

  case "$RESPONSE" in
    [Yy]|[Yy][Ee][Ss]) ;;
    [Nn]|[Nn][Oo])
      echo "schmutz: installation cancelled — ULA not approved"
      exit 0
      ;;
    "")
      echo "schmutz: installation cancelled — no response"
      exit 0
      ;;
    *)
      echo "schmutz: invalid response '$RESPONSE' — installation cancelled"
      exit 0
      ;;
  esac

  echo "schmutz: ULA acknowledged, proceeding with installation..."
  echo ""
fi

echo "schmutz: enrolling $HOSTNAME ($OS_VERSION, $ARCH)"

# --- POST to SSE endpoint ---
TMP=$(mktemp -d)
trap "rm -rf $TMP" EXIT

# Refresh session token — the baked-in token may have expired if the script
# was cached or took time to execute. Re-fetch a fresh install token now.
FRESH_SESSION=$(curl -sf "$BASE_URL/install" 2>/dev/null | grep '^SESSION=' | head -1 | cut -d= -f2 | tr -d '"')
if [ -n "$FRESH_SESSION" ]; then
  SESSION="$FRESH_SESSION"
fi

# Build comprehensive JSON payload for fingerprinting
cat > "$TMP/payload.json" << PAYLOAD
{
  "method": "new",
  "session": "$SESSION",
  "hostname": "$HOSTNAME",
  "os": "$OS",
  "os_version": "$OS_VERSION",
  "arch": "$ARCH",
  "kernel": "$KERNEL",
  "timezone": "$TIMEZONE",
  "locale": "$LOCALE",
  "cpu_info": "$CPU_INFO",
  "cpu_cores": $CPU_CORES,
  "memory_mb": $MEM_MB,
  "machine_id": "$MACHINE_ID",
  "serial": "$SERIAL",
  "product_name": "$PRODUCT_NAME",
  "bios_version": "$BIOS_VERSION",
  "hardware_hash": "$HW_HASH",
  "disk_serials": [$DISK_SERIALS],
  "boot_id": "$BOOT_ID",
  "uptime_secs": $UPTIME,
  "macs": [$MACS],
  "dns_servers": [$DNS_SERVERS],
  "gateway": "$GATEWAY",
  "open_ports": [$OPEN_PORTS],
  "package_count": $PKG_COUNT,
  "ssh_host_keys": [$SSH_KEYS]
}
PAYLOAD

# Send and capture SSE response
curl -sf -X POST "$BASE_URL/api/enroll/stream" \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d @"$TMP/payload.json" > "$TMP/sse_response.txt"

# Parse SSE events — extract fields directly from file to avoid shell quoting issues
# with large identity blobs containing certs, backslashes, and special characters.

# Check if we got rejected
if grep -q '"status":"rejected"' "$TMP/sse_response.txt" 2>/dev/null; then
  echo "schmutz: rejected"
  exit 1
fi

# Extract all fields in one python pass over the last data line
eval "$(grep "^data: " "$TMP/sse_response.txt" 2>/dev/null | tail -1 | sed 's/^data: //' | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    print('ID=' + json.dumps(d.get('id','')))
    print('NICKNAME=' + json.dumps(d.get('nickname','')))
    print('STATUS=' + json.dumps(d.get('status','')))
    print('ENROLLMENT_TAG=' + json.dumps(d.get('tag','')))
except Exception:
    print('ID=')
    print('NICKNAME=')
    print('STATUS=')
    print('ENROLLMENT_TAG=')
" 2>/dev/null || true)"

# Helper function to send progress updates back to controller via Ziti.
# Uses Ziti SDK to connect to the progress-service, which is already authenticated
# via the device's Ziti identity. No manual tag validation needed — Ziti handles auth.
send_progress() {
  local step="$1"
  [ -z "$ID" ] && return 0  # Can't send if we don't have ID yet
  [ ! -f /opt/kontango/identity.json ] && return 0  # No Ziti identity yet

  # Connect to progress-service via Ziti and send newline-delimited JSON
  # The Ziti tunnel handles authentication — just send the update
  {
    printf '{"step":"%%s","tag":"%%s","device_id":"%%s","hostname":"%%s","nickname":"%%s","timestamp":%%d}\n' \
      "$step" "$ENROLLMENT_TAG" "$ID" "$HOSTNAME" "$NICKNAME" "$(date +%%s)"
  } | timeout 5 /opt/kontango/bin/ziti agent dial --id-token /opt/kontango/identity.json progress-service >/dev/null 2>&1 || true
}

if [ -z "$ID" ]; then
  echo "schmutz: enrollment failed — no identity received"
  exit 1
fi

echo "schmutz: enrolled as $NICKNAME ($ID) [$STATUS]"

# Extract and save identity JSON (read from file to avoid shell quoting issues with large blobs)
# The 'identity' field is a pre-serialized JSON string — parse it twice.
grep "^data: " "$TMP/sse_response.txt" 2>/dev/null | tail -1 | sed 's/^data: //' | python3 -c "
import sys, json
d = json.load(sys.stdin)
identity = d.get('identity', '{}')
if isinstance(identity, str):
    identity = json.loads(identity)
json.dump(identity, sys.stdout, indent=2)
" > "$TMP/identity.json" 2>/dev/null

# --- Install ---
mkdir -p /opt/kontango/bin

# Save identity
cp "$TMP/identity.json" /opt/kontango/identity.json
chmod 600 /opt/kontango/identity.json

# Save machine record
cat > /opt/kontango/machine.json << MACHINE
{"id":"$ID","nickname":"$NICKNAME","hostname":"$HOSTNAME","status":"$STATUS","mode":"$MODE"}
MACHINE
chmod 600 /opt/kontango/machine.json

# Download ziti (resolve 'latest' tag via GitHub API)
if [ "$ZITI_VERSION" = "latest" ]; then
  ZITI_VERSION=$(curl -sfL "https://api.github.com/repos/openziti/ziti/releases/latest" | python3 -c "import sys,json; print(json.load(sys.stdin)['tag_name'].lstrip('v'))" 2>/dev/null || echo "2.0.0")
fi
ZITI_URL="https://github.com/openziti/ziti/releases/download/v${ZITI_VERSION}/ziti-${OS}-${ARCH}-${ZITI_VERSION}.tar.gz"
if [ ! -f /opt/kontango/bin/ziti ]; then
  echo "schmutz: downloading ziti v${ZITI_VERSION}…"
  send_progress "downloading-ziti"
  curl -fsSL "$ZITI_URL" -o "$TMP/ziti.tar.gz"
  tar -xzf "$TMP/ziti.tar.gz" -C "$TMP/"
  cp "$TMP/ziti" /opt/kontango/bin/ziti 2>/dev/null || cp "$TMP/ziti-${OS}-${ARCH}/ziti" /opt/kontango/bin/ziti 2>/dev/null || find "$TMP" -name "ziti" -type f -exec cp {} /opt/kontango/bin/ziti \;
  chmod +x /opt/kontango/bin/ziti
  send_progress "ziti-installed"
fi

# Install systemd service
if command -v systemctl >/dev/null 2>&1; then
  cat > /etc/systemd/system/tango-tunnel.service << SVC
[Unit]
Description=Tango Tunnel ($NICKNAME)
Documentation=https://kontango.io/docs
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/kontango/bin/ziti tunnel host -i /opt/kontango/identity.json -v
Restart=on-failure
RestartSec=5s
StartLimitInterval=300
StartLimitBurst=5
LimitNOFILE=65536
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tango-tunnel
EnvironmentFile=-/opt/kontango/schmutz.env

[Install]
WantedBy=multi-user.target
SVC
  systemctl daemon-reload
  systemctl enable tango-tunnel.service
  systemctl start tango-tunnel.service
  send_progress "tunnel-starting"

  # Wait for tunnel to be healthy — poll ziti agent IPC, fall back to service check.
  echo "schmutz: waiting for tunnel…"
  TUNNEL_OK=0
  for i in $(seq 1 30); do
    if /opt/kontango/bin/ziti agent stats --app-type tunnel --timeout 2s >/dev/null 2>&1; then
      TUNNEL_OK=1; break
    fi
    if systemctl is-active tango-tunnel.service >/dev/null 2>&1; then
      TUNNEL_OK=1; break
    fi
    sleep 2
  done
  if [ "$TUNNEL_OK" = "1" ]; then
    echo "schmutz: tunnel connected ✓"
    send_progress "tunnel-connected"
  else
    echo "schmutz: tunnel did not become healthy — agent will retry on its own"
    send_progress "tunnel-starting-fallback"
  fi

  # Download agent through the overlay — public API no longer needed after this point.
  if [ ! -f /opt/kontango/bin/schmutz ]; then
    echo "schmutz: downloading agent (overlay)…"
    send_progress "downloading-agent"
    curl -fsSL "$BASE_URL/hook-bin/schmutz-${OS}-${ARCH}" -o /opt/kontango/bin/schmutz \
      || curl -fsSL "$OVERLAY_URL/download/schmutz-join-${OS}-${ARCH}" -o /opt/kontango/bin/schmutz \
      || curl -fsSL "$BASE_URL/download/schmutz-join-${OS}-${ARCH}" -o /opt/kontango/bin/schmutz
    chmod +x /opt/kontango/bin/schmutz
    send_progress "agent-downloaded"
  fi

  cat > /etc/systemd/system/tango-agent.service << AGENTSVC
[Unit]
Description=Tango Agent ($NICKNAME)
Documentation=https://kontango.io/docs
After=network-online.target tango-tunnel.service
Wants=network-online.target
BindsTo=tango-tunnel.service

[Service]
Type=simple
ExecStart=/opt/kontango/bin/schmutz agent -identity /opt/kontango/identity.json -fallback $BASE_URL
Restart=on-failure
RestartSec=10s
StartLimitInterval=300
StartLimitBurst=5
LimitNOFILE=65536
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tango-agent
EnvironmentFile=-/opt/kontango/schmutz.env

[Install]
WantedBy=multi-user.target
AGENTSVC
  systemctl daemon-reload
  systemctl enable tango-agent.service
  systemctl start tango-agent.service
  send_progress "agent-starting"
  if systemctl is-active tango-agent.service >/dev/null 2>&1; then
    echo "schmutz: agent started ✓"
    send_progress "agent-started"
  else
    echo "schmutz: agent may still be starting"
    send_progress "agent-starting"
  fi

  # Portal — local web UI showing docs + machine status (portal mode only).
  cat > /etc/systemd/system/tango-portal.service << PORTALSVC
[Unit]
Description=Tango Portal ($NICKNAME)
Documentation=https://kontango.io/docs
After=network-online.target tango-tunnel.service
Wants=network-online.target
BindsTo=tango-tunnel.service

[Service]
Type=simple
ExecStart=/opt/kontango/bin/schmutz portal --listen 127.0.0.1:8800 --identity /opt/kontango/identity.json
Restart=on-failure
RestartSec=5s
StartLimitInterval=300
StartLimitBurst=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=tango-portal
EnvironmentFile=-/opt/kontango/schmutz.env

[Install]
WantedBy=multi-user.target
PORTALSVC
  systemctl daemon-reload
  if [ "$MODE" = "portal" ]; then
    systemctl enable tango-portal.service
    systemctl start tango-portal.service
    if systemctl is-active tango-portal.service >/dev/null 2>&1; then
      echo "schmutz: portal started ✓"
    else
      echo "schmutz: portal may still be starting"
    fi
  else
    echo "schmutz: portal installed (inactive — service mode)"
  fi

  # Install Caddy egress proxy (portal mode only — not needed for service mode)
  if [ "$MODE" = "portal" ] && [ ! -f /opt/kontango/bin/caddy ]; then
    echo "schmutz: downloading caddy (overlay)…"
    curl -fsSL "$OVERLAY_URL/download/caddy-${OS}-${ARCH}" -o /opt/kontango/bin/caddy \
      || curl -fsSL "$BASE_URL/download/caddy-${OS}-${ARCH}" -o /opt/kontango/bin/caddy \
      || { echo "schmutz: caddy download failed (non-fatal in service mode)"; true; }
    [ -f /opt/kontango/bin/caddy ] && chmod +x /opt/kontango/bin/caddy
  fi

  # Write base Caddyfile — content depends on mode.
  if [ ! -f /opt/kontango/Caddyfile ]; then
    if [ "$MODE" = "portal" ]; then
      cat > /opt/kontango/Caddyfile << CADDYFILE
# Tango portal — user-facing gateway to the mesh.
# Managed by schmutz. Application routes added when profiles are deployed.

$NICKNAME.tango {
    reverse_proxy 127.0.0.1:8800
}
CADDYFILE
    else
      cat > /opt/kontango/Caddyfile << 'CADDYFILE'
# Tango service node — internal services only.
# Managed by schmutz. Routes added when profiles are deployed.
CADDYFILE
    fi
  fi

  if [ "$MODE" = "portal" ] && [ -f /opt/kontango/bin/caddy ]; then
    cat > /etc/systemd/system/tango-caddy.service << CADDYSVC
[Unit]
Description=Tango Caddy Egress ($NICKNAME)
After=network-online.target tango-tunnel.service
Wants=network-online.target

[Service]
Type=notify
ExecStart=/opt/kontango/bin/caddy run --config /opt/kontango/Caddyfile --adapter caddyfile
ExecReload=/opt/kontango/bin/caddy reload --config /opt/kontango/Caddyfile --adapter caddyfile
Restart=on-failure
RestartSec=5s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
CADDYSVC
    systemctl daemon-reload
    systemctl enable tango-caddy.service
    systemctl start tango-caddy.service
    if systemctl is-active tango-caddy.service >/dev/null 2>&1; then
      echo "schmutz: caddy started ✓"
    else
      echo "schmutz: caddy may still be starting"
    fi
  fi

  # Install OpenBao (agent mode) — for secret injection into applications.
  # Downloaded from GitHub releases. The binary is large (~70MB) but includes
  # the agent, which renders secrets as env vars or files and exec's the app.
  # The systemd unit is installed but NOT enabled/started — it activates when
  # the controller pushes a profile via the apply instruction.
  if [ ! -f /opt/kontango/bin/bao ]; then
    BAO_OS=$(echo "$OS" | sed 's/linux/Linux/;s/darwin/Darwin/')
    BAO_ARCH=$(echo "$ARCH" | sed 's/amd64/x86_64/;s/arm64/arm64/')
    BAO_URL="https://github.com/openbao/openbao/releases/download/v${BAO_VERSION}/bao_${BAO_VERSION}_${BAO_OS}_${BAO_ARCH}.tar.gz"
    echo "schmutz: downloading openbao v${BAO_VERSION}…"
    curl -fsSL "$BAO_URL" -o "$TMP/bao.tar.gz" \
      || curl -fsSL "$OVERLAY_URL/download/bao-${OS}-${ARCH}" -o /opt/kontango/bin/bao
    if [ -f "$TMP/bao.tar.gz" ]; then
      tar -xzf "$TMP/bao.tar.gz" -C "$TMP/"
      cp "$TMP/bao" /opt/kontango/bin/bao
    fi
    chmod +x /opt/kontango/bin/bao
  fi

  # Create profiles and templates directories.
  mkdir -p /opt/kontango/profiles /opt/kontango/templates

  # Bao agent systemd unit — NOT enabled. Starts when a profile is applied.
  cat > /etc/systemd/system/tango-bao-agent.service << BAOSVC
[Unit]
Description=Tango Bao Agent ($NICKNAME)
After=network-online.target tango-tunnel.service
Requires=tango-tunnel.service

[Service]
Type=simple
ExecStart=/opt/kontango/bin/bao agent -config /opt/kontango/bao-agent.hcl
Restart=on-failure
RestartSec=5s
KillMode=process

[Install]
WantedBy=multi-user.target
BAOSVC
  systemctl daemon-reload
  echo "schmutz: bao agent installed (inactive — waiting for profile)"

  # Install healthcheck timer and send initial heartbeat
  # Tag is required to prevent bot spoofing of health data
  send_healthcheck() {
    TUNNEL_OK=$(systemctl is-active tango-tunnel.service 2>/dev/null >/dev/null && echo "true" || echo "false")
    AGENT_OK=$(systemctl is-active tango-agent.service 2>/dev/null >/dev/null && echo "true" || echo "false")
    curl -sf -X POST "$BASE_URL/api/health/device" \
      -H "Content-Type: application/json" \
      -d "{\"id\":\"$ID\",\"nickname\":\"$NICKNAME\",\"tunnel\":$TUNNEL_OK,\"agent\":$AGENT_OK,\"tag\":\"$ENROLLMENT_TAG\"}" \
      >/dev/null 2>&1 || true
  }
  send_healthcheck
  echo "schmutz: initial healthcheck sent"

  # Create systemd timer for periodic healthchecks
  cat > /etc/systemd/system/tango-healthcheck.service << HCSVC
[Unit]
Description=Tango Healthcheck ($NICKNAME)
After=network-online.target tango-tunnel.service

[Service]
Type=oneshot
ExecStart=/bin/sh -c 'TUNNEL=\$\$(systemctl is-active tango-tunnel.service 2>/dev/null >/dev/null && echo true || echo false); AGENT=\$\$(systemctl is-active tango-agent.service 2>/dev/null >/dev/null && echo true || echo false); curl -sf -X POST "$BASE_URL/api/health/device" -H "Content-Type: application/json" -d "{\"id\":\"$ID\",\"nickname\":\"$NICKNAME\",\"tunnel\":\$\$TUNNEL,\"agent\":\$\$AGENT,\"tag\":\"$ENROLLMENT_TAG\"}" >/dev/null 2>&1 || true'
HCSVC

  cat > /etc/systemd/system/tango-healthcheck.timer << HCTIMER
[Unit]
Description=Tango Healthcheck Timer

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min
Unit=tango-healthcheck.service

[Install]
WantedBy=timers.target
HCTIMER

  systemctl daemon-reload
  systemctl enable tango-healthcheck.timer
  systemctl start tango-healthcheck.timer
  echo "schmutz: healthcheck timer installed"
fi

echo ""
echo "done."
echo "  mode:      $MODE"
echo "  nickname:  $NICKNAME"
echo "  id:        $ID"
echo "  identity:  /opt/kontango/identity.json"
echo "  status:    $STATUS"
if [ "$MODE" = "portal" ]; then
  echo ""
  echo "  portal:    http://$NICKNAME.tango"
  echo "  Sign in to claim this machine."
fi
`
