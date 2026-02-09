# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /prom-relabel-proxy ./cmd/prom-relabel/

# Runtime stage
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /prom-relabel-proxy /prom-relabel-proxy
COPY configs/config.yaml /etc/prom-relabel-proxy/config.yaml

EXPOSE 8080 9090

ENTRYPOINT ["/prom-relabel-proxy"]
CMD ["-config", "/etc/prom-relabel-proxy/config.yaml"]
