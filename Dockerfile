# Stage 1: Build binary
FROM golang:1.25-alpine AS builder

ARG VERSION=dev

WORKDIR /app

# git: for VCS-tagged module downloads; ca-certificates: for HTTPS fetches
RUN apk add --no-cache ca-certificates git

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a static, stripped binary with injected version
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-w -s -X main.version=${VERSION}" \
      -o /app/searxng-mcp ./cmd/searxng-mcp

# Stage 2: Minimal production image
FROM alpine:3.20

ARG VERSION=dev

# OCI image labels
LABEL org.opencontainers.image.title="go-searxng-mcp" \
      org.opencontainers.image.description="SearXNG MCP server (stdio) exposing search/read tools to LLM clients" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.source="https://github.com/srinikhiltumu/go-searxng-mcp" \
      org.opencontainers.image.maintainer="srinikhiltumu"

# ca-certificates: HTTPS calls to SearXNG; tzdata: correct timestamps/logs
RUN apk add --no-cache ca-certificates tzdata

# Non-root user for least-privilege execution
RUN adduser -D -u 10001 app

WORKDIR /app

# Copy the statically linked binary from the builder stage
COPY --from=builder /app/searxng-mcp /usr/local/bin/searxng-mcp

# Ensure the working directory is owned by the non-root user
RUN chown -R 10001:10001 /app

# Run as the non-root user
USER 10001

# This is a stdio MCP server: it communicates over stdin/stdout, so an
# HTTP-based HEALTHCHECK is not applicable. Explicitly disable it.
HEALTHCHECK NONE

ENTRYPOINT ["/usr/local/bin/searxng-mcp"]
