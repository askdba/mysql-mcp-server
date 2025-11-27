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

```sql
CREATE USER 'mcp'@'localhost' IDENTIFIED BY 'strongpass';
GRANT SELECT ON *.* TO 'mcp'@'localhost';
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
