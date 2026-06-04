# MCP Resources: Query Optimization Guides — Design

**Date:** 2026-06-04  
**Target version:** v1.7.1 (GA blocker)  
**Author:** Alkin Tezuysal

---

## Problem

Two query optimization guides exist in `docs/` and are referenced in the README, but they are invisible to Claude through MCP. The `run_query` tool vaguely says "Apply MySQL optimization guidelines before execution" with no pointer to the actual content. The `promptQueryAdvisor` prompt generates inline EXPLAIN-based advice but never draws on the documented patterns.

## Goal

Surface both optimization guides as readable MCP resources so Claude can pull them on demand when writing or optimizing queries. Tighten the `run_query` tool description and the `query_advisor` prompt to point users toward the right workflow.

---

## Architecture

Three files changed, one new file created. No existing tool logic touched.

### 1. `docs/embed.go` (new)

Package `docs` with `//go:embed` for both guide files. Placed inside `docs/` because Go's embed directive requires the source file to share a directory with the embedded files — `..` paths are not permitted. Exported as string variables so the Homebrew binary carries the content without needing the `docs/` directory at runtime.

```go
package docs

import _ "embed"

//go:embed query_optimization_guide.md
var QueryOptimizationGuide string

//go:embed mysql_query_optimization_comprehensive.md
var QueryOptimizationComprehensive string
```

### 2. `cmd/mysql-mcp-server/resources.go` (new)

Single exported function `registerResources(s *mcp.Server)` called from `main.go` after `registerPrompts`. Registers two resources:

| URI | Name | Source |
|-----|------|--------|
| `docs://mysql-mcp-server/query-optimization-guide` | SQL Query Optimization Guide | `docs.QueryOptimizationGuide` |
| `docs://mysql-mcp-server/query-optimization-comprehensive` | Comprehensive MySQL Query Optimization Guide | `docs.QueryOptimizationComprehensive` |

Each handler returns `mcp.TextResourceContents` with `MIMEType: "text/markdown"`.

### 3. `cmd/mysql-mcp-server/main.go` (modify)

Add `registerResources(s)` call immediately after the existing `registerPrompts(s)` call. One line change.

### 4. `cmd/mysql-mcp-server/types.go` (modify)

Update `RunQueryInput.SQL` jsonschema tag to append:

> For complex queries, use the `query_advisor` prompt which runs EXPLAIN and surfaces optimization guidance.

### 5. `cmd/mysql-mcp-server/prompts.go` (modify)

Append to `promptQueryAdvisor`'s closing instruction string:

> Two optimization reference guides are available as MCP resources (`docs://mysql-mcp-server/query-optimization-guide` and `docs://mysql-mcp-server/query-optimization-comprehensive`) if you need detailed patterns or index strategies.

---

## What is NOT changed

- `internal/util/sql_validator.go` — no validation changes
- The guide markdown files themselves — unchanged
- All existing tools, prompts, and HTTP handlers

---

## Testing

- `go build ./...` — confirms embed compiles and binary links
- `go test ./...` — no regressions
- Manual: start server, use an MCP client to call `resources/list` and confirm both URIs appear; call `resources/read` on each and confirm markdown content is returned

## Success criteria

- `resources/list` returns two entries with correct URIs and MIME type `text/markdown`
- `resources/read` on each URI returns the full markdown content of the respective guide
- `run_query` tool description visible in MCP tool listing includes the `query_advisor` pointer
- `go test ./...` green
- Binary size increase is the embedded markdown content only (~45 KB)
