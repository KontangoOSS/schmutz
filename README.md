# schmutz

Schmutz is the device enrollment agent for the Kontango overlay network. It runs on a machine that wants to join the network, collects system metrics to prove it's a real device, and retrieves a Ziti identity that gives it access to the overlay.

## How It Works

Enrollment is a two-pass process:

1. **First contact** — The agent POSTs basic system info to `POST /enroll` on the controller. The controller opens a 60-second telemetry window and returns a `stream_token` and `node_url`.

2. **Telemetry stream** — The agent sends system metrics (CPU, memory, load) to `POST /stream` every 5 seconds for the duration of the window. The `stream_token` authenticates each submission. The `node_url` pins all requests to the specific controller node that opened the window (enrollment windows are in-memory per-node, not shared across the cluster).

3. **Second contact** — After the window closes, the agent retries `POST /enroll` on the pinned node. The controller runs the decision engine against the collected metrics and either approves or rejects the device.

4. **Approved** — The controller creates a Ziti identity, assigns it a tier (`sandbox` or `quarantine`), and returns an OTT (one-time token) JWT. The agent uses this to complete Ziti enrollment and get a signed certificate for overlay access.

```
Agent                          Controller
  |                                |
  |-- POST /enroll --------------->|  (hostname, os, arch, fingerprint)
  |<-- pending (token, node_url) --|
  |                                |
  |-- POST /stream (every 5s) --->|  (metrics + stream_token)
  |<-- 204 ------------------------|
  |  ... (60 seconds) ...          |
  |-- POST /enroll (retry) ------->|  (pinned to original node)
  |<-- approved (JWT, tier) -------|
  |                                |
  |  [enroll Ziti identity]        |
```

## Device Fingerprint

The fingerprint is a SHA256 hash of `/etc/machine-id`. It's used as the stable identifier for a device across enrollment attempts. Known devices (previously approved) skip the telemetry window and get instant re-enrollment.

## Tiers

| Tier | When | Access |
|------|------|--------|
| `sandbox` | Trusted IP (known controller IPs) | Broader overlay access |
| `quarantine` | Unknown device, passed decision engine | Minimal overlay access |

Tier maps to Ziti role attributes on the identity, which controls which services the device can reach.

## Decision Engine

The controller scores devices on:

| Check | Weight | When scored |
|-------|--------|-------------|
| Trusted | 30 | Client IP is a known trusted address |
| Duplicate | 40 | Fingerprint already has a Ziti identity |
| Sysinfo | 30 | System info looks legitimate |
| Stream | 50 | Device streamed metrics (60+ for full score) |

Score is normalized to 100. Devices scoring ≥50 are approved. Trusted devices are approved regardless of score.

## Building

```bash
cd src
go build -o ../bin/schmutz ./cmd/schmutz
```

## Running

```bash
schmutz enroll --domain ctrl.konoss.org
```

The agent runs the full enrollment flow and exits when complete. A separate service uses the enrolled identity for overlay connectivity.

## Notes

- Stream traffic runs over plain HTTPS during enrollment — the device has no Ziti identity yet.
- The `/stream` endpoint accepts any JSON with `fingerprint` and `stream_token`. Extra fields are stored and passed to the decision engine.
- A custom `id` field in the enrollment payload allows device attribution (e.g. a user ID or asset tag).
