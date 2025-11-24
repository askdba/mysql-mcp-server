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
| MYSQL_READONLY | No | 1 | Enforce read-only |

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

- Rejects non-read-only SQL
- Enforces row limit
- Enforces timeout

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
