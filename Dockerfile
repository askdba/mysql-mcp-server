# Build stage - use Alpine and download Go 1.24
FROM alpine:3.20 AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata curl

# Download and install Go 1.24.3 (latest 1.24.x)
ENV GO_VERSION=1.24.3
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o go.tar.gz \
    && tar -C /usr/local -xzf go.tar.gz \
    && rm go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV PATH="${GOPATH}/bin:${PATH}"

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY . .

# Build the binary
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /mysql-mcp-server ./cmd/mysql-mcp-server

# Final stage - minimal image
FROM alpine:3.20

# Install ca-certificates for HTTPS connections
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN adduser -D -g '' appuser

# Copy the binary
COPY --from=builder /mysql-mcp-server /usr/local/bin/mysql-mcp-server

# Use non-root user
USER appuser

# Environment variables (can be overridden)
ENV MYSQL_DSN=""
ENV MYSQL_MAX_ROWS="200"
ENV MYSQL_QUERY_TIMEOUT_SECONDS="30"
ENV MYSQL_MCP_EXTENDED="0"
ENV MYSQL_MCP_JSON_LOGS="0"

# The MCP server uses stdio, so no ports to expose
ENTRYPOINT ["mysql-mcp-server"]
