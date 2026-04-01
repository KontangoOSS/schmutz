# Bottleneck Analysis

[← Advanced Reference](../README.md)

---

Every connection passes through five stages. Each has a distinct cost
profile. This page identifies the bottlenecks and their mitigations.

---

## Connection Lifecycle Timing

```mermaid
flowchart LR
    A["Accept\n~0.01ms"] --> B["Peek\n1-10ms\n(read timeout bound)"]
    B --> C["Classify\n~0.05ms\n(CPU-bound)"]
    C --> D["Dial\n5-50ms\n(network RTT)"]
    D --> E["Relay\nseconds to hours\n(io.Copy)"]

    style A fill:#e6f9e6,stroke:#38a169
    style B fill:#fff8e6,stroke:#f58220
    style C fill:#e6f9e6,stroke:#38a169
    style D fill:#ffe6e6,stroke:#e53e3e
    style E fill:#f0f4ff,stroke:#4a86c8
```

```mermaid
gantt
    title Connection Lifecycle Timing (typical)
    dateFormat X
    axisFormat %L ms

    section Overhead
    TCP Accept           :a, 0, 1
    Read ClientHello     :b, 1, 5
    Parse + JA4          :c, 5, 6
    Rule Match           :d, 6, 7
    BoltDB Writes (4x)   :e, 7, 10
    Ziti Dial            :f, 10, 30

    section Relay
    Bidirectional Copy   :g, 30, 200
```

---

## Bottlenecks by Severity

```mermaid
flowchart TD
    subgraph bottlenecks["Bottlenecks by Severity"]
        direction TB
        B1["Ziti Dial (network RTT)\nImpact: HIGH\nEvery new connection pays this cost"]
        B2["BoltDB Single Writer\nImpact: MEDIUM\n4 write txns per connection serialize"]
        B3["File Descriptors\nImpact: HARD LIMIT\nulimit -n must exceed MaxConnections"]
        B4["Rule Matching\nImpact: LOW\nLinear scan, but rules are few"]
    end

    style B1 fill:#ffe6e6,stroke:#e53e3e
    style B2 fill:#fff8e6,stroke:#f58220
    style B3 fill:#ffe6e6,stroke:#e53e3e
    style B4 fill:#e6f9e6,stroke:#38a169
```

---

## Ziti Dial Latency

The dominant latency source. The Ziti SDK must contact the controller,
compute a route, and establish a circuit.

```mermaid
flowchart LR
    subgraph factors["Latency Factors"]
        direction TB
        F1["Controller RTT\n(5-30ms depending on region)"]
        F2["Route computation\n(<1ms for simple topologies)"]
        F3["Circuit setup\n(1-2 additional router hops)"]
        F4["SDK overhead\n(mTLS handshake, session caching)"]
    end

    style factors fill:#f0f4ff,stroke:#4a86c8
```

| Scenario | Typical Dial Latency |
|:---------|:--------------------|
| Controller on same host | 1-5 ms |
| Controller in same region | 5-15 ms |
| Controller cross-region | 15-50 ms |
| Controller under heavy load | 50-200 ms |
| Session already cached | 1-3 ms |

**Mitigations**:

- Deploy Ziti controllers close to edge nodes (same region)
- The Ziti SDK caches sessions; repeated dials to the same service are faster
- Consider multiple smaller services instead of one catch-all (better session reuse)

---

## BoltDB Single-Writer Lock

BoltDB allows only one write transaction at a time. All other write
transactions queue. Read transactions run concurrently.

**Per-connection write cost**: each connection triggers up to 4 write
transactions (JA4, SNI, rate limit, stat). At high connection rates
(>10,000 new connections/second), these serialize and become a bottleneck.

```mermaid
flowchart LR
    subgraph writes["Write Operations (serialized)"]
        direction TB
        W1["RecordJA4 — JSON marshal + Put"]
        W2["RecordSNI — JSON marshal + Put"]
        W3["CheckRateLimit — binary read + write + cleanup"]
        W4["IncrStat — binary read + write"]
        W5["HP persist — binary write (every 10s)"]
    end

    subgraph reads["Read Operations (concurrent)"]
        direction TB
        R1["ListJA4 — ForEach + JSON unmarshal"]
        R2["ListSNI — ForEach + JSON unmarshal"]
        R3["GetStat — binary read"]
    end

    style writes fill:#fff8e6,stroke:#f58220
    style reads fill:#e6f9e6,stroke:#38a169
```

**Mitigations**:

- Batch writes (future optimization -- combine JA4 + SNI + stat into one tx)
- Reduce writes: stats could use in-memory counters with periodic flush
- Rate limit records are the most write-heavy; consider in-memory rate limiter

---

## File Descriptor Limits

Each active connection consumes 2 file descriptors (client socket + Ziti
circuit). Plus BoltDB holds the database file open. Ensure:

```bash
# Check current limit
ulimit -n

# Set for the schmutz process (e.g., in systemd unit)
LimitNOFILE=65535
```

`MaxConnections` should be set to at most `(ulimit_n - 100) / 2` to leave
headroom for BoltDB, Ziti SDK, and logging.

---

## CPU Profile

Where CPU time is spent per connection, in approximate order:

```mermaid
flowchart TD
    subgraph cpu["CPU Cost Breakdown"]
        direction TB
        C1["TLS ClientHello parsing\n~5%\n(byte reads, no allocation)"]
        C2["JA4 computation\n~15%\n(sort + 2x SHA-256)"]
        C3["Rule matching\n~5%\n(linear scan, filepath.Match)"]
        C4["BoltDB writes\n~25%\n(serialized, fsync)"]
        C5["Ziti SDK\n~30%\n(mTLS, protobuf, session mgmt)"]
        C6["io.Copy relay\n~20%\n(syscalls, context switching)"]
    end

    style C1 fill:#e6f9e6,stroke:#38a169
    style C2 fill:#fff8e6,stroke:#f58220
    style C3 fill:#e6f9e6,stroke:#38a169
    style C4 fill:#fff8e6,stroke:#f58220
    style C5 fill:#ffe6e6,stroke:#e53e3e
    style C6 fill:#f0f4ff,stroke:#4a86c8
```

**JA4 computation**: sort cipher suites `O(n log n)` (n ~ 15-20), sort
extensions (same), two SHA-256 hashes of ~100 byte inputs. Total: ~2-5
microseconds.

**Rule matching**: linear scan `O(R)` where R is the number of rules.
`filepath.Match` allocates internally for glob patterns. `net.ParseCIDR`
allocates per call. For 20 rules: ~1-3 microseconds.

---

## Relay Throughput

Once the circuit is established, `io.Copy` with 32 KB buffers moves bytes
in a tight syscall loop. Throughput is bounded by Ziti circuit bandwidth
(100 Mbps - 1 Gbps) and kernel TCP buffers, not by Schmutz. CPU cost of
relay is near zero -- Schmutz does not inspect or modify the TLS stream.
