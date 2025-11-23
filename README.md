# mysql-mcp-server

A MySQL server implementation for the Model Context Protocol (MCP), written in Go.

## Features
- List databases
- List tables
- Describe tables
- Run read-only SQL queries
- Timeouts, max-row limits, and safe MCP stdio transport

## Quickstart

```bash
go build -o bin/mysql-mcp-server ./cmd/mysql-mcp-server
export MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/mysql?parseTime=true"
./bin/mysql-mcp-server
