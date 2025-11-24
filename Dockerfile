# Stage 1: Build the Go binary
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies (if go.sum exists)
RUN if [ -f go.sum ]; then go mod download; fi

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a \
    -o mcp-hub \
    ./cmd/mcp-hub

# Stage 2: Minimal runtime with Docker CLI
FROM alpine:latest

# Install Docker CLI (for Docker transport support)
RUN apk add --no-cache docker-cli ca-certificates

# Copy the binary from builder
COPY --from=builder /build/mcp-hub /usr/local/bin/mcp-hub

# Create non-root user and add to docker group
RUN addgroup -g 980 docker && \
    addgroup -g 1000 mcpuser && \
    adduser -D -u 1000 -G mcpuser mcpuser && \
    adduser mcpuser docker && \
    mkdir -p /app && \
    chown -R mcpuser:mcpuser /app

# Set working directory
WORKDIR /app

# Switch to non-root user
USER mcpuser

# Expose the default port
EXPOSE 8080

# The binary accepts --config flag for configuration file path
ENTRYPOINT ["/usr/local/bin/mcp-hub"]
CMD ["--config", "/app/config.json"]
