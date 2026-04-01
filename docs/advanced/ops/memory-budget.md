# Memory Budget

[← Advanced Reference](../README.md)

---

Schmutz is designed for low overhead per connection. This page covers the
memory cost of each component and scaling math for capacity planning.

---

## Per-Connection Memory

```mermaid
flowchart TD
    subgraph conn["Per-Connection Memory"]
        direction TB
        M1["replayConn buffer\n200-500 bytes\n(ClientHello size)"]
        M2["Ziti circuit state\n~2-4 KB\n(SDK internal)"]
        M3["io.Copy buffers\n2 x 32 KB = 64 KB\n(Go default)"]
        M4["Goroutine stacks\n3 x 8 KB = 24 KB\n(initial, grows as needed)"]
        M5["Total: ~90 KB per connection"]
    end

    style conn fill:#f0f4ff,stroke:#4a86c8
    style M5 fill:#fff8e6,stroke:#f58220
```

| Component | Size | Notes |
|:----------|:-----|:------|
| replayConn buffer | 200-500 bytes | Stores the raw ClientHello for replay to the Ziti circuit. Freed when the buffer is fully read |
| Ziti circuit state | ~2-4 KB | SDK-internal session, circuit ID, crypto state |
| io.Copy buffers | 2 x 32 KB | One per direction. Go's `io.Copy` allocates a 32 KB buffer internally |
| Goroutine stacks | 3 x 8 KB | Initial stack size. Grows on demand (up to 1 GB default max) |
| **Total** | **~90 KB** | Per active connection, steady state |

---

## Goroutine Model

```mermaid
flowchart TD
    subgraph main["Main Loop (1 goroutine)"]
        Accept["Accept TCP connections"]
    end

    subgraph perconn["Per Connection (3 goroutines)"]
        Handler["Handler goroutine\nPeek → Classify → Dial"]
        RelayIn["Relay: Client → Backend"]
        RelayOut["Relay: Backend → Client"]
        Handler --> RelayIn
        Handler --> RelayOut
    end

    subgraph bg["Background (2 goroutines)"]
        HPTick["HP Tick\n(every 10s)"]
        Signal["Signal Handler\n(SIGINT/SIGTERM)"]
    end

    Accept -->|"go handleConnection()"| Handler

    style main fill:#f0f4ff,stroke:#4a86c8
    style perconn fill:#e6f9e6,stroke:#38a169
    style bg fill:#f5f0ff,stroke:#7c3aed
```

**Per active connection**: 3 goroutines

1. **Handler**: accepts the connection, peeks the ClientHello, classifies,
   dials Ziti, then blocks on `wg.Wait()` until both relay goroutines finish
2. **Relay in** (`io.Copy(backend, client)`): copies bytes from client to
   Ziti circuit
3. **Relay out** (`io.Copy(client, backend)`): copies bytes from Ziti
   circuit back to client

**Background**: 2 goroutines total (not per connection)

- HP tick: fires every `PersistSec` seconds, applies passive regen, persists HP
- Signal handler: waits for SIGINT/SIGTERM

**Total goroutines** = 2 (background) + 1 (accept loop) + 3N (connections)

---

## Scaling Math

```mermaid
flowchart LR
    subgraph load["Concurrent Connections"]
        direction TB
        L1["100 conns\n~9 MB RAM\n~300 goroutines"]
        L2["1,000 conns\n~90 MB RAM\n~3,000 goroutines"]
        L3["10,000 conns\n~900 MB RAM\n~30,000 goroutines"]
        L4["50,000 conns\n~4.5 GB RAM\n~150,000 goroutines"]
    end

    style L1 fill:#e6f9e6,stroke:#38a169
    style L2 fill:#e6f9e6,stroke:#38a169
    style L3 fill:#fff8e6,stroke:#f58220
    style L4 fill:#ffe6e6,stroke:#e53e3e
```

At 10,000 concurrent connections (the default `MaxConnections`), this is
~900 MB of memory. The actual resident set will be lower because idle
connections with no data flowing have minimal stack usage.

At 10,000 connections: ~30,003 goroutines. Go's scheduler handles this
comfortably.

---

## Horizontal Scaling

```mermaid
flowchart TD
    subgraph horizontal["Horizontal Scaling"]
        direction LR
        DNS["DNS Round-Robin"] --> N1["Node 1"]
        DNS --> N2["Node 2"]
        DNS --> N3["Node 3"]
        DNS --> N4["Node N"]
    end

    subgraph independent["Each Node"]
        direction TB
        I1["Own BoltDB"]
        I2["Own HP pool"]
        I3["Own Ziti identity"]
        I4["Own rate limit counters"]
    end

    style horizontal fill:#f0f4ff,stroke:#4a86c8
    style independent fill:#e6f9e6,stroke:#38a169
```

**Nodes share nothing**. Adding capacity is: spin up a VM, install Schmutz,
add the IP to DNS. No cluster coordination. No state migration. No leader
election.

**Trade-off**: per-source rate limits are per-node. A client hitting 3 nodes
gets 3x the effective rate limit. This is acceptable because DNS round-robin
distributes reasonably evenly, rate limits are a safety net (not a precise
throttle), and the HP system provides a second line of defense.
