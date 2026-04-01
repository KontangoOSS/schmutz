# Schmutz

Layer 4 edge classifier that reads TLS ClientHello metadata (SNI, JA4 fingerprint,
source IP) and routes raw TCP streams into the Ziti overlay — without terminating TLS.

## Problem

Public traffic arrives at DO edge nodes on :443. Today, Traefik terminates TLS and
proxies to localhost services (Bao, ZITADEL). This means every edge node holds LE certs,
runs a reverse proxy, and knows about backends. Adding a service means editing Traefik
config on 3 nodes.

## Solution

Replace the edge reverse proxy with a single Go binary that:

1. Accepts TCP connections on :443
2. Peeks at the TLS ClientHello (does NOT terminate TLS)
3. Extracts SNI hostname + computes JA4 fingerprint
4. Matches against a rule table
5. Dials the matching Ziti service (or drops the connection)
6. Relays raw bytes bidirectionally (TLS passthrough)

TLS termination, cert management, and application routing all happen inside k8s.
The edge nodes become stateless classifiers with zero knowledge of backends.

## Architecture

```
                   Internet
                      |
            *.kontango.io (Cloudflare)
            CNAME → edge.kontango.io
            A → 203.0.113.1  (ctrl-1)
            A → 203.0.113.2 (ctrl-2)
            A → 203.0.113.3 (ctrl-3)
                      |
         ┌────────────┼────────────┐
         ▼            ▼            ▼
   ┌──────────┐ ┌──────────┐ ┌──────────┐
   │ edge-gw  │ │ edge-gw  │ │ edge-gw  │   :443, TLS passthrough
   │ ctrl-1   │ │ ctrl-2   │ │ ctrl-3   │   reads SNI + JA4
   └────┬─────┘ └────┬─────┘ └────┬─────┘   dials Ziti service
        │             │            │
        └─────────────┼────────────┘
                      │
                 Ziti overlay
              (controller Raft)
                      │
         ┌────────────┼────────────┐
         ▼            ▼            ▼
   public-auth   public-zrok   public-default
         │            │            │
         ▼            ▼            ▼
   ┌─────────────────────────────────────┐
   │           k8s cluster               │
   │                                     │
   │  ZITADEL    zrok frontend   Traefik │
   │  (direct)   (direct)     (catch-all)│
   │                                     │
   │  cert-manager handles all TLS       │
   └─────────────────────────────────────┘
```

## What the Edge Node Runs

After migration, each DO node runs exactly:

| Process           | Port  | Purpose                                    |
|-------------------|-------|--------------------------------------------|
| ziti-controller   | 1280  | Raft member, policy engine, route compute  |
| ziti-router       | 3022  | Fabric data plane, client connections      |
| schmutz      | 443   | SNI classifier → Ziti dial (this binary)   |
| ziti-tunnel       | —     | Overlay mesh for Bao peering (existing)    |
| openbao           | 8200  | Secrets manager (overlay-only access)      |

No reverse proxy. No certs. No backend knowledge.

## ClientHello Parsing

