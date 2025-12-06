# MySQL MCP Server

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
- Supports MySQL 5.7, 8.0, 8.4, 9.0
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
| MYSQL_MCP_AUDIT_LOG | No | – | Path to audit log file |
| MYSQL_MCP_VECTOR | No | 0 | Enable vector tools for MySQL 9.0+ (set to 1) |
| MYSQL_MAX_OPEN_CONNS | No | 10 | Max open database connections |
| MYSQL_MAX_IDLE_CONNS | No | 5 | Max idle database connections |
| MYSQL_CONN_MAX_LIFETIME_MINUTES | No | 30 | Connection max lifetime in minutes |

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

## Claude Desktop Integration

Edit:

```
~/Library/Application Support/Claude/claude_desktop_config.json
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

```bash
make test
make integration
```

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
version: "3.9"
services:
  mysql:
    image: mysql:8.0.36
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

## Project Structure

```
cmd/mysql-mcp-server/   -> Server entrypoint
internal/config/        -> Configuration loader
internal/mysql/         -> MySQL client + tests
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
