# Edge Node Identity Model

## Principles

Edge nodes are ephemeral, anonymous, and isolated. They are disposable
infrastructure. Their value comes from their position (public IP, geographic
location), not their identity.

### 1. Ephemeral — born to die

Every edge node identity is created at install time and destroyed when the
node is decommissioned. There is no migration, no backup, no recovery.

```
Spin up VM → install.sh → identity created + enrolled → operational
Node dies  → identity is dead → spin up a replacement → new identity
```

The enrollment JWT is single-use. Once the node enrolls, the JWT is deleted.
If the enrolled identity file is lost, the node is dead. This is intentional.
Edge nodes are cattle, not pets.

### 2. Anonymous — identity doesn't matter

All edge node identities share the same attribute: `schmutzs`. They have
identical permissions. The controller doesn't care which specific edge node
dials a service — they're all equivalent. The node name is for observability
only (logs, metrics). It has no policy significance.

```bash
# These are functionally identical
edge-gw-1  → attributes: schmutzs  → can dial: #public-services
edge-gw-2  → attributes: schmutzs  → can dial: #public-services
edge-gw-99 → attributes: schmutzs  → can dial: #public-services
```

Adding or removing an edge node requires zero policy changes. The attribute
`schmutzs` is all that matters.

### 3. Isolated — no peer awareness

Edge nodes CANNOT reach each other. There is no service that allows
edge-to-edge communication. Each node connects to:

- Ziti controllers (port 1280, for control plane)
- Ziti routers (for data plane circuits to interior services)

That's it. An edge node cannot:
- Dial another edge node's services
- Bind any service (dial-only)
- Access management APIs
- Access Bao on other nodes
- Access any interior service not tagged `#public-services`

```
edge-gw-1 ──dial──→ public-default ──→ k8s Traefik     ✓
edge-gw-1 ──dial──→ public-auth    ──→ k8s ZITADEL     ✓
edge-gw-1 ──dial──→ do-bao-2       ──→ ctrl-2 Bao      ✗ (no policy)
edge-gw-1 ──dial──→ konoss-git     ──→ Forgejo          ✗ (no policy)
edge-gw-1 ──bind──→ anything       ──→                   ✗ (no bind policy)
edge-gw-1 ──dial──→ edge-gw-2      ──→                   ✗ (no such service)
```

### 4. Proximity-based — the overlay decides

When an edge node dials `public-default`, the controller routes to the
nearest healthy terminator. The edge node has no say in this. It doesn't
know where the terminator is, how many there are, or which one it gets.

```
edge-gw-1 (NYC) dials "public-default"
  → controller picks k8s-tunneler on NYC router (lowest cost)

edge-gw-3 (LON) dials "public-default"
  → controller picks k8s-tunneler on LON router (lowest cost)
```

If the nearest terminator dies, the controller re-routes to the next
nearest. The edge node doesn't retry or failover — the overlay does it
transparently on the next Dial().

### 5. No cross-edge failover

Edge nodes do not bounce traffic to each other. If an edge node is
overwhelmed (HP at 0%), it does ONE thing: stops accepting connections.

The failover mechanism is DNS, not overlay:
1. Edge node stops responding on :443
2. Cloudflare health check detects failure (or client TCP timeout)
3. Client retries → Cloudflare gives a different A record
4. Traffic lands on a healthy edge node

No overlay coordination. No gossip protocol. No "hey neighbor, take my
traffic." The edge nodes are independent. DNS is the load balancer.

The ONLY exception is the Ziti controller Raft cluster. Controllers DO
talk to each other — but that's the control plane, not the edge gateway.
The controller is a different concern that happens to run on the same
machine.

## Ziti Configuration

### Identity creation (during install)

```bash
# Auto-generated name — not meaningful, just unique
NODE_ID=$(head -c 6 /dev/urandom | base32 | tr '[:upper:]' '[:lower:]' | head -c 10)

ziti edge create identity "edge-gw-${NODE_ID}" \
    -a "schmutzs" \
    -o /tmp/edge-gw.jwt

ziti edge enroll -j /tmp/edge-gw.jwt -o /opt/zstack/ziti/edge-gw.json

# Burn the JWT immediately
rm -f /tmp/edge-gw.jwt
```

### Policies (created once, applies to all edge nodes forever)

```bash
# Dial only — edge gateways can reach public-facing services
ziti edge create service-policy edge-gw-dial Dial \
    --identity-roles "#schmutzs" \
    --service-roles "#public-services" \
    --semantic "AnyOf"
```

No bind policy. No cross-service access. No management access.

### Decommissioning

```bash
# Node is dead — clean up the identity
ziti edge delete identity "edge-gw-${NODE_ID}"

# Remove IP from DNS
# (Cloudflare API or manual)
```

The identity is garbage collected. The node never comes back.

## Why this works

Traditional edge infrastructure (CDN, load balancers, WAFs) requires
careful configuration per node — certs, backends, health checks, session
affinity. Adding a node is a project.

Edge nodes in this model are stateless classifiers. They hold:
- A Ziti identity (enrolled once, burned on death)
- A rule config (pushed via Raft, same everywhere)
- A BoltDB (local observability, not load-bearing)
- An HP pool (local adaptive defense, not shared)

Nothing on the node is precious. Nothing needs backup. Nothing needs
migration. The only state that matters is in the controller Raft cluster,
and that's replicated across 3+ controllers that the edge nodes don't
even manage.

Spin up, classify traffic, die, get replaced. That's the lifecycle.
