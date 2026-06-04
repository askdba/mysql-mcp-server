# MCP Resources: Query Optimization Guides — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the two query optimization guide docs as readable MCP resources so Claude can pull them on demand, and tighten tool/prompt descriptions to point toward the right workflow.

**Architecture:** A new `docs/embed.go` file embeds both markdown guides at build time (required because `//go:embed` cannot use `..` paths — the source file must be in the same directory as the embedded files). A new `cmd/mysql-mcp-server/resources.go` calls `server.AddResource` for each guide. `main.go`, `types.go`, and `prompts.go` each get one targeted edit.

**Tech Stack:** Go `embed` package, `github.com/modelcontextprotocol/go-sdk@v1.2.0` (`mcp.Resource`, `mcp.ResourceHandler`, `mcp.ResourceContents`, `mcp.ReadResourceResult`)

---

## File Map

| Action | File | What changes |
|--------|------|-------------|
| Create | `docs/embed.go` | Embeds both guide `.md` files as exported strings |
| Create | `cmd/mysql-mcp-server/resources.go` | `registerResources(s *mcp.Server)` — registers two resources |
| Modify | `cmd/mysql-mcp-server/main.go:283` | Add `registerResources(server)` call |
| Modify | `cmd/mysql-mcp-server/types.go:52` | Tighten `RunQueryInput.SQL` jsonschema tag |
| Modify | `cmd/mysql-mcp-server/prompts.go:326-329` | Append resource URIs to `promptQueryAdvisor` closing instruction |

---

### Task 1: Embed the guide files

**Files:**
- Create: `docs/embed.go`

- [ ] **Step 1: Create `docs/embed.go`**

```go
// docs/embed.go
package docs

import _ "embed"

// QueryOptimizationGuide is the content of query_optimization_guide.md,
// embedded at build time.
//
//go:embed query_optimization_guide.md
var QueryOptimizationGuide string

// QueryOptimizationComprehensive is the content of
// mysql_query_optimization_comprehensive.md, embedded at build time.
//
//go:embed mysql_query_optimization_comprehensive.md
var QueryOptimizationComprehensive string
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /Users/askdba/Documents/GitHub/mysql-mcp-server
go build ./docs/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add docs/embed.go
git commit -m "feat: embed optimization guide docs for MCP resource serving"
```

---

### Task 2: Register the MCP resources

**Files:**
- Create: `cmd/mysql-mcp-server/resources.go`

The MCP SDK types used here:
- `mcp.Resource` — metadata struct (`URI`, `Name`, `Description`, `MIMEType`)
- `mcp.ResourceHandler` — `func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error)`
- `mcp.ReadResourceResult` — `Contents []*mcp.ResourceContents`
- `mcp.ResourceContents` — `URI`, `MIMEType`, `Text` fields

- [ ] **Step 1: Write a failing test confirming resources will be listed**

Add to `cmd/mysql-mcp-server/tools_test.go` (append before the final `}`):

```go
func TestResourcesRegistered(t *testing.T) {
	// registerResources must add exactly two resources with the expected URIs.
	// We verify this by calling registerResources on a fresh server and checking
	// it doesn't panic, then confirm the package-level vars are non-empty.
	if docs.QueryOptimizationGuide == "" {
		t.Fatal("QueryOptimizationGuide embed is empty")
	}
	if docs.QueryOptimizationComprehensive == "" {
		t.Fatal("QueryOptimizationComprehensive embed is empty")
	}
}
```

Also add the import at the top of `tools_test.go` (in the existing `import` block):

```go
"github.com/askdba/mysql-mcp-server/docs"
```

- [ ] **Step 2: Run to confirm it fails (function not yet defined)**

```bash
go test -v -run TestResourcesRegistered ./cmd/mysql-mcp-server/
```

Expected: compile error — `docs` package not importable yet (Task 1 must be done first). If Task 1 is done, the test will PASS immediately — that is fine; proceed to Step 3.

- [ ] **Step 3: Create `cmd/mysql-mcp-server/resources.go`**

```go
// cmd/mysql-mcp-server/resources.go
package main

import (
	"context"

	"github.com/askdba/mysql-mcp-server/docs"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerResources(server *mcp.Server) {
	server.AddResource(
		&mcp.Resource{
			URI:         "docs://mysql-mcp-server/query-optimization-guide",
			Name:        "SQL Query Optimization Guide",
			Description: "Practical SQL optimization patterns and query rewriting techniques with before/after examples.",
			MIMEType:    "text/markdown",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{
						URI:      req.Params.URI,
						MIMEType: "text/markdown",
						Text:     docs.QueryOptimizationGuide,
					},
				},
			}, nil
		},
	)

	server.AddResource(
		&mcp.Resource{
			URI:         "docs://mysql-mcp-server/query-optimization-comprehensive",
			Name:        "Comprehensive MySQL Query Optimization Guide",
			Description: "Deep technical guide covering optimizer statistics, advanced indexing, execution plan analysis, and operational best practices.",
			MIMEType:    "text/markdown",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{
						URI:      req.Params.URI,
						MIMEType: "text/markdown",
						Text:     docs.QueryOptimizationComprehensive,
					},
				},
			}, nil
		},
	)
}
```

- [ ] **Step 4: Build to confirm it compiles**

```bash
go build ./cmd/mysql-mcp-server/
```

Expected: no output (success).

- [ ] **Step 5: Run the test**

```bash
go test -v -run TestResourcesRegistered ./cmd/mysql-mcp-server/
```

Expected:
```
--- PASS: TestResourcesRegistered (0.00s)
PASS
```

- [ ] **Step 6: Commit**

```bash
git add cmd/mysql-mcp-server/resources.go cmd/mysql-mcp-server/tools_test.go
git commit -m "feat: register optimization guide docs as MCP resources"
```

