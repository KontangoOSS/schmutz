# Origin Story

[← Back to README](../README.md)

---

How Schmutz came to be — from a broken cluster to a zero-trust edge firewall.

## The Problem

We had a 3-node cluster running a zero-trust overlay network. Each node ran
everything: overlay controllers (Raft HA), routers, a secrets manager (also
Raft), an identity provider, a database, a reverse proxy, and a tunnel sharing
service. Six processes, two Raft clusters, all on the same three machines.

```mermaid
flowchart TD
    subgraph node["Each node ran everything"]
        C["Overlay Controller\n(Raft)"]
        R["Router"]
        B["Secrets Manager\n(Raft)"]
        IDP["Identity Provider"]
        DB["Database"]
        RP["Reverse Proxy"]
    end

    C <-->|"coupled"| B

    style node fill:#ffe6e6,stroke:#e53e3e
    style C fill:#feebc8,stroke:#dd6b20
    style B fill:#feebc8,stroke:#dd6b20
```

The immediate problem: we needed to take down the overlay Raft cluster for
maintenance, but the secrets manager's Raft cluster ran on the same three
nodes. Taking down the overlay could take down secrets. **The two systems
were coupled by infrastructure, not by design.**

The planned fix was to add secrets manager replicas in Kubernetes. But the
K8s cluster didn't exist yet. Dead end.

## The First Fix: Tunnelers

Instead of waiting for K8s, we set up overlay tunnelers on each node. Each
tunneler binds its own node's secrets manager as a service, and can dial the
other nodes' instances through the overlay.

```mermaid
flowchart LR
    subgraph N1["Node 1"]
        T1["tunnel-1"] -->|"binds"| B1["vault-1"]
    end

    subgraph N2["Node 2"]
        T2["tunnel-2"] -->|"binds"| B2["vault-2"]
    end

    subgraph N3["Node 3"]
        T3["tunnel-3"] -->|"binds"| B3["vault-3"]
    end

    T1 <-->|"overlay"| T2
    T2 <-->|"overlay"| T3
    T1 <-->|"overlay"| T3

    style N1 fill:#e6f9e6,stroke:#38a169
    style N2 fill:#e6f9e6,stroke:#38a169
    style N3 fill:#e6f9e6,stroke:#38a169
```

**This decoupled secrets peering from direct IP connectivity.** Cross-node
communication verified through the overlay, not through bare IP.

## The Architecture Conversation

With the tunnelers working, we started questioning the entire architecture:

> **"Is the reverse proxy even needed at the edge?"**

The reverse proxy on each node terminates TLS, routes to local services, and
serves a dark 404 for everything else. But enrolled clients bypass the proxy
entirely — the tunneler intercepts DNS and routes through the overlay.
**The proxy only exists for unenrolled clients.**

> **"Could you terminate TLS inside the cluster instead?"**

Yes. If the ingress runs inside K8s, the edge nodes don't need a reverse
proxy at all. Traffic enters on :443, gets forwarded through the overlay,
and the interior handles TLS termination and routing.

```mermaid
flowchart LR
    subgraph before["Before: proxy at the edge"]
        B1["Client"] --> B2["Reverse Proxy\n(TLS termination)"] --> B3["App"]
    end

    subgraph after["After: classify at the edge"]
        A1["Client"] --> A2["Schmutz\n(TLS passthrough)"] --> A3["Overlay"] --> A4["Interior\n(TLS termination)"] --> A5["App"]
    end

    style before fill:#ffe6e6,stroke:#e53e3e
    style after fill:#e6f9e6,stroke:#38a169
```

> **"So what does the edge node actually do?"**

**Classify.** That's it. Read the TLS ClientHello, decide what to do with
the connection, dial the right service. The edge node becomes a classifier,
not a proxy.

> **"And the overlay handles all the routing?"**

Yes. When the classifier calls `zitiCtx.Dial("service-name")`, the
controller looks up who binds that service, computes the optimal route
through the router fabric, and establishes a circuit. No DNS inside the
overlay. No IP routing. **Service-name-based routing with policy enforcement.**

## SNI + JA4: Classification at Layer 4

The key insight: you can read the TLS ClientHello without terminating the
handshake. The ClientHello is sent in plaintext — encryption hasn't started
yet.

