# schmutz — CLAUDE.md

## What schmutz is

Schmutz is the universal machine agent for the Kontango overlay network. It runs on any machine — controller, workstation, server, sidecar — and makes that machine a first-class participant in the Ziti overlay. It is the first thing that runs on any new or re-deployed node. Everything else depends on it completing successfully.

## What schmutz does

Schmutz runs in two phases:

### Phase 1 — Enrollment

The machine has no Ziti identity yet. Schmutz contacts the controller over plain HTTPS and proves the device is real:

1. `POST /enroll` — sends hostname, OS, arch, platform, fingerprint, agent_data
2. Controller opens a 60-second telemetry window, returns `stream_token` + `node_url`
3. Agent streams metrics to `POST /stream` every 5s for the window duration
4. Agent retries `POST /enroll` on the pinned `node_url` after the window
5. Controller runs the decision engine — approves or rejects
6. On approval: controller issues a Ziti OTT (JWT). Agent enrolls the Ziti identity and writes the identity file to disk.

The `node_url` pins all requests to the specific controller that opened the window — enrollment windows are in-memory per node, not shared across the raft cluster.

### Phase 2 — Post-Approval Bootstrap

After the Ziti identity is enrolled, schmutz configures the machine so it is a stable, consistent member of the overlay. This is the most important phase — it must always succeed and always produce the same result regardless of whether this is a fresh install or a re-run on an existing node.

**The post-approval bootstrap does the following, in order:**

1. **Start the Ziti tunnel** — `ziti-edge-tunnel` in `run` mode using the enrolled identity. The tunnel provides the `ziti0` interface and the Ziti DNS resolver at `100.64.0.2`.

2. **Configure DNS** — systemd-resolved is configured so that:
   - `.tango` and `.zone` queries route to the Ziti DNS resolver (`100.64.0.2`) on the `ziti0` interface only
   - `DefaultRoute=no` on `ziti0` — Ziti DNS is NOT a default resolver; it only answers for its two domains
   - All other queries go to the real upstream resolvers (DO resolvers, or whatever the host had before)
   - The dnsmasq shim (`127.0.0.2`) is removed — systemd-resolved handles scoping natively

3. **Bind the four dark services** via Ziti. These services have no public ports — they are reachable only through the Ziti overlay:
   - `api.<node>.zone` — the schmutz agent API (health, config, commands)
   - `monitor.<node>.zone` — metrics/health endpoint (Prometheus-compatible)
   - `ssh.<node>.zone` — SSH access to this node
   - `tls.<node>.zone` — TLS endpoint (Caddy, or raw TLS termination)

   `<node>` is the enrollment slug or hostname assigned during enrollment. These names resolve only inside the Ziti overlay — they are dark to the public internet.

4. **Configure the Ziti edge router** (if this is a controller node) — TPROXY mode so inbound public traffic on `:80`/`:443` is intercepted by Ziti and forwarded to Caddy locally on `127.0.0.1:8080`/`127.0.0.1:8443`. This is only run on nodes with the `controller` role attribute on their Ziti identity.

5. **Write systemd units** for all of the above and enable them. All units must have `Restart=always` and `StartLimitIntervalSec=0`.

## DNS architecture

Two overlay DNS domains:

| Domain | Purpose |
|--------|---------|
| `.tango` | Infrastructure services — `influxdb.tango`, `auth.tango`, `ctrl-1.tango`, etc. |
| `.zone` | Node-scoped dark services — `api.<node>.zone`, `ssh.<node>.zone`, etc. |

Both resolve only inside the Ziti overlay. Neither is reachable from the public internet. systemd-resolved routes both to `100.64.0.2` scoped to `ziti0`, with `DefaultRoute=no`.

## Overlay DNS IP range

`100.64.0.1/10` — Ziti assigns virtual IPs for services from this range. The DNS resolver is at `100.64.0.2`.

## The four dark services (minimum viable node)

Every node enrolled by schmutz must bind these four services. They are the minimum for the node to be operable remotely:

| Service name | Protocol | What it is |
|---|---|---|
| `api.<node>.zone` | HTTP | Schmutz agent API — health, status, config push |
| `monitor.<node>.zone` | HTTP | Prometheus-compatible metrics scrape endpoint |
| `ssh.<node>.zone` | TCP | SSH — forwarded to local `sshd` on port 22 |
| `tls.<node>.zone` | TCP | TLS ingress — forwarded to Caddy on `127.0.0.1:8443` |

All four are Ziti `bind` services. The schmutz agent hosts them. They have no open ports on the host network interface.

## Roles and tiers

The Ziti identity created during enrollment has role attributes that control what the node can access:

