# Schmutz — Composite-Key Identity & Microservice Architecture

[← Back to README](../README.md)

> **Status:** Design doc. Replaces nothing in [DESIGN.md](DESIGN.md) or
> [IDENTITY.md](IDENTITY.md) — it extends them. The current two-phase enrollment
> model still applies; this doc covers the **identity-anchor** model that sits
> underneath it, the **per-class privilege ceilings** that come from it, and the
> **microservice split** required to build it cleanly.

---

## 1. Why this design exists

Schmutz today enrolls a machine, gets it a Ziti identity, and configures the
overlay. That works for honeypots, browser sessions, and lab boxes — devices
where "is this the same device we saw before" is enough.

It is not enough for the **internals of the network** — controllers, routers,
hypervisors. For those, we need identity that:

- Cannot be reproduced even with full root on a different machine
- Survives OS reinstall (the *machine* is the same, the OS is replaceable)
- Is partitioned so the machine can authenticate itself without admin
- Has a privilege ceiling determined by the strength of attestation, not by claim

This document defines that model and the agent that enforces it.

---

## 2. The composite-key principle

The TPM is a **factor of attestation**, not a stored fact. We do not put the
TPM EK in Bao. Instead, every machine has two halves of a composite secret:

```
composite_key = HKDF( machine_secret, bao_secret, identity_name, salt )
                       ↑                ↑
                   on the box       in Bao entity
                   (TPM-sealed       (random, generated
                    or LUKS-sealed)   at enrollment)
```

| Side | Lives where | Lifetime | Access |
|---|---|---|---|
| `machine_secret` | TPM-sealed (preferred) or LUKS-sealed file | Persists across OS reinstall on same hardware | Only the machine itself |
| `bao_secret` | `infrastructure/secret/<name>/composite` in Bao | Persists until entity is decommissioned | Admin-only direct read; verifier service for composite check |

**Properties:**

- Machine alone → has half, can't produce composite
- Bao alone → has half, can't produce composite
- Attacker who exfiltrates Bao → has every `bao_secret` but no `machine_secret`s
- Attacker who clones a disk → has `machine_secret` only if it was LUKS-sealed
  AND attacker has the LUKS key; has nothing if it was TPM-sealed
- Both halves required → and the verifier only releases its half if the
  surrounding facts (MAC, anchor metadata, attestation freshness) line up

Composite verification happens **on every Bao session refresh**. A stolen
approle credential is good for at most one refresh window before failing.

---

## 3. Attestation classes & privilege ceilings

A machine's `attestation_class` is determined automatically at enrollment by
what schmutz can actually use. The machine does not get to claim its class —
schmutz reports facts, Tinkerbell decides the class, Bao policy enforces the
ceiling.

| Attestation class | Allowed `kind` | Allowed `tier` | Notes |
|---|---|---|---|
| `tpm-secureboot` | controller, router, hypervisor, tunnel-server | network | Strongest. Required for ingress and network plane. |
| `tpm-only` | hypervisor, tunnel-server, workstation | network or edge | Strong but no early-boot guarantee. |
| `luks` | tunnel-client, workstation | edge | Disk seal only; weakest acceptable for network identity. |
| `none` | sensor, IoT, lab-fleet, quarantine | edge or quarantine | Composite key only; no hardware root of trust. |

The user submitting an enrollment does not specify any of this. They install
schmutz and pick a name. Schmutz inspects the hardware, Tinkerbell decides
the class, and the user is told what their machine can and cannot do.

> A box that wants to be a controller but only achieves `attestation_class=luks`
> is **rejected**. The user must either install on hardware with TPM + Secure
> Boot or accept a lower role.

---

## 4. Where things live

### On the machine (TPM-sealed where possible)

| What | Why |
|---|---|
| `machine_secret` (32 random bytes) | Bao-side composite half; never leaves TPM |
| TPM attestation key (when present) | Signs proofs |
| Approle `secret-id` | For Bao login; TPM-sealed if possible, else LUKS-sealed |
| Local read-cache of own entity | Allows boot when Bao is briefly unreachable |

### In Bao `infrastructure/identity/entity/<name>/metadata` (public-readable)

