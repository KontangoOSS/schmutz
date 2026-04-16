# Architecture

[← Back to README](../README.md)

---

## Package Layout

```
src/
├── cmd/schmutz/        # Entry point, CLI flags
├── internal/
│   ├── agent/          # Version and user-agent constants
│   ├── pipeline/       # Bootstrap pipeline — Context and Step interface
│   └── register/       # Enrollment step (POST /enroll + POST /stream)
```

## Pipeline

Schmutz uses a linear bootstrap pipeline defined in `internal/pipeline`. Each step implements:

```go
type Step interface {
    Name() string
    Skip(ctx *Context) bool
    Run(ctx *Context) error
}
```

`Context` carries state across steps (hostname, OS, arch, domain, JWT, tier, etc.). Steps check `Skip()` before running so already-complete work is idempotent.

Current steps:
1. **Collect** — gathers hostname, OS, arch, platform
2. **Register** — runs the two-pass enrollment flow (see below)
3. **Enroll** — uses the JWT to enroll the Ziti identity via the Ziti SDK

## Enrollment Flow (Register Step)

The `register` package implements the core enrollment protocol:

```
1. POST /enroll
   - sends: hostname, os, arch, platform, fingerprint, agent_data
   - receives: status, stream_token, node_url, retry_after

2. If status == "pending":
   - start streamMetrics goroutine (every 5s)
   - wait retry_after seconds
   - retry POST /enroll on pinned node_url

3. If status == "approved":
   - store JWT and tier in pipeline.Context
   - continue to Enroll step
```

## Fingerprint

The device fingerprint is `SHA256(/etc/machine-id)` returned as a lowercase hex string. It's the stable device identifier used by the controller to track enrollment state and prevent duplicates.

## Stream Authentication

When the controller issues a pending response, it includes a `stream_token` — a 32-byte random hex string tied to the enrollment window. Every `/stream` POST must include both `fingerprint` and `stream_token`. The controller validates the token against the window before accepting metrics.

## Node Pinning

Enrollment windows are in-memory on each controller node and not shared across the cluster. The `node_url` in the pending response tells the agent which specific controller opened its window. All subsequent `/enroll` retries and `/stream` POSTs go directly to that node, bypassing the load balancer.
