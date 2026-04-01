# Origin Story

How Schmutz came to be — from a broken Kubernetes cluster to a zero-trust edge firewall.

## The Problem (March 27, 2026)

We had a 3-node DigitalOcean cluster running the Kontango zero-trust overlay network. Each node ran everything: Ziti controllers (Raft HA), Ziti routers, OpenBao (secrets, also Raft), ZITADEL (OIDC), PostgreSQL, Traefik (reverse proxy), zrok (tunnel sharing). Six processes, two Raft clusters, all on the same three machines.

The immediate problem: we needed to take down the Ziti Raft cluster for maintenance, but OpenBao's Raft cluster ran on the same three nodes. Taking down Ziti could take down Bao. The two systems were coupled by infrastructure, not by design.

The planned fix was to add OpenBao replicas in a Kubernetes cluster. But the K8s cluster (Talos on Proxmox, DMZ VLAN) didn't exist yet — the VMs had never been provisioned. The kubeconfig pointed to unreachable IPs. Dead end.

## The First Fix: Tunnelers

Instead of waiting for K8s, we set up Ziti tunnelers on each DO node. Each tunneler binds its own node's Bao instance as a Ziti service, and can dial the other nodes' Bao services through the overlay. This decouples Bao peering from direct IP connectivity.

Three identities (`do-tunnel-1/2/3`), six services (`do-bao-1/2/3` + `do-ziti-ctrl-1/2/3`), bind policies per node, shared dial policy. Each tunneler runs `ziti tunnel run` with built-in DNS (`-d 100.64.0.1/10 --dnsUpstream udp://1.1.1.1:53`).

We tested it: `curl http://do-bao-2.kontango.io:8200/v1/sys/health` from ctrl-1 returns Bao's health through the overlay. The DNS resolver on each node points to `127.0.0.1` first (Ziti tunnel DNS), then `1.1.1.1` for non-Ziti names. Cross-node Bao connectivity verified from every node.

## The Architecture Conversation

With the tunnelers working, we started questioning the entire architecture. The conversation went like this:

**"Is the reverse proxy even needed at the edge?"**

Traefik on each DO node terminates TLS, proxies to localhost Bao and ZITADEL, and serves a dark 404 for everything else. But enrolled Ziti clients bypass the reverse proxy entirely — the tunneler intercepts DNS and routes through the overlay. The reverse proxy only exists for unenrolled clients (browsers hitting public URLs).

**"Could you terminate inside the cluster instead?"**

Yes. If Traefik runs inside K8s (which it already does in the DMZ helmfile), the DO edge nodes don't need a reverse proxy at all. Traffic enters on :443, gets forwarded through the overlay to K8s Traefik, which terminates TLS and routes by Host header. One Traefik, one set of certs, one config.

**"How does Cloudflare know where to send the traffic?"**

It doesn't need to. The wildcard CNAME (`*.kontango.io → edge.kontango.io`) resolves to all DO node IPs. The client picks one. The classification happens AFTER the TCP connection is established, when the TLS ClientHello is sent. Cloudflare is a dumb pipe — one CNAME, one set of A records. All the intelligence is at the edge.

**"So Ziti handles all the routing?"**

Yes. When the edge classifier calls `zitiCtx.Dial("public-default")`, the Ziti controller (via Raft) looks up who binds that service, computes the optimal route through the router fabric, and establishes a circuit. No DNS inside the overlay. No IP routing. Service-name-based routing with policy enforcement.

**"And the Raft cluster IS the routing table?"**

Exactly. Every binding, every policy, every terminator, every route — it's all in the Raft log, replicated across all controllers. No external database. No etcd. No consul. The Ziti controller cluster is the distributed state store for the entire network. Geo-replicated across NYC, SFO, and LON.

## The Design Emerges

The conversation crystallized into a clean separation:

| Layer | What it does | Where it runs |
|-------|-------------|---------------|
| **Edge** (Schmutz) | Classify + forward | DO nodes (public IPs) |
| **Overlay** (Ziti) | Policy-based routing | Controller Raft (on Schmutz nodes) |
| **Interior** (K8s) | TLS termination, app routing | K8s cluster (private) |

