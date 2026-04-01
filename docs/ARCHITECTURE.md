# Architecture

[← Back to README](../README.md)

---

## The Two Layers

Schmutz splits your network into two zones with a hard boundary between them.

```mermaid
flowchart TB
    subgraph Edge["🛡️ EDGE — Schmutz"]
        direction LR
        E1["Public IPs"]
        E2["Disposable"]
        E3["Stateless"]
        E4["Never terminates TLS"]
    end

    subgraph Overlay["🔐 Ziti Overlay"]
        direction LR
        O1["mTLS encrypted"]
        O2["Service-based routing"]
        O3["Policy enforcement"]
    end

    subgraph Interior["🏠 INTERIOR — Yours"]
        direction LR
        I1["Private"]
        I2["Persistent"]
        I3["Stateful"]
        I4["Runs your apps"]
    end

    Edge -->|"classifies and relays"| Overlay
    Overlay -->|"routes to service"| Interior

    style Edge fill:#fff3e6,stroke:#f58220
    style Overlay fill:#f0f4ff,stroke:#4a86c8
    style Interior fill:#e6f9e6,stroke:#38a169
```

The edge exists to take the hits. The interior exists to do the work.

---

## Traffic Flows

Three paths. Three outcomes.

### Enrolled Client (on the overlay)

```mermaid
sequenceDiagram
    participant L as 💻 Your Laptop
    participant T as 🔒 Ziti Tunneler
    participant O as 🌐 Overlay Fabric
    participant I as 🏠 Ingress Controller
    participant A as 📦 Your App

    L->>T: app.example.com
    Note over T: DNS intercepted locally
    T->>O: Dial "app-service" (mTLS)
    O->>I: Route to nearest terminator
    I->>A: Forward (HTTP)
    A->>L: Response (encrypted end-to-end)

    Note over L,A: Schmutz is NOT involved.<br/>Enrolled clients bypass the edge entirely.
```

### Unenrolled Client (from the internet)

```mermaid
sequenceDiagram
    participant B as 🌐 Browser
    participant D as 📡 DNS
    participant S as 🛡️ Schmutz (edge)
    participant O as 🔒 Overlay
    participant I as 🏠 Ingress
    participant A as 📦 App

    B->>D: app.example.com?
    D->>B: CNAME → edge.example.com → 203.0.113.1

    B->>S: TCP :443 + TLS ClientHello
    Note over S: Read SNI: "app.example.com"<br/>Compute JA4 fingerprint<br/>Match rule → "app-service"
    S->>O: Dial "app-service" (Ziti SDK)
    O->>I: Route through fabric
    I->>A: Terminate TLS, forward request
    A->>B: Response (TLS passthrough via Schmutz)
```

### Scanner / Bot

```mermaid
sequenceDiagram
    participant X as 🤖 Scanner
    participant S as 🛡️ Schmutz

    X->>S: TCP :443 + TLS ClientHello
    Note over S: Read SNI: "app.example.com"<br/>Compute JA4: zgrab2 fingerprint<br/>Match rule → "block-scanners"
    S--xX: DROP (connection reset, no response)

    Note over X,S: Scanner learns nothing.<br/>No service name. No cert. No banner.
```

---

## DNS Pattern

```mermaid
flowchart LR
    Client["Client"] -->|"app.example.com"| DNS["DNS"]
    DNS -->|"CNAME"| Edge["edge.example.com"]
    Edge --> N1["203.0.113.1\nedge-1"]
    Edge --> N2["203.0.113.2\nedge-2"]
    Edge --> N3["203.0.113.3\nedge-3"]

    style DNS fill:#f0f4ff,stroke:#4a86c8
    style N1 fill:#fff3e6,stroke:#f58220
    style N2 fill:#fff3e6,stroke:#f58220
    style N3 fill:#fff3e6,stroke:#f58220
```

```
*.example.com      CNAME   edge.example.com
edge.example.com   A       203.0.113.1
edge.example.com   A       203.0.113.2
edge.example.com   A       203.0.113.3
```

One wildcard CNAME. A-records for each edge node. DNS round-robin distributes
load. All intelligence is at the edge — the DNS layer is deliberately dumb.

Adding a new domain requires zero DNS changes. The wildcard already covers it.
Just add a rule to your Schmutz config.

---

## Edge Node Anatomy

