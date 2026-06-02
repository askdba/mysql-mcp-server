// cmd/mysql-mcp-server/prompts.go
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askdba/mysql-mcp-server/internal/util"
)

// registerPrompts registers MCP prompt handlers with the server.
// Each prompt gathers live database data and returns it pre-formatted as a
// conversation message, so Claude receives the context it needs without the
// user having to run individual tools first.
func registerPrompts(server *mcp.Server) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "health_check",
		Title:       "MySQL Health Check",
		Description: "Gather live health metrics (ping, server info, active processes, slow queries) and get a health assessment with prioritized recommendations.",
	}, promptHealthCheck)

	server.AddPrompt(&mcp.Prompt{
		Name:        "schema_audit",
		Title:       "Schema Audit",
		Description: "Collect full schema details for a database (tables, indexes, foreign keys) and get design recommendations.",
		Arguments: []*mcp.PromptArgument{
			{Name: "database", Title: "Database", Description: "Database name to audit", Required: true},
		},
	}, promptSchemaAudit)

	server.AddPrompt(&mcp.Prompt{
		Name:        "query_advisor",
		Title:       "Query Advisor",
		Description: "Run EXPLAIN on a SELECT, collect index info for referenced tables, and get query optimization advice.",
		Arguments: []*mcp.PromptArgument{
			{Name: "sql", Title: "SQL", Description: "SELECT query to analyze", Required: true},
			{Name: "database", Title: "Database", Description: "Database context for the query", Required: false},
		},
	}, promptQueryAdvisor)

	server.AddPrompt(&mcp.Prompt{
		Name:        "connection_debug",
		Title:       "Connection Debug",
		Description: "Diagnose connectivity, timeout settings, SSL config, and connection pool state for the active MySQL connection.",
	}, promptConnectionDebug)
}

// promptHealthCheck gathers ping, server info, optional process list, and optional
// slow query log, then asks Claude to produce a health assessment.
func promptHealthCheck(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	var sb strings.Builder
	sb.WriteString("# MySQL Health Check\n\n")

	// Ping
	_, pingOut, _ := toolPing(ctx, nil, PingInput{})
	if pingOut.Success {
		fmt.Fprintf(&sb, "**Connectivity**: OK — %d ms\n\n", pingOut.LatencyMs)
	} else {
		fmt.Fprintf(&sb, "**Connectivity**: FAILED — %s\n\n", pingOut.Message)
	}

	// Server info with health metrics
	_, srvOut, srvErr := toolServerInfo(ctx, nil, ServerInfoInput{Detailed: true})
	if srvErr != nil {
		fmt.Fprintf(&sb, "**Server Info**: unavailable (%v)\n\n", srvErr)
	} else {
		uptimeDays := srvOut.Uptime / 86400
		uptimeHours := (srvOut.Uptime % 86400) / 3600
		sb.WriteString("**Server**\n")
		fmt.Fprintf(&sb, "- Version: %s (%s)\n", srvOut.Version, srvOut.ServerEngine)
		fmt.Fprintf(&sb, "- Comment: %s\n", srvOut.VersionComment)
		fmt.Fprintf(&sb, "- Uptime: %dd %dh\n", uptimeDays, uptimeHours)
		fmt.Fprintf(&sb, "- User: %s\n", srvOut.CurrentUser)
		fmt.Fprintf(&sb, "- Charset / collation: %s / %s\n", srvOut.CharacterSet, srvOut.Collation)
		fmt.Fprintf(&sb, "- Connections: %d active / %d max\n", srvOut.ThreadsConnected, srvOut.MaxConnections)
		if h := srvOut.Health; h != nil {
			fmt.Fprintf(&sb, "- Threads running: %d\n", h.ThreadsRunning)
			fmt.Fprintf(&sb, "- Slow queries (since startup): %d\n", h.SlowQueries)
			if h.Questions > 0 {
				fmt.Fprintf(&sb, "- Questions (since startup): %d\n", h.Questions)
			}
			if h.BufferPoolHitPercent != nil {
				fmt.Fprintf(&sb, "- InnoDB buffer pool hit rate: %.2f%%\n", *h.BufferPoolHitPercent)
			}
		}
		sb.WriteString("\n")
	}

	// Active processes (requires MYSQL_MCP_PROCESS_ADMIN=1)
	if cfg != nil && cfg.ProcessAdmin {
		_, procOut, procErr := toolProcessList(ctx, nil, ProcessListInput{})
		if procErr == nil {
			fmt.Fprintf(&sb, "**Active Processes** (%d)\n", len(procOut.Processes))
			for _, p := range procOut.Processes {
				info := ""
				if p.Info != "" {
					info = ": " + util.TruncateQuery(p.Info, 80)
				}
				fmt.Fprintf(&sb, "- [%d] %s cmd=%s time=%ds%s\n",
					p.ID, p.User, p.Command, p.Time, info)
			}
			if procOut.Note != "" {
				fmt.Fprintf(&sb, "  (%s)\n", procOut.Note)
			}
			sb.WriteString("\n")
		}
	}

	// Slow query log (requires MYSQL_MCP_SLOW_QUERY_TOOL=1)
	if cfg != nil && cfg.SlowQueryTool {
		_, slowOut, slowErr := toolSlowQueryLog(ctx, nil, SlowQueryLogInput{Limit: 10})
		if slowErr == nil {
			fmt.Fprintf(&sb, "**Slow Query Log** (mode: %s)\n", slowOut.Mode)
			for _, r := range slowOut.Rows {
				fmt.Fprintf(&sb, "- query_time=%s rows_examined=%d: %s\n",
					r.QueryTime, r.RowsExamined, util.TruncateQuery(r.Query, 120))
			}
			if slowOut.Message != "" {
				fmt.Fprintf(&sb, "- %s\n", slowOut.Message)
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("---\n")
	sb.WriteString("Please analyze the health of this MySQL instance. " +
		"Rate overall health (good/degraded/critical), highlight any concerns with severity (low/medium/high), " +
		"identify optimization opportunities, and suggest concrete next steps.")

	return &mcp.GetPromptResult{
		Description: "Live MySQL health data",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: sb.String()}},
		},
	}, nil
}

