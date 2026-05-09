# APPNAME

APPDESCRIPTION

## Project Structure

```
APPNAME/
├── config/
│   ├── konfig.json          # Kontango / Konfig app config (includes app UUID)
│   ├── .env                 # Local environment variables
│   ├── .env.example         # Environment template
│   └── .editorconfig        # Editor settings
├── src/                     # Application source code
├── tests/                   # Test files
├── build/
│   ├── container/           # Dockerfiles, compose configs
│   ├── binary/              # Compiled builds
│   └── scripts/             # Build automation
├── docs/                    # Documentation (cloned into MkDocs)
│   ├── api/                 # API specs (swagger.json + openapi.yaml)
│   ├── guides/              # Quickstart, architecture, runbook
│   ├── archive/             # Archived documents
│   └── brandkit/            # Branding assets
├── scripts/
│   └── ci/                  # CI scripts (discover.sh)
├── .woodpecker/             # Woodpecker CI pipelines
├── .forgejo/                # Forgejo Actions workflows
├── Makefile                 # Build/test/lint targets
├── KONTANGO.md              # Kontango ecosystem info
└── LISCENSE.md              # MIT license
```

## Conventions

- Environment config via `config/.env` file
- Secrets via Phase Secrets Manager (never hardcode)
- Conventional commits: feat, fix, docs, refactor, test, chore
- CI runs on push to main and on pull requests
- App config in `config/konfig.json`
- API docs require both `swagger.json` and `openapi.yaml` in `docs/api/`
- The `docs/` directory is cloned into MkDocs for the documentation site

## Architecture

<!-- Describe your application's architecture here once you've chosen your stack -->
