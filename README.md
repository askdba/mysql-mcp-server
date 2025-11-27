# MySQL MCP Server

A fast, read-only MySQL Server for the Model Context Protocol (MCP) written in Go.

This project exposes safe MySQL introspection tools to Claude Desktop via MCP. Claude can explore databases, describe schemas, and execute controlled read-only SQL queries — ideal for secure development assistance, debugging, analytics, and schema documentation.

## Features

- Fully read-only (blocks all non-SELECT/SHOW/DESCRIBE/EXPLAIN)
- MCP tools:
  - list_databases
  - list_tables
  - describe_table
  - run_query (safe and row-limited)
  - ping (connectivity check with latency)
  - server_info (version, uptime, config)
- Supports MySQL 5.7, 8.0, 8.4
- Query timeouts
- Single Go binary
- Unit and integration tests (Testcontainers)
- Native integration with Claude Desktop MCP

## Installation

Clone and build:

```bash
git clone https://github.com/askdba/mysql-mcp-server.git
cd mysql-mcp-server
make build
```

Binary output:

```
bin/mysql-mcp-server
```

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
| MYSQL_MAX_OPEN_CONNS | No | 10 | Max open database connections |
| MYSQL_MAX_IDLE_CONNS | No | 5 | Max idle database connections |
| MYSQL_CONN_MAX_LIFETIME_MINUTES | No | 30 | Connection max lifetime in minutes |

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

```bash
docker build -t mysql-mcp-server .
```

docker-compose:

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
    build: .
    depends_on:
      - mysql
    environment:
      MYSQL_DSN: "root:rootpass@tcp(mysql:3306)/testdb?parseTime=true"
```

Run:

```bash
docker compose up --build
```

## Project Structure

```
cmd/mysql-mcp-server/   -> Server entrypoint
internal/config/        -> Configuration loader
internal/mysql/         -> MySQL client + tests
bin/                    -> Built binaries
```

## Development

```bash
make fmt
make run
make build
```

## License

Apache License 2.0  
© 2025 Alkin Tezuysal
