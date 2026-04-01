# Schmutz — join any zero-trust network

ENDPOINT ?= https://join.kontango.net

## Join a network
join:
	curl -sf $(ENDPOINT)/install | sh

## Build the join binary from source
build:
	cd src && go build -ldflags="-s -w" -o ../build/binary/schmutz-join ./cmd/join/

## Build for all platforms
release:
	@mkdir -p build/binary
	@for target in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64; do \
		os=$${target%%/*}; arch=$${target##*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		name="schmutz-join-$${os}-$${arch}$${ext}"; \
		echo "  $$name"; \
		cd src && GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -ldflags="-s -w" \
			-o ../build/binary/$$name ./cmd/join/ && cd ..; \
	done

## Run tests
test:
	cd src && go test ./...

## Clean
clean:
	rm -rf build/binary/*
