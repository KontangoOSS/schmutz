# APPNAME

APPDESCRIPTION

## Quick Start

```bash
# Clone and configure
git clone https://git.konoss.org/APPORG/APPNAME.git
cd APPNAME
cp config/.env.example config/.env
```

See [Quick Start Guide](docs/guides/quickstart.md) for full setup instructions.

## Project Structure

```
APPNAME/
├── config/
│   ├── konfig.json        Kontango / Konfig app config
│   ├── .env               Local environment variables
│   ├── .env.example       Environment template
│   └── .editorconfig      Editor settings
├── src/                   Application source code
├── tests/                 Test suite
├── build/
│   ├── container/         Dockerfiles, compose configs
│   ├── binary/            Compiled builds
│   └── scripts/           Build automation
├── docs/                  Documentation (syncs to MkDocs)
│   ├── api/               API specs (swagger.json + openapi.yaml)
│   ├── guides/            Quickstart, architecture, runbook
│   ├── archive/           Archived documents
│   └── brandkit/          Branding assets
├── scripts/
│   └── ci/                CI scripts (discover.sh)
├── .woodpecker/           Woodpecker CI pipelines
├── .forgejo/              Forgejo Actions workflows
├── Makefile               Build/test/lint targets
├── KONTANGO.md            Kontango ecosystem info
└── LISCENSE.md            MIT license
```

## CI/CD Pipeline

```
PR opened       →  02-validate  →  03-build (dry-run)
Merge to main   →  01-discover  →  02-validate  →  03-build  →  04-deploy
                   (metadata)     (creds+files)   (container)   (lint→test→provision→deploy→docs)
```

See [.woodpecker/README.md](.woodpecker/README.md) for full pipeline documentation.

## Documentation

- [Quick Start](docs/guides/quickstart.md)
- [API Reference](docs/api/index.md)
- [Architecture](docs/guides/architecture.md)
- [Runbook](docs/guides/runbook.md)
- [Kontango Ecosystem](KONTANGO.md)

## License

MIT — see [LISCENSE.md](LISCENSE.md)
