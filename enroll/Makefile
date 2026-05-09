.PHONY: build test docker docker-push lint clean

BINARIES := enroll-server schmutz-server
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

build: $(addprefix build/,$(BINARIES))

build/%:
	go build -ldflags="-s -w" -o $@ ./cmd/$*

test:
	go test -race ./...

docker: docker-enroll-server docker-schmutz-server

docker-enroll-server:
	docker build -t git.kontango.io/kore/enroll-server:latest \
	             -t git.kontango.io/kore/enroll-server:$(GIT_SHA) \
	             --build-arg BINARY=enroll-server .

docker-schmutz-server:
	docker build -t git.kontango.io/kore/schmutz-server:latest \
	             -t git.kontango.io/kore/schmutz-server:$(GIT_SHA) \
	             --build-arg BINARY=schmutz-server .

docker-push: docker
	docker push git.kontango.io/kore/enroll-server:latest
	docker push git.kontango.io/kore/enroll-server:$(GIT_SHA)
	docker push git.kontango.io/kore/schmutz-server:latest
	docker push git.kontango.io/kore/schmutz-server:$(GIT_SHA)

lint:
	go vet ./...

clean:
	rm -rf build/
