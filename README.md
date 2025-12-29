# MySQL MCP Server

<div align="center">
  <img src="./MysqlMCPServerBanner.png" alt="MySQL MCP Server Banner" width="800"/>
  
  [![Version](https://img.shields.io/badge/version-1.3.1-blue.svg)](https://github.com/askdba/mysql-mcp-server/releases)
  [![Go](https://img.shields.io/badge/go-1.24+-00ADD8.svg)](https://golang.org/)
  [![License](https://img.shields.io/badge/license-Apache--2.0-green.svg)](LICENSE)
</div>

A fast, read-only MySQL Server for the Model Context Protocol (MCP) written in Go.

This project exposes safe MySQL introspection tools to Claude Desktop via MCP. Claude can explore databases, describe schemas, and execute controlled read-only SQL queries — ideal for secure development assistance, debugging, analytics, and schema documentation.

## Features

- Fully read-only (blocks all non-SELECT/SHOW/DESCRIBE/EXPLAIN)
- **Multi-DSN Support**: Connect to multiple MySQL instances, switch via tool
- **Vector Search** (MySQL 9.0+): Similarity search on vector columns
- MCP tools:
  - list_databases, list_tables, describe_table
  - run_query (safe and row-limited)
  - ping, server_info
  - list_connections, use_connection (multi-DSN)
  - vector_search, vector_info (MySQL 9.0+)
- Supports MySQL 8.0, 8.4, 9.0+
- Query timeouts, structured logging, audit logs
- Single Go binary
- Unit and integration tests (Testcontainers)
- Native integration with Claude Desktop MCP

## Installation

### Homebrew (macOS/Linux)

```bash
brew install askdba/tap/mysql-mcp-server
```

### Docker

```bash
docker pull ghcr.io/askdba/mysql-mcp-server:latest
```

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

Run the interactive setup script:

```bash
./scripts/quickstart.sh
```

This will:
1. Test your MySQL connection
2. Optionally create a read-only MCP user
3. Generate your Claude Desktop configuration
4. Optionally load a test dataset

## Configuration

Environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| MYSQL_DSN | Yes | – | MySQL DSN |
| MYSQL_MAX_ROWS | No | 200 | Max rows returned |
| MYSQL_QUERY_TIMEOUT_SECONDS | No | 30 | Query timeout |
| MYSQL_MCP_EXTENDED | No | 0 | Enable extended tools (set to 1) |
| MYSQL_MCP_JSON_LOGS | No | 0 | Enable JSON structured logging (set to 1) |
| MYSQL_MCP_TOKEN_TRACKING | No | 0 | Enable estimated token usage tracking (set to 1) |
| MYSQL_MCP_TOKEN_MODEL | No | cl100k_base | Tokenizer encoding to use for estimation |
| MYSQL_MCP_AUDIT_LOG | No | – | Path to audit log file |
| MYSQL_MCP_VECTOR | No | 0 | Enable vector tools for MySQL 9.0+ (set to 1) |
| MYSQL_MCP_HTTP | No | 0 | Enable REST API mode (set to 1) |
| MYSQL_HTTP_PORT | No | 9306 | HTTP port for REST API mode |
| MYSQL_HTTP_RATE_LIMIT | No | 0 | Enable rate limiting for HTTP mode (set to 1) |
| MYSQL_HTTP_RATE_LIMIT_RPS | No | 100 | Rate limit: requests per second |
| MYSQL_HTTP_RATE_LIMIT_BURST | No | 200 | Rate limit: burst size |
| MYSQL_MAX_OPEN_CONNS | No | 10 | Max open database connections |
| MYSQL_MAX_IDLE_CONNS | No | 5 | Max idle database connections |
| MYSQL_CONN_MAX_LIFETIME_MINUTES | No | 30 | Connection max lifetime in minutes |
| MYSQL_CONN_MAX_IDLE_TIME_MINUTES | No | 5 | Max idle time before connection is closed |
| MYSQL_PING_TIMEOUT_SECONDS | No | 5 | Database ping/health check timeout |
| MYSQL_HTTP_REQUEST_TIMEOUT_SECONDS | No | 60 | HTTP request timeout in REST API mode |

### Multi-DSN Configuration

Configure multiple MySQL connections using numbered environment variables:

```bash
# Default connection
export MYSQL_DSN="user:pass@tcp(localhost:3306)/db1?parseTime=true"

# Additional connections
export MYSQL_DSN_1="user:pass@tcp(prod-server:3306)/production?parseTime=true"
export MYSQL_DSN_1_NAME="production"
export MYSQL_DSN_1_DESC="Production database"

export MYSQL_DSN_2="user:pass@tcp(staging:3306)/staging?parseTime=true"
export MYSQL_DSN_2_NAME="staging"
export MYSQL_DSN_2_DESC="Staging database"
```

Or use JSON configuration:

```bash
export MYSQL_CONNECTIONS='[
  {"name": "production", "dsn": "user:pass@tcp(prod:3306)/db?parseTime=true", "description": "Production"},
  {"name": "staging", "dsn": "user:pass@tcp(staging:3306)/db?parseTime=true", "description": "Staging"}
]'
```

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
```

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
mysql-mcp-server v1.3.1
  Build time: 2025-12-21T11:43:11Z
  Git commit: a1b2c3d
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
      "command": "/absolute/path/to/bin/mysql-mcp-server",
      "env": {
        "MYSQL_DSN": "root:password@tcp(127.0.0.1:3306)/mysql?parseTime=true",
        "MYSQL_MAX_ROWS": "200"
      }
    }
  }
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

- Rejects non-read-only SQL
- Enforces row limit
- Enforces timeout

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

Get execution plan for a SELECT query.

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

### Connection Pool

Configure the connection pool for your workload:

```bash
export MYSQL_MAX_OPEN_CONNS=20      # Max open connections
export MYSQL_MAX_IDLE_CONNS=10      # Max idle connections  
export MYSQL_CONN_MAX_LIFETIME_MINUTES=60  # Connection lifetime
```

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

**Test against all MySQL versions:**

```bash
make test-integration-all
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
# MySQL 8.0 (port 3306)
export MYSQL_TEST_DSN="root:testpass@tcp(localhost:3306)/testdb?parseTime=true"

# MySQL 8.4 (port 3307)
export MYSQL_TEST_DSN="root:testpass@tcp(localhost:3307)/testdb?parseTime=true"

# MySQL 9.0 (port 3308)
export MYSQL_TEST_DSN="root:testpass@tcp(localhost:3308)/testdb?parseTime=true"
```

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

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/api` | API index with all endpoints |
| GET | `/api/databases` | List databases |
| GET | `/api/tables?database=` | List tables |
| GET | `/api/describe?database=&table=` | Describe table |
| POST | `/api/query` | Run SQL query |
| GET | `/api/ping` | Ping database |
| GET | `/api/server-info` | Server info |
| GET | `/api/connections` | List connections |
| POST | `/api/connections/use` | Switch connection |

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

**Vector endpoints** (requires `MYSQL_MCP_VECTOR=1`):

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/vector/search` | Vector similarity search |
| GET | `/api/vector/info?database=` | Vector column info |

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

Releases are automated via GitHub Actions and GoReleaser.

To create a new release:

```bash
git tag v1.0.0
git push origin v1.0.0
```

This will automatically:
1. Build binaries for macOS, Linux, and Windows
2. Create a GitHub Release with changelog
3. Push Docker image to `ghcr.io/askdba/mysql-mcp-server`
4. Update Homebrew formula (if configured)

## License

Apache License 2.0  
© 2025 Alkin Tezuysal