| Tier | When assigned | Access |
|------|---------------|--------|
| `sandbox` | Trusted IP (known controller IPs) | Broader overlay access |
| `quarantine` | Unknown device that passed the decision engine | Minimal access |

Roles are set by the controller at enrollment time. Schmutz does not set them — it only reads the tier from the enrollment response and stores it for reference.

## Node pinning during enrollment

Enrollment windows are in-memory on each controller node and not replicated across the raft cluster. The `node_url` in the first `/enroll` response pins all subsequent `/enroll` retries and `/stream` POSTs to the specific controller that opened the window. Schmutz must respect this pinning — sending retries to the load balancer URL will fail.

## Device fingerprint

`SHA256(/etc/machine-id)` as lowercase hex. Stable identifier. Used by the controller to detect duplicates and restore previously approved identities. Known devices skip the telemetry window on re-enrollment.

## Idempotency requirement

Every step in the post-approval bootstrap must be safe to re-run. If schmutz is run again on an already-enrolled node, it must detect existing state and skip or overwrite cleanly. It must never leave the node in a broken intermediate state.

## File locations

| File | Location |
|------|---------|
| Enrolled Ziti identity | `/opt/schmutz/identity.json` (root) or `~/.schmutz/identity.json` (non-root) |
| Ziti binary | Downloaded to `/opt/tango/bin/ziti` or `~/.schmutz/bin/ziti` |
| Systemd units written by schmutz | `/etc/systemd/system/schmutz-*.service` |

## What schmutz is NOT

- Schmutz is not the Ziti controller — the controller is a separate service (`kontango-controller`)
- Schmutz is not the Caddy ingress — Caddy runs separately; schmutz only binds the `tls.<node>.zone` TCP service that points to Caddy
- Schmutz does not manage Bao — Bao secrets are consumed by the controller, not the agent
- Schmutz does not run on DO controllers as a device — DO controllers are enrolled as controller-tier nodes, not as managed devices

## Build

```bash
cd src
go build -o ../bin/schmutz ./cmd/schmutz
```

## Commands

```bash
schmutz join --url https://ctrl.konoss.org --slug <slug>   # Enroll and bootstrap
schmutz agent                                               # Run the agent (after enrollment)
schmutz run                                                 # Run as edge gateway (controller nodes)
schmutz bootstrap                                           # Re-run post-approval bootstrap only
```

## SDK rules — non-negotiable

- **Only the Go SDK.** Use `github.com/openziti/sdk-golang` for all Ziti integration. No C SDK (`ziti-sdk-c`), no cgo, no `ziti` binary subprocess for tunnel operations.
- **Current version:** `github.com/openziti/sdk-golang v1.7.0` — compatible with OpenZiti v2.0.0-pre11+. Update this comment when bumping.
- **No OIDC on machine identities.** Machine identities use cert-only auth (set by the controller at enrollment). Never add OIDC flows to the agent.
- **No `ziti` binary subprocess for tunneling.** `exec.Command("ziti", "tunnel", ...)` is forbidden. The Go SDK provides `ctx.Listen()` and `ctx.Dial()` — use them.
- **`EnrollJWT` may use the Go SDK's `Enroll()` function** — not the CLI. The `ziti edge enroll` subprocess is also forbidden.

## Certificate separation — non-negotiable

Three cert types exist in this system. They must never be mixed:

| Type | Issuer | Used for | Lives in |
|------|--------|----------|----------|
| Ziti identity cert | Ziti controller intermediate CA (NetFoundry PKI) | Overlay authentication | `/etc/schmutz/identity.json` |
| LE/public TLS cert | Let's Encrypt | Public HTTPS (Caddy) | Caddy cert store |
| SPIFFE URI | Embedded in Ziti cert by controller PKI | Internal overlay routing only | Inside `identity.json` |

- Never use the Bao/Kontango system CA to issue Ziti identity certs.
- Never use Ziti identity certs for public TLS.
- SPIFFE URIs are internal-only — never expose them externally or use them for public routing.
- The `validateIdentityCA()` check in `enroll.go` enforces this at runtime — do not remove it.

## Enrollment protocol — do not change

The SSE call-and-response protocol in `internal/enroll/enroll.go` (`Register()`) is the contract between the agent and the controller. Changes to this protocol require coordinated changes in `schmutz-controller`. The protocol:

1. Agent POSTs device info to `POST /api/enroll/stream` with `Accept: text/event-stream`
2. Controller streams: `verify` → `decision` → `progress` → `identity`
3. `identity` event contains fully enrolled identity JSON — write directly to disk
4. Service names in `identity.services` drive which Ziti services the agent binds

Do not add JWT exchange steps, polling loops, or additional HTTP roundtrips to enrollment.
