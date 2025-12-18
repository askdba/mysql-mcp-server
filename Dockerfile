# =============================================================================
# MySQL MCP Server - Hardened Multi-stage Dockerfile
# =============================================================================
# Security features:
# - Multi-stage build to minimize attack surface
# - Distroless base image (no shell, no package manager)
# - Non-root user execution
# - Static binary with no external dependencies
# - HEALTHCHECK for container orchestration
# =============================================================================

# -----------------------------------------------------------------------------
# Stage 1: Builder
# -----------------------------------------------------------------------------
FROM golang:1.24-alpine AS builder

# Install git for version info and ca-certificates
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version injection
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

# Build static binary with version info
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w \
        -X main.Version=${VERSION} \
        -X main.GitCommit=${GIT_COMMIT} \
        -X main.BuildTime=${BUILD_TIME}" \
    -o /mysql-mcp-server \
    ./cmd/mysql-mcp-server

# Verify the binary works
RUN /mysql-mcp-server --version

# -----------------------------------------------------------------------------
# Stage 2: Final - Distroless (most secure)
# -----------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

# Labels for container metadata
LABEL org.opencontainers.image.title="MySQL MCP Server" \
      org.opencontainers.image.description="Model Context Protocol server for MySQL databases" \
      org.opencontainers.image.url="https://github.com/askdba/mysql-mcp-server" \
      org.opencontainers.image.source="https://github.com/askdba/mysql-mcp-server" \
      org.opencontainers.image.vendor="askdba" \
      org.opencontainers.image.licenses="Apache-2.0"

# Copy timezone data and CA certificates from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /mysql-mcp-server /mysql-mcp-server

# Use non-root user (provided by distroless:nonroot)
USER nonroot:nonroot

# Environment variables with secure defaults
ENV MYSQL_DSN="" \
    MYSQL_MAX_ROWS="200" \
    MYSQL_QUERY_TIMEOUT_SECONDS="30" \
    MYSQL_MCP_EXTENDED="0" \
    MYSQL_MCP_JSON_LOGS="0"

# Health check - verify binary responds
# Note: Distroless has no shell, so we use the binary directly
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/mysql-mcp-server", "--version"]

# The MCP server uses stdio, no ports to expose
ENTRYPOINT ["/mysql-mcp-server"]
