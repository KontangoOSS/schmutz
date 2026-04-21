# Schmutz — L4 zero-trust edge firewall + enrollment client

.PHONY: build build-join build-bootstrap release release-bootstrap release-schmutz test lint clean help

BINARY   := schmutz
BUILD    := build/binary
SRC      := src
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

## Build the edge gateway
build:
	cd $(SRC) && go build -ldflags="-s -w" -o ../$(BUILD)/$(BINARY) ./cmd/schmutz

## Build the join client
build-join:
	cd $(SRC) && go build -ldflags="-s -w" -o ../$(BUILD)/schmutz-join ./cmd/join/

## Build join client for all platforms
release:
	@mkdir -p $(BUILD)
	@for target in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64; do \
		os=$${target%%/*}; arch=$${target##*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		name="schmutz-join-$${os}-$${arch}$${ext}"; \
		echo "  $$name"; \
		cd $(SRC) && GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -ldflags="-s -w" \
			-o ../$(BUILD)/$$name ./cmd/join/ && cd ..; \
	done

## Run tests
test:
	cd $(SRC) && go test ./...

## Vet
lint:
	cd $(SRC) && go vet ./...

## Clean build artifacts
clean:
	rm -rf $(BUILD)/*

## Build the agent binary
build-agent:
	cd $(SRC) && go build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD)/schmutz-agent ./cmd/agent

## Cross-compile agent for Linux amd64 and arm64
release-agent:
	@mkdir -p $(BUILD)
	cd $(SRC) && GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD)/schmutz-agent-linux-amd64 ./cmd/agent
	cd $(SRC) && GOOS=linux GOARCH=arm64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD)/schmutz-agent-linux-arm64 ./cmd/agent

## Cross-compile schmutz for Linux amd64 and arm64
release-schmutz:
	@mkdir -p $(BUILD)
	cd $(SRC) && GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD)/schmutz-linux-amd64 ./cmd/schmutz
	cd $(SRC) && GOOS=linux GOARCH=arm64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD)/schmutz-linux-arm64 ./cmd/schmutz

## Build bootstrap binary
build-bootstrap:
	cd $(SRC) && go build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD)/schmutz-bootstrap ./cmd/bootstrap

## Cross-compile bootstrap
release-bootstrap:
	@mkdir -p $(BUILD)
	cd $(SRC) && GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD)/schmutz-bootstrap-linux-amd64 ./cmd/bootstrap
	cd $(SRC) && GOOS=linux GOARCH=arm64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o ../$(BUILD)/schmutz-bootstrap-linux-arm64 ./cmd/bootstrap

## Show targets
help:
	@grep -E '^##' Makefile | sed 's/## //'