// promptSchemaAudit collects tables, indexes, and foreign keys for a database,
// then asks Claude for a design review.
func promptSchemaAudit(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	database := strings.TrimSpace(req.Params.Arguments["database"])
	if database == "" {
		return nil, fmt.Errorf("database argument is required")
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Schema Audit: %s\n\n", database)

	// Tables
	_, tablesOut, tablesErr := toolListTables(ctx, nil, ListTablesInput{Database: database})
	if tablesErr != nil {
		return nil, fmt.Errorf("list_tables: %w", tablesErr)
	}

	fmt.Fprintf(&sb, "**Tables** (%d total)\n", len(tablesOut.Tables))
	tableNames := make([]string, 0, len(tablesOut.Tables))
	for _, t := range tablesOut.Tables {
		rowsStr := "?"
		if t.Rows != nil {
			rowsStr = fmt.Sprintf("%d", *t.Rows)
		}
		comment := ""
		if t.Comment != "" {
			comment = " — " + t.Comment
		}
		fmt.Fprintf(&sb, "- %s (engine: %s, ~%s rows%s)\n", t.Name, t.Engine, rowsStr, comment)
		tableNames = append(tableNames, t.Name)
	}
	sb.WriteString("\n")

	// Indexes — cap at 20 tables to keep prompt size reasonable
	const maxTablesForIndexes = 20
	auditCount := len(tableNames)
	if auditCount > maxTablesForIndexes {
		auditCount = maxTablesForIndexes
	}

	if auditCount > 0 {
		sb.WriteString("**Indexes**\n")
		for _, tbl := range tableNames[:auditCount] {
			_, idxOut, idxErr := toolListIndexes(ctx, nil, ListIndexesInput{Database: database, Table: tbl})
			if idxErr != nil {
				fmt.Fprintf(&sb, "- %s: (error: %v)\n", tbl, idxErr)
				continue
			}
			if len(idxOut.Indexes) == 0 {
				fmt.Fprintf(&sb, "- %s: (no indexes)\n", tbl)
				continue
			}
			fmt.Fprintf(&sb, "- %s:\n", tbl)
			for _, idx := range idxOut.Indexes {
				unique := ""
				if !idx.NonUnique {
					unique = " UNIQUE"
				}
				fmt.Fprintf(&sb, "  - %s (%s)%s [%s]\n", idx.Name, idx.Columns, unique, idx.Type)
			}
		}
		if len(tableNames) > maxTablesForIndexes {
			fmt.Fprintf(&sb, "  (...%d more tables omitted)\n", len(tableNames)-maxTablesForIndexes)
		}
		sb.WriteString("\n")
	}

	// Foreign keys for the whole database in one call
	_, fkOut, fkErr := toolForeignKeys(ctx, nil, ForeignKeysInput{Database: database})
	if fkErr == nil {
		fmt.Fprintf(&sb, "**Foreign Keys** (%d)\n", len(fkOut.ForeignKeys))
		for _, fk := range fkOut.ForeignKeys {
			fmt.Fprintf(&sb, "- %s.%s → %s.%s (ON UPDATE %s, ON DELETE %s) [constraint: %s]\n",
				fk.Table, fk.Column, fk.ReferencedTable, fk.ReferencedColumn,
				fk.OnUpdate, fk.OnDelete, fk.Name)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("Please audit this database schema. Identify: " +
		"missing indexes (especially on FK columns and common WHERE/JOIN/ORDER BY candidates), " +
		"design issues (over-normalization, denormalization, VARCHAR vs TEXT misuse, missing NOT NULL, etc.), " +
		"redundant or duplicate indexes, naming inconsistencies, " +
		"and any potential performance or data-integrity concerns. " +
		"Provide specific, actionable SQL recommendations where applicable.")

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Schema audit for %q", database),
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: sb.String()}},
		},
	}, nil
}

