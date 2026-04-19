# MySQL MCP Server

<div align="center">
  <img src="./MysqlMCPServerBanner.png" alt="MySQL MCP Server Banner" width="800"/>
  
  [![Version](https://img.shields.io/badge/version-1.7.0-rc.4-blue.svg)](https://github.com/askdba/mysql-mcp-server/releases)
  [![Go](https://img.shields.io/badge/go-1.24+-00ADD8.svg)](https://golang.org/)
  [![License](https://img.shields.io/badge/license-Apache--2.0-green.svg)](LICENSE)
</div>

A fast, read-only MySQL Server for the Model Context Protocol (MCP) written in Go.

This project exposes safe MySQL introspection tools to Claude Desktop via MCP. Claude can explore databases, describe schemas, and execute controlled read-only SQL queries — ideal for secure development assistance, debugging, analytics, and schema documentation.

## Features

- Fully read-only (blocks all non-SELECT/SHOW/DESCRIBE/EXPLAIN)
- **Multi-DSN Support**: Connect to multiple MySQL or MariaDB instances, switch via tool
- **MariaDB Support**: Native compatibility with MariaDB 10.x and 11.x
- **Vector Search** (MySQL 9.0+): Similarity search on vector columns
- MCP tools:
  - list_databases, list_tables, describe_table
  - run_query (safe and row-limited)
  - ping, server_info
  - list_connections, use_connection (multi-DSN)
  - vector_search, vector_info (MySQL 9.0+)
- Supports MySQL 8.0, 8.4, 9.0+ and MariaDB 10.x, 11.x
- Query timeouts, structured logging, audit logs; optional **live token metrics** and **`/status`** dashboard in HTTP mode
- **Performance**: configurable pool/query timeouts, server-side row caps, `explain_query` plan warnings
- Single Go binary
- Unit and integration tests (Testcontainers)
- Native integration with Claude Desktop MCP

## Installation

### Homebrew (macOS/Linux)

```bash
brew install askdba/tap/mysql-mcp-server
```

**Update local installation** (after a new release):

```bash
brew update && brew upgrade mysql-mcp-server
```

**Apple Silicon — “Cannot install under Rosetta 2 in ARM default prefix (/opt/homebrew)”**  
Your shell is running **Homebrew under Rosetta (Intel)** while **`/opt/homebrew`** is **ARM**. They must match. Use one of:

- Run upgrades as ARM: **`arch -arm64 brew upgrade mysql-mcp-server`** (or **`arch -arm64 /opt/homebrew/bin/brew upgrade mysql-mcp-server`**).
- **Terminal.app → Get Info** → uncheck **Open using Rosetta**, then open a new terminal.
- Put **`/opt/homebrew/bin`** first in **`PATH`** so you use the ARM `brew` and binary.

**Claude Desktop:** on Apple Silicon, point **`command`** at **`/opt/homebrew/bin/mysql-mcp-server`**. **`/usr/local/opt/mysql-mcp-server/...`** is normally **Intel** Homebrew and can stay on an old version.

### Docker

```bash
docker pull ghcr.io/askdba/mysql-mcp-server:latest
```

> Note: Docker image tags use the raw version number without a leading "v"
> (e.g., `1.5.0`, not `v1.5.0`).

### Download Binary

Download the latest release from [GitHub Releases](https://github.com/askdba/mysql-mcp-server/releases).

Available for:
- macOS (Intel & Apple Silicon)
- Linux (amd64 & arm64)
- Windows (amd64)

### Build from Source

```bash
git clone https://github.com/askdba/mysql-mcp-server.git
cd mysql-mcp-server
make build
```

Binary output: `bin/mysql-mcp-server`

## Quickstart

### Option A: Homebrew (macOS/Linux)

```bash
brew install askdba/tap/mysql-mcp-server
mysql-mcp-server --version
```

Then follow the client-specific setup in the [Claude Desktop](#claude-desktop-integration), [Claude Code](#claude-code-integration), or [Cursor IDE](#cursor-ide-integration) sections below.

> **Tip:** `brew info askdba/tap/mysql-mcp-server` shows a config reminder after install.

### Option B: Build from Source

```bash
git clone https://github.com/askdba/mysql-mcp-server.git
cd mysql-mcp-server
make build
```

When running from a cloned repo you can also use the interactive setup script:

```bash
./scripts/quickstart.sh
```

This will test your MySQL connection, optionally create a read-only MCP user, and generate your Claude Desktop configuration.

## Configuration

Environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| MYSQL_DSN | Yes | – | MySQL DSN |
| MYSQL_MAX_ROWS | No | 200 | Max rows returned per query |
| MYSQL_QUERY_TIMEOUT_SECONDS | No | 30 | Query timeout (seconds); wins over `MYSQL_QUERY_TIMEOUT` when both are set |
| MYSQL_QUERY_TIMEOUT | No | – | Query timeout in **milliseconds** (e.g. `30000`); used only if `MYSQL_QUERY_TIMEOUT_SECONDS` is unset |
| MYSQL_POOL_SIZE | No | – | Alias for `MYSQL_MAX_OPEN_CONNS` (pool size); `MYSQL_MAX_OPEN_CONNS` overrides when both are set |
| MYSQL_MCP_EXTENDED | No | 0 | Enable extended tools (set to 1) |
| MYSQL_MCP_ENABLE_ADD_CONNECTION | No | 0 | Set `1` to expose the MCP tool **`add_connection`** (runtime DSN registration; see [add_connection](#add_connection)). Requires **`MYSQL_MCP_EXTENDED=1`**. Not available over the HTTP REST API. |
| MYSQL_MCP_JSON_LOGS | No | 0 | Enable JSON structured logging (set to 1) |
| MYSQL_MCP_TOKEN_TRACKING | No | 0 | Enable estimated token usage tracking (set to 1) |
| MYSQL_MCP_TOKEN_MODEL | No | cl100k_base | Tokenizer encoding to use for estimation |
| MYSQL_MCP_TOKEN_CARD | No | **on** when `MYSQL_MCP_HTTP` is set | **`/status`** live token dashboard + listing in **`GET /api`**; omit to use default **on**; set to **0** to disable |
| MYSQL_MCP_AUDIT_LOG | No | – | Path to audit log file |
| MYSQL_MCP_ALLOWED_DATABASES | No | – | Comma-separated schema allowlist (empty = all allowed). With an allowlist, **`run_query`** rejects **`SHOW DATABASES`** / **`SHOW DATABASES LIKE`**—use **`list_databases`**. |
| MYSQL_MCP_STRICT_READ_ONLY | No | 0 | Set `1` to enable `transaction_read_only=ON` on new connections |
| MYSQL_MCP_PROCESS_ADMIN | No | 0 | Set `1` to enable **`process_list`** / **`kill_query`** (extended); **`kill_query`** issues **`KILL QUERY`** (cancels the running statement only, not the connection) |
| MYSQL_MCP_READ_AUDIT_TOOL | No | 0 | Set `1` to enable `read_audit_log` when audit path is set |
| MYSQL_MCP_SLOW_QUERY_TOOL | No | 0 | Set `1` to enable `slow_query_log` tool (extended) |
| MYSQL_MCP_VECTOR | No | 0 | Enable vector tools for MySQL 9.0+ (set to 1) |
| MYSQL_MCP_HTTP | No | 0 | Enable REST API mode (set to 1); **mutually exclusive** with stdio MCP |
| MYSQL_MCP_METRICS_HTTP | No | 0 | With **stdio MCP only**: expose **`/status`** + **`/api/metrics/tokens`** on **`MYSQL_HTTP_PORT`** (same process as Claude/Cursor) |
| MYSQL_HTTP_PORT | No | 9306 | Port for REST API **or** metrics sidecar |
| MYSQL_HTTP_RATE_LIMIT | No | 0 | Enable rate limiting for HTTP mode (set to 1) |
| MYSQL_HTTP_RATE_LIMIT_RPS | No | 100 | Rate limit: requests per second |
| MYSQL_HTTP_RATE_LIMIT_BURST | No | 200 | Rate limit: burst size |
| MYSQL_MAX_OPEN_CONNS | No | 10 | Max open database connections (overrides `MYSQL_POOL_SIZE` when both are set) |
| MYSQL_MAX_IDLE_CONNS | No | 5 | Max idle database connections |
| MYSQL_CONN_MAX_LIFETIME_MINUTES | No | 30 | Connection max lifetime in minutes |
| MYSQL_CONN_MAX_IDLE_TIME_MINUTES | No | 5 | Max idle time before connection is closed |
| MYSQL_PING_TIMEOUT_SECONDS | No | 5 | Database ping/health check timeout |
| MYSQL_MCP_DB_RETRY_MAX | No | 3 | Retries for transient errors on **`run_query`** and **`ping`** (0 disables retries) |
| MYSQL_MCP_DB_RETRY_MAX_INTERVAL_MS | No | 10000 | Max exponential-backoff interval between retries (milliseconds) |
| MYSQL_HTTP_REQUEST_TIMEOUT_SECONDS | No | 60 | HTTP request timeout in REST API mode |
| MYSQL_SSL | No | – | Enable SSL/TLS for connections (true, false, skip-verify, preferred) |

### SSL/TLS Configuration

Enable encrypted connections to MySQL servers:

| SSL Value | Description |
|-----------|-------------|
| `true` | Enable TLS with certificate verification |
| `false` | Disable TLS (default) |
| `skip-verify` | Enable TLS without certificate verification (self-signed certs) |
| `preferred` | Maps to `skip-verify` (Go MySQL driver limitation) |

> **Note:** The Go MySQL driver doesn't support `tls=preferred`. When you specify `preferred`, it is automatically mapped to `skip-verify` to ensure TLS is enabled.

**Environment variable:**

```bash
# Enable SSL for all connections
export MYSQL_SSL="true"

# Or per-connection SSL
export MYSQL_DSN_1_SSL="skip-verify"
```

**Config file:**

```yaml
connections:
  production:
    dsn: "user:pass@tcp(prod:3306)/db?parseTime=true"
    ssl: "true"
```

### SSH Tunneling (Bastion Host)

> **Security — host keys (read this):** By default the server **verifies the SSH bastion’s host key** (same idea as OpenSSH). Without that, a network attacker could present a fake bastion and intercept database traffic. Use a **`known_hosts`** file (default: **`~/.ssh/known_hosts`**) or pin the key with **`MYSQL_SSH_HOST_KEY_FINGERPRINT`**. Paths support **`~`**. Turning verification off is **opt-in only** and **dangerous**: set **`MYSQL_SSH_STRICT_HOST_KEY_CHECKING=false`** or YAML **`strict_host_key_checking: false`** (JSON: **`ssh_strict_host_key_checking: false`**) only if you accept that risk.

Connect to MySQL through an SSH bastion when the database is not directly reachable:

| Variable | Description |
|----------|-------------|
| `MYSQL_SSH_HOST` | Bastion hostname |
| `MYSQL_SSH_USER` | SSH username |
| `MYSQL_SSH_KEY_PATH` | Path to private key file (`~` expanded) |
| `MYSQL_SSH_PORT` | SSH port (default 22) |
| `MYSQL_SSH_KNOWN_HOSTS` | Path to OpenSSH `known_hosts` file (optional; default `~/.ssh/known_hosts` when verifying) |
| `MYSQL_SSH_HOST_KEY_FINGERPRINT` | Pin server key, e.g. `SHA256:...` or legacy `MD5:...` / `aa:bb:...` (optional; overrides file lookup for the callback) |
| `MYSQL_SSH_STRICT_HOST_KEY_CHECKING` | Default strict. Set to `false` / `0` / `no` / `off` to **disable** host key verification (**insecure**) |

**Environment variables:**

```bash
export MYSQL_SSH_HOST="bastion.example.com"
export MYSQL_SSH_USER="deploy"
export MYSQL_SSH_KEY_PATH="$HOME/.ssh/id_rsa"
export MYSQL_SSH_KNOWN_HOSTS="$HOME/.ssh/known_hosts"
# Optional: instead of a file, pin the bastion key (from ssh-keygen -lf):
# export MYSQL_SSH_HOST_KEY_FINGERPRINT="SHA256:...."
export MYSQL_DSN="user:pass@tcp(mysql.internal:3306)/mydb?parseTime=true"
```

**Config file:**

```yaml
connections:
  production:
    dsn: "user:pass@tcp(mysql.internal:3306)/mydb?parseTime=true"
    ssh:
      host: "bastion.example.com"
      user: "deploy"
      key_path: "~/.ssh/id_rsa"
      port: 22  # optional, default 22
      strict_host_key_checking: true   # default when omitted; false = insecure, opt-in only
      known_hosts: "~/.ssh/known_hosts"  # optional
      host_key_fingerprint: "SHA256:...."  # optional alternative to known_hosts
```

### Multi-DSN Configuration

Configure multiple MySQL connections using numbered environment variables:

```bash
# Default connection
export MYSQL_DSN="user:pass@tcp(localhost:3306)/db1?parseTime=true"

# Additional connections
export MYSQL_DSN_1="user:pass@tcp(prod-server:3306)/production?parseTime=true"
export MYSQL_DSN_1_NAME="production"
export MYSQL_DSN_1_DESC="Production database"
export MYSQL_DSN_1_SSL="true"  # Enable SSL for this connection

export MYSQL_DSN_2="user:pass@tcp(staging:3306)/staging?parseTime=true"
export MYSQL_DSN_2_NAME="staging"
export MYSQL_DSN_2_DESC="Staging database"
```

Or use JSON configuration:

```bash
export MYSQL_CONNECTIONS='[
  {"name": "production", "dsn": "user:pass@tcp(prod:3306)/db?parseTime=true", "description": "Production", "ssl": "true"},
  {"name": "staging", "dsn": "user:pass@tcp(staging:3306)/db?parseTime=true", "description": "Staging"}
]'
```

**Runtime vs static DSNs:** Connections defined here (env vars, `MYSQL_CONNECTIONS`, or YAML) are loaded at process startup into the shared `ConnectionManager`. The optional MCP tool **`add_connection`** (requires **`MYSQL_MCP_EXTENDED=1`** and **`MYSQL_MCP_ENABLE_ADD_CONNECTION=1`**) can register **additional** named DSNs later without restart. New names must not collide with existing connection names. Runtime-registered pools use the same global “active” connection as config-loaded ones—see [add_connection](#add_connection). There is no separate per-client session; all MCP clients using the same server process share one active DSN unless you run multiple server instances.

### Configuration File

As an alternative to environment variables, you can use a YAML or JSON configuration file.

**Config file search order:**
1. `--config /path/to/config.yaml` (command line flag)
2. `MYSQL_MCP_CONFIG` environment variable
3. `./mysql-mcp-server.yaml` (current directory)
4. `~/.config/mysql-mcp-server/config.yaml` (user config)
5. `/etc/mysql-mcp-server/config.yaml` (system config)

**Example config file (`mysql-mcp-server.yaml`):**

```yaml
# Database connections
connections:
  default:
    dsn: "user:pass@tcp(localhost:3306)/mydb?parseTime=true"
    description: "Local development database"
  production:
    dsn: "readonly:pass@tcp(prod:3306)/prod?parseTime=true"
    description: "Production (read-only)"
    ssl: "true"  # Enable TLS with certificate verification

# Query settings
query:
  max_rows: 200
  timeout_seconds: 30

# Connection pool
pool:
  max_open_conns: 10
  max_idle_conns: 5
  conn_max_lifetime_minutes: 30

# Features
features:
  extended_tools: true
  vector_tools: false

# HTTP/REST API (optional)
http:
  enabled: false
  port: 9306
```

**Command line options:**

```bash
# Use specific config file
mysql-mcp-server --config /path/to/config.yaml

# Validate config file
mysql-mcp-server --validate-config /path/to/config.yaml

# Print current configuration as YAML
mysql-mcp-server --print-config

# Silent mode: suppress INFO/WARN logs (only errors to stderr)
mysql-mcp-server --silent --config /path/to/config.yaml

# Daemon mode: run HTTP server in background (Unix; use with MYSQL_MCP_HTTP=1)
MYSQL_MCP_HTTP=1 mysql-mcp-server --daemon --config /path/to/config.yaml
```

**Silent and daemon mode:** Use `-s` / `--silent` to reduce log noise in production (INFO and WARN are suppressed; ERROR still goes to stderr). Use `-d` / `--daemon` to run the HTTP server detached in the background on Unix. For long-running services, use the example [systemd unit](contrib/systemd/mysql-mcp-server.service) or [launchd plist](contrib/launchd/com.askdba.mysql-mcp-server.plist). See [Silent and daemon mode](docs/silent-and-daemon.md) for details.

**Priority:** Environment variables override config file values, allowing:
- Base configuration in file
- Environment-specific overrides via env vars
- Docker/K8s secret injection via env vars

See [`examples/config.yaml`](examples/config.yaml) and [`examples/config.json`](examples/config.json) for complete examples.

Example:

```bash
export MYSQL_DSN="root:password@tcp(127.0.0.1:3306)/mysql?parseTime=true"
export MYSQL_MAX_ROWS=200
export MYSQL_QUERY_TIMEOUT_SECONDS=30
```

Run:

```bash
make run
```

### Version Information

Check the installed version:

```bash
mysql-mcp-server --version
```

Output:
```
mysql-mcp-server v1.7.0-rc.4
  Build time: <build time>
  Git commit: <git sha>
```

## Claude Desktop Integration

Edit your Claude Desktop configuration file:

**macOS:**
```
~/Library/Application Support/Claude/claude_desktop_config.json
```

**Windows:**
```
%APPDATA%\Claude\claude_desktop_config.json
```

**Linux:**
```
~/.config/Claude/claude_desktop_config.json
```

Add:

```json
{
  "mcpServers": {
    "mysql": {
      "command": "mysql-mcp-server",
      "env": {
        "MYSQL_DSN": "user:password@tcp(127.0.0.1:3306)/mydb?parseTime=true",
        "MYSQL_MAX_ROWS": "200",
        "MYSQL_MCP_EXTENDED": "1"
      }
    }
  }
}
```

> **Note:** If Claude Desktop cannot find the binary, replace `"mysql-mcp-server"` with the full path from `which mysql-mcp-server`.

**Token dashboard (`/status`) in the same process as MCP:** stdio MCP does not open HTTP by default. Set **`MYSQL_MCP_METRICS_HTTP=1`** so this instance also listens on **`MYSQL_HTTP_PORT`** (default **9306**) for **`http://127.0.0.1:9306/status`** and **`/api/metrics/tokens`** — the same counters as Claude’s tool calls. Use **`MYSQL_MCP_TOKEN_TRACKING=1`** so the dashboard is meaningful. Do **not** set **`MYSQL_MCP_HTTP=1`** here (that mode replaces MCP with full REST only).

```json
"env": {
  "MYSQL_DSN": "user:password@tcp(127.0.0.1:3306)/mydb?parseTime=true",
  "MYSQL_MCP_TOKEN_TRACKING": "1",
  "MYSQL_MCP_METRICS_HTTP": "1",
  "MYSQL_HTTP_PORT": "9306"
}
```

Restart Claude Desktop.

## Cursor IDE Integration

Cursor IDE supports the Model Context Protocol (MCP). Configure it to use mysql-mcp-server:

Edit your Cursor MCP configuration file:

**macOS:**
```
~/.cursor/mcp.json
```

**Windows:**
```
%APPDATA%\Cursor\mcp.json
```

**Linux:**
```
~/.config/Cursor/mcp.json
```

Add:

```json
{
  "mcpServers": {
    "mysql": {
      "command": "mysql-mcp-server",
      "env": {
        "MYSQL_DSN": "root:password@tcp(127.0.0.1:3306)/mydb?parseTime=true",
        "MYSQL_MAX_ROWS": "200",
        "MYSQL_MCP_EXTENDED": "1"
      }
    }
  }
}
```

**Token dashboard (`/status`) in the same process as MCP:** Same behavior as Claude Desktop — stdio MCP does not open HTTP by default. Set **`MYSQL_MCP_TOKEN_TRACKING=1`**, **`MYSQL_MCP_METRICS_HTTP=1`**, and **`MYSQL_HTTP_PORT`** (default **9306**) for **`http://127.0.0.1:9306/status`** and **`/api/metrics/tokens`**. Do **not** set **`MYSQL_MCP_HTTP=1`** here (full REST replaces stdio MCP).

```json
"env": {
  "MYSQL_DSN": "root:password@tcp(127.0.0.1:3306)/mydb?parseTime=true",
  "MYSQL_MAX_ROWS": "200",
  "MYSQL_MCP_EXTENDED": "1",
  "MYSQL_MCP_TOKEN_TRACKING": "1",
  "MYSQL_MCP_METRICS_HTTP": "1",
  "MYSQL_HTTP_PORT": "9306"
}
```

Restart Cursor after editing `mcp.json`.

**With Docker:**

```json
{
  "mcpServers": {
    "mysql": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "-e", "MYSQL_DSN=root:password@tcp(host.docker.internal:3306)/mydb?parseTime=true",
        "-e", "MYSQL_MCP_EXTENDED=1",
        "ghcr.io/askdba/mysql-mcp-server:latest"
      ]
    }
  }
}
```

Restart Cursor after saving the configuration.

## Claude Code Integration

Claude Code supports MCP servers via the CLI or a project-scoped `.mcp.json` file.

### Option 1: CLI (quick setup)

```bash
claude mcp add --transport stdio \
  --env MYSQL_DSN="user:password@tcp(127.0.0.1:3306)/mydb?parseTime=true" \
  --env MYSQL_MCP_EXTENDED=1 \
  mysql -- mysql-mcp-server
```

### Option 2: Project `.mcp.json`

This repo includes a `.mcp.json` that Claude Code auto-discovers when you open the project. Set your DSN in the shell before starting Claude Code:

```bash
export MYSQL_DSN="user:password@tcp(127.0.0.1:3306)/mydb?parseTime=true"
claude  # start Claude Code — MySQL tools will be available automatically
```

To use the same config in your own project, copy `.mcp.json` to your project root and set `MYSQL_DSN` in your shell.

**Verify the integration** by asking Claude Code: *"List all databases on this MySQL server."*

## MCP Tools

### list_databases

Returns non-system databases.

### list_tables

Input:

```json
{ "database": "employees" }
```

### describe_table

Input:

```json
{ "database": "employees", "table": "salaries" }
```

### run_query

Input:

```json
{ "sql": "SELECT id, name FROM users LIMIT 5" }
```

Optional database context:

```json
{ "sql": "SELECT * FROM users LIMIT 5", "database": "myapp" }
```

**Offset pagination** (SELECT/UNION without an existing `LIMIT` in the SQL): pass **`offset`** (zero-based). The tool appends **`LIMIT (max_rows+1) OFFSET n`** server-side, returns at most **`max_rows`** rows, and sets **`has_more`** / **`next_offset`** when another page may exist. Do not add your own `LIMIT` when using **`offset`**.

- Rejects non-read-only SQL
- Enforces row limit
- Enforces timeout
- Retries transient connection/network errors with backoff (see **`MYSQL_MCP_DB_RETRY_MAX`**)

### ping

Tests database connectivity and returns latency.

Output:

```json
{ "success": true, "latency_ms": 2, "message": "pong" }
```

### server_info

Returns MySQL server details.

Output:

```json
{
  "version": "8.0.36",
  "version_comment": "MySQL Community Server - GPL",
  "uptime_seconds": 86400,
  "current_database": "myapp",
  "current_user": "mcp@localhost",
  "character_set": "utf8mb4",
  "collation": "utf8mb4_0900_ai_ci",
  "max_connections": 151,
  "threads_connected": 5
}
```

### list_connections

List all configured MySQL connections.

Output:

```json
{
  "connections": [
    {"name": "production", "dsn": "user:****@tcp(prod:3306)/db", "active": true},
    {"name": "staging", "dsn": "user:****@tcp(staging:3306)/db", "active": false}
  ],
  "active": "production"
}
```

### use_connection

Switch to a different MySQL connection.

Input:

```json
{ "name": "staging" }
```

Output:

```json
{
  "success": true,
  "active": "staging",
  "message": "Switched to connection 'staging'",
  "database": "staging_db"
}
```

### add_connection

**MCP only.** Available when the server runs in **MCP mode** (stdio) with **`MYSQL_MCP_EXTENDED=1`** and **`MYSQL_MCP_ENABLE_ADD_CONNECTION=1`**. It registers a new named MySQL connection at runtime and switches the **process-wide** active connection for **that MCP process**. There is **no** HTTP route to register a new DSN. **REST API mode** (`MYSQL_MCP_HTTP=1`) is a **separate** process: [`GET /api/connections`](#api-endpoints) and [`POST /api/connections/use`](#api-endpoints) only list and switch connections **loaded in that HTTP server instance**—they do **not** see names added via MCP in a stdio process, and MCP does not reach into a separate HTTP-only deployment.

Enable:

```bash
export MYSQL_MCP_EXTENDED=1
export MYSQL_MCP_ENABLE_ADD_CONNECTION=1
```

Input:

```json
{
  "name": "analytics",
  "dsn": "appuser:secret@tcp(db.example.com:3306)/warehouse?parseTime=true",
  "description": "Optional label"
}
```

| Field | Required | Rules |
|-------|----------|--------|
| `name` | Yes | Non-empty; must not match an existing connection name (atomic check in `ConnectionManager`). |
| `dsn` | Yes | Non-empty; must parse as a valid MySQL DSN. **`root`** as the MySQL user is **rejected** at registration time. |
| `description` | No | Stored with the connection metadata. |

Behavior: builds a `ConnectionConfig`, opens a pool, **`Ping()`** with the tool request context (timeouts/cancellation), then **`SetActive(name)`**. On activation failure after a successful add, the server removes the new pool (**rollback**). The active connection applies to **all** tools and clients on this process until switched again (`use_connection` or another `add_connection`).

Output (success):

```json
{
  "success": true,
  "active": "analytics",
  "message": "Added and switched to connection 'analytics'"
}
```

Typical errors (tool returns an error result): duplicate name, invalid DSN, `root` user blocked, unreachable host (ping failure), activation/rollback failure. See [Runtime connection registration (add_connection)](#runtime-connection-registration-add_connection) for security implications.

## Vector Tools (MySQL 9.0+)

Enable with:

```bash
export MYSQL_MCP_VECTOR=1
```

### vector_search

Perform similarity search on vector columns.

Input:

```json
{
  "database": "myapp",
  "table": "embeddings",
  "column": "embedding",
  "query": [0.1, 0.2, 0.3, ...],
  "limit": 10,
  "select": "id, title, content",
  "distance_func": "cosine"
}
```

Output:

```json
{
  "results": [
    {"distance": 0.123, "data": {"id": 1, "title": "Doc 1", "content": "..."}},
    {"distance": 0.456, "data": {"id": 2, "title": "Doc 2", "content": "..."}}
  ],
  "count": 2
}
```

Distance functions: `cosine` (default), `euclidean`, `dot`

### vector_info

List vector columns in a database.

Input:

```json
{ "database": "myapp" }
```

Output:

```json
{
  "columns": [
    {"table": "embeddings", "column": "embedding", "dimensions": 768, "index_name": "vec_idx"}
  ],
  "vector_support": true,
  "mysql_version": "9.0.0"
}
```

## Extended Tools (MYSQL_MCP_EXTENDED=1)

Enable with:

```bash
export MYSQL_MCP_EXTENDED=1
```

Runtime registration of additional DSNs (MCP tool **`add_connection`**) also requires **`MYSQL_MCP_ENABLE_ADD_CONNECTION=1`** — see [add_connection](#add_connection).

### list_indexes

List indexes on a table.

```json
{ "database": "myapp", "table": "users" }
```

### show_create_table

Get the CREATE TABLE statement.

```json
{ "database": "myapp", "table": "users" }
```

### explain_query

Get execution plan for a SELECT query. Responses include optional **`warnings`** (e.g. full table scan, filesort) when the plan suggests optimizations.

```json
{ "sql": "SELECT * FROM users WHERE id = 1", "database": "myapp" }
```

### list_views

List views in a database.

```json
{ "database": "myapp" }
```

### list_triggers

List triggers in a database.

```json
{ "database": "myapp" }
```

### list_procedures

List stored procedures.

```json
{ "database": "myapp" }
```

### list_functions

List stored functions.

```json
{ "database": "myapp" }
```

### list_partitions

List table partitions.

```json
{ "database": "myapp", "table": "events" }
```

### database_size

Get database size information.

```json
{ "database": "myapp" }
```

Or get all databases:

```json
{}
```

### table_size

Get table size information.

```json
{ "database": "myapp" }
```

### foreign_keys

List foreign key constraints.

```json
{ "database": "myapp", "table": "orders" }
```

### list_status

List MySQL server status variables.

```json
{ "pattern": "Threads%" }
```

### list_variables

List MySQL server configuration variables.

```json
{ "pattern": "%buffer%" }
```

## Security Model

### SQL Safety (Paranoid Mode)

The server enforces strict SQL validation:

**Allowed operations:**
- `SELECT`, `SHOW`, `DESCRIBE`, `EXPLAIN`

**Blocked patterns:**
- Multi-statement queries (semicolons)
- File operations: `LOAD_FILE()`, `INTO OUTFILE`, `INTO DUMPFILE`
- DDL: `CREATE`, `ALTER`, `DROP`, `TRUNCATE`, `RENAME`
- DML: `INSERT`, `UPDATE`, `DELETE`, `REPLACE`
- Admin: `GRANT`, `REVOKE`, `FLUSH`, `KILL`, `SHUTDOWN`
- Dangerous functions: `SLEEP()`, `BENCHMARK()`, `GET_LOCK()`
- Transaction control: `BEGIN`, `COMMIT`, `ROLLBACK`

### Recommended MySQL User

```sql
CREATE USER 'mcp'@'localhost' IDENTIFIED BY 'strongpass';
GRANT SELECT ON *.* TO 'mcp'@'localhost';
```

### Runtime connection registration (add_connection)

When **`MYSQL_MCP_ENABLE_ADD_CONNECTION=1`** (and extended mode), any MCP client that can invoke tools may call **`add_connection`**. This server exposes MCP over **stdio** by default; it does **not** add a separate HTTP Basic/API-key layer beyond whatever protects access to that MCP process (typically local stdio from the host application). **Treat MCP access as privileged:** anyone who can call tools can attempt to register a new outbound DSN to hosts reachable from the server. If you also run **REST API mode** (`MYSQL_MCP_HTTP=1`) in another process or deployment, secure that interface independently—it is separate from MCP.

**Controls enforced by the server today:**

- **DSN parsing:** Invalid DSNs are rejected before opening a connection.
- **User blocklist:** The literal MySQL user **`root`** is rejected for runtime registration to reduce accidental high-privilege pools.
- **Duplicate names:** Registration is refused if the name already exists (`ConnectionManager`); adds are not silently merged with static config names.
- **Connectivity check:** A pool is opened and **`Ping()`** runs before the connection is considered registered; failures do not leave a half-open registration (failed adds are rolled back if activation fails after add).
- **Read-only mode:** If **`MYSQL_MCP_STRICT_READ_ONLY=1`**, new pools still get `transaction_read_only=ON` on the driver path like other connections.

**Operational guidance:** Disable **`MYSQL_MCP_ENABLE_ADD_CONNECTION`** (default `0`) unless you need runtime registration. Prefer firewall/network policies so the server host can only reach approved database endpoints. Use least-privilege MySQL users in registered DSNs (same as [Recommended MySQL User](#recommended-mysql-user)). Review [Multi-DSN Configuration](#multi-dsn-configuration) for how static and runtime entries coexist in one process.

## Observability

### JSON Structured Logging

Enable JSON logs for production:

```bash
export MYSQL_MCP_JSON_LOGS=1
```

Output:
```json
{"timestamp":"2025-01-15T10:30:00.123Z","level":"INFO","message":"query executed","fields":{"tool":"run_query","duration_ms":15,"row_count":42}}
```

### Audit Logging

Enable query audit trail:

```bash
export MYSQL_MCP_AUDIT_LOG=/var/log/mysql-mcp-audit.jsonl
```

Each query is logged with timing, success/failure, and row counts.

### Token Usage Estimation (Optional)

Enable estimated token counting for tool inputs/outputs to monitor LLM context usage:

```bash
export MYSQL_MCP_TOKEN_TRACKING=1
export MYSQL_MCP_TOKEN_MODEL=cl100k_base  # default, used by GPT-4/Claude
```

Or via YAML config file:

```yaml
logging:
  token_tracking: true
  token_model: "cl100k_base"
```

When enabled:
- JSON logs include a `tokens` object with estimated input/output/total tokens
- Audit log entries for `run_query` include `input_tokens` and `output_tokens`
- All other tools also emit token estimates when `token_tracking` is enabled

**Example JSON log output:**

```json
{
  "level": "INFO",
  "msg": "query executed",
  "tool": "run_query",
  "duration_ms": 45,
  "row_count": 10,
  "tokens": {
    "input_estimated": 25,
    "output_estimated": 150,
    "total_estimated": 175,
    "model": "cl100k_base"
  }
}
```

**Notes:**
- Token counts are *estimates* using tiktoken encoding, not actual LLM billing
- For payloads exceeding 1MB, a heuristic (~4 bytes per token) is used to prevent memory spikes
- The feature is disabled by default to avoid overhead when not needed

### Query Timing

All queries are automatically timed and logged with:
- Execution duration (milliseconds)
- Row count returned
- Tool name
- Truncated query (for debugging)

## Performance Tuning

### Connection pool and query timeouts

Configure the pool and how long queries may run (see also **`MYSQL_POOL_SIZE`** and **`MYSQL_QUERY_TIMEOUT`** / **`MYSQL_QUERY_TIMEOUT_SECONDS`** in the [configuration table](#configuration)):

```bash
export MYSQL_MAX_OPEN_CONNS=20      # Max open connections (or use MYSQL_POOL_SIZE as an alias)
export MYSQL_MAX_IDLE_CONNS=10      # Max idle connections
export MYSQL_CONN_MAX_LIFETIME_MINUTES=60  # Connection lifetime
export MYSQL_QUERY_TIMEOUT=30000    # Optional: timeout in ms (30 s) if you do not use MYSQL_QUERY_TIMEOUT_SECONDS
```

`run_query` applies a server-side **`LIMIT`** when absent, returns **`truncated`** when more rows exist than the cap (non-pagination mode), returns **`has_more`** / **`next_offset`** when **`offset`** pagination is used, and may **`warning`** on `SELECT *`. Use **`explain_query`** for plan **`warnings`** (full scans, filesort, etc.).

**MySQL `max_execution_time` vs MCP timeouts:** The server enforces **`MYSQL_QUERY_TIMEOUT_SECONDS`** (or **`MYSQL_QUERY_TIMEOUT`** in ms) on the Go side for every tool. That is independent of the MySQL session variable `max_execution_time` (often `0`, meaning “no engine-side cap”). For operator clarity: configure MCP query timeout for how long the client should wait; configure MySQL if you also want the optimizer to abort expensive SELECTs.

**Concurrent tool calls:** Each parallel MCP tool call may use a pooled connection. If the host issues several tools at once, set **`MYSQL_MAX_OPEN_CONNS`** (alias **`MYSQL_POOL_SIZE`**) high enough—e.g. **10–20**—so threads do not queue behind a single connection.

### Security options and privileged tools (extended mode)

| Variable | Purpose |
|----------|---------|
| `MYSQL_MCP_ALLOWED_DATABASES` | Comma-separated schema allowlist. When set, tools that take a `database` argument must use an allowed name; `list_databases` / `database_size` only expose allowed schemas; `run_query` requires `database` and cannot be used to hop schemas via omission. **`run_query`** rejects **`SHOW DATABASES`** and **`SHOW DATABASES LIKE`** (use **`list_databases`**). Qualified names in SQL, **`EXPLAIN`** (including **`FORMAT=`** / **`EXTENDED`**), and inner DML in **`EXPLAIN`** are checked against the allowlist. **`slow_query_log`** (table mode) only returns `mysql.slow_log` rows whose **`db`** column matches an allowed schema (case-insensitive); rows with null/empty `db` are omitted. |
| `MYSQL_MCP_STRICT_READ_ONLY` | When `1`, new driver connections run with `transaction_read_only=ON` (harder to accidentally issue writes if grants allow them). |
| `MYSQL_MCP_PROCESS_ADMIN` | Enables **`process_list`** and **`kill_query`** (issues **`KILL QUERY`**, not connection kill; plus HTTP `/api/processlist`, `/api/kill`). Requires appropriate MySQL privileges (`CONNECTION_ADMIN` / `PROCESS`, etc.). |
| `MYSQL_MCP_READ_AUDIT_TOOL` | Enables **`read_audit_log`** when **`MYSQL_MCP_AUDIT_LOG`** is set (tail of the audit JSON file). |
| `MYSQL_MCP_SLOW_QUERY_TOOL` | Enables **`slow_query_log`** (reads `mysql.slow_log` when `log_output` includes `TABLE`, otherwise returns file settings). |

**`server_info`:** Pass **`detailed: true`** (MCP) or **`?detailed=1`** (HTTP) for ping latency, **`Threads_running`**, **`Slow_queries`**, **`Questions`**, and InnoDB buffer pool hit rate when stats are available. If **`MYSQL_MCP_TOKEN_TRACKING=1`**, **`token_metrics`** is always included (cumulative since process start).

YAML file equivalents live under **`security:`** in the config file (`allowed_databases`, `strict_read_only`, `process_admin`, `read_audit_tool`, `slow_query_tool`).

## Testing

### Prerequisites

- Go 1.24+
- Docker and Docker Compose (for integration tests)
- MySQL client (optional, for manual database access)

### Unit Tests

Run unit tests (no external dependencies):

```bash
make test
```

### Integration Tests

Integration tests run against real MySQL instances using Docker Compose.

**Test against MySQL 8.0 (default):**

```bash
make test-integration-80
```

**Test against MySQL 8.4:**

```bash
make test-integration-84
```

**Test against MySQL 9.0:**

```bash
make test-integration-90
```

**Test against all supported versions (MySQL & MariaDB):**

```bash
make test-integration-all
```

**Test specifically against MariaDB 11.4:**

```bash
make test-integration-mariadb-11
```

### Sakila Database Tests

The [Sakila sample database](https://dev.mysql.com/doc/sakila/en/) provides comprehensive integration testing with a realistic DVD rental store schema featuring:
- 16 tables with foreign key relationships
- Views, stored procedures, and triggers
- FULLTEXT indexes and ENUM/SET types
- Sample data for complex query testing

**Run Sakila tests:**

```bash
# Against MySQL 8.0
make test-sakila

# Against MySQL 8.4
make test-sakila-84

# Against MySQL 9.0
make test-sakila-90
```

For a multi-version Sakila test matrix (including alternate ports and health-check helpers), see [`test-steps.md`](test-steps.md) at the repo root (canonical) or [`docs/sakila-test-steps.md`](docs/sakila-test-steps.md).

The Sakila tests cover:
- Multi-table JOINs (film→actors, customer→address→city→country)
- Aggregation queries (COUNT, SUM, AVG, GROUP BY)
- Subqueries (IN, NOT IN, correlated)
- View queries
- FULLTEXT search
- Index and foreign key verification

### Security Tests

Run SQL injection and validation security tests:

```bash
make test-security
```

### Manual Database Management

Start and stop test MySQL containers manually:

```bash
# Start MySQL 8.0 container
make test-mysql-up

# Start all MySQL versions (8.0, 8.4, 9.0)
make test-mysql-all-up

# Stop all test containers
make test-mysql-down

# View container logs
make test-mysql-logs
```

### Environment Variables

When running tests manually, set the DSN:

```bash
# MySQL 8.0 (host port 13306 in docker-compose.test.yml — avoids local MySQL on 3306)
export MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:13306)/testdb?parseTime=true"

# MySQL 8.4 (port 3307)
export MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:3307)/testdb?parseTime=true"

# MySQL 9.0 (port 3308)
export MYSQL_TEST_DSN="mcpuser:mcppass00@tcp(127.0.0.1:3308)/testdb?parseTime=true"
```

Test credentials are **`mcpuser` / `mcppass00`** (see `tests/sql/mcp_test_user.sql`). On **`testdb`**, this user has DDL/DML so integration tests can create fixtures; **`sakila`** stays read-only via `mcp_test_user_sakila.sql`. After upgrading an existing Docker volume, recreate it so init scripts run: `docker compose -f docker-compose.test.yml down -v`.

Prefer **`127.0.0.1`** over **`localhost`** when the database runs in Docker (especially on macOS): `localhost` may resolve to IPv6 (`::1`) while the published port is IPv4-only, causing connection timeouts until the DSN uses `127.0.0.1`.

The **`mysql80`** service publishes **13306** on the host (not **3306**) so integration tests do not accidentally connect to a different MySQL instance already listening on **3306**.

Then run tests directly:

```bash
go test -v -tags=integration ./tests/integration/...
```

### Test Database Schema

Integration tests use two databases:

**testdb** (`tests/sql/init.sql`):
- Tables: `users`, `orders`, `products`, `special_data`
- Views: `user_orders`
- Stored procedures: `get_user_by_id`
- Sample test data for basic integration tests

**sakila** (`tests/sql/sakila-schema.sql`, `tests/sql/sakila-data.sql`):
- 16 tables: `actor`, `film`, `customer`, `rental`, `payment`, `inventory`, etc.
- 6 views: `film_list`, `customer_list`, `staff_list`, `sales_by_store`, etc.
- Stored functions and procedures
- FULLTEXT indexes on `film_text`
- Realistic sample data (50 actors, 30 films, 10 customers, 20 rentals)

## Docker

### Using Pre-built Image

**Basic usage:**

```bash
docker run -e MYSQL_DSN="user:password@tcp(host.docker.internal:3306)/mydb" \
  ghcr.io/askdba/mysql-mcp-server:latest
```

> **Note:** Use `host.docker.internal` instead of `localhost` to connect from inside the container to MySQL on your host machine (macOS/Windows).

**With extended tools enabled:**

```bash
docker run \
  -e MYSQL_DSN="user:password@tcp(host.docker.internal:3306)/mydb" \
  -e MYSQL_MCP_EXTENDED=1 \
  ghcr.io/askdba/mysql-mcp-server:latest
```

**With all options:**

```bash
docker run \
  -e MYSQL_DSN="user:password@tcp(host.docker.internal:3306)/mydb" \
  -e MYSQL_MCP_EXTENDED=1 \
  -e MYSQL_MCP_VECTOR=1 \
  -e MYSQL_MAX_ROWS=500 \
  -e MYSQL_QUERY_TIMEOUT_SECONDS=60 \
  ghcr.io/askdba/mysql-mcp-server:latest
```

### Claude Desktop with Docker

Edit `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "mysql": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "-e", "MYSQL_DSN=user:password@tcp(host.docker.internal:3306)/mydb",
        "ghcr.io/askdba/mysql-mcp-server:latest"
      ]
    }
  }
}
```

**With extended tools:**

```json
{
  "mcpServers": {
    "mysql": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "-e", "MYSQL_DSN=user:password@tcp(host.docker.internal:3306)/mydb",
        "-e", "MYSQL_MCP_EXTENDED=1",
        "ghcr.io/askdba/mysql-mcp-server:latest"
      ]
    }
  }
}
```

### Docker Compose

```yaml
services:
  mysql:
    image: mysql:8.4
    environment:
      MYSQL_ROOT_PASSWORD: rootpass
      MYSQL_DATABASE: testdb
    ports:
      - "3306:3306"

  mcp:
    image: ghcr.io/askdba/mysql-mcp-server:latest
    depends_on:
      - mysql
    environment:
      MYSQL_DSN: "root:rootpass@tcp(mysql:3306)/testdb?parseTime=true"
      MYSQL_MCP_EXTENDED: "1"
```

Run:

```bash
docker compose up
```

### Build Locally

```bash
docker build -t mysql-mcp-server .
```

## REST API Mode

Enable HTTP REST API mode to use with ChatGPT, Gemini, or any HTTP client:

```bash
export MYSQL_DSN="user:password@tcp(localhost:3306)/mydb"
export MYSQL_MCP_HTTP=1
export MYSQL_HTTP_PORT=9306  # Optional, defaults to 9306
mysql-mcp-server
```

### Rate Limiting

Enable per-IP rate limiting for production deployments:

```bash
export MYSQL_HTTP_RATE_LIMIT=1
export MYSQL_HTTP_RATE_LIMIT_RPS=100    # 100 requests/second
export MYSQL_HTTP_RATE_LIMIT_BURST=200  # Allow bursts up to 200
```

When rate limited, clients receive HTTP 429 (Too Many Requests) with a `Retry-After: 1` header.

### Running as a service (daemon)

To run the REST API server in the background or under a process manager:

- **Unix (foreground detach):** `MYSQL_MCP_HTTP=1 mysql-mcp-server --daemon --config /path/to/config.yaml` (forks and exits the parent; child runs detached).
- **systemd:** Copy and customize [contrib/systemd/mysql-mcp-server.service](contrib/systemd/mysql-mcp-server.service), then `systemctl enable --now mysql-mcp-server`.
- **macOS launchd:** Copy and customize [contrib/launchd/com.askdba.mysql-mcp-server.plist](contrib/launchd/com.askdba.mysql-mcp-server.plist) into `~/Library/LaunchAgents/` or `/Library/LaunchDaemons/`.
- **Windows:** Use a Windows Service wrapper or run in a terminal; `--daemon` is not supported.

Use `--silent` to suppress INFO/WARN logs when running under a service manager. See [docs/silent-and-daemon.md](docs/silent-and-daemon.md).

### API Endpoints

**Discovery (`GET /api`):** The JSON response includes an **`endpoints`** map that lists **only routes the server has registered** for the current configuration—same rules as the mux: core routes always; extended routes only if **`MYSQL_MCP_EXTENDED=1`**; **`/api/processlist`** and **`/api/kill`** only if extended **and** **`MYSQL_MCP_PROCESS_ADMIN=1`**; **`/api/audit-log`** only if extended **and** read-audit is enabled (**`MYSQL_MCP_READ_AUDIT_TOOL=1`** with **`MYSQL_MCP_AUDIT_LOG`**); **`/api/slow-log`** only if extended **and** **`MYSQL_MCP_SLOW_QUERY_TOOL=1`**; vector routes only if **`MYSQL_MCP_VECTOR=1`**; **`/status`** appears in the index only when the token card is enabled. **`modes`** in the JSON reflects **`extended`**, **`vector`**, and **`token_card`**.

**Runtime DSN registration:** There is **no** HTTP route to register a new connection. The MCP tool **`add_connection`** (optional; **`MYSQL_MCP_EXTENDED=1`** and **`MYSQL_MCP_ENABLE_ADD_CONNECTION=1`**) is the only supported registration path and applies to the **MCP (stdio) process** only. In **REST API mode**, the process never registers tools; `GET /api/connections` lists only connections configured for **that HTTP server process** (not connections added by MCP elsewhere).

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/api` | API index: registered endpoints + **`modes`** (see Discovery above) |
| GET | `/api/databases` | List databases |
| GET | `/api/tables?database=` | List tables |
| GET | `/api/describe?database=&table=` | Describe table |
| POST | `/api/query` | Run SQL query |
| GET | `/api/ping` | Ping database |
| GET | `/api/server-info` | Server info |
| GET | `/api/connections` | List connections (masked DSNs) known to **this HTTP server process** (config-loaded; no MCP `add_connection` in REST mode) |
| POST | `/api/connections/use` | Switch active connection: JSON body `{"name": "<connection_name>"}`. Invalid JSON or missing `name` → **`400`**. Unknown connection name → **`200`** with **`success: false`** in the JSON body (same semantics as MCP `use_connection`). |

**`POST /api/connections/use` response:** JSON with `success`, `active`, `message`, `database` when applicable (see [Response Format](#response-format)).

**Not implemented over HTTP:** `POST /api/connections` (register new DSN) — use MCP **`add_connection`** when enabled.

**Extended endpoints** (requires `MYSQL_MCP_EXTENDED=1`):

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/indexes?database=&table=` | List indexes |
| GET | `/api/create-table?database=&table=` | Show CREATE TABLE |
| POST | `/api/explain` | Explain query |
| GET | `/api/views?database=` | List views |
| GET | `/api/triggers?database=` | List triggers |
| GET | `/api/procedures?database=` | List procedures |
| GET | `/api/functions?database=` | List functions |
| GET | `/api/partitions?database=&table=` | List table partitions |
| GET | `/api/size/database?database=` | Database size |
| GET | `/api/size/tables?database=` | Table sizes |
| GET | `/api/foreign-keys?database=` | Foreign keys |
| GET | `/api/status?pattern=` | Server status |
| GET | `/api/variables?pattern=` | Server variables |
| GET | `/api/audit-log?lines=` | Tail lines from the MCP audit log (JSON). Listed only when extended **and** **`MYSQL_MCP_READ_AUDIT_TOOL=1`** with **`MYSQL_MCP_AUDIT_LOG`** configured. |
| GET | `/api/slow-log?limit=` | Slow query log rows or file/table settings. Listed only when extended **and** **`MYSQL_MCP_SLOW_QUERY_TOOL=1`**. |
| GET | `/api/processlist` | Active MySQL threads (`SHOW FULL PROCESSLIST`). Listed only when extended **and** **`MYSQL_MCP_PROCESS_ADMIN=1`**. Requires MySQL **`PROCESS`** (or equivalent) to succeed. |
| POST | `/api/kill` | Cancel the **current statement** on a connection: JSON body `{"id": <positive integer>}` (same id as **`/api/processlist`**). Executes **`KILL QUERY`**—the client connection stays open. Listed only when extended **and** **`MYSQL_MCP_PROCESS_ADMIN=1`**. Requires privilege to run **`KILL QUERY`** for that thread (e.g. **`CONNECTION_ADMIN`** or **`PROCESS`** as applicable). |

**Vector endpoints** (requires `MYSQL_MCP_VECTOR=1`):

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/vector/search` | Vector similarity search |
| GET | `/api/vector/info?database=` | Vector column info |

**Token metrics** (HTTP mode; **`/status`** defaults **on** when `MYSQL_MCP_HTTP` is set—use `MYSQL_MCP_TOKEN_CARD=0` to hide it; YAML `features.token_card` applies when not using that default):

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/metrics/tokens` | Cumulative token usage, cost estimate, recent queries (always registered; zeros when tracking off) |
| GET | `/status` | Live HTML dashboard (auto-refresh); listed in `GET /api` when the feature is enabled |

### Example Usage

**List databases:**
```bash
curl http://localhost:9306/api/databases
```

**Run a query:**
```bash
curl -X POST http://localhost:9306/api/query \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT * FROM users LIMIT 5", "database": "myapp"}'
```

**Get server info:**
```bash
curl http://localhost:9306/api/server-info
```

**List / switch connections (no HTTP “add” endpoint):**
```bash
curl http://localhost:9306/api/connections
curl -X POST http://localhost:9306/api/connections/use \
  -H "Content-Type: application/json" \
  -d '{"name": "staging"}'
```
Registering a **new** DSN at runtime is **MCP-only** via the **`add_connection`** tool when **`MYSQL_MCP_ENABLE_ADD_CONNECTION=1`** (see [add_connection](#add_connection)).

### Response Format

All responses follow this format:

```json
{
  "success": true,
  "data": { ... }
}
```

Error responses:

```json
{
  "success": false,
  "error": "error message"
}
```

### ChatGPT Custom GPT Integration

1. Start the REST API server on a publicly accessible host
2. Create a Custom GPT with Actions
3. Import the OpenAPI schema from `/api`
4. Configure authentication if needed

### Docker with REST API

```bash
docker run -p 9306:9306 \
  -e MYSQL_DSN="user:password@tcp(host.docker.internal:3306)/mydb" \
  -e MYSQL_MCP_HTTP=1 \
  -e MYSQL_MCP_EXTENDED=1 \
  ghcr.io/askdba/mysql-mcp-server:latest
```

## Documentation

- **[SQL Query Optimization Guide](docs/query_optimization_guide.md)**: Practical optimization patterns and query rewriting techniques using the Stack Exchange schema.
- **[Comprehensive MySQL Query Optimization Guide](docs/mysql_query_optimization_comprehensive.md)**: Deep technical insights into the query optimizer, advanced indexing strategies, execution plan analysis, and operational best practices.

## Project Structure

```
cmd/mysql-mcp-server/
├── main.go             -> Server entrypoint and tool registration
├── types.go            -> Input/output struct types for tools
├── tools.go            -> Core MCP tool handlers
├── tools_extended.go   -> Extended MCP tool handlers
├── http.go             -> HTTP REST API handlers and server
├── connection.go       -> Multi-DSN connection manager
└── logging.go          -> Structured and audit logging

internal/
├── api/                -> HTTP middleware and response utilities
├── config/             -> Configuration loader from environment
├── mysql/              -> MySQL client wrapper + tests
└── util/               -> Shared utilities (SQL validation, identifiers)

examples/               -> Example configs and test data
scripts/                -> Quickstart and utility scripts
bin/                    -> Built binaries
```

## Examples

The `examples/` folder contains:

- **`claude_desktop_config.json`** - Example Claude Desktop configuration
- **`test-dataset.sql`** - Demo database with tables, views, and sample data

Load the test dataset:

```bash
mysql -u root -p < examples/test-dataset.sql
```

This creates a `mcp_demo` database with:
- 5 categories, 13 products, 8 customers
- 9 orders with 16 order items
- Views: `order_summary`, `product_inventory`
- Stored procedure: `GetCustomerOrders`
- Stored function: `GetProductStock`

## Development

```bash
make fmt       # Format code
make lint      # Run linter
make test      # Run unit tests
make build     # Build binary
make release   # Build release binaries
```

## Releasing

Releases are automated via GitHub Actions and GoReleaser. Full checklist: **[docs/releasing.md](docs/releasing.md)**.

Quick steps: update [CHANGELOG.md](CHANGELOG.md), commit, then `git tag vX.Y.Z` and `git push origin vX.Y.Z`. After the workflow runs, complete post-release steps (CHANGELOG on main if needed, Homebrew tap README, local `brew upgrade`). See the doc for details.

## License

Apache License 2.0  
© 2025 Alkin Tezuysal