| Field | Purpose |
|---|---|
| `mac` | Primary NIC MAC; DHCP/PXE-boot match |
| `ip` | Current IP (drift-tracked) |
| `os_family`, `os_version` | Drift-tracked |
| `arch` | x86_64, arm64, etc. |
| `kind`, `tier`, `location` | Role classification |
| `attestation_class` | Drives privilege ceiling |
| `tpm_present`, `secureboot_enabled` | Boolean classifiers, not raw values |
| `enrolled_at`, `last_attested_at` | Lifecycle timestamps |
| `privilege_ceiling` | Mirror of class-derived ceiling for visibility |

### In Bao `infrastructure/secret/<name>/` (admin-only direct read)

| Path | Why it's admin-only |
|---|---|
| `composite` | The Bao-side half of the composite key |
| `bmc_password` | Out-of-band hardware management |
| `disk_luks_passphrase` | Recovery if the machine boots without TPM |
| `recovery_root_password` | Last-resort console access |
| `approle_role_id` | The role-id half — secret-id stays on machine |

### Never anywhere except in TPM

- TPM private key material
- The cleartext composite key (only ever computed transiently in the verifier)

---

## 5. Where schmutz lives in the boot stack

Schmutz operates at the highest-trust layer the host platform allows.

```
Layer 1 — TPM / Secure Enclave           (chip, not code)
Layer 2 — UEFI / Secure Boot             (firmware-signed app — not used today)
Layer 3 — Bootloader (GRUB)              (verifies signatures, doesn't run agents)
Layer 4 — initramfs                      ← schmutz on Linux lives HERE
Layer 5 — userspace systemd service      (schmutz runtime mode)
```

**Linux machines run schmutz in initramfs (Layer 4)** as the strongest position
that's actually achievable across our fleet. The initramfs hook:

- Reads TPM PCRs to verify the boot was clean
- Unseals `machine_secret` from TPM
- Logs into Bao, fetches own entity, verifies anchor
- Computes composite proof, sends to verifier
- On success: unseals disk LUKS key, mounts root, transfers control to systemd
- On failure: drops to recovery shell with network access for admin investigation

After the OS finishes booting, **schmutz transitions to runtime mode** (Layer 5)
as a systemd service that handles drift detection and periodic re-attestation.

### Per-OS layer mapping

| OS | Boot trust | Schmutz layer | Notes |
|---|---|---|---|
| Linux (Tinkerbell-built image) | UEFI SB + initramfs | Layer 4 + Layer 5 | The strong path; default for the network plane |
| Linux (manually installed) | Whatever the install gave us | Layer 5 only | Documented as degraded |
| FreeBSD (OPNsense) | FreeBSD loader | Layer 5 | Documented exception |
| Windows | UEFI + Windows boot manager | Layer 5 (Windows service) using TBS for TPM | Future |
| macOS | iBoot / T2 / M-series | Layer 5 (LaunchDaemon) — degraded | Future, very limited |