```mermaid
flowchart TD
    subgraph Node["🖥️ Edge Node"]
        TCP["TCP Listener\n:443"]
        TCP --> Parse["ClientHello Parser"]
        Parse --> JA4["JA4 Calculator"]
        Parse --> SNI["SNI Extractor"]
        JA4 --> Rules["Rule Engine\n(YAML config)"]
        SNI --> Rules
        Rules -->|match| Dial["Ziti Dialer"]
        Rules -->|no match| Drop["Drop"]
        Dial --> Relay["TCP Relay\n(bidirectional pipe)"]
    end

    HP["🩹 HP System"] -.->|"adjusts thresholds"| Rules
    DB["💾 BoltDB"] -.->|"fingerprint tracking"| JA4
    DB -.->|"HP persistence"| HP

    style Node fill:#fff3e6,stroke:#f58220
    style Drop fill:#ffe6e6,stroke:#e53e3e
    style Relay fill:#e6f9e6,stroke:#38a169
```

No reverse proxy. No certificate store. No backend registry. No application
logic. ~15MB static binary, no dependencies, no runtime.

---

## The Security Boundary

```mermaid
flowchart LR
    subgraph knows["✅ Edge Node Knows"]
        K1["SNI from ClientHello"]
        K2["JA4 fingerprint"]
        K3["Source IP"]
        K4["Ziti service names"]
        K5["Its own HP level"]
    end

    subgraph doesnt["🚫 Edge Node Does NOT Know"]
        D1["Plaintext request content"]
        D2["TLS private keys"]
        D3["Backend IPs or locations"]
        D4["Backend application logic"]
        D5["Other edge nodes' state"]
    end

    style knows fill:#e6f9e6,stroke:#38a169
    style doesnt fill:#ffe6e6,stroke:#e53e3e
```

The edge node can't leak what it doesn't have. If compromised, the attacker
gets: a list of Ziti service names and a BoltDB of JA4 fingerprints. They
don't get certificates, backend access, or any way to impersonate a
legitimate client.

---

## Ziti Integration

Schmutz is a Ziti SDK application. Each edge node has a Ziti identity with
**dial-only** permissions.

```mermaid
flowchart TD
    subgraph identity["🪪 Edge Node Identity"]
        CAN["✅ CAN dial:\npublic-app\npublic-auth\nhoneypot"]
        CANNOT_D["🚫 CANNOT dial:\nsecrets\nmanagement\ndatabases"]
        CANNOT_B["🚫 CANNOT bind:\nanything"]
        CANNOT_A["🚫 CANNOT access:\ncontroller API\nrouter admin"]
    end

    style CAN fill:#e6f9e6,stroke:#38a169
    style CANNOT_D fill:#ffe6e6,stroke:#e53e3e
    style CANNOT_B fill:#ffe6e6,stroke:#e53e3e
    style CANNOT_A fill:#ffe6e6,stroke:#e53e3e
```

When Schmutz dials a service:

```mermaid
sequenceDiagram
    participant S as 🛡️ Schmutz
    participant C as 🎛️ Ziti Controller
    participant R as 🔀 Router Fabric
    participant T as 📦 Terminator

    S->>C: Dial "app-service"
    C->>C: Look up who binds "app-service"
    C->>C: Compute optimal route
    C->>R: Establish circuit (mTLS)
    R->>T: Connect to terminator
    T->>S: Circuit ready
    Note over S,T: Schmutz relays raw bytes through the circuit
```

The overlay is the routing table, the policy engine, and the encryption
layer. Schmutz just decides which door to knock on.

---

## Scaling

```mermaid
flowchart LR
    DNS["DNS Round-Robin"] --> E1["edge-1\n🟢 HP: 95%"]
    DNS --> E2["edge-2\n🟡 HP: 60%"]
    DNS --> E3["edge-3\n🟢 HP: 88%"]
    DNS -.->|"TTL expires"| E4["edge-4\n🔴 HP: 0%\n(being replaced)"]

    E1 --> Overlay["Ziti Fabric"]
    E2 --> Overlay
    E3 --> Overlay

    style E1 fill:#c6f6d5,stroke:#38a169
    style E2 fill:#fefcbf,stroke:#d69e2e
    style E3 fill:#c6f6d5,stroke:#38a169
    style E4 fill:#fed7d7,stroke:#e53e3e
    style Overlay fill:#f0f4ff,stroke:#4a86c8
```

Edge nodes share nothing. No distributed state. No consensus. No leader.

- **Adding capacity:** spin up a VM, install Schmutz, add the IP to DNS
- **Removing capacity:** remove the IP from DNS, destroy the VM
- **Replacing a node:** DNS TTL expires, clients move to the next one

The Ziti fabric picks the best path to the backend regardless of which
edge node the client lands on.
