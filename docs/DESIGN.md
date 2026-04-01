# Design

[← Back to README](../README.md)

---

Layer 4 edge classifier that reads TLS ClientHello metadata (SNI, JA4
fingerprint, source IP) and routes raw TCP streams into the Ziti overlay —
without terminating TLS.

## Why Layer 4?

Most edge security tools work at Layer 7 — they terminate TLS, inspect HTTP,
and make decisions based on headers and body content. This requires holding
certificates, parsing protocols, and maintaining significant state.

Schmutz works at Layer 4. It reads exactly one thing: the **TLS ClientHello**.

```mermaid
flowchart LR
    subgraph L7["Layer 7 (traditional)"]
        direction TB
        L7a["Terminate TLS"] --> L7b["Parse HTTP"]
        L7b --> L7c["Inspect headers/body"]
        L7c --> L7d["Route request"]
    end

    subgraph L4["Layer 4 (Schmutz)"]
        direction TB
        L4a["Read ClientHello"] --> L4b["Extract SNI + JA4"]
        L4b --> L4c["Match rule"]
        L4c --> L4d["Relay raw bytes"]
    end

    style L7 fill:#ffe6e6,stroke:#e53e3e
    style L4 fill:#e6f9e6,stroke:#38a169
```

| | Layer 7 | Layer 4 (Schmutz) |
|:---|:---|:---|
| Sees plaintext? | Yes | No |
| Holds certificates? | Yes | No |
| Knows backends? | Yes | No |
| Protocol-specific? | Yes (HTTP, gRPC, etc.) | No (any TLS) |
| State per connection? | High | Minimal |

---

## The Classification Pipeline

```mermaid
flowchart TD
    A["TCP Accept"] --> B["Read ClientHello\n(with timeout)"]
    B --> C{Valid hello?}
    C -->|"No hello\nwithin timeout"| D["🚫 DROP\n(port scanner)"]
    C -->|"Empty SNI"| E["🚫 DROP\n(scanner or\nmisconfigured)"]
    C -->|"Valid"| F["Calculate JA4\nfingerprint"]
    F --> G["Walk rules\ntop-down"]
    G --> H{Match?}
    H -->|"JA4 match"| I["Execute action\n(usually DROP)"]
    H -->|"SNI match"| J["Dial Ziti service"]
    H -->|"Wildcard match"| K["Dial catch-all"]
    H -->|"No match"| L["Close"]
    J --> M["🔄 Relay bytes\nbidirectionally"]
    K --> M

    style D fill:#ffe6e6,stroke:#e53e3e
    style E fill:#ffe6e6,stroke:#e53e3e
    style I fill:#ffe6e6,stroke:#e53e3e
    style L fill:#f5f5f5,stroke:#999
    style M fill:#e6f9e6,stroke:#38a169
```

---

## JA4 Fingerprinting

JA4 is a method for fingerprinting TLS client implementations. It hashes:

```mermaid
flowchart LR
    subgraph hello["TLS ClientHello"]
        V["TLS version"]
        C["Cipher suites\n(sorted)"]
        E["Extensions\n(sorted)"]
        S["Signature algorithms"]
        G["Supported groups"]
    end

    hello --> Hash["SHA-256\ntruncated"]
    Hash --> FP["JA4 Fingerprint\nt13d191000_9dc..._e7c2..."]

    style hello fill:#f0f4ff,stroke:#4a86c8
    style FP fill:#f5f0ff,stroke:#7c3aed
```

The resulting fingerprint identifies the **TLS library**, not the user's claim.

| Client | JA4 Fingerprint | Notes |
|:---|:---|:---|
| Chrome 124 | `t13d1517h2_...` | Unique to Chrome's BoringSSL |
| Firefox 125 | `t13d1516h2_...` | Unique to Firefox's NSS |
| curl | `t13d191000_...` | OpenSSL-based |
| Python requests | `t13d201100_...` | urllib3 / OpenSSL |
| zgrab2 | `t13d191000_9dc...` | Scanner — caught |
| masscan | `t13d301000_4bf...` | Scanner — caught |

A bot can fake a User-Agent header. It can't fake its TLS library.

