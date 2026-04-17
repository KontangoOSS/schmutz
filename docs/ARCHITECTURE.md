# Architecture

[← Back to README](../README.md)

---

## Package Layout

```
src/
├── cmd/schmutz/            # Entry point, CLI flags, command dispatch
├── internal/
│   ├── agent/              # Identity paths, heartbeat, telemetry, SSH key provisioning
│   ├── bootstrap/          # Phase 2 — post-approval bootstrap steps
│   │   ├── dns.go          # systemd-resolved configuration (.tango + .zone scoping)
│   │   ├── services.go     # Bind the four dark Ziti services
│   │   ├── tproxy.go       # TPROXY iptables rules (controller nodes only)
│   │   └── systemd.go      # Write and enable systemd units
│   ├── pipeline/           # Linear step runner — Context and Step interface
│   ├── register/           # Phase 1 — enrollment (POST /enroll + POST /stream)
│   └── detect/             # OS, arch, hostname, machine-id detection
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

`Context` carries state across steps. Steps check `Skip()` before running so already-complete work is idempotent.

### Phase 1 Steps (enrollment)

1. **Collect** — gathers hostname, OS, arch, platform, fingerprint
2. **Register** — two-pass enrollment flow: POST /enroll → stream → retry
3. **Enroll** — uses the JWT to enroll the Ziti identity via the Ziti SDK

### Phase 2 Steps (post-approval bootstrap)

4. **StartTunnel** — starts `ziti-edge-tunnel` with the enrolled identity; brings up `ziti0`
5. **ConfigureDNS** — writes systemd-resolved drop-in; scopes `.tango` and `.zone` to `ziti0` with `DefaultRoute=no`
6. **BindServices** — registers the four dark Ziti services (`api`, `monitor`, `ssh`, `tls`) as bind services on the overlay
7. **ConfigureTPROXY** — (controller nodes only) writes iptables TPROXY rules for `:80`/`:443`; skipped if the identity lacks the `controller` role attribute
8. **WriteUnits** — writes and enables all systemd units for tunnel, agent, and bound services

All Phase 2 steps are idempotent — re-running on an enrolled node converges state without breaking anything.

---

## Enrollment Flow (Register Step)

```
Agent                              Controller
  |                                    |
  |-- POST /enroll ------------------->|  hostname, os, arch, fingerprint, agent_data
  |<-- pending (stream_token, node_url)|
  |                                    |
  |-- POST /stream (every 5s) -------->|  metrics + stream_token  (pinned to node_url)
  |<-- 204 ----------------------------|
  |  ... 60 seconds ...               |
  |-- POST /enroll (retry) ----------->|  (pinned to node_url)
  |<-- approved (JWT, tier) -----------|
  |                                    |
  |  [ziti edge enroll -j <jwt>]       |
  |  [Phase 2 bootstrap]               |
```

### Node Pinning

Enrollment windows are in-memory on each controller node and not replicated across the raft cluster. The `node_url` in the pending response pins all subsequent retries and stream POSTs to the specific controller that opened the window. The load balancer is bypassed for the duration of enrollment.

---

## The Four Dark Services

Every enrolled node binds these Ziti services. No public ports are opened:

| Service name | Ziti type | Local target | Purpose |
|---|---|---|---|
| `api.<node>.zone` | bind | `127.0.0.1:9080` | Schmutz agent HTTP API |
| `monitor.<node>.zone` | bind | `127.0.0.1:9081` | Prometheus metrics endpoint |
| `ssh.<node>.zone` | bind (TCP) | `127.0.0.1:22` | SSH access |
| `tls.<node>.zone` | bind (TCP) | `127.0.0.1:8443` | TLS ingress (Caddy) |

`<node>` is the enrollment slug. These are dark — no DNS record exists outside the Ziti overlay, no public port is open.

---

## DNS Scoping

systemd-resolved drop-in written to `/etc/systemd/resolved.conf.d/ziti.conf`:

```ini
[Resolve]
DNS=100.64.0.2
Domains=~tango ~zone
```

And the `ziti0` interface link config written via `resolvectl`:

```
resolvectl dns ziti0 100.64.0.2
resolvectl domain ziti0 ~tango ~zone
resolvectl default-route ziti0 false
```

Result: `.tango` and `.zone` resolve through Ziti. Everything else goes to upstream resolvers. No dnsmasq. No `+DefaultRoute`.

---

## Fingerprint

`SHA256(/etc/machine-id)` as lowercase hex. Stable across reboots. Used by the controller to detect duplicates and restore previously approved identities. Known devices skip the telemetry window on re-enrollment.

---

## Identity File Locations

| Context | Path |
|---------|------|
| Running as root | `/opt/schmutz/identity.json` |
| Running as non-root | `~/.schmutz/identity.json` |
| Ziti binary | `/opt/tango/bin/ziti` (root) or `~/.schmutz/bin/ziti` (non-root) |
| Systemd units | `/etc/systemd/system/schmutz-*.service` |
