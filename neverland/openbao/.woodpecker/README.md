# Woodpecker CI Pipelines

Numbered prefix indicates execution order. All files use `.yaml` extension.

**Status: DISABLED** — Workflows are in `disabled/` until a Woodpecker instance is connected.

| Pipeline | Trigger | Purpose |
|----------|---------|---------|
| `01-discover.yaml` | Push main, manual | Clone repo, run `discover.sh` for CI metadata |
| `02-validate.yaml` | Push, PR, manual | Validate credentials, fetch secrets, check standard files |
| `03-build.yaml` | Push main, PR, manual | Build container image, push to Forgejo registry (dry-run on PR) |
| `04-deploy.yaml` | Push main, manual | Lint → Test → Provision → Deploy → Docs sync |

## Lifecycle

```
PR opened/updated  →  02-validate  →  03-build (dry-run)

Merge to main      →  01-discover
                   →  02-validate
                   →  03-build (push)
                   →  04-deploy:
                        1. Authenticate (Phase + Vaultwarden + Konfig)
                        2. Lint (auto-detect: Go/Node/Python)
                        3. Test (make test or language fallback)
                        4. Provision (Proxmox LXC, skip if exists)
                        5. Deploy (git pull + .env + restart)
                        6. Docs sync (Ticketarr DOC ticket)
```

## Secrets Required

| Secret | Provider | Description |
|--------|----------|-------------|
| `forgejo_username` | Woodpecker | Forgejo registry/API username |
| `forgejo_password` | Woodpecker | Forgejo registry/API password |
| `PHASE_SERVICE_TOKEN` | Woodpecker | Phase secrets manager token |
| `BW_MASTER_PASSWORD` | Woodpecker | Vaultwarden master password |
| `BOT_USER` | Woodpecker | Forgejo bot username for git clone |
| `BOT_TOKEN` | Woodpecker | Forgejo bot API token for git clone |
