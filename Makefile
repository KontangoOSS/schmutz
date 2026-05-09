# Schmutz platform — umbrella Makefile
#
# Sub-repos expected as siblings under the same parent directory:
#   ../schmutz-agent/       → the agent binary
#   ../schmutz-enroll/      → enroll-server (hub API)
#   ../schmutz-controller-new/  → management plane
#   ../schmutz-shared/      → shared Go types
#   ../caddy/               → custom Caddy build

-include .env
export FORGEJO_TOKEN

.PHONY: build build-caddy build-enroll build-controller build-agent \
        bin bin-enroll bin-agent up down logs bootstrap test clean help

# ── Docker images ────────────────────────────────────────────────────────────

build: build-caddy build-enroll build-controller build-agent
	@echo "All images built."

build-caddy:
	docker build -t schmutz-caddy:dev ../caddy/

build-enroll:
	docker build --build-arg FORGEJO_TOKEN=$(FORGEJO_TOKEN) \
		-t schmutz-enroll:dev ../schmutz-enroll/

build-controller:
	docker build --build-arg FORGEJO_TOKEN=$(FORGEJO_TOKEN) \
		-t schmutz-controller:dev ../schmutz-controller-new/

build-agent:
	docker build --build-arg FORGEJO_TOKEN=$(FORGEJO_TOKEN) \
		-t schmutz-agent:dev ../schmutz-agent/

# ── Production binaries ───────────────────────────────────────────────────────

bin: bin-enroll bin-agent

bin-enroll:
	cd ../schmutz-enroll && \
		GONOSUMDB=git.konoss.org GOPROXY=direct,https://proxy.golang.org \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -ldflags='-s -w' \
		-o /tmp/enroll-server ./cmd/enroll-server/
	@echo "→ /tmp/enroll-server ($(shell stat -c%s /tmp/enroll-server 2>/dev/null || echo ?) bytes)"

bin-agent:
	cd ../schmutz-agent && \
		GONOSUMDB=git.konoss.org GOPROXY=direct,https://proxy.golang.org \
		GOOS=linux GOARCH=amd64 \
		go build -trimpath -ldflags='-s -w' \
		-o /tmp/schmutz ./cmd/schmutz/
	@echo "→ /tmp/schmutz ($(shell stat -c%s /tmp/schmutz 2>/dev/null || echo ?) bytes)"

# ── Compose stack ────────────────────────────────────────────────────────────

up:
	FORGEJO_TOKEN=$(FORGEJO_TOKEN) docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

bootstrap:
	./scripts/bao-bootstrap.sh

# ── Tests ─────────────────────────────────────────────────────────────────────

test:
	@echo "=== schmutz-shared ==="
	cd ../schmutz-shared && \
		GONOSUMDB=git.konoss.org GOPROXY=direct,https://proxy.golang.org go test ./...
	@echo "=== schmutz-enroll ==="
	cd ../schmutz-enroll && \
		GONOSUMDB=git.konoss.org GOPROXY=direct,https://proxy.golang.org go test ./...
	@echo "=== schmutz-agent ==="
	cd ../schmutz-agent && \
		GONOSUMDB=git.konoss.org GOPROXY=direct,https://proxy.golang.org go test ./...
	@echo "=== schmutz-controller ==="
	cd ../schmutz-controller-new && go test ./...
	@echo "All tests passed."

# ── Cleanup ───────────────────────────────────────────────────────────────────

clean:
	docker compose down -v
	docker rmi schmutz-caddy:dev schmutz-enroll:dev schmutz-controller:dev schmutz-agent:dev 2>/dev/null || true

## Show targets
help:
	@echo "Docker images:  make build | build-{caddy,enroll,controller,agent}"
	@echo "Binaries:       make bin | bin-{enroll,agent}"
	@echo "Stack:          make up | down | logs | bootstrap"
	@echo "Tests:          make test"
	@echo "Cleanup:        make clean"
