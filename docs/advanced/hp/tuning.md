# HP Tuning Guide

[← Advanced Reference](../README.md)

---

All HP parameters are configurable via the YAML config file. This page
covers every parameter, its default value, and guidance on when and how
to adjust it.

---

## Config Struct

```go
type Config struct {
    MaxHP         float64  // 1000.0  — pool ceiling
    RegenRate     float64  // 1.0     — HP/sec passive recovery
    RouteReward   float64  // 0.5     — HP gained per successful route
    DropCost      float64  // 2.0     — HP lost per rule-matched drop
    DialFailCost  float64  // 1.0     — HP lost per failed Ziti dial
    BadHelloCost  float64  // 5.0     — HP lost per malformed ClientHello
    RateLimitCost float64  // 3.0     — HP lost per rate-limited connection
    FloodCost     float64  // 0.5     — base connection cost (multiplied by level)
    PersistSec    int      // 10      — BoltDB write interval
}
```

```yaml
health:
  max_hp: 1000
  regen_rate: 1.0
  route_reward: 0.5
  drop_cost: 2.0
  dial_fail_cost: 1.0
  bad_hello_cost: 5.0
  rate_limit_cost: 3.0
  flood_cost: 0.5
  persist_sec: 10
```

---

## Parameter Reference

### MaxHP (default: 1000.0)

The pool ceiling. Higher values mean the node absorbs more punishment
before changing behavior.

| Scenario | Recommendation |
|:---------|:---------------|
| High-traffic edge with frequent probes | Increase to 2000-5000 |
| Low-traffic internal service | Default (1000) is fine |
| Honeypot / canary node | Decrease to 100-500 for faster reaction |

The level thresholds (75%, 50%, 25%) are percentages, so they scale
automatically with MaxHP.

### RegenRate (default: 1.0 HP/sec)

Passive recovery speed. This is the only guaranteed healing -- it ticks
regardless of traffic.

| Scenario | Recommendation |
|:---------|:---------------|
| Want faster recovery after attacks | Increase to 2.0-5.0 |
| Want the node to stay defensive longer | Decrease to 0.5 |
| Recovery time from 0 to full | `MaxHP / RegenRate` seconds |

At default values: 1000 / 1.0 = 1000 seconds (~16.7 minutes) for full
recovery.

### RouteReward (default: 0.5)

HP gained per successful route. Represents the "positive signal" of
legitimate traffic.

| Scenario | Recommendation |
|:---------|:---------------|
| High confidence in traffic legitimacy | Increase to 1.0-2.0 |
| Default (most deployments) | 0.5 |

Note: at Yellow, RouteReward equals ConnectionCost (break even). Increasing
RouteReward means legitimate traffic heals the node even at Yellow.

### DropCost (default: 2.0)

HP lost when a rule-matched drop occurs. This is a confirmed bad
connection that matched an explicit drop rule.

| Scenario | Recommendation |
|:---------|:---------------|
| Default | 2.0 |
| Many false-positive drops expected | Decrease to 1.0 |

### DialFailCost (default: 1.0)

HP lost when a Ziti dial fails. This usually indicates a backend issue,
not an attack, so the cost is lower than other drain events.

### BadHelloCost (default: 5.0)

HP lost per malformed ClientHello. This is the heaviest per-event cost
because invalid TLS handshakes are the strongest signal of malicious
intent (scanners, protocol probes, banner grabs).

| Scenario | Recommendation |
|:---------|:---------------|
| Default | 5.0 |
| Exposed to heavy scanning (public IP) | Keep at 5.0 or increase |
| Behind a load balancer that health-checks with raw TCP | Decrease to 1.0 |

### RateLimitCost (default: 3.0)

HP lost per rate-limited connection. Hitting a rate limit means a source
IP is connecting faster than allowed -- suspicious but not conclusive.

### FloodCost (default: 0.5)

Base connection cost at Yellow. Multiplied by 3 at Orange and 10 at Red.
This is the "cost of doing business" during elevated threat levels.

| Level | Actual Cost |
|:------|:------------|
| Green | 0 |
| Yellow | FloodCost (0.5) |
| Orange | FloodCost x 3 (1.5) |
| Red | FloodCost x 10 (5.0) |

### PersistSec (default: 10)

How often HP is written to BoltDB. Also controls regen tick frequency.

| Scenario | Recommendation |
|:---------|:---------------|
| Default | 10 |
| Want less disk I/O | Increase to 30-60 |
| Want minimal HP loss on crash | Decrease to 5 |

On restart, HP is restored from the last persisted value. Maximum drift
is `PersistSec` seconds of regen.

---

## Traffic Pattern Recommendations

### Public edge node (default target)

Use defaults. The 1000 HP pool absorbs ~200 bad hellos before hitting
Red, and recovers in ~17 minutes. This is appropriate for nodes behind
DNS round-robin where other nodes can absorb traffic during recovery.

### High-traffic production edge

```yaml
health:
  max_hp: 5000
  regen_rate: 5.0
  bad_hello_cost: 5.0
```

Larger pool absorbs larger bursts. Faster regen recovers quicker.
Full recovery: 5000 / 5.0 = 1000 seconds (~17 minutes, same ratio).

### Honeypot / canary

```yaml
health:
  max_hp: 200
  regen_rate: 0.5
  bad_hello_cost: 10.0
```

Reacts fast (40 bad hellos to Red), stays defensive longer (400 seconds
to recover). Good for early-warning nodes that alert on level changes.

### Behind health-checking load balancer

```yaml
health:
  bad_hello_cost: 1.0
  dial_fail_cost: 0.5
```

Reduce costs for events that the load balancer triggers legitimately
(raw TCP health checks, backend-down scenarios).