The edge node doesn't terminate TLS, doesn't hold certs, doesn't know about backends. It reads one plaintext field (SNI from the ClientHello), picks a Ziti service name, and pipes raw bytes through the overlay.

## SNI Classification + JA4 Fingerprinting

The key insight: you can read the TLS ClientHello without terminating the handshake. The ClientHello is sent in plaintext (encryption hasn't started yet). From it, you get:

- **SNI** — which hostname the client wants
- **JA4 fingerprint** — a hash of the TLS library's cipher suites, extensions, and version

JA4 identifies the TLS library, not the client's claim. Chrome, Firefox, curl, Python requests, Go net/http, scanners — they all have different fingerprints. A bot claiming to be a browser but using a scanner's TLS library gets caught.

This gives us classification at Layer 4:
- Known scanner JA4 → DROP (never enters the overlay)
- Auth domain SNI → route to ZITADEL directly
- zrok share SNI → route to zrok frontend
- Everything else → route to K8s Traefik (dark service)

## The HP System

An edge node has finite capacity. Under attack, it should become more selective — like a bouncer who gets stricter as the venue fills up.

Each Schmutz node has a Health Points (HP) pool:
- **Green** (>75%): normal operation
- **Yellow** (50-75%): rate limits halve
- **Orange** (25-50%): unknown JA4 fingerprints get dropped
- **Red** (0-25%): only explicitly named rules pass, catch-all drops

Legitimate traffic heals the node. Dropped connections, failed dials, malformed ClientHellos drain it. HP regenerates passively over time. HP persists across restarts in BoltDB.

The "price" of processing a connection increases as HP drops. At Green, routing a connection is nearly free. At Red, every connection costs significantly. The node becomes self-regulating — it sheds load before it falls over.

## Identity Model

Edge nodes are cattle, not pets:

1. **Ephemeral** — identity is created at install time, dies with the node
2. **Anonymous** — all nodes share one attribute (`schmutzs`), identical permissions
3. **Isolated** — nodes cannot reach each other, no peer awareness
4. **Public IP required** — the installer refuses to run on private addresses
5. **Sealed claim** — the node encrypts a hardware fingerprint with the controller's public key at install time. Only the controller can verify the claim.

Adding an edge node is: spin up a VM, run the installer, add IP to DNS.
Removing one is: delete the A record, destroy the VM.
No config changes, no cert provisioning, no backend registration.

## The Name

Schmutz is Yiddish for "a little dirt." It's the thing that catches all the filth before it gets inside your house. The edge nodes are disposable — they get dirty so the interior stays clean. When one gets too filthy (HP at zero, overwhelmed), you throw it away and get a new one.

## What We Built

A 15MB static Go binary that:
- Parses TLS ClientHello and computes JA4 fingerprints
- Classifies connections against a YAML rule table
- Dials Ziti services through the HA Raft controller cluster
- Relays raw bytes bidirectionally (TLS passthrough)
- Tracks JA4 fingerprints and SNI hostnames in BoltDB
- Adapts defensively via an HP system that tightens under attack
- Requires a public IP to install
- Burns its enrollment JWT after first use

The binary was battle-tested on all three DO nodes. SNI classification, JA4 fingerprinting, rule matching, BoltDB recording, and HP tracking all verified with live traffic.

## Timeline

All of this — the architecture conversation, the design, the Go binary, the BoltDB store, the HP system, the identity model, the installer, the deployment to three DO nodes, the battle testing — happened in a single session on March 27, 2026.

## What's Next

1. **Interior bouncer** — a second Schmutz instance inside K8s where teams define their own routing rules
2. **Config via Raft** — push rule updates through the Ziti controller, all edge nodes get them simultaneously
3. **JA4 allowlist** — BoltDB tracks seen fingerprints, at Orange+ only known-good pass
4. **Cloudflare DNS automation** — HP at zero triggers automatic A record removal
5. **Multi-region K8s** — the overlay routes to the nearest cluster, edge nodes don't need to know