Reference: [JA4+ by FoxIO](https://github.com/FoxIO-LLC/ja4)

---

## SNI Routing

After passing the JA4 check, the SNI determines the destination:

```yaml
rules:
  - name: auth
    sni: "auth.example.com"
    service: auth-provider

  - name: shares
    sni: "*.share.example.com"
    service: share-frontend

  - name: catch-all
    sni: "*"
    service: default-ingress
```

Each service name maps to a Ziti service. The Ziti controller handles
routing — finding who binds the service, computing the path through the
router mesh, and establishing the circuit.

---

## The Relay

Once classification succeeds and a Ziti connection is established, Schmutz
becomes a dumb pipe:

```mermaid
flowchart LR
    Client["Client"] -->|"encrypted bytes"| S["Schmutz"]
    S -->|"same bytes"| Ziti["Ziti Circuit"]
    Ziti -->|"response bytes"| S
    S -->|"same bytes"| Client

    style S fill:#fff3e6,stroke:#f58220
```

```go
// Simplified — actual code handles errors, timeouts, HP accounting
go io.Copy(zitiConn, clientConn)
go io.Copy(clientConn, zitiConn)
```

Raw bytes in, raw bytes out. Schmutz never decrypts, inspects, or modifies
the TLS stream.

---

## The HP System

HP (Health Points) is an adaptive defense mechanism. Every Schmutz node
maintains an HP pool (0–1000, persisted in BoltDB).

### How HP changes

```mermaid
flowchart TD
    subgraph heals["💚 Heals HP"]
        H1["+1 Successful relay"]
        H2["+1 per 10s passive regen"]
    end

    subgraph drains["❤️ Drains HP"]
        D1["-1 Rule-matched drop"]
        D2["-2 No ClientHello (timeout)"]
        D3["-3 Malformed ClientHello"]
        D4["-5 Failed Ziti dial"]
    end

    heals --> HP["HP Pool\n0 — 1000"]
    drains --> HP

    style heals fill:#e6f9e6,stroke:#38a169
    style drains fill:#ffe6e6,stroke:#e53e3e
    style HP fill:#f0f4ff,stroke:#4a86c8
```

### How HP affects behavior

```mermaid
flowchart LR
    G["🟢 Green\n> 750 HP\nAll rules\nevaluated"] --> Y["🟡 Yellow\n500–750 HP\nRate limits\nhalved"]
    Y --> O["🟠 Orange\n250–500 HP\nUnknown JA4s\ndropped"]
    O --> R["🔴 Red\n0–250 HP\nNamed rules only\nCatch-all drops"]
    R -.->|"Attack stops\nHP regenerates"| G

    style G fill:#c6f6d5,stroke:#38a169
    style Y fill:#fefcbf,stroke:#d69e2e
    style O fill:#feebc8,stroke:#dd6b20
    style R fill:#fed7d7,stroke:#e53e3e
```

The effect is organic: under normal load, the node is permissive. Under
attack, it progressively tightens. At zero, it's a wall. The operator
doesn't need to intervene — the node defends itself.

HP persists across restarts in BoltDB.

---

## State Management

Schmutz uses [bbolt](https://github.com/etcd-io/bbolt) for local state:

```mermaid
flowchart TD
    subgraph db["💾 BoltDB (single file)"]
        B1["fingerprints\nJA4 hash → first seen,\nlast seen, count"]
        B2["sni_stats\nSNI → hit count,\nlast seen"]
        B3["hp\nCurrent HP value,\nlast drain/heal events"]
        B4["rate\nSource IP → connection\ncount, window"]
    end

    style db fill:#f5f0ff,stroke:#7c3aed
```

| Bucket | Contents | Purpose |
|:---|:---|:---|
| `fingerprints` | JA4 hash → first seen, last seen, count | Track client diversity |
| `sni_stats` | SNI → hit count, last seen | Monitor domain popularity |
| `hp` | Current HP value, last events | Adaptive defense |
| `rate` | Source IP → count, window | Per-source rate limiting |

BoltDB is a single-file embedded database. No server process. No network.
Each edge node has its own, independent state file.

---

## What Schmutz Doesn't Do

```mermaid
flowchart LR
    subgraph does["✅ Schmutz does"]
        Y1["Read ClientHello"]
        Y2["Classify by SNI + JA4"]
        Y3["Dial Ziti services"]
        Y4["Relay raw bytes"]
        Y5["Track fingerprints"]
        Y6["Adapt via HP"]
    end

    subgraph doesnt["🚫 Schmutz does NOT"]
        N1["Terminate TLS"]
        N2["Hold certificates"]
        N3["Know backend IPs"]
        N4["Inspect HTTP"]
        N5["Share state"]
        N6["Authenticate users"]
    end

    style does fill:#e6f9e6,stroke:#38a169
    style doesnt fill:#ffe6e6,stroke:#e53e3e
```

These are features, not limitations. Every thing Schmutz doesn't do is
a thing that can't be compromised on the edge.