```mermaid
flowchart TD
    CH["TLS ClientHello\n(plaintext)"] --> SNI["📛 SNI\nwhich hostname?"]
    CH --> JA4["🧬 JA4\nwhich TLS library?"]

    SNI --> Rules["Rule Engine"]
    JA4 --> Rules

    Rules --> Route["Route to service"]
    Rules --> Drop["Drop (scanner)"]
    Rules --> Ghost["Ghost 404"]

    style CH fill:#f0f4ff,stroke:#4a86c8
    style Route fill:#e6f9e6,stroke:#38a169
    style Drop fill:#ffe6e6,stroke:#e53e3e
    style Ghost fill:#f5f5f5,stroke:#999
```

JA4 identifies the TLS library, not the client's claim. Chrome, Firefox,
curl, Python requests, Go net/http, scanners — they all have different
fingerprints. A bot claiming to be a browser but using a scanner's TLS
library? Caught at the handshake.

## The HP System

An edge node has finite capacity. Under attack, it should become more
selective — like a bouncer who gets pickier as the venue fills up.

```mermaid
flowchart LR
    G["🟢 Green\n> 75%\nAll welcome"] --> Y["🟡 Yellow\n50–75%\nRate limits halve"]
    Y --> O["🟠 Orange\n25–50%\nUnknowns dropped"]
    O --> R["🔴 Red\n0–25%\nVIPs only"]
    R -.->|"attack stops"| G

    style G fill:#c6f6d5,stroke:#38a169
    style Y fill:#fefcbf,stroke:#d69e2e
    style O fill:#feebc8,stroke:#dd6b20
    style R fill:#fed7d7,stroke:#e53e3e
```

Legitimate traffic heals the node. Bad traffic drains it. HP regenerates
passively over time. The node becomes self-regulating — it sheds load
before it falls over.

## The Name

**Schmutz** is Yiddish for "a little dirt." It's the thing that catches all
the filth before it gets inside your house.

The edge nodes are disposable — they get dirty so the interior stays clean.
When one gets too filthy (HP at zero, overwhelmed), you throw it away and
get a new one.

A smudge on every application that you don't notice. But the smudge can save
your ass in a bad situation.

## What We Built

A 15MB static Go binary that:

```mermaid
flowchart TD
    subgraph binary["📦 schmutz (15MB, static, zero deps)"]
        F1["Parses TLS ClientHello"]
        F2["Computes JA4 fingerprints"]
        F3["Classifies against YAML rules"]
        F4["Dials Ziti overlay services"]
        F5["Relays raw bytes (TLS passthrough)"]
        F6["Tracks fingerprints in BoltDB"]
        F7["Adapts via HP system"]
        F8["Burns enrollment JWT after first use"]
    end

    style binary fill:#f0f4ff,stroke:#4a86c8
```

## What's Next

```mermaid
flowchart TD
    NOW["v1.0 — Edge classifier\n(SNI + JA4 + HP)"] --> A["Interior classifier\nInside K8s, team-owned rules"]
    NOW --> B["Config via overlay\nPush rules through Ziti"]
    NOW --> C["JA4 allowlist\nAt Orange+, only known-good passes"]
    NOW --> D["DNS automation\nHP=0 triggers DNS removal"]
    NOW --> E["Multi-region K8s\nOverlay routes to nearest cluster"]

    style NOW fill:#e6f9e6,stroke:#38a169
    style A fill:#f0f4ff,stroke:#4a86c8
    style B fill:#f0f4ff,stroke:#4a86c8
    style C fill:#f0f4ff,stroke:#4a86c8
    style D fill:#f0f4ff,stroke:#4a86c8
    style E fill:#f0f4ff,stroke:#4a86c8
```

1. **Interior classifier** — a second Schmutz inside K8s where teams define their own routing rules
2. **Config via overlay** — push rule updates through the controller, all edge nodes get them simultaneously
3. **JA4 allowlist** — BoltDB tracks seen fingerprints; at Orange+, only known-good passes
4. **DNS automation** — HP at zero triggers automatic DNS record removal
5. **Multi-region K8s** — the overlay routes to the nearest cluster; edge nodes don't need to know
