# Stage 1: Build binary
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install ca-certificates for HTTPS calls
RUN apk add --no-cache ca-certificates git

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build statically linked binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/searxng-mcp ./cmd/searxng-mcp

# Stage 2: Production image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/searxng-mcp /usr/local/bin/searxng-mcp

# Run stdio MCP server as default container entrypoint
ENTRYPOINT ["/usr/local/bin/searxng-mcp"]
