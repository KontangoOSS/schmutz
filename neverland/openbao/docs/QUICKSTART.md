# Quick Start

## Prerequisites

- Docker & Docker Compose
- Git access to `git.konoss.org`

## Setup

```bash
# Clone the repository
git clone https://git.konoss.org/APPORG/APPNAME.git
cd APPNAME

# Configure environment
cp config/.env.example config/.env
# Edit config/.env with your settings
```

## Next Steps

1. Add your source code to `src/`
2. Add a `Dockerfile` to `build/container/`
3. Configure `config/konfig.json` with your app's Konfig UUID
4. Override `make test` and `make lint` targets in the Makefile
5. Write tests in `tests/`
6. Add `swagger.json` and `openapi.yaml` to `docs/api/`
7. Update this guide with your specific setup steps