// promptQueryAdvisor runs EXPLAIN on the provided SELECT, collects indexes for
// referenced tables, then asks Claude for optimization recommendations.
func promptQueryAdvisor(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	sqlText := strings.TrimSpace(req.Params.Arguments["sql"])
	if sqlText == "" {
		return nil, fmt.Errorf("sql argument is required")
	}
	database := strings.TrimSpace(req.Params.Arguments["database"])

	var sb strings.Builder
	sb.WriteString("# Query Advisor\n\n")
	sb.WriteString("**Query:**\n```sql\n")
	sb.WriteString(sqlText)
	sb.WriteString("\n```\n\n")

	// EXPLAIN
	_, explainOut, explainErr := toolExplainQuery(ctx, nil, ExplainQueryInput{
		SQL:      sqlText,
		Database: database,
		Format:   "traditional", // row-based output for table rendering
	})
	if explainErr != nil {
		fmt.Fprintf(&sb, "**EXPLAIN Error**: %v\n\n", explainErr)
	} else {
		sb.WriteString("**Execution Plan:**\n\n")

		// Render plan as a markdown table using key EXPLAIN columns
		planCols := []string{"id", "select_type", "table", "type", "possible_keys", "key", "key_len", "rows", "filtered", "Extra"}
		sb.WriteString("| " + strings.Join(planCols, " | ") + " |\n")
		sb.WriteString("|" + strings.Repeat(" --- |", len(planCols)) + "\n")

		tableNames := make([]string, 0)
		planRows, _ := explainOut.Plan.([]map[string]interface{})
		for _, row := range planRows {
			sb.WriteString("|")
			for _, k := range planCols {
				v := fmt.Sprintf("%v", row[k])
				if v == "<nil>" || v == "%!v(MISSING)" {
					v = ""
				}
				fmt.Fprintf(&sb, " %s |", v)
			}
			sb.WriteString("\n")
			// Collect real table names for index lookup (skip derived/subquery markers)
			if tbl, ok := row["table"].(string); ok && tbl != "" && !strings.HasPrefix(tbl, "<") {
				tableNames = append(tableNames, tbl)
			}
		}
		sb.WriteString("\n")

		if len(explainOut.Warnings) > 0 {
			sb.WriteString("**Optimizer Warnings:**\n")
			for _, w := range explainOut.Warnings {
				fmt.Fprintf(&sb, "- %s\n", w)
			}
			sb.WriteString("\n")
		}

		// Indexes on referenced tables (only when database is provided)
		if database != "" && len(tableNames) > 0 {
			sb.WriteString("**Indexes on referenced tables:**\n")
			seen := make(map[string]bool)
			for _, tbl := range tableNames {
				if seen[tbl] {
					continue
				}
				seen[tbl] = true
				_, idxOut, idxErr := toolListIndexes(ctx, nil, ListIndexesInput{Database: database, Table: tbl})
				if idxErr != nil {
					fmt.Fprintf(&sb, "- %s: (error fetching indexes)\n", tbl)
					continue
				}
				if len(idxOut.Indexes) == 0 {
					fmt.Fprintf(&sb, "- %s: (no indexes)\n", tbl)
					continue
				}
				fmt.Fprintf(&sb, "- %s:\n", tbl)
				for _, idx := range idxOut.Indexes {
					unique := ""
					if !idx.NonUnique {
						unique = " UNIQUE"
					}
					fmt.Fprintf(&sb, "  - %s (%s)%s [%s]\n", idx.Name, idx.Columns, unique, idx.Type)
				}
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("---\n")
	sb.WriteString("Please analyze this query's execution plan. Identify: " +
		"full-table scans (type=ALL), missing indexes, suboptimal join types, " +
		"high row estimates vs filtered rows, and any Extra warnings (Using filesort, Using temporary). " +
		"Provide specific CREATE INDEX statements and query rewrite suggestions where applicable.")

	return &mcp.GetPromptResult{
		Description: "Query execution plan with optimization advice",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: sb.String()}},
		},
	}, nil
}