The TLS ClientHello is the first message in a TLS handshake. It's sent in plaintext
(the encryption hasn't started yet). We read these fields:

| Field              | Use                                        |
|--------------------|--------------------------------------------|
| SNI                | Primary routing decision (which service?)  |
| TLS version        | Part of JA4 fingerprint                    |
| Cipher suites      | Part of JA4 fingerprint                    |
| Extensions         | Part of JA4 fingerprint                    |
| ALPN               | Protocol hint (h2 vs http/1.1)             |

### JA4 Fingerprint

JA4 is a hash of the ClientHello structure. Same TLS library = same JA4 regardless
of what the client claims to be. This identifies:

- Chrome vs Firefox vs Safari vs curl vs Python requests vs Go net/http
- Known bot frameworks (scrapy, headless chrome, etc.)
- Scanners (masscan, zgrab, nuclei)

The fingerprint is computed locally — no external lookup needed. A static list of
known-good and known-bad JA4 hashes ships with the binary.

## Rule Engine

Rules are evaluated in order. First match wins.

```yaml
rules:
  # Block known scanners before they enter the overlay
  - name: block-scanners
    ja4:
      - "t13d191000_..." # zgrab2
      - "t13d301000_..." # masscan
    action: drop

  # Auth traffic from real browsers goes direct to ZITADEL
  - name: auth-browser
    sni: "auth.kontango.io"
    ja4_not:
      - "t13d..." # known bot fingerprints
    service: public-auth

  # zrok shares
  - name: zrok-shares
    sni: "*.share.kontango.io"
    service: public-zrok

  # Everything else → dark service
  - name: catch-all
    sni: "*"
    service: public-default
```

### Rule Fields

| Field      | Type       | Description                                  |
|------------|------------|----------------------------------------------|
| `name`     | string     | Rule name (logging)                          |
| `sni`      | string     | SNI glob pattern (`*` = wildcard)            |
| `ja4`      | []string   | Match if JA4 is in this list                 |
| `ja4_not`  | []string   | Match if JA4 is NOT in this list             |
| `src_cidr` | []string   | Source IP CIDR match                         |
| `service`  | string     | Ziti service name to dial                    |
| `action`   | string     | `route` (default) or `drop`                  |
| `rate`     | string     | Per-source rate limit (e.g., "100/m")        |

## Ziti Identity Privileges

The schmutz identity on each node needs minimal permissions:

### Identity

| Field      | Value                                        |
|------------|----------------------------------------------|
| Name       | `edge-gw-{N}` (e.g., `edge-gw-1`)          |
| Type       | Default                                      |
| Attributes | `schmutzs`                              |

### Policies Required

```bash
# The edge gateway can DIAL public-facing services only
ziti edge create service-policy edge-gw-dial Dial \
    --identity-roles "#schmutzs" \
    --service-roles "#public-services" \
    --semantic "AnyOf"
```

The identity can ONLY dial. It cannot bind. It cannot access internal services.
It cannot reach Bao, cannot reach management APIs, cannot reach anything that
isn't explicitly tagged `#public-services`.

### Services

Each public-facing service is a separate Ziti service:

```bash
# Auth — terminates at ZITADEL directly (no Traefik hop)
ziti edge create config public-auth-intercept intercept.v1 '{
  "protocols": ["tcp"],
  "addresses": ["public-auth.internal"],
  "portRanges": [{"low": 443, "high": 443}]
}'
ziti edge create config public-auth-host host.v1 '{
  "protocol": "tcp",
  "address": "<zitadel-pod-ip-or-svc>",
  "port": 443
}'
ziti edge create service public-auth \
    --configs public-auth-intercept,public-auth-host \
    -a "public-services"

# zrok shares
ziti edge create service public-zrok \
    --configs public-zrok-intercept,public-zrok-host \
    -a "public-services"

# Default catch-all → k8s Traefik (dark service)
ziti edge create service public-default \
    --configs public-default-intercept,public-default-host \
    -a "public-services"
```

### Bind Side (k8s)

The k8s tunneler DaemonSet binds these services:

```bash
ziti edge create service-policy public-services-bind Bind \
    --identity-roles "#k8s-tunnelers" \
    --service-roles "#public-services" \
    --semantic "AnyOf"
```

## Controller Privileges

The Ziti controller on each DO node needs:

| Port | Protocol | Source     | Purpose                          |
|------|----------|-----------|----------------------------------|
| 1280 | TCP/mTLS | Anywhere  | Controller Raft + client auth    |
| 3022 | TCP/mTLS | Anywhere  | Router fabric links              |
| 443  | TCP      | Anywhere  | Edge gateway (public web)        |
| 8200 | TCP      | Overlay   | Bao API (tunneler-only)          |
| 8201 | TCP      | Overlay   | Bao Raft (tunneler-only)         |
| 22   | TCP      | Home IP   | SSH management                   |

Ports 1280 and 3022 are mTLS-protected — only enrolled identities with valid Ziti PKI
certs can connect. Port 443 accepts raw TCP from the internet (schmutz classifies it).
Ports 8200/8201 are overlay-only (UFW blocks external, tunneler handles routing).

## Tunneler Privileges

The `do-tunnel-{N}` identity on each node (set up in the current session):

| Privilege  | Scope                                         |
|------------|-----------------------------------------------|
| Bind       | `do-bao-{N}` (own node's Bao 8200-8201)      |
| Bind       | `do-ziti-ctrl-{N}` (own node's controller 1280)|
| Dial       | `#do-bao-services` (all nodes' Bao)           |
| Dial       | `#do-ziti-services` (all nodes' controllers)  |
| DNS        | Resolves overlay names via built-in resolver   |

The tunneler can ONLY reach Bao and Ziti controller ports on peers. It cannot
reach application services, cannot reach k8s, cannot reach the internet through
the overlay.

## Systemd Service

```ini
[Unit]
Description=Schmutz (edge-gw-{N})
After=ziti-controller.service ziti-router.service ziti-tunnel.service
Wants=ziti-router.service

[Service]
Type=simple
User=schmutz
Group=schmutz
ExecStart=/opt/zstack/bin/schmutz \
    --identity /opt/zstack/ziti/edge-gw-{N}.json \
    --config /opt/zstack/schmutz/config.yaml \
    --listen :443
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/zstack/log
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-party.target
```

The binary runs as a dedicated user with only `CAP_NET_BIND_SERVICE` (to bind :443).
No root. No filesystem write access except logs.

## Observability

Every connection is logged as structured JSON:

```json
{
  "ts": "2026-03-27T20:15:00Z",
  "src": "203.0.113.42:54321",
  "sni": "auth.kontango.io",
  "ja4": "t13d1517h2_8daaf6152771_02713d6af862",
  "rule": "auth-browser",
  "service": "public-auth",
  "action": "route",
  "duration_ms": 1523,
  "bytes_in": 4096,
  "bytes_out": 32768,
  "edge_node": "ctrl-1",
  "region": "nyc3"
}
```

Dropped connections:

```json
{
  "ts": "2026-03-27T20:15:01Z",
  "src": "198.51.100.99:12345",
  "sni": "admin.kontango.io",
  "ja4": "t13d191000_9dc949149365_e7c285222651",
  "rule": "block-scanners",
  "action": "drop",
  "edge_node": "ctrl-1",
  "region": "nyc3"
}
```

These logs feed into Loki/Grafana for real-time dashboards:
- Connections per edge node per minute
- Top SNIs requested
- JA4 fingerprint distribution (browsers vs bots vs unknown)
- Drop rate by rule
- Circuit establishment latency

## Build

```bash
cd src/schmutz
go build -o schmutz ./cmd/schmutz
```

## File Structure

```
src/schmutz/
├── DESIGN.md              # This file
├── cmd/
│   └── schmutz/
│       └── main.go        # Entrypoint
├── internal/
│   ├── classifier/
│   │   ├── classifier.go  # Rule engine
│   │   └── rules.go       # Rule types + matching
│   ├── clienthello/
│   │   ├── parse.go       # TLS ClientHello parser
│   │   └── ja4.go         # JA4 fingerprint computation
│   ├── relay/
│   │   └── relay.go       # Bidirectional byte relay
│   └── config/
│       └── config.go      # YAML config loader
├── config.example.yaml    # Example rule config
├── go.mod
└── go.sum
```
