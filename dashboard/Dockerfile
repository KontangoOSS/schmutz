FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/ziti-dash ./cmd/ziti-dash

FROM alpine:3
RUN apk add --no-cache ca-certificates
COPY --from=build /bin/ziti-dash /usr/local/bin/ziti-dash
COPY frontend/ /app/frontend/

WORKDIR /app
ENV WEB_DIR=frontend
ENV LISTEN_ADDR=:8080

EXPOSE 8080
ENTRYPOINT ["ziti-dash"]
