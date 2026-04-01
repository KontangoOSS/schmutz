# Rate Limit Windows

[← Advanced Reference](../README.md)

---

The `rate_limit` bucket implements sliding-window rate limiting per source
IP. Each window is a fixed time interval, and each source IP gets an
independent counter per window.

---

## Key Format

Keys follow the pattern `{srcIP}/{windowEpoch}`, where `windowEpoch` is
computed by integer division of the current Unix timestamp by the window
size in seconds.

```mermaid
flowchart LR
    subgraph key["Key Construction"]
        direction TB
        K1["Source IP: 192.0.2.50"]
        K2["Window: 60 seconds"]
        K3["Unix time: 1743350400"]
        K4["Epoch: 1743350400 / 60 = 29055840"]
        K5["Key: 192.0.2.50/29055840"]
        K1 --> K3 --> K4 --> K5
    end

    subgraph val["Value"]
        direction TB
        V1["uint64 counter"]
        V2["big-endian 8 bytes"]
        V3["e.g., 0x0000000000000017 = 23 connections"]
        V1 --> V2 --> V3
    end

    style key fill:#f0f4ff,stroke:#4a86c8
    style val fill:#fff8e6,stroke:#f58220
```

---

## Epoch Calculation

The epoch groups all connections from a source IP within a time window
into a single counter. When the window rolls over, a new key is created.

```mermaid
flowchart LR
    subgraph timeline["Time Windows (60s rate limit)"]
        direction LR
        W1["Epoch 29055839\n14:23:00 - 14:23:59\n(old, eligible for cleanup)"]
        W2["Epoch 29055840\n14:24:00 - 14:24:59\n(current window)"]
        W3["Epoch 29055841\n14:25:00 - 14:25:59\n(future)"]
    end

    style W1 fill:#f5f5f5,stroke:#999
    style W2 fill:#e6f9e6,stroke:#38a169
    style W3 fill:#f0f4ff,stroke:#4a86c8
```

---

## Counter Increment

Values are `uint64` counters stored as big-endian 8 bytes. On each
connection from a source IP:

1. Read the current counter for the key (0 if absent)
2. If the counter already equals or exceeds the limit, deny
3. Otherwise, increment and write back

---

## CheckRateLimit Flow

```mermaid
flowchart TD
    Start["CheckRateLimit(srcIP, windowSec, maxCount)"] --> Compute["windowEpoch = now / windowSec\nkey = srcIP/windowEpoch"]
    Compute --> Read["Read counter from key"]
    Read --> Check{"counter >= maxCount?"}
    Check -->|"yes"| Deny["return false (rate limited)"]
    Check -->|"no"| Incr["counter++\nwrite back"]
    Incr --> Cleanup["Scan srcIP/* keys\ndelete old epochs"]
    Cleanup --> Allow["return true (allowed)"]

    style Deny fill:#ffe6e6,stroke:#e53e3e
    style Allow fill:#e6f9e6,stroke:#38a169
```

The full sequence inside a single write transaction:

1. Compute `windowEpoch = now / windowSec`
2. Build key `"{srcIP}/{windowEpoch}"`
3. Read current counter (0 if absent)
4. If counter >= maxCount, return `allowed = false`
5. Increment counter, write back
6. Clean up old windows for this srcIP (best effort)

---

## Old Window Cleanup

After incrementing, the function scans all keys with the same srcIP
prefix. Any key whose epoch, multiplied by `windowSec`, falls before the
current window start (`now - windowSec`) is deleted. This runs inside the
same write transaction.

This piggyback cleanup keeps the bucket small without requiring a
separate garbage collection goroutine.

---

## Transaction Semantics

`CheckRateLimit` uses a **write transaction** (`db.Update`), not a read
transaction. This is because it atomically reads, increments, and cleans
up in a single transaction. BoltDB's single-writer lock means rate limit
checks are serialized -- one check completes before the next begins.

At very high connection rates, this serialization can become a bottleneck.
See [Bottlenecks](../ops/bottlenecks.md) for mitigation strategies.
