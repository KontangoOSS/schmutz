# neverland — Tinkerbell management REST API

.PHONY: build test lint clean run docker-build docker-load deploy help

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
IMAGE   := git.kontango.io/kore/neverland:latest

## Build the neverland binary
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags="-s -w -X main.version=$(VERSION)" \
		-o ./neverland ./cmd/neverland/

## Run locally (requires KUBECONFIG to point at the target cluster)
run:
	go run ./cmd/neverland/

## Run unit tests
test:
	go test ./...

## Run go vet (treated as lint for now)
lint:
	go vet ./...

## Build the Docker image
docker-build:
	docker build -t $(IMAGE) .

## Build and import the image into a local k3s containerd
docker-load: docker-build
	docker save $(IMAGE) | sudo k3s ctr images import -

## Roll out a fresh deployment of neverland in tink-system
deploy: docker-load
	kubectl -n tink-system rollout restart deployment neverland
	kubectl -n tink-system rollout status deployment neverland --timeout=60s

## Remove build artifacts
clean:
	rm -f ./neverland

## Show this help
help:
	@grep -E '^##' Makefile | sed 's/## //'
