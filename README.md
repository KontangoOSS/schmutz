<h1 align="center">
  <br />
  schmutz
  <br />
</h1>

<p align="center">
  <em>/ʃmʊts/</em> — Yiddish for "a little dirt." A smudge. The stuff that gets on everything.
</p>

<p align="center">
  <strong>Enroll any machine into a zero-trust overlay network with one command.</strong>
</p>

<p align="center">
  <a href="https://github.com/KontangoOSS/schmutz/releases"><img src="https://img.shields.io/github/v/release/KontangoOSS/schmutz?style=flat-square&color=4a86c8&label=release" alt="release" /></a>
  &nbsp;
  <a href="https://openziti.io"><img src="https://img.shields.io/badge/powered_by-OpenZiti-f58220?style=flat-square" alt="OpenZiti" /></a>
  &nbsp;
  <a href="https://openbao.org"><img src="https://img.shields.io/badge/secrets-OpenBao-6b21a8?style=flat-square" alt="OpenBao" /></a>
  &nbsp;
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-38a169?style=flat-square" alt="MIT" /></a>
  &nbsp;
  <img src="https://img.shields.io/badge/go-1.26-00ADD8?style=flat-square&logo=go" alt="Go 1.26" />
  &nbsp;
  <a href="https://github.com/KontangoOSS/schmutz/stargazers"><img src="https://img.shields.io/github/stars/KontangoOSS/schmutz?style=flat-square&color=e3b341&label=stars" alt="stars" /></a>
</p>

<br />

<p align="center">
  <em>It gets on everything. You barely notice it's there.<br />
  Your machines are connected, your secrets are managed, your services are reachable by name.<br />
  From anywhere. To anything. Without opening a single port.</em>
</p>

<br />

---

## What is Schmutz?

Schmutz is a **device enrollment agent** for the Kontango zero-trust platform. It runs on any machine — laptop, server, LXC container, VM, Raspberry Pi — and does three things:

