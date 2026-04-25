# schmutz

The device agent for a Kontango Ziti overlay network. Schmutz runs on any
machine you want to enroll: laptop, server, container, sidecar. It collects
hardware and OS facts, registers with a controller, receives a Ziti identity,
and binds the device's local services onto the overlay so they're reachable
by name from anywhere else on the same overlay.

```
$ schmutz enroll --controller https://ctrl-1.example.com
  ✓ hostname
  ✓ os
  ✓ banned
  → quarantine (no credentials or fingerprint match)
  … creating identity
2026/04/25 09:23 schmutz: enrolled as "fennelzap" (status=quarantine
  services=[ssh-fennelzap http-fennelzap https-fennelzap nats-fennelzap])

$ schmutz install-service
schmutz: wrote /etc/systemd/system/schmutz.service
schmutz: service enabled and started

$ schmutz status
schmutz 0.3.1
  controller node: no
  hardware hash:   6de4c2723f93ac0d
  device class:    tpm-attested
  ...
```

## What schmutz is not

- Not a Ziti SDK reimplementation. Schmutz embeds the official OpenZiti Go
  SDK for in-process binds and ships the upstream `ziti` CLI alongside
  itself for tunnel/host/proxy/tproxy commands that need the full feature
  surface.
- Not a controller. See [schmutz-controller](https://git.konoss.org/kore/schmutz-controller)
  for the receiving side.

## Install

### Binary install (Linux)

Download the v0.3.1 release tarball for your architecture from the
controller's install endpoint or the [GitHub releases page](https://git.konoss.org/kore/schmutz/releases):

```bash
curl -fsSL https://ctrl-1.example.com/install/schmutz-linux-amd64 \
  -o /usr/local/bin/schmutz
chmod +x /usr/local/bin/schmutz

schmutz enroll --controller https://ctrl-1.example.com
schmutz install-service
```

If your controller's install endpoint requires a code, set
`X-Install-Code` on the curl call (the install page in your browser will
tell you the code).

### Tarball with bundled `ziti` CLI

The release tarball at
`schmutz_<version>_linux_<arch>.tar.gz` ships both the schmutz binary and
the upstream `ziti` CLI pinned to a known-good version, so subcommands
like `schmutz tunnel tproxy` work without extra installs.

```bash
tar -xzf schmutz_0.3.1_linux_amd64.tar.gz
sudo install -m 0755 schmutz /usr/local/bin/
sudo install -m 0755 ziti     /usr/local/bin/
```

### Docker

```bash
docker run --rm \
  -v schmutz-config:/etc/schmutz \
  -e SCHMUTZ_CONTROLLER=https://ctrl-1.example.com \
  -e SCHMUTZ_INSTALL_CODE=<code-from-controller> \
  git.konoss.org/kore/schmutz:0.3.1
```

The container auto-enrolls on first start and runs the in-process tunnel.

## Subcommands

```
schmutz enroll              # discover this device, register with controller, store Ziti identity
schmutz start               # in-process tunnel — bind services using the Go SDK
schmutz tunnel run          # alias for `schmutz start`
schmutz tunnel host         # wraps `ziti tunnel host`
schmutz tunnel proxy        # wraps `ziti tunnel proxy`
schmutz tunnel tproxy       # wraps `ziti tunnel tproxy` (needs CAP_NET_ADMIN)
schmutz status              # local diagnostics, --json for machine-readable
schmutz fingerprint         # dump the discovery payload as JSON
schmutz install-service     # systemd unit install (modes: tunnel, tproxy)
schmutz uninstall           # stop, disable, remove the unit (--purge for full)
schmutz update              # self-update binary with sha256 verification
schmutz version
```

## Discovery layers

The discovery probe collects facts in privilege tiers. Each fact carries
its source layer so the controller can score trust per tier — facts read
from a TPM weigh more than self-reported hostname.

| Layer | Privilege | Examples |
|---|---|---|
| L0 | any | hostname, OS, kernel, arch, CPU model, RAM, uptime |
| L1 | non-root | NICs, MAC addresses, IPs, routes, DNS, timezone, locale, container/cloud hints |
| L2 | root | DMI sys/board/chassis/BIOS, disk serials, GPU IDs, SSH host keys, listening ports, package count |
| L3 | root + capabilities | TPM presence, kernel module hints |
| L4 | network egress (opt-in) | public IP, geolocation, ASN |

`L4` is **off by default**. Enable per-run with `SCHMUTZ_DISCOVER_PUBLIC=1`.
Schmutz never reaches out to a third party for fingerprinting unless you
say so explicitly.

The agent sends whatever layers it has access to. A rootless install gets
L0+L1; a privileged install gets L0–L3. The controller treats every field
as opportunistic — it extracts what's there and ignores what isn't.

## Device class

From the layered evidence schmutz derives a class:

| Class | Trust anchor |
|---|---|
| `tpm-attested` | bare metal with working TPM |
| `dmi-attested` | bare metal, real DMI fields, no TPM |
| `cloud-vm` | known cloud (DigitalOcean, AWS, GCP, Azure) |
| `vm` | generic hypervisor (KVM, VMware, Xen, …) |
| `lxc` | LXC container — fingerprint shadows host, needs parent attestation |
| `docker` | Docker container — image-anchored, ephemeral |
| `unattested` | none of the above |

The controller uses class to decide what privilege ceiling the device
gets. Bare metal with TPM gets a fuller ceiling than a shared LXC.

## Hardware fingerprint

A 16-character hex digest of `sha256(MACs + product_serial + machine-id)`.
Stable across reboots, changes if any of those identifiers change. The
controller uses it to recognize a device on re-enrollment so a previously
approved device doesn't get re-quarantined.

## Building from source

```bash
cd src
go build -o ../bin/schmutz ./cmd/schmutz
```

The agent is pure Go; `CGO_ENABLED=0` produces a fully static binary that
runs on `scratch`, `busybox`, or any libc.

To build the release artifacts (cross-compile + bundled `ziti` CLI +
Docker images), use [GoReleaser](https://goreleaser.com/):

```bash
goreleaser release --snapshot --clean
```

## Documentation

- [docs/IDENTITY.md](docs/IDENTITY.md) — Ziti identity model
- [docs/COMPOSITE-IDENTITY.md](docs/COMPOSITE-IDENTITY.md) — composite-key
  identity model + attestation classes + microservice split (the
  network-plane design)
- [docs/TECH-DEBT.md](docs/TECH-DEBT.md) — known imperfections, deferred work

## License

See [LICENSE](LICENSE).
