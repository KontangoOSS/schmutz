# HP Algorithm Overview

[← Advanced Reference](../README.md)

---

HP (Health Points) is Schmutz's adaptive defense mechanism. Every edge node
maintains a floating-point HP pool that rises with legitimate traffic and
falls under attack. As HP drains, the node progressively tightens its
acceptance policy -- no operator intervention required.

---

## HP Pool: The Core Abstraction

```mermaid
flowchart LR
    subgraph pool["HP Pool"]
        direction TB
        V["float64 value"]
        R["Range: 0.0 -- 1000.0"]
        P["Persisted to BoltDB\nevery 10 seconds"]
    end

    subgraph state["Current State"]
        HP["HP: 847.5"]
        PCT["Percent: 84.75%"]
        LVL["Level: Green"]
    end

    pool --> state

    style pool fill:#f0f4ff,stroke:#4a86c8
    style state fill:#e6f9e6,stroke:#38a169
```

The pool is protected by a `sync.RWMutex`. All modifications acquire
a write lock; reads (`HP()`, `Percent()`, `Level()`) acquire a read lock.
The pool starts at `MaxHP` (1000.0) on first run, or restores from BoltDB
on subsequent starts.

---

## The Four Levels

HP percentage maps to a defensive posture. Thresholds are fixed at 75%,
50%, and 25%.

```mermaid
stateDiagram-v2
    [*] --> Green
    Green --> Yellow: HP <= 75%
    Yellow --> Orange: HP <= 50%
    Orange --> Red: HP <= 25%
    Red --> Orange: HP > 25%
    Orange --> Yellow: HP > 50%
    Yellow --> Green: HP > 75%

    state Green {
        [*]: HP > 750
        note right of [*]: Normal operation\nAll rules evaluated\nFull rate limits
    }
    state Yellow {
        [*]: HP 500-750
        note right of [*]: Rate limits halved\nConnection cost applied
    }
    state Orange {
        [*]: HP 250-500
        note right of [*]: Unknown JA4s dropped\nRate limits quartered
    }
    state Red {
        [*]: HP 0-250
        note right of [*]: Catch-all traffic dropped\nRate limits at 10%\nNamed rules only
    }
```

| Level | HP Range | Percent | Behavior |
|:------|:---------|:--------|:---------|
| Green | 750-1000 | >75% | Normal operation. All configured rules apply as written |
| Yellow | 500-750 | 50-75% | Rate limits halved. Each connection costs 0.5 HP on top of event costs |
| Orange | 250-500 | 25-50% | Unknown JA4 fingerprints dropped. Connection cost triples |
| Red | 0-250 | 0-25% | Catch-all rules drop. Only explicitly named SNI rules pass |

```go
func (p *Pool) Level() Level {
    pct := p.Percent()
    switch {
    case pct > 75:  return Green
    case pct > 50:  return Yellow
    case pct > 25:  return Orange
    default:        return Red
    }
}
```

---

## ShouldDropUnknown and ShouldDropCatchAll

Two boolean methods control what gets shed as HP drops.

```mermaid
flowchart TD
    subgraph green["Green (>75%)"]
        G1["All traffic evaluated"]
        G2["Unknown JA4: allowed"]
        G3["Catch-all: allowed"]
    end

    subgraph yellow["Yellow (50-75%)"]
        Y1["All traffic evaluated"]
        Y2["Unknown JA4: allowed"]
        Y3["Catch-all: allowed"]
    end

    subgraph orange["Orange (25-50%)"]
        O1["ShouldDropUnknown: true"]
        O2["Unknown JA4: DROPPED"]
        O3["Catch-all: allowed"]
    end

    subgraph red["Red (0-25%)"]
        R1["ShouldDropUnknown: true"]
        R2["ShouldDropCatchAll: true"]
        R3["Only named rules pass"]
    end

    green --> yellow --> orange --> red

    style green fill:#e6f9e6,stroke:#38a169
    style yellow fill:#fff8e6,stroke:#f58220
    style orange fill:#fff8e6,stroke:#f58220
    style red fill:#ffe6e6,stroke:#e53e3e
```

```go
// Activates at Orange (>= 2) — unknown fingerprints are dropped
func (p *Pool) ShouldDropUnknown() bool {
    return p.Level() >= Orange
}

// Activates at Red (>= 3) — catch-all wildcard rules stop routing
func (p *Pool) ShouldDropCatchAll() bool {
    return p.Level() >= Red
}
```

In the gateway loop, catch-all shedding is applied directly:

```go
if result.Rule == "catch-all" && hp.ShouldDropCatchAll() {
    result.Action = "drop"
    result.Rule = "hp-red-catchall-shed"
}
```

---

## Design Rationale

**Why a single float64?** The HP pool is intentionally simple. One number
captures the node's overall health. No histograms, no percentiles, no
sliding windows. The simplicity makes the algorithm predictable and
debuggable.

**Why not share HP across nodes?** Each node's HP reflects its own
experience. A targeted attack on one node does not affect others. Sharing
HP would create a distributed state problem and a new attack vector (drain
one node to affect all).