1. **Enrolls** the machine into a [Ziti](https://openziti.io) overlay network, getting it a cryptographic identity
2. **Binds** the machine's local services onto the overlay so they're reachable by name from anywhere on the same network
3. **Manages secrets** — fetches credentials from [OpenBao](https://openbao.org) using that identity, writes them to `/run/bao-token`, and keeps them fresh

After enrollment, your machine has a stable overlay hostname like `my-server.tango`. Other enrolled machines can reach it at that name. Nothing else can. No firewall rules. No VPN config. No public IPs required.

```
$ schmutz enroll --controller https://ctrl-1.example.com --token ZE-abc123
  ✓ hostname / os / hardware fingerprint
  ✓ Ziti identity enrolled as "machine-a1b2c3d4"
  ✓ services bound: ssh-machine-a1b2c3d4.tango → :22
  ✓ Bao bundle fetched, /run/bao-token written
  ✓ systemd unit installed and started

$ schmutz status
  identity:  machine-a1b2c3d4
  overlay:   connected
  services:  ssh-machine-a1b2c3d4.tango [:22]
  bao-token: valid, refreshes in 14m
  uptime:    3d 7h
```

---

## The Stack

Schmutz is the agent layer of a larger platform. Here's how the pieces fit:

```
┌─────────────────────────────────────────────────────────┐
│                    Kontango Platform                     │
│                                                         │
│  ┌──────────────────┐    ┌──────────────────────────┐  │
│  │  schmutz-agent   │    │    schmutz-enroll        │  │
│  │  (this project)  │    │    (controller server)   │  │
│  │                  │    │                          │  │
│  │  • enrolls       │◄──►│  • issues tokens         │  │
│  │  • binds Ziti    │    │  • provisions identities │  │
│  │  • holds token   │    │  • manages Bao AppRoles  │  │
│  │  • discovers API │    │  • hub enrollment API    │  │
│  └──────────────────┘    └──────────────────────────┘  │
│           │                          │                  │
│           ▼                          ▼                  │
│  ┌──────────────────────────────────────────────────┐  │
│  │                   OpenZiti                        │  │
│  │   Overlay network — encrypted, zero-trust,        │  │
│  │   software-defined. No open ports required.       │  │
│  └──────────────────────────────────────────────────┘  │
│           │                          │                  │
│           ▼                          ▼                  │
│  ┌──────────────────────────────────────────────────┐  │
│  │                   OpenBao                         │  │
│  │   Secret management — AppRole auth, scoped        │  │
│  │   tokens, per-deployment secret isolation.        │  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### Components

| Repo | What it does |
|---|---|
| [`schmutz-agent`](https://github.com/KontangoOSS/schmutz-agent) | The `schmutz` binary — installs on any machine, handles enrollment + Ziti binding + Bao token lifecycle |
| `schmutz-enroll` | Enroll-server — the hub API that issues tokens, provisions Ziti identities, and builds Bao bundles |
| `schmutz-controller` | Controller — manages profiles, trust decisions, and identity approval |
| `schmutz-shared` | Shared Go types — the wire format for substrates, blueprints, and deployment records |
| [`schmutz-plugins`](https://github.com/KontangoOSS/schmutz-plugins) | Open-source Woodpecker CI plugins that provision and maintain the machines schmutz runs on |

---

## How Enrollment Works

```
Operator                  Hub (enroll-server)              Machine (schmutz-agent)
   │                             │                                 │
   │─ POST /api/v1/enrollments ─►│                                 │
   │  (issues single-use token)  │                                 │
   │◄─ { token: "ZE-abc123" } ──│                                 │
   │                             │                                 │
   │                             │◄── schmutz enroll --token ──────│
   │                             │                                 │
   │                             │── creates Ziti identity ───────►│ (Ziti controller)
   │                             │── provisions Bao AppRole ──────►│ (OpenBao)
   │                             │                                 │
   │                             │─► { ziti_identity, bao_bundle }►│
   │                             │                                 │
   │                             │           writes /etc/schmutz/agent.json
   │                             │           writes /etc/schmutz/identity.json
   │                             │           starts bao-jwt refresh loop
   │                             │           binds overlay services
```

**After enrollment:**
- The machine has a stable overlay identity (e.g. `machine-a1b2c3d4`)
- Local services are reachable by overlay name from any enrolled peer
- `/run/bao-token` is refreshed every 10 minutes via AppRole → OIDC JWT → scoped token
- Containers on the same host can bind-mount `/run/bao-token:ro` and call Bao directly

---

## Device Discovery

The agent probes the machine in privilege tiers and sends whatever it can access. The controller uses this to assign a device class and set trust ceilings.

| Layer | Privilege | Examples |
|---|---|---|
| L0 | any | hostname, OS, kernel, arch, CPU model, RAM |
| L1 | non-root | NICs, MACs, IPs, routes, DNS, cloud hints |
| L2 | root | DMI, disk serials, SSH host keys, listening ports |
| L3 | root + caps | TPM presence, kernel module hints |
| L4 | network (opt-in) | public IP, ASN — **off by default** |

**Device classes:**

| Class | Trust anchor |
|---|---|
| `tpm-attested` | Bare metal with working TPM |
| `dmi-attested` | Bare metal, real DMI, no TPM |
| `cloud-vm` | DigitalOcean, AWS, GCP, Azure |
| `vm` | Generic hypervisor (KVM, VMware, Xen) |
| `lxc` | LXC container |
| `docker` | Docker container |
| `unattested` | None of the above |

---

## Secret Management

Schmutz is NOT the secrets store. [OpenBao](https://openbao.org) is.

Schmutz manages the **identity→secret binding**: it holds a Ziti identity, uses that identity to authenticate to Bao via OIDC JWT, and keeps `/run/bao-token` fresh with a scoped token that has exactly the permissions that deployment needs.

```
machine identity (Ziti cert)    ≠    secret identity (Bao AppRole + JWT)
       │                                       │
       │ authenticates to:                     │ authorizes:
       │ overlay network                       │ deployment secrets only
       ▼                                       ▼
  ssh-machine-*.tango           {tenant}/secret/apps/{app}/{deployment}/*
```

> **These are completely separate systems.** The Ziti cert is for overlay networking. The Bao JWT is for secret access. They are related only by the fact that the Ziti identity name appears as a claim in the Bao JWT for audit purposes.

---

## Subcommands

```
schmutz enroll          # enroll this machine into the overlay
schmutz bao-enroll      # fetch + install Bao bundle, write initial /run/bao-token
schmutz bao-login       # refresh /run/bao-token (run by systemd timer every 10m)
schmutz start           # in-process Ziti tunnel (binds services via Go SDK)
schmutz discover        # scan localhost services, publish API schema to Bao
schmutz status          # local diagnostics (--json for machine-readable)
schmutz fingerprint     # print the discovery payload as JSON
schmutz install-service # write systemd units (tunnel + bao-login timer)
schmutz update          # self-update binary with sha256 verification
schmutz version
```

---

## Quick Start

```bash
# On the machine you want to enroll (token from your operator):
curl -fsSL https://ctrl-1.example.com/agent.sh | bash -s -- ZE-<your-token>
```

The installer:
1. Downloads the `schmutz` binary for your architecture
2. Runs `schmutz enroll` with your token
3. Installs the systemd service + bao-login timer
4. You're on the overlay

---

## Documentation

- [docs/IDENTITY.md](docs/IDENTITY.md) — Ziti identity model, what gets created at enrollment
- [docs/COMPOSITE-IDENTITY.md](docs/COMPOSITE-IDENTITY.md) — composite-key identity, attestation classes, privilege ceilings
- [docs/ORIGIN.md](docs/ORIGIN.md) — where schmutz came from (spoiler: L4 TCP classifier → device agent)
- [docs/TECH-DEBT.md](docs/TECH-DEBT.md) — known imperfections and deferred work, honest edition

---

## Philosophy

- **Zero trust means zero.** Not "trust after login." Not "trust inside the VPN." Zero.
- **Identity is the perimeter.** Not IP addresses. Not firewall rules. What you are, cryptographically.
- **Secrets should be boring.** Fetched automatically, scoped tightly, rotated constantly. Never in a config file.
- **The overlay is invisible.** Your services have names. They work. That's it.
- **One command.** Enrollment shouldn't require reading a manual.

---

## License

MIT. See [LICENSE](LICENSE).

---

<p align="center">
  Built by <a href="https://kontango.net">Kontango</a> &nbsp;·&nbsp;
  Powered by <a href="https://openziti.io">OpenZiti</a> + <a href="https://openbao.org">OpenBao</a>
</p>
