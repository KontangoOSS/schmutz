# Ziti Dial Lifecycle

[← Advanced Reference](../README.md)

---

When Schmutz classifies a connection and determines it should be routed to
a Ziti service, it calls `zitiCtx.Dial(serviceName)`. This page covers the
full dial sequence, route computation, error handling, and reconnection.

---

## The Dial Sequence

```mermaid
sequenceDiagram
    participant S as Schmutz
    participant SDK as Ziti SDK
    participant C as Ziti Controller
    participant R1 as Router A
    participant R2 as Router B
    participant T as Terminator

    S->>SDK: Dial("public-app")

    Note over SDK: Check local cache for<br/>service "public-app"

    SDK->>C: CreateSession("public-app", Dial)
    Note over C: Validate: does this identity<br/>have dial permission?
    C->>C: Look up terminators for<br/>"public-app"
    C->>C: Compute optimal route<br/>(cost-based, latency-aware)
    C-->>SDK: Session + circuit route<br/>[Router A -> Router B -> Terminator]

    SDK->>R1: CreateCircuit (mTLS)
    R1->>R2: Extend circuit (fabric link)
    R2->>T: Extend to terminator
    T-->>SDK: Circuit established

    SDK-->>S: net.Conn (Ziti circuit)

    Note over S,T: Schmutz now has a net.Conn<br/>that tunnels through the overlay

    S->>SDK: Write(clientHelloBytes)
    SDK->>R1: Encrypted relay
    R1->>R2: Forward
    R2->>T: Forward
    T->>T: Deliver to bound service

    T-->>S: Response bytes (reverse path)
```

---

## Key Properties

1. **Schmutz never knows the route.** The controller computes it. Schmutz
   gets back a `net.Conn` and writes bytes into it.

2. **The controller validates permissions.** If the identity does not have
   dial access to the requested service, the dial fails immediately.

3. **Route computation is dynamic.** The controller picks the lowest-cost
   path through the router mesh. If a router goes down, the next dial
   gets a different route.

4. **The circuit is mTLS end-to-end.** Every hop is encrypted. The
   ClientHello bytes from the original client are encrypted again inside
   the Ziti circuit -- double-wrapped TLS.

---

## The Ziti Service Model

Schmutz routes by service name, not by IP address. This is a fundamental
property of the overlay.

```mermaid
flowchart TD
    subgraph schmutz["Schmutz (edge)"]
        S1["Knows: service name\n'public-app'"]
        S2["Does NOT know:\n- Backend IP\n- Backend port\n- Backend location\n- Number of backends"]
    end

    subgraph controller["Controller"]
        C1["Service: 'public-app'"]
        C2["Terminators:\n- tunneler-a (cost 10)\n- tunneler-b (cost 20)"]
        C3["Picks lowest-cost\nterminator"]
    end

    subgraph backends["Interior"]
        B1["tunneler-a\n(binds 'public-app')\nForwards to 10.x.x.5:443"]
        B2["tunneler-b\n(binds 'public-app')\nForwards to 10.y.y.5:443"]
    end

    schmutz -->|"Dial('public-app')"| controller
    controller -->|"Route to best\nterminator"| backends

    style schmutz fill:#fff8e6,stroke:#f58220
    style controller fill:#f0f4ff,stroke:#4a86c8
    style backends fill:#e6f9e6,stroke:#38a169
```

A service can have multiple terminators in different regions. The controller
routes to the best one based on cost metrics. If `tunneler-a` goes down,
subsequent dials automatically route to `tunneler-b`. Schmutz does not need
to know about this failover -- it just dials the same service name.

---

## Error Handling

When `zitiCtx.Dial(serviceName)` fails, Schmutz:

1. Increments the `conn_dial_failed` counter in BoltDB
2. Records the failure in the HP system (`RecordDialFail()`, -1.0 HP)
3. Logs the error with full context (service name, source IP, SNI, JA4)
4. Closes the client connection (no error response sent)

```go
zitiConn, err := zitiCtx.Dial(result.Service)
if err != nil {
    db.IncrStat("conn_dial_failed")
    hp.RecordDialFail()
    connLogger.Error("dial failed",
        "service", result.Service,
        "error", err,
    )
    return  // defer closes client conn
}
```

Common dial failure causes: service does not exist (typo in config), no
available terminators (backend down), policy denies access (identity
revoked), controller unreachable (network partition), or context timeout.

The Ziti SDK manages its own reconnection. If a controller becomes
unreachable, it tries the next in the `ztAPIs` list. Schmutz's
`ReadTimeout` applies only to the initial ClientHello read, not the dial.

---

## Full Connection Lifecycle

From TCP accept to relay completion:

```mermaid
flowchart TD
    ACC["TCP Accept"] --> LIM{Active conns\n< limit?}
    LIM -->|No| REJ["Reject\nconn_rejected_limit++"]
    LIM -->|Yes| PEEK["Peek ClientHello\n(10s timeout)"]
    PEEK --> VCH{Valid\nClientHello?}
    VCH -->|No| BAD["Drop\nconn_bad_clienthello++\nRecordBadHello()"]
    VCH -->|Yes| JA4["Compute JA4"]
    JA4 --> CLS["Classify:\nSNI + JA4 + srcIP"]
    CLS --> REC["Record JA4 + SNI\nin BoltDB"]
    REC --> HPD{HP drop\npolicies?}
    HPD -->|"Red + catch-all"| SHED["Shed to drop\nhp-red-catchall-shed"]
    HPD -->|Pass| ACT{Action?}
    ACT -->|drop| DROP["Drop\nconn_dropped++\nRecordDrop()"]
    ACT -->|route| RL{Rate limit\ncheck?}
    RL -->|Exceeded| RLIM["Drop\nconn_rate_limited++\nRecordRateLimit()"]
    RL -->|OK| DIAL["zitiCtx.Dial(service)"]
    DIAL --> DRES{Dial\nsucceeded?}
    DRES -->|No| DFAIL["Drop\nconn_dial_failed++\nRecordDialFail()"]
    DRES -->|Yes| ROUTE["conn_routed++\nRecordRoute()"]
    ROUTE --> RELAY["relay.Bidirectional(\nreplayConn, zitiConn)"]
    RELAY --> DONE["Log: completed\n+ duration + bytes"]
    SHED --> DROP

    style REJ fill:#ffe6e6,stroke:#e53e3e
    style BAD fill:#ffe6e6,stroke:#e53e3e
    style DROP fill:#ffe6e6,stroke:#e53e3e
    style RLIM fill:#ffe6e6,stroke:#e53e3e
    style DFAIL fill:#ffe6e6,stroke:#e53e3e
    style RELAY fill:#e6f9e6,stroke:#38a169
    style DONE fill:#e6f9e6,stroke:#38a169
    style DIAL fill:#f0f4ff,stroke:#4a86c8
    style JA4 fill:#f5f0ff,stroke:#7c3aed
```

Every path results in either a successful relay or connection close with a
specific counter increment and HP event. No state is leaked to the client
in any failure path -- the connection simply closes.

---

## Design Rationale

**Why the SDK instead of a tunnel?** Schmutz classifies each connection
individually and dials different services based on SNI and JA4. The SDK
gives programmatic access to the dial operation that a transparent tunnel
cannot provide.

**Why dial per connection?** Ziti circuits are lightweight (<10ms within a
region). Per-connection circuits get the optimal route at dial time and
automatically fail over if a terminator goes down.

**Why no error response to client?** Any information returned to the client
is information an attacker can use. A closed socket is indistinguishable
from a drop.
