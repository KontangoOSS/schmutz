# Schmutz

**Join any zero-trust network with one command.**

```bash
curl -sf https://your-network.example/install | sh
```

That's it. You're on the network.

*Schmutz is Yiddish for "a little dirt." It catches all the filth before it gets inside.*

## Install

**Linux / macOS:**
```bash
curl -sf https://your-network.example/install | sh
```

**Windows:**
Download `schmutz-join-windows-amd64.exe` from [Releases](https://github.com/KontangoOSS/schmutz/releases) and run:
```powershell
.\schmutz-join-windows-amd64.exe https://your-network.example
```

**Any endpoint** — point it wherever your network controller lives.

## What It Does

1. Registers your machine with the network controller
2. Collects machine info (hostname, OS, arch) for identity verification
3. Enrolls using a one-time token — displayed once, never again
4. Downloads and starts the tunnel — all from server-provided config
5. Verifies connectivity and reports your network nickname

After enrollment, all communication is encrypted end-to-end through the overlay. No open ports. No VPN. No firewall rules.

## Components

| Binary | Purpose |
|--------|---------|
| `schmutz-join` | Join a network — register, enroll, connect |
| `schmutz` | Edge firewall — L4 classifier with JA4 fingerprinting |

## Platforms

| OS | Arch | Binary |
|----|------|--------|
| Linux | x86_64 | `schmutz-join-linux-amd64` |
| Linux | ARM64 | `schmutz-join-linux-arm64` |
| Linux | ARM | `schmutz-join-linux-arm` |
| macOS | Intel | `schmutz-join-darwin-amd64` |
| macOS | Apple Silicon | `schmutz-join-darwin-arm64` |
| Windows | x86_64 | `schmutz-join-windows-amd64.exe` |

Zero dependencies. Download and run.

## Build

```bash
make build        # Build join + edge for current platform
make release      # Cross-compile join for all 6 platforms
make test         # Run tests
```

## Project Structure

```
schmutz/
├── install.sh           Universal shell installer (downloads binary, runs it)
├── Makefile             Build targets
├── src/
│   ├── cmd/
│   │   ├── join/        Enrollment client
│   │   └── schmutz/     Edge classifier
│   └── internal/
│       ├── join/        Registration, platform detection, downloads
│       ├── classifier/  L4 traffic classification
│       ├── clienthello/ TLS ClientHello parser
│       ├── fingerprint/ JA4 computation
│       └── relay/       Byte relay
├── docs/                Architecture docs
└── build/binary/        Build output (gitignored)
```

## Server API

Any server implementing these endpoints works with the Schmutz client:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/register` | POST | Register a machine |
| `/api/config` | GET | Get machine config (auth required) |
| `/api/enroll` | POST | Enroll with token |
| `/api/health` | GET | Health check |
| `/install` | GET | Serve install script |
| `/download/<binary>` | GET | Serve platform binary |

## Security

- **One-time token** — displayed once at registration, never again
- **Nickname verification** — server-generated, required for all subsequent calls
- **Quarantine by default** — new machines start with minimal access
- **No PII** — hostname, OS, arch, MACs only. No user data. No telemetry.
- **Zero open ports** — the machine is invisible after enrollment

## Acknowledgments

Built on [OpenZiti](https://openziti.io) by the team at [NetFoundry](https://netfoundry.io). Thank you for building the future of networking in the open.

## About

Built by [Kontango](https://kontango.io).

- [kontango.io](https://kontango.io)
- [github.com/KontangoOSS](https://github.com/KontangoOSS)

## License

MIT — see [LICENSE](LICENSE).
