# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

A read-only MySQL MCP (Model Context Protocol) server written in Go. It exposes safe MySQL introspection tools to Claude Desktop, Cursor IDE, Claude Code, and HTTP clients (ChatGPT). The server enforces read-only access via a paranoid SQL validator and supports multiple simultaneous database connections.

## Commands

```bash
# Build & run
make build           # produces ./bin/mysql-mcp-server
make run             # build + run (requires MYSQL_DSN env var)
make release         # multi-platform binaries (linux/darwin/windows, amd64/arm64)
make docker          # build Docker image

# Code quality
make fmt             # format Go code
make fmt-check       # check formatting without modifying
make lint            # golangci-lint (falls back to go vet)
make vet             # go vet only
make security        # gosec security scanner
make vuln            # govulncheck
make qa              # fmt + vet + lint + test (quick QA)
make qa-full         # full QA pipeline including integration tests

# Testing
make test                    # unit tests only
make test-security           # SQL validator tests
make test-integration        # full integration suite via Docker Compose
make test-integration-80     # against MySQL 8.0
make test-integration-84     # against MySQL 8.4
make test-integration-90     # against MySQL 9.0
make test-integration-all    # all MySQL + MariaDB versions
make test-integration-ssh    # SSH tunnel integration test
make test-sakila             # Sakila database integration tests
make coverage                # test coverage report
make coverage-html           # HTML coverage report

# Docker helpers for integration tests
make test-mysql-up
make test-mysql-down
make test-mysql-logs
```

To run a single test:
```bash
go test ./cmd/mysql-mcp-server/... -run TestFunctionName -v
go test ./internal/util/... -run TestSQLValidator -v
```

Integration tests require Docker. They use `testcontainers-go` to spin up MySQL containers automatically.

## Architecture

### Modes

The server runs in two modes, selected at startup:
- **MCP mode** (default, stdio): Used by Claude Desktop / Cursor / Claude Code. Speaks the MCP protocol over stdin/stdout.
- **HTTP mode** (`--http-port`): REST API for ChatGPT and generic HTTP clients.

An optional metrics HTTP sidecar (`--metrics-port`) tracks token usage with a dashboard.

### Request Flow

```
AI Client → MCP stdio or HTTP REST
         → Tool Handler (tools.go / tools_extended.go / tools_diagnostics.go)
         → wrapTool() decorator (logging, token tracking, error handling)
         → SQL Validator (read-only enforcement)
         → ConnectionManager (multi-DSN pool per connection)
         → go-sql-driver/mysql
         → MySQL / MariaDB
```

### Key Files

| File | Purpose |
|---|---|
| `cmd/mysql-mcp-server/main.go` | Entry point, MCP server setup, global state |
| `cmd/mysql-mcp-server/tools.go` | Core tools: `list_databases`, `list_tables`, `describe_table`, `run_query`, `ping`, `use_connection` |
| `cmd/mysql-mcp-server/tools_extended.go` | Extended tools: `explain_query`, `list_indexes`, `show_create_table`, `database_size`, `foreign_keys`, etc. |
| `cmd/mysql-mcp-server/tools_diagnostics.go` | Diagnostic tools: `process_list`, `kill_query`, `slow_query_log` |
| `cmd/mysql-mcp-server/connection.go` | `ConnectionManager`: multi-DSN pool registry, atomic active-connection switching |
| `cmd/mysql-mcp-server/http.go` | HTTP REST API server and handlers |
| `cmd/mysql-mcp-server/tool_wrappers.go` | `wrapTool()` decorator applied to all tool handlers |
| `internal/config/config.go` | Config struct; load priority: env vars → config file → defaults |
| `internal/mysql/client.go` | DB client: connection pooling, query execution, timeouts, retry logic |
| `internal/util/sql_validator.go` | Paranoid read-only SQL enforcement — blocks all non-SELECT/SHOW/DESCRIBE/EXPLAIN |
| `internal/sshtunnel/tunnel.go` | SSH bastion tunnel with host key verification |

### Connection Manager

`ConnectionManager` (in `connection.go`) is a singleton that maintains one `*sql.DB` pool per named DSN. Tools call `GetActive()` to get the current pool. The `use_connection` and `add_connection` tools call `SetActive()` to switch atomically. The manager uses an RLock for probing existing connections and a write lock when adding new ones; pending adds are reserved to prevent duplicate I/O.

### SQL Safety

`internal/util/sql_validator.go` enforces read-only access:
- Allowlist: `SELECT`, `SHOW`, `DESCRIBE`, `EXPLAIN`, `WITH ... SELECT`
- Blocklist: DDL, DML, `LOAD_FILE`, dangerous functions, multi-statement queries, comments that could mask payloads
- Runs before every query reaches the database

### Tool Registration Pattern

All MCP tools are registered via the Go MCP SDK. Each handler is wrapped with `wrapTool()` for consistent logging, token estimation, and error formatting. Types for all tool inputs/outputs live in `types.go`.

### Configuration

Config is loaded from (highest priority first):
1. Environment variables (e.g., `MYSQL_DSN`, `MYSQL_HOST`, `QUERY_TIMEOUT_SECONDS`)
2. YAML/JSON config file (`--config` flag)
3. Defaults

Multiple DSNs can be specified via `MYSQL_DSN_LIST` (comma-separated) or in the config file's `connections` array.
