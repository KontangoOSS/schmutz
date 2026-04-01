# Edge Architecture

## Current State (2026-03-27)

Each DO node runs everything:

```
ctrl-1/2/3 (DO)
├── ziti-controller    (port 1280, Raft)
├── ziti-router        (port 3022, fabric)
├── ziti-tunnel        (do-tunnel-{N}, overlay mesh for Bao peering)
├── openbao            (port 8200/8201, Raft)
├── traefik            (port 80/443, TLS termination, local reverse proxy)
├── zitadel            (port 8085, OIDC)
└── postgresql         (port 5432, ZITADEL backend)
```

Traefik on each node terminates TLS and proxies to localhost Bao + ZITADEL.
Everything else gets a dark 404.

## Target State

Each DO node runs only Ziti infrastructure:

```
ctrl-1/2/3 (DO)
├── ziti-controller    (port 1280, Raft)
├── ziti-router        (port 3022, fabric)
├── ziti-tunnel        (do-tunnel-{N}, overlay mesh for Bao peering)
├── openbao            (port 8200/8201, Raft, overlay-only)
└── schmutz       (port 443, SNI classifier → Ziti dial)
```

No reverse proxy. No TLS termination. No certs. No ZITADEL. No Postgres.

```
k8s cluster (DMZ or cloud)
├── traefik            (ingress, TLS termination, cert-manager)
├── zitadel            (OIDC)
├── postgresql (CNPG)  (ZITADEL + app databases)
├── zrok               (controller + frontend)
├── ziti-tunnel        (DaemonSet, binds public-* services)
└── apps               (ticketarr, konfig, etc.)
```

## Why No Reverse Proxy at the Edge

The schmutz binary replaces Traefik/Caddy on the DO nodes. It:

1. Accepts raw TCP on :443
2. Peeks at the TLS ClientHello (reads SNI + computes JA4)
3. Classifies the connection (match against rules)
4. Dials a Ziti service (or drops)
5. Relays raw bytes (TLS passthrough)

It never terminates TLS, never holds certificates, never knows about backends.
The connection arrives encrypted and leaves encrypted. Only the SNI (sent in
plaintext per the TLS spec) is used for routing.

TLS termination happens inside k8s, where cert-manager handles Let's Encrypt
certificates and Traefik routes by Host header.

## Traffic Flows

### Enrolled client (workstation, CI, service account)

```
Client
  → tunneler intercepts DNS: ticketarr.kontango.io → 100.64.0.x
  → Ziti overlay: dial "konoss-ticketarr"
  → controller: find terminator → router-home-tunnel
  → circuit: client → router → router-home → backend 10.0.0.x:9090
```

Does not touch the DO edge at all. No public internet. No reverse proxy.

### Unenrolled client (browser, public internet)

```
Browser
  → DNS: ticketarr.kontango.io → CNAME edge.kontango.io → 203.0.113.1
  → TCP connect to 203.0.113.1:443
  → TLS ClientHello: SNI=ticketarr.kontango.io
  → schmutz: classify → rule "catch-all" → service "public-default"
  → zitiCtx.Dial("public-default")
  → controller: find terminator → k8s tunneler
  → circuit: edge-gw → router → k8s-router → k8s tunneler → Traefik pod
  → Traefik: terminates TLS, Host: ticketarr.kontango.io → dark 404
```

### Unenrolled client hitting a real public service (ZITADEL OIDC)

```
Browser
  → DNS: auth.kontango.io → CNAME edge.kontango.io → 203.0.113.2
  → TCP connect to 203.0.113.2:443
  → TLS ClientHello: SNI=auth.kontango.io
  → schmutz: classify → rule "auth" → service "public-auth"
  → zitiCtx.Dial("public-auth")
  → controller: find terminator → k8s tunneler
  → circuit: edge-gw → router → k8s-router → k8s tunneler → ZITADEL pod
  → ZITADEL: terminates TLS, handles OIDC flow
```

Note: ZITADEL gets the traffic directly — no Traefik hop. The schmutz
classified it at Layer 4 and routed it to a dedicated Ziti service.

## DNS Model

Only two DNS records matter:

| Record              | Type  | Value                                        |
|---------------------|-------|----------------------------------------------|
| edge.kontango.io    | A     | 203.0.113.1, 203.0.113.2, 203.0.113.3|
| *.kontango.io       | CNAME | edge.kontango.io                             |

Same pattern for all domains (example.org, example.net, etc.).

Adding an edge node: add its IP to the A record.
Removing an edge node: remove its IP from the A record.
Adding a service: add a rule in the schmutz config + a Ziti service.

No per-subdomain DNS changes. No per-node reverse proxy config.

## Privilege Model

### Schmutz Identity

| Field      | Value            |
|------------|------------------|
| Attributes | `schmutzs`  |
| Can Dial   | `#public-services` only |
| Can Bind   | Nothing          |
| Can Reach  | k8s services only (via overlay) |

Cannot reach Bao, cannot reach management APIs, cannot reach internal services.

### Tunneler Identity (do-tunnel-{N})

| Field      | Value                    |
|------------|--------------------------|
| Attributes | `do-tunnels,do-tunnel-{N}` |
| Can Bind   | Own node's Bao + controller only |
| Can Dial   | All nodes' Bao + controller only |

Cannot reach application services, cannot reach k8s.

### Ziti Controller

| Port | Source   | Purpose                          |
|------|----------|----------------------------------|
| 1280 | Anywhere | mTLS control plane (Raft + auth) |
| 3022 | Anywhere | mTLS data plane (router links)   |

Open to the internet but protected by mTLS — only enrolled identities with
valid PKI certs can connect. No secret exposed.

### Summary

Each component has exactly the privileges it needs. The schmutz can only
forward public traffic to public services. The tunneler can only peer Bao nodes.
The controller handles its own authentication. No component has broad access.
```