**Persistence note:** the initramfs hook lives inside the OS image. A
Tinkerbell-built image always includes it. A manual reinstall to a different
OS family loses the initramfs hook (it's Linux-specific) — but the Bao anchor
remains valid because it depends on TPM/DMI/MAC, all of which are
OS-agnostic. The agent has to be re-installed; the identity does not.

---

## 6. Microservice split

Today, "schmutz" is one binary doing many jobs. To get this design right we
split it into focused components, each with a clear contract.

```
┌─────────────────────────────────────────────────────────────────────┐
│                          On the machine                              │
│                                                                      │
│  ┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐    │
│  │ schmutz-init    │   │ schmutz-agent   │   │ schmutz-cli     │    │
│  │ (initramfs hook)│   │ (systemd svc)   │   │ (admin tool)    │    │
│  └────────┬────────┘   └────────┬────────┘   └────────┬────────┘    │
│           │                     │                     │             │
│           └─────────┬───────────┴─────────────────────┘             │
│                     │                                                │
│           ┌─────────▼─────────┐                                     │
│           │ libschmutz        │  shared core lib                    │
│           │ - hwidentity      │                                     │
│           │ - composite       │                                     │
│           │ - bao client      │                                     │
│           │ - ziti enroll     │                                     │
│           │ - sealing (TPM/   │                                     │
│           │   LUKS)           │                                     │
│           └───────────────────┘                                     │
└─────────────────────────────────────────────────────────────────────┘
                       │ network
┌──────────────────────▼──────────────────────────────────────────────┐
│                     Off the machine                                  │
│                                                                      │
│  ┌──────────────────────┐  ┌──────────────────────┐                 │
│  │ schmutz-installer    │  │ schmutz-controller   │                 │
│  │ (pxe / iso /         │  │ (intake + quarantine │                 │
│  │  cloud-init / curl)  │  │  + 404rd, exists)    │                 │
│  └──────────────────────┘  └──────────┬───────────┘                 │
│                                       │                              │
│                                       ▼                              │
│                            ┌──────────────────────┐                 │
│                            │ tinkerbell           │                 │
│                            │ (super-admin in      │                 │
│                            │  Bao infrastructure/)│                 │
│                            └──────────┬───────────┘                 │
│                                       │                              │
│                            ┌──────────▼───────────┐                 │
│                            │ attest.tango         │                 │
│                            │ (composite verifier  │                 │
│                            │  service — sidecar   │                 │
│                            │  to Bao)             │                 │
│                            └──────────────────────┘                 │
│                                                                      │
│  ┌──────────────────────┐                                           │
│  │ schmutz-flavors      │  (kore/schmutz-flavors/ — see §7)         │
│  │ (per-service recipes │                                           │
│  │  pulled at apply-    │                                           │
│  │  time by agent)      │                                           │
│  └──────────────────────┘                                           │
└─────────────────────────────────────────────────────────────────────┘
```

### 6.1 `libschmutz` — shared core library

Pure Go, no side effects. Used by every binary below.

- `hwidentity` — collects TPM/DMI/disk/NIC facts; produces the anchor object
- `composite` — HKDF + HMAC primitives; sealing/unsealing helpers
- `seal` — TPM operations (tpm2-tss bindings) + LUKS fallback
- `bao` — Bao client wrapper with namespace + approle helpers
- `ziti` — Ziti identity enrollment and refresh
- `attest` — composite proof generation

### 6.2 `schmutz-init` — initramfs hook (NEW)

Tiny binary, statically linked, runs in initramfs context.

- Linux only
- Detects TPM availability
- Generates or unseals `machine_secret`
- Attempts Bao login + composite proof
- Unseals LUKS root partition
- Hands off to systemd
- On failure: drops to networked recovery shell

Has access to: a single network interface, kmod loader, libtss2, libcryptsetup.
**Does not have access to:** the full filesystem, package manager, anything
that could be tampered with by a compromised OS userspace.

### 6.3 `schmutz-agent` — runtime systemd service

What today's `schmutz` binary mostly is. Refactored to use `libschmutz`.

- Periodic re-attestation (default once per day; tunable)
- Drift detection (Tier 3+4 fields, reports to Tinkerbell)
- Ziti tunnel management (`ziti-edge-tunnel` lifecycle)
- DNS configuration (`.tango`, `.zone` scoping via systemd-resolved)
- Bind dark services
- Heartbeat to schmutz-controller
- Receives commands (restart, re-enroll, lock)

### 6.4 `schmutz-cli` — admin tool

For humans working on the box directly.

```
schmutz status                    show current attestation, class, ceiling
schmutz attest                    force a re-attestation right now
schmutz enroll                    interactive enrollment (calls installer)
schmutz reseal                    rotate machine_secret (after major HW change)
schmutz drift                     show what's changed since last attestation
schmutz logs                      tail the agent log
schmutz reset --i-know-what-i-am-doing
```

### 6.5 `schmutz-installer` — installer methods

Schmutz needs to land on a machine. It does so via several installer methods.

> **Terminology note:** these are *installer methods*, NOT to be confused with
> *schmutz flavors* (§7), which are **service profiles**. An installer method
> is *how* schmutz gets onto the box; a flavor is *what* schmutz makes the box
> do once it's running. The two axes are independent — every installer method
> lands a generic schmutz that can then have any flavor applied.

| Method | Use case | Mechanism |
|---|---|---|
| `pxe` | Tinkerbell-managed bare-metal | iPXE script + Tinkerbell workflow + custom initramfs |
| `iso` | One-off bare-metal install | Bootable ISO with embedded schmutz-init in initramfs |
| `cloud-init` | DigitalOcean / EC2 / etc. | cloud-init writes config + downloads schmutz |
| `curl` | Already-running Linux box (degraded) | `curl https://schmutz.tango/install \| sudo sh` — Layer 5 only |
| `package` | Distros where we have repos | `.deb` / `.rpm` — Layer 5 only, optional initramfs hook install |

The installer **never** ships secret material. It downloads schmutz, runs the
correct enrollment flow for the method, and the secret material is generated
or fetched from Bao based on the box's actual hardware.

### 6.6 `schmutz-controller` — intake hub (existing)

Already exists. Already handles honeypot/quarantine/404rd intake. Future
additions:

- **Hardware enrollment intake** — first-boot machines hit it, it forwards to
  Tinkerbell. Same quarantine pattern as honeypots, but for hardware.
- Machines requesting class upgrades (e.g., a `tunnel-client` upgrading to
  `tunnel-server` after TPM is enabled) — admin-mediated.

### 6.7 `attest.tango` — composite verifier service (NEW, off-machine)

A tiny Go service that mediates composite-key verification. Runs alongside
Bao on the same hosts (sidecar pattern).

- Reads `bao_secret` from Bao using a narrow approle (`attest-verifier`
  policy, read-only on `infrastructure/secret/*/composite`)
- Receives `(name, challenge, machine_proof)` from a machine
- Computes `expected = HMAC(bao_secret, challenge)`
- Combines with `machine_proof` per the composite algorithm
- Returns OK/DENY + a signed verification token
- Logs every attempt (success + failure) with full context
- Refuses if the entity is marked `compromised` in Bao

The verifier is small enough to audit thoroughly. Bao itself is not modified.

### 6.8 `tinkerbell` — super-admin in `infrastructure/`

Holds the `tinkerbell-admin` approle. Owns the lifecycle:

- Decides `attestation_class` from hardware facts
- Writes anchors at enrollment
- Issues per-machine approles
- Enforces the privilege ceiling rules
- Handles class upgrades (admin-mediated)

---

## 7. Schmutz flavors — service profiles

**A flavor is a Makefile target for service deployment.** It is a declarative
bundle that turns "a generic Linux box with schmutz on it" into "a working
node hosting service X." The same flavor applied to any compatible box
produces the same result.

> Not to be confused with installer methods (§6.5). Installer methods say
> *how* schmutz lands; flavors say *what* schmutz makes the box do.

### 7.1 Why flavors exist

Today, deploying a new service on a box means:
- Hand-write systemd units
- Hand-create Bao approles + policies
- Hand-add Ziti role-attributes + service-policies
- Hand-render config from Bao secrets
- Hope it matches the other boxes that run the same service

That doesn't scale and produces drift. A **flavor** captures the entire
recipe in one PR-reviewable manifest. `schmutz apply ticketarr` becomes a
single, idempotent command that does all of the above the same way every
time, on any box.

### 7.2 Manifest schema

```yaml
# kore/schmutz-flavors/ticketarr/manifest.yaml
flavor: ticketarr
version: 1.2.0

# What Ziti role-attributes this flavor adds to the box's identity
ziti:
  role_attributes:
    - host-ticketarr
    - tier-edge
    - tunnel
  bind_services:
    - ticketarr-web      # ticketarr.tango → :8080
    - ticketarr-api      # api.ticketarr.tango → :8081
  dial_services:
    - host-bao
    - host-konmail
    - host-grafana

# What Bao access this flavor needs
bao:
  namespace: kontango
  approle_template: "{{flavor}}-on-{{machine}}"   # e.g. ticketarr-on-hank
  policies:
    - app-ticketarr
  secrets_to_pull:
    - secret/apps/ticketarr/database
    - secret/apps/ticketarr/oidc
    - secret/apps/ticketarr/admin

# Files to render on disk (templates pulled from the flavor repo)
files:
  - dest: /etc/ticketarr/config.yaml
    template: templates/config.yaml.tmpl
    mode: "0640"
    owner: ticketarr
  - dest: /etc/ticketarr/secrets.env
    template: templates/secrets.env.tmpl
    mode: "0600"
    owner: ticketarr

# Systemd units to install + enable
systemd_units:
  - ticketarr.service
  - ticketarr-bao-renew.timer

# Health checks the agent runs continuously
health:
  - http: http://localhost:8080/health
    interval: 30s
  - port: 8081
    type: tcp
    interval: 60s

# Compatibility
compatible_with: [bare-host, postfix]
incompatible_with: [controller, hypervisor]

# Privilege ceiling enforcement
requires_attestation_class: [tpm-secureboot, tpm-only, luks, none]
# (this is edge-class, no minimum)
```

### 7.3 Where flavors live

A separate repo per flavor (or a monorepo of flavors), so:

```
kore/schmutz-flavors/
├── bare-host/
│   ├── manifest.yaml
│   ├── templates/
│   └── tests/
├── controller/
├── hypervisor/
├── postfix/
├── ticketarr/
├── arr-stack/
├── forgejo/
└── README.md
```

**Why a separate repo from `schmutz/` itself:** flavors will change much more
often than schmutz core. Flavor PRs shouldn't trigger schmutz CI. Different
review groups apply (service maintainers review their own flavor; schmutz
maintainers review schmutz core). And flavor versioning is independent.

The schmutz-agent pulls flavors at apply-time, caches locally at
`/var/lib/schmutz/flavors/<name>@<version>/`, and re-fetches on `upgrade` or
`reconcile`.

### 7.4 The Makefile-style CLI

```bash
schmutz list                       # what flavors are available
schmutz applied                    # what flavors are active on this box
schmutz apply ticketarr            # add ticketarr to this box
schmutz apply ticketarr@1.2.0      # pin to a specific version
schmutz remove ticketarr           # take it off (reverses everything)
schmutz upgrade ticketarr          # pull latest manifest, reconcile
schmutz upgrade --all              # upgrade every applied flavor
schmutz reconcile                  # re-apply everything from scratch
schmutz dry-run ticketarr          # show what would change
schmutz diff ticketarr             # what's different from desired state
schmutz validate ticketarr         # lint a flavor manifest
```

Apply / remove / upgrade are all idempotent. Running the same command twice
produces the same result. Running `reconcile` from a known state is the
recovery path when something has drifted.

### 7.5 Apply lifecycle

When a user runs `schmutz apply ticketarr`:

```
1. Check the box's attestation_class against requires_attestation_class
   → reject if box doesn't meet the floor
2. Check compatible_with / incompatible_with against currently-applied flavors
   → reject if there's a conflict
3. Pull the flavor manifest + templates from the flavor repo (cached)
4. Verify manifest signature (future — flavor repo is signed)
5. Acquire flavor lock (only one apply/remove at a time per box)
6. Bao operations:
   - Create approle: kontango/auth/approle/ticketarr-on-{{machine}}
   - Bind policies declared in manifest
   - Generate secret-id, seal it with machine_secret, write to disk
7. Ziti operations:
   - Add declared role-attributes to box's identity (via tinkerbell-admin)
   - Service-policies that match those attributes auto-grant access
   - Bind hosted services (declare-side)
8. Filesystem operations:
   - Render each templated file from Bao secrets
   - Set ownership and mode
   - Atomic write (temp file + rename)
9. Systemd operations:
   - Place unit files in /etc/systemd/system/
   - daemon-reload, enable, start
10. Health checks:
   - Run each declared check; require ALL to pass within timeout
   - On failure: rollback (steps 9 → 6 in reverse)
11. Record applied state:
   - /var/lib/schmutz/applied/ticketarr.json
   - Reports state to schmutz-controller (which forwards to inventory)
```

`schmutz remove ticketarr` reverses every step. `schmutz upgrade ticketarr`
diffs old→new manifest and applies only the delta.

### 7.6 Interaction with composite-identity (§2-§4)

Flavors are **gated by the box's identity tier**:

- The box has an `attestation_class` written by Tinkerbell at enrollment
  (in the entity metadata, §4)
- A flavor declares `requires_attestation_class: [list]`
- `schmutz apply` refuses if the box's class is below the flavor's floor

Examples:

| Flavor | requires_attestation_class | Why |
|---|---|---|
| `controller` | `[tpm-secureboot]` | Network plane ingress; needs early-boot trust |
| `hypervisor` | `[tpm-secureboot, tpm-only]` | Manages other VMs; needs at minimum TPM |
| `bao-replica` | `[tpm-secureboot]` | Holds the secrets; cannot run on weaker boxes |
| `postfix` | `[tpm-secureboot, tpm-only, luks]` | Mail relay; LUKS acceptable |
| `ticketarr` | `[tpm-secureboot, tpm-only, luks, none]` | Edge service; any class |
| `honeypot-404rd` | `[any]` | Honeypots are expected to be exposed |

This means the **hardware floor for a service is enforced by Bao policy +
Tinkerbell** at apply-time. You cannot accidentally run `bao-replica` on a
laptop in `attestation_class=none`. The composite-identity model is the
ground truth; flavors layer on top.

### 7.7 Conflict resolution

Two flavors that want to render the same file → **refuse the apply** and
require admin to declare an explicit composite flavor. No silent merging,
no last-write-wins.

If two flavors both bind the same Ziti service name, refuse. If two flavors
declare incompatible systemd dependencies, refuse. The principle: flavors
are independent recipes that compose only when explicitly compatible.

### 7.8 Versioning

- Flavors version with semver in their manifest
- Default behavior: `schmutz apply ticketarr` pulls latest from the flavor
  repo's `main` branch
- `schmutz apply ticketarr@1.2.0` pins to a tagged version
- `schmutz applied` shows the version of every applied flavor
- `schmutz upgrade --all` pulls latest, re-applies; gates on health checks

### 7.9 Multi-tenancy

Flavors are **generic**; tenant-specific variations are handled at apply-time
via parameters:

```bash
schmutz apply ticketarr --tenant=acme --domain=acme.example.com
```

The manifest can declare required parameters; apply rejects if any are
missing. Tenant data lives in Bao under
`kontango/secret/tenants/<tenant>/...` and the flavor's templates pull from
there based on the `--tenant` parameter.

This means we ship one `ticketarr` flavor, not `ticketarr-acme` and
`ticketarr-dillon` and N others. The flavor is the recipe; the tenant is
the data.

### 7.10 Sample catalog

Drawing from the actual fleet:

| Flavor | What it provides | requires_attestation_class | Compatible boxes |
|---|---|---|---|
| `bare-host` | Base ziti tunnel + DNS scoping + monitoring | `[any]` | every box |
| `controller` | Ziti controller + raft join + Caddy edge | `[tpm-secureboot]` | tier-network only |
| `hypervisor` | Proxmox cluster membership + VM monitoring | `[tpm-secureboot, tpm-only]` | tier-network only |
| `router` | Edge-router hooks + ziti router | `[tpm-secureboot, tpm-only]` | tier-network only |
| `bao-replica` | Bao raft member | `[tpm-secureboot]` | tier-network only |
| `postfix` | konmail integration / mail relay | `[tpm-secureboot, tpm-only, luks]` | any |
| `forgejo` | Forgejo + push-mirror | `[tpm-secureboot, tpm-only, luks]` | any |
| `ticketarr` | Ticket service | `[any]` | any |
| `arr-stack` | Sonarr/Radarr/etc. | `[any]` | any edge |
| `tunnel-only` | Just join the overlay, dial-only | `[any]` | any |
| `honeypot-404rd` | Run a 404rd honeypot | `[any]` | any |

A new service ships as a new flavor PR, reviewable, testable, versioned.

### 7.11 What schmutz-agent does at runtime for flavors

Beyond apply/remove/upgrade, the agent continuously:

- Runs the health checks declared by every applied flavor
- Watches for drift in rendered files (re-renders if Bao secrets rotated)
- Renews flavor approle secret-ids before expiry
- Reports applied-state + health to schmutz-controller (→ machine_inventory)
- Refuses class-degrading changes (you can't downgrade a `controller` box's
  attestation_class while the controller flavor is still applied)

---

## 8. End-to-end flows

### 7.1 First-boot enrollment of a new bare-metal machine

```
1. Machine PXE boots; iPXE script loads Tinkerbell-built initramfs
2. schmutz-init runs in initramfs:
   a. Inspects TPM, Secure Boot, LUKS availability
   b. Generates machine_secret (32 random bytes)
   c. Seals it (TPM if available, else LUKS file)
   d. Collects hwidentity (Tier 1+2+3 facts)
3. schmutz-init contacts schmutz-controller (well-known PXE URL):
   - Sends hwidentity + proposed machine_secret-public-half
4. schmutz-controller forwards to tinkerbell:
   - Tinkerbell verifies the box is real (telemetry window — Phase 1
     of existing enrollment)
   - Tinkerbell decides attestation_class from facts
   - Tinkerbell asks admin (or auto-approves for known-MAC pre-seed):
     "new machine seeking enrollment, class=X, MAC=Y"
5. On approval, tinkerbell:
   - Generates bao_secret (32 random bytes)
   - Creates entity in infrastructure/identity/entity/<name>
   - Writes public anchor metadata
   - Writes bao_secret to infrastructure/secret/<name>/composite
   - Mints approle for the entity, returns role_id+secret_id
   - Issues Ziti OTT for the entity's tier
6. schmutz-init receives credentials:
   - Seals approle secret-id
   - Enrolls Ziti identity
   - Performs first composite-verify against attest.tango
   - On success: continues boot, hands off to systemd
7. schmutz-agent starts as systemd service:
   - Establishes ziti tunnel
   - Configures DNS scoping
   - Begins periodic attestation loop
```

### 7.2 Subsequent boots of an enrolled machine

```
1. UEFI Secure Boot verifies GRUB → kernel → initramfs
2. schmutz-init runs:
   a. Reads TPM PCRs to verify clean boot
   b. Unseals machine_secret from TPM
   c. Logs into Bao via approle
   d. Reads own entity public anchor
   e. Compares to physical reality (MAC, DMI, disk WWNs)
      → mismatch in Tier 1 → REFUSE BOOT, drop to recovery shell
      → mismatch in Tier 2 → WARN, continue, alert admin
      → no mismatch → continue
   f. Sends composite proof to attest.tango
   g. On success: unseals LUKS root key, mounts root
3. systemd starts; schmutz-agent takes over runtime duties
```

### 7.3 Periodic re-attestation

```
schmutz-agent runs daily (cron-style timer):
  1. Re-collects hwidentity
  2. Diffs against last-stored anchor
  3. Logs into Bao
  4. Sends composite proof to attest.tango
  5. Reports drift to schmutz-controller (which forwards to Tinkerbell)
  6. On Tier 1+2 mismatch:
     - Marks self as suspect
     - Refuses to renew Ziti session
     - Pages admin
```

### 7.4 Class upgrade (e.g., new hardware enables TPM)

```
1. User runs: schmutz attest --propose-upgrade
2. schmutz-agent re-collects hwidentity, finds TPM is now active
3. Sends upgrade request to schmutz-controller → tinkerbell
4. Tinkerbell verifies the upgrade is legitimate (was the machine offline?
   are these the same disks? same MACs? + composite still verifies)
5. Admin reviews; approves or denies
6. On approval:
   - Tinkerbell updates entity attestation_class + privilege_ceiling
   - schmutz reseals machine_secret using TPM
   - Ziti identity gets updated role-attributes
```

---

## 9. Privilege partitioning in Bao

```
policy: machine-self
  read   identity/entity/name/{{identity.entity.name}}
  read   auth/approle/role/{{identity.entity.name}}/role-id
  # Composite check happens through attest.tango — machines never
  # read /infrastructure/secret/*/composite directly.

policy: attest-verifier
  read   infrastructure/secret/+/composite
  # Used only by attest.tango sidecar.

policy: tinkerbell-admin (already exists)
  full read+write on entities, groups, approles, secrets

policy: human-admin (you, future ops)
  full read+write on everything
  + can mark entities compromised
  + can mint break-glass tokens
  + dual-control required for composite rotation

policy: cluster-peer
  read identity/entity/name/* (only public anchor fields)
  cannot read any /infrastructure/secret/* path
```

---

## 10. Failure modes & recovery

| Failure | Detection | Recovery |
|---|---|---|
| Bao unreachable at boot | schmutz-init can't log in | Use local read-cache of last-known anchor; degraded mode for one boot; if still failing on next boot, recovery shell |
| TPM cleared (unplanned) | machine_secret unsealable | Drop to recovery shell; admin verifies, runs `schmutz reseal --recover` to regenerate machine_secret + update Bao with admin token |
| TPM cleared (planned) | machine_secret unsealable but admin pre-authorized | schmutz-init reads admin pre-auth token from Bao, regenerates secret, continues |
| Composite check fails | attest.tango returns DENY | Refuse session refresh; Ziti identity stops working; alert via secondary channel (404rd or BMC) |
| Disk failure (replace one) | Disk WWN drift detected | Tier 2 warning; admin acks; anchor updated |
| Motherboard replacement | DMI UUID + chassis serial drift | Tier 1 mismatch; refuse boot; admin runs `schmutz reseal --motherboard-replaced` |
| Entire machine replacement (same name desired) | Hard re-enrollment | Admin: `bao delete identity/entity/name/<name>` + new PXE boot enrolls fresh |
| Bao compromise | External detection | Mark all entities compromised via human-admin; rotate every bao_secret; machines auto-fail composite until rotation completes |
| Machine compromise | Tier 4 drift + behavioral | Admin marks entity compromised in Bao; next composite check fails; machine is locked out of overlay |

---

## 11. What we keep from existing schmutz

The existing two-phase model in [DESIGN.md](DESIGN.md) is not thrown out — it's
**reframed as the enrollment intake** that runs in front of the composite
identity flow:

- The 60-second telemetry window proves the box is real
- The decision engine still scores devices
- The OTT-based Ziti identity issuance still happens

What's added on top:

- Hardware classification + attestation_class assignment
- Composite key generation and storage split
- Per-class privilege ceiling enforcement
- Initramfs-time verification on Linux
- Periodic composite re-attestation
- Microservice split into init / agent / cli / installer / verifier

The existing `register/` and `pipeline/` packages become subcomponents of
`schmutz-agent` (Layer 5). The new `schmutz-init` (Layer 4) is the addition.

---

## 12. Build sequence

Suggested order — each step is independently shippable and testable.

1. **`libschmutz/hwidentity`** — collector that produces the full anchor JSON.
   Run locally on every machine; populate `infrastructure/` entities with
   real Tier 1+2+3 values. No behavioral change yet.
2. **`libschmutz/seal`** — TPM + LUKS sealing primitives. Test against pve and
   hank (have TPM); slim-1/2 (LUKS-only); a DO controller (no TPM, no LUKS,
   degraded mode).
3. **`libschmutz/composite`** — HKDF/HMAC + the composite algorithm. Pure
   library, easy to test.
4. **`attest.tango` service** — small Go service that sits next to Bao;
   verifies composite proofs. Standalone; the rest of schmutz can use it
   before the rest exists.
5. **`schmutz-cli attest`** — simplest possible client. Use to verify the
   end-to-end flow works for the existing 4 controllers' identities.
6. **`schmutz-agent` rework** — incorporate libschmutz; add daily attest loop.
7. **`schmutz-init`** — initramfs hook. Build the Tinkerbell base image with
   it baked in. Test on a single fresh box first.
8. **Class enforcement** — Tinkerbell starts refusing class upgrades that
   exceed actual attestation strength.
9. **Roll out to network plane** — pve, hank, slim-1/2 get reinstalled with
   the new image during scheduled windows.
10. **Roll out to edge plane** — workstation, lab fleet, customer hardware.
11. **`schmutz-flavors` repo** — scaffold the flavor catalog repo
    (`kore/schmutz-flavors/`). Start with `bare-host` and `tunnel-only` as
    canonical examples.
12. **`schmutz apply` / `remove` / `reconcile`** — implement the flavor
    lifecycle in the agent + CLI. Bao approle creation, ziti role-attribute
    additions, file rendering, systemd unit installation.
13. **Convert existing services to flavors** — re-express today's hand-deployed
    services (404rd, konmail, forgejo, ticketarr, arr-stack, etc.) as flavors.
    Each conversion is one PR per service. Old hand-deployed paths can stay
    until each service has been re-applied via flavor.

---

## 13. Open questions to close before building

1. **Verifier algorithm details** — exact HKDF inputs, challenge format,
   response format, replay protection. Needs a security-design pass before code.
2. **Class-upgrade policy** — how aggressive on auto-approval? Today's lean:
   never auto-approve a class upgrade for tier-network; always require admin.
3. **Recovery shell network access** — over Ziti only? Over the underlay too
   (in case Ziti is the problem)? Lean: Ziti only, with a documented break-glass
   physical-access procedure.
4. **TPM-less enrollment paths** — slim-1/2 today have no TPM. Do we accept
   them at `attestation_class=luks` indefinitely or plan a hardware refresh?
5. **Per-OS schmutz roadmap** — Linux ships Layers 4+5 first; Windows/BSD/macOS
   are Layer 5 only. When (if ever) do we invest in non-Linux Layer 4?
6. **Composite rotation cadence** — never (only on incident)? Annually? On
   admin demand? Lean: never automatic; admin-triggered after suspected leak.

---

## 14. Glossary

| Term | Definition |
|---|---|
| **Anchor** | The set of unforgeable hardware facts about a machine, stored in `infrastructure/identity/entity/<name>/metadata`. |
| **Attestation class** | The strongest sealing primitive a machine can use: `tpm-secureboot`, `tpm-only`, `luks`, or `none`. |
| **Composite key** | Derived value requiring both `machine_secret` and `bao_secret` to compute. Used as the runtime authenticator. |
| **Drift** | Change in a machine's attested facts since the last attestation. |
| **Privilege ceiling** | The maximum `kind` and `tier` a machine is allowed to claim, derived from its attestation class. |
| **Tier 1 / 2 / 3 / 4** | Trust tiers for hardware facts. Tier 1 = cryptographic root; Tier 4 = software identity. |
