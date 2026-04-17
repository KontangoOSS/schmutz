# Design

[← Back to README](../README.md)

---

## Goals

- Enroll real machines into the Kontango overlay with minimal friction
- Give the controller enough signal to distinguish real devices from bots/scanners
- Keep the agent small and dependency-light — it runs as part of an installer
- Be safe to run on untrusted machines: no secrets on disk until enrollment completes
- After approval, bring any machine to a fully operational overlay state automatically and idempotently

## Two Phases

Schmutz has two distinct phases. Phase 1 runs before the machine has a Ziti identity. Phase 2 runs after. They are separated by controller approval.

---

## Phase 1 — Enrollment

### Two-Pass Protocol

A bot hitting `/enroll` once can fake system info. It's much harder to sustain 60 seconds of plausible real-time telemetry. The two-pass flow uses the telemetry window as a lightweight proof-of-work:

1. **First POST** — sends system info, opens a window, issues a `stream_token`
2. **Stream** — agent sends real metrics every 5 seconds for ~60 seconds
3. **Second POST** — triggers the decision engine against the collected data

The `stream_token` is a short-lived random credential that authenticates stream submissions to the window. It is never stored on disk — if the agent crashes during streaming, it restarts from the first POST and gets a new token.

### Why HTTPS, Not Ziti

During enrollment the device has no Ziti identity. Stream traffic goes over plain HTTPS to `/stream` on the controller. This is acceptable because:

- The stream token prevents unauthorized writes to the window
- The metrics themselves are not sensitive
- The window evicts after 90 seconds regardless

Once enrolled, a device has a Ziti identity and all future communication uses the overlay.

### Decision Engine

The controller scores devices against collected data:

| Checker | What it looks for |
|---------|-------------------|
| Trusted | Client IP in known trusted list |
| Duplicate | Fingerprint already enrolled |
| Sysinfo | Plausible hostname, OS, arch |
| Stream | Sufficient metrics received |

Stream scoring: 0 metrics → 0 pts, 1–4 → 10, 5–19 → 30, 20–59 → 40, 60+ → 50. An honest device streaming every 5 seconds over 60 seconds sends ~12 metrics (30 pts). Trusted devices are approved regardless of score.

### Tiers

Tier is assigned at enrollment and maps to Ziti role attributes on the identity:

- `sandbox` — trusted client IP; more permissive service access
- `quarantine` — unknown device that passed scoring; minimal service access

Tier is stored in Bao and used on re-enrollment to restore the same identity class.

### Future: Ziti-Bootstrap Path

Planned but not yet implemented. The controller would issue a throwaway Ziti identity with `dial-only` permission to a single service (`enrollment-stream.tango`). The agent would enroll this identity and stream metrics over Ziti. On approval, the throwaway identity is deleted and the real identity is issued, removing the HTTPS dependency entirely.

---

## Phase 2 — Post-Approval Bootstrap

This is what makes a machine a stable, operational member of the overlay. It runs immediately after enrollment completes and must always produce the same result — on a fresh install or a re-run on an existing node.

### Step 1 — Start the Ziti Tunnel

`ziti-edge-tunnel` starts in `run` mode using the enrolled identity file. This brings up the `ziti0` tun interface and starts the Ziti DNS resolver at `100.64.0.2`.

### Step 2 — Configure DNS

systemd-resolved is configured so that:

- `.tango` and `.zone` queries route to `100.64.0.2` on `ziti0` only
- `DefaultRoute=no` — Ziti DNS is never used for public name resolution
- All other queries go to the host's upstream resolvers unchanged
- The dnsmasq shim is removed — systemd-resolved handles scoping natively

This gives consistent, scoped overlay DNS on every node without interfering with public DNS.

### Step 3 — Bind the Four Dark Services

Every enrolled node binds these four Ziti services. They have no open ports on the host network — they are reachable only through the Ziti overlay:

| Service | Protocol | What it does |
|---------|----------|-------------|
| `api.<node>.zone` | HTTP | Schmutz agent API — health, status, config push |
| `monitor.<node>.zone` | HTTP | Prometheus-compatible metrics scrape endpoint |
| `ssh.<node>.zone` | TCP | SSH forwarded to local `sshd` on port 22 |
| `tls.<node>.zone` | TCP | TLS ingress forwarded to Caddy on `127.0.0.1:8443` |

`<node>` is the enrollment slug or hostname. These names resolve only inside the Ziti overlay. They are dark to the public internet.

### Step 4 — Configure TPROXY (Controller Nodes Only)

On nodes with the `controller` role attribute, schmutz additionally configures the Ziti edge router in TPROXY mode. iptables rules redirect inbound `:80` and `:443` to the Ziti router, which forwards them to Caddy on `127.0.0.1:8080` and `127.0.0.1:8443` via Ziti service policy. All public ingress passes through the Ziti fabric before reaching Caddy.

### Step 5 — Write and Enable Systemd Units

All tunnel, agent, and binding services are written as systemd units with `Restart=always` and `StartLimitIntervalSec=0`. The node must survive reboots and service restarts without manual intervention.

### Idempotency

Every step checks existing state before acting. Re-running schmutz on an enrolled node must be safe. It detects the existing identity, skips re-enrollment, and converges any configuration that has drifted.

---

## DNS Architecture

Two overlay DNS domains are used by this network:

| Domain | Purpose | Examples |
|--------|---------|---------|
| `.tango` | Shared infrastructure services | `influxdb.tango`, `auth.tango`, `ctrl-1.tango` |
| `.zone` | Node-scoped dark services | `api.ctrl-1.zone`, `ssh.ctrl-1.zone` |

Both domains resolve only inside the Ziti overlay via `100.64.0.2`, scoped to `ziti0` with `DefaultRoute=no`. Neither is reachable from the public internet.
