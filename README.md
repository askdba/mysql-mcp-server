# mysql-mcp-server

![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-green.svg)
![Go Version](https://img.shields.io/badge/Go-1.22+-blue)
![Status](https://img.shields.io/badge/status-experimental-orange)

A lightweight, open-source **MySQL Server implementation for the Model Context Protocol (MCP)** written in Go.  
This server exposes MySQL database introspection and querying capabilities to any MCP-compatible client (ChatGPT, Claude, VS Code extensions, custom agents, etc.).

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