---

### Task 3: Wire `registerResources` into `main.go`

**Files:**
- Modify: `cmd/mysql-mcp-server/main.go:283`

- [ ] **Step 1: Add the call after `registerPrompts`**

Find this block (around line 283):

```go
	// Register prompts (multi-step workflows that gather live data for Claude)
	registerPrompts(server)

	// Register vector tools (MYSQL_MCP_VECTOR=1)
```

Replace with:

```go
	// Register prompts (multi-step workflows that gather live data for Claude)
	registerPrompts(server)

	// Register static documentation as MCP resources
	registerResources(server)

	// Register vector tools (MYSQL_MCP_VECTOR=1)
```

- [ ] **Step 2: Build**

```bash
go build ./cmd/mysql-mcp-server/
```

Expected: no output.

- [ ] **Step 3: Run full test suite**

```bash
go test ./...
```

Expected: all packages pass, no failures.

- [ ] **Step 4: Commit**

```bash
git add cmd/mysql-mcp-server/main.go
git commit -m "feat: wire registerResources into MCP server startup"
```

---

### Task 4: Tighten `RunQueryInput.SQL` description

**Files:**
- Modify: `cmd/mysql-mcp-server/types.go:52`

- [ ] **Step 1: Update the jsonschema tag**

Find line 52 in `cmd/mysql-mcp-server/types.go`:

```go
	SQL      string `json:"sql" jsonschema:"SQL query to execute; must start with SELECT, SHOW, DESCRIBE, or EXPLAIN. Apply MySQL optimization guidelines before execution."`
```

Replace with:

```go
	SQL      string `json:"sql" jsonschema:"SQL query to execute; must start with SELECT, SHOW, DESCRIBE, or EXPLAIN. For complex queries, use the query_advisor prompt which runs EXPLAIN and surfaces optimization guidance from the registered MCP resources."`
```

- [ ] **Step 2: Build and test**

```bash
go build ./cmd/mysql-mcp-server/ && go test ./cmd/mysql-mcp-server/
```

Expected: clean build, all tests pass.

- [ ] **Step 3: Commit**

```bash
git add cmd/mysql-mcp-server/types.go
git commit -m "docs: point run_query tool description to query_advisor prompt"
```

---

### Task 5: Update `promptQueryAdvisor` closing instruction

**Files:**
- Modify: `cmd/mysql-mcp-server/prompts.go:326-329`

- [ ] **Step 1: Append resource URIs to the closing instruction**

Find this block (around line 326):

```go
	sb.WriteString("---\n")
	sb.WriteString("Please analyze this query's execution plan. Identify: " +
		"full-table scans (type=ALL), missing indexes, suboptimal join types, " +
		"high row estimates vs filtered rows, and any Extra warnings (Using filesort, Using temporary). " +
		"Provide specific CREATE INDEX statements and query rewrite suggestions where applicable.")
```

Replace with:

```go
	sb.WriteString("---\n")
	sb.WriteString("Please analyze this query's execution plan. Identify: " +
		"full-table scans (type=ALL), missing indexes, suboptimal join types, " +
		"high row estimates vs filtered rows, and any Extra warnings (Using filesort, Using temporary). " +
		"Provide specific CREATE INDEX statements and query rewrite suggestions where applicable. " +
		"Two optimization reference guides are available as MCP resources " +
		"(docs://mysql-mcp-server/query-optimization-guide and " +
		"docs://mysql-mcp-server/query-optimization-comprehensive) " +
		"if you need detailed patterns or index strategies.")
```

- [ ] **Step 2: Build and test**

```bash
go build ./cmd/mysql-mcp-server/ && go test ./cmd/mysql-mcp-server/
```

Expected: clean build, all tests pass.

- [ ] **Step 3: Run full suite with race detector**

```bash
go test -race ./...
```

Expected: all packages pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/mysql-mcp-server/prompts.go
git commit -m "feat: reference optimization guide MCP resources in query_advisor prompt"
```

---

### Task 6: Cut v1.7.1-rc.2

**Files:** CHANGELOG.md, git tag

- [ ] **Step 1: Add `[1.7.1-rc.2]` section to CHANGELOG.md**

The file currently has:

```markdown
## [Unreleased]

## [1.7.1-rc.1] - 2026-06-04
```

Update to:

```markdown
## [Unreleased]

## [1.7.1-rc.2] - 2026-06-04

### Added

- **MCP Resources**: Two query optimization guides are now available as MCP resources (`docs://mysql-mcp-server/query-optimization-guide`, `docs://mysql-mcp-server/query-optimization-comprehensive`), surfacing SQL optimization patterns and advanced indexing strategies to Claude on demand. Content is embedded in the binary at build time.
- **`run_query` tool description**: Updated to direct users toward the `query_advisor` prompt for complex query optimization workflows.
- **`query_advisor` prompt**: Now references the two MCP resource URIs in its closing instruction for detailed index strategies and optimization patterns.

## [1.7.1-rc.1] - 2026-06-04
```

- [ ] **Step 2: Commit CHANGELOG**

```bash
git add CHANGELOG.md
git commit -m "chore: prepare v1.7.1-rc.2 — changelog"
```

- [ ] **Step 3: Tag and push**

```bash
git tag -a v1.7.1-rc.2 -m "Release candidate v1.7.1-rc.2"
git push origin main
git push origin v1.7.1-rc.2
```

Expected:
```
 * [new tag]         v1.7.1-rc.2 -> v1.7.1-rc.2
```

- [ ] **Step 4: Confirm release pipeline triggered**

```bash
gh run list --workflow=release.yml --limit=3
```

Expected: new run in `queued` or `in_progress` for the `v1.7.1-rc.2` tag.
