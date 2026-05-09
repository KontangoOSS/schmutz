# APPNAME

APPDESCRIPTION

## At a Glance

| | |
|---|---|
| **Org** | APPORG |
| **License** | MIT |
| **CI** | Woodpecker CI |
| **Secrets** | Phase / Vaultwarden |
| **Registry** | git.konoss.org/APPORG/APPNAME |

## Quick Links

- [Quick Start Guide](guides/quickstart.md)
- [API Reference](api/index.md)
- [Architecture](guides/architecture.md)
- [Runbook](guides/runbook.md)

## CI/CD Pipeline

| Stage | Trigger | What it does |
|-------|---------|--------------|
| 01-discover | main, manual | Repo metadata + Konfig API lookup |
| 02-validate | push, PR | Credentials, secrets, standard file checks |
| 03-build | main, PR | Container build (push on main, dry-run on PR) |
| 04-deploy | main, manual | Lint → Test → Provision → Deploy → Docs sync |
