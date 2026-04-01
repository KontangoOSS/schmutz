# Schmutz — L4 zero-trust edge firewall

.PHONY: build build-static test lint clean

BINARY   := schmutz
BUILD    := build/binary
SRC      := src
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	cd $(SRC) && go build -o ../$(BUILD)/$(BINARY) ./cmd/schmutz

build-static:
	cd $(SRC) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -ldflags="-s -w -X main.version=$(VERSION)" \
		-o ../$(BUILD)/$(BINARY) ./cmd/schmutz

test:
	cd $(SRC) && go test ./...

lint:
	cd $(SRC) && go vet ./...

clean:
	rm -f $(BUILD)/$(BINARY)
