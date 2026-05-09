FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o neverland ./cmd/neverland/

FROM debian:12-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    qemu-utils \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /build/neverland /opt/neverland/neverland
WORKDIR /opt/neverland
ENV LISTEN_ADDR=0.0.0.0:8091
EXPOSE 8091
ENTRYPOINT ["/opt/neverland/neverland"]
