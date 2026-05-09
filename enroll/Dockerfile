# Multi-stage build. Selects which binary to ship via --build-arg BINARY=name.
FROM golang:1.25 AS builder
ARG BINARY
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags="-s -w" -o /out/app ./cmd/${BINARY}

FROM gcr.io/distroless/base-debian12
COPY --from=builder /out/app /app
ENTRYPOINT ["/app"]