// promptConnectionDebug collects ping, server info, configured connections, and
// relevant system variables, then asks Claude to diagnose any issues.
func promptConnectionDebug(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	var sb strings.Builder
	sb.WriteString("# MySQL Connection Debug Report\n\n")

	// Ping
	_, pingOut, _ := toolPing(ctx, nil, PingInput{})
	if pingOut.Success {
		fmt.Fprintf(&sb, "**Ping**: OK (%d ms)\n\n", pingOut.LatencyMs)
	} else {
		fmt.Fprintf(&sb, "**Ping**: FAILED — %s\n\n", pingOut.Message)
	}

	// Server info
	_, srvOut, srvErr := toolServerInfo(ctx, nil, ServerInfoInput{Detailed: true})
	if srvErr != nil {
		fmt.Fprintf(&sb, "**Server Info**: unavailable (%v)\n\n", srvErr)
	} else {
		sb.WriteString("**Server**\n")
		fmt.Fprintf(&sb, "- Version: %s (%s)\n", srvOut.Version, srvOut.ServerEngine)
		fmt.Fprintf(&sb, "- Current user: %s\n", srvOut.CurrentUser)
		fmt.Fprintf(&sb, "- Threads connected: %d / max_connections %d\n",
			srvOut.ThreadsConnected, srvOut.MaxConnections)
		if h := srvOut.Health; h != nil {
			fmt.Fprintf(&sb, "- Threads running: %d\n", h.ThreadsRunning)
		}
		sb.WriteString("\n")
	}

	// Configured connections
	_, connsOut, connsErr := toolListConnections(ctx, nil, ListConnectionsInput{})
	if connsErr == nil {
		fmt.Fprintf(&sb, "**Configured Connections** (active: %s)\n", connsOut.Active)
		for _, c := range connsOut.Connections {
			active := ""
			if c.Active {
				active = " ← active"
			}
			desc := ""
			if c.Description != "" {
				desc = " [" + c.Description + "]"
			}
			fmt.Fprintf(&sb, "- %s: %s%s%s\n", c.Name, c.DSN, desc, active)
		}
		sb.WriteString("\n")
	}

	// Targeted system variables: connection limits, timeouts, SSL, packet size
	wantVars := map[string]bool{
		"max_connections":          true,
		"wait_timeout":             true,
		"interactive_timeout":      true,
		"connect_timeout":          true,
		"net_read_timeout":         true,
		"net_write_timeout":        true,
		"max_allowed_packet":       true,
		"have_ssl":                 true,
		"ssl_cipher":               true,
		"require_secure_transport": true,
		"thread_stack":             true,
		"back_log":                 true,
	}
	_, varsOut, varsErr := toolListVariables(ctx, nil, ListVariablesInput{})
	if varsErr == nil {
		sb.WriteString("**Relevant System Variables**\n")
		for _, v := range varsOut.Variables {
			if wantVars[strings.ToLower(v.Name)] {
				fmt.Fprintf(&sb, "- %s = %s\n", v.Name, v.Value)
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("Please diagnose any connection or configuration issues shown above. " +
		"Explain what the key timeout and connection-limit variables mean for connection stability, " +
		"flag anything misconfigured or potentially risky, " +
		"and suggest specific configuration changes or client-side fixes.")

	return &mcp.GetPromptResult{
		Description: "MySQL connection diagnostics",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: sb.String()}},
		},
	}, nil
}
