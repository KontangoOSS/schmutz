# schmutz tech debt — known issues and deferred work

This is the live list of things we know are imperfect but chose to ship anyway.
Each entry should explain *why* we deferred so the next person to look at it
understands the trade-off.

## Bootstrap requires non-443 outbound (high impact, deferred)

**The problem.** A fresh schmutz install needs three TLS ports outbound
to the controller plane:

| Port | Purpose | When |
|---|---|---|
| 443 | Caddy: install page, binary download, `/api/enroll/stream` SSE | Bootstrap |
| 1280 | Ziti edge controller API (`/edge/client/v1/*`) | OTT consumption + steady-state SDK |
| 3022/3023 | Ziti router fabric/edge listeners | Steady-state overlay traffic |

Most networks allow all three. Some don't:

- Captive portals before login
- Restrictive hotel Wi-Fi
- Corporate egress firewalls that whitelist 443 only
- Some airport Wi-Fi

When 1280 is blocked, the agent can download the binary (443 works) but
cannot complete enrollment — the SSE stream returns an OTT JWT, but the
Ziti SDK call to `https://ctrl-N.konoss.org:1280/edge/client/v1/enroll`
hangs.

**Why we shipped this way.** Doing 443-only requires a real chunk of
work and is easy to get wrong:

1. Rebuild Caddy with the `layer4` plugin (not in the current binary).
2. SNI-split port 443 between Caddy's HTTP routes and a forwarder that
   sends Ziti traffic to the edge controller and edge router.
3. Reverse-proxy `/edge/client/v1/*` and `/edge/management/v1/*` from
   443 → localhost:1280 inside Caddy.
4. Patch the OTT JWT `iss` field so the SDK posts to 443, not 1280.
5. Add a `wss://0.0.0.0:443` listener on each edge router OR have the
   L4 plugin forward router connections.
6. Test against several captive-portal-style networks.

Half a day of focused work, not something to do the night before
travel.

**Workaround for users today.** If `schmutz enroll` hangs after
"creating identity" or returns a TLS dial timeout, the network is
likely blocking 1280. Tether to a phone or move to another network.
Cellular and home networks always allow 1280.

**Tracking.** Tagged as "Slice 6 — edge-on-443 consolidation" in the
agent build plan. Not blocking the v0.3.1 release.

---

## Bao-as-config is not yet the source of truth (deferred)

The discovery package collects rich data and sends it to the controller.
What the agent reads back as runtime config — log level, extra services,
capability list — still comes from `/etc/schmutz/manifest.yaml`, written
once at enroll time.

The capabilities design (`docs/superpowers/specs/2026-04-24-schmutz-capabilities-design.md`)
calls for the controller to push config over NATS and the agent to read
secrets via `bao-agent`. None of that wiring exists yet.

**Status:** v0.3.2 territory. v0.3.1 ships agent + GoReleaser only.

---

## Capability runtime is single-mode (deferred)

The agent today binds 5 fixed services (ssh, http, https, nats, docs).
The 13-capability declarative config from the same design doc is the
v0.4 / 2.0 work.

---

## Self-update goes over public 443 (deferred — minor)

The `schmutz update` subcommand fetches new binaries from
`https://ctrl-N.konoss.org/install/schmutz-linux-amd64`. The design wants
this to flow over the Ziti overlay (`schmutz.tango:80` or similar) so the
public install endpoint can disappear after a device is enrolled.

**Status:** Trivial config change once the overlay binary host service
exists. Not blocking.
