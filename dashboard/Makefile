.PHONY: build run docker test

build:
	go build -o build/ziti-dash ./cmd/ziti-dash

run: build
	ZITI_ADMIN_PASS=$${ZITI_ADMIN_PASS:?set ZITI_ADMIN_PASS} ./build/ziti-dash

docker:
	docker build -t ziti-dash .

test:
	go vet ./...
