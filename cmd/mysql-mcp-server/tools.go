// cmd/mysql-mcp-server/tools.go
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/askdba/mysql-mcp-server/internal/dbretry"
	"github.com/askdba/mysql-mcp-server/internal/util"
	"github.com/go-sql-driver/mysql"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Pre-compiled regex patterns (compiled once at startup for performance)
var (
	vectorDimensionsRegex = regexp.MustCompile(`vector\((\d+)\)`)
)

// ===== Core Tool Handlers =====

func toolListDatabases(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListDatabasesInput,
) (*mcp.CallToolResult, ListDatabasesOutput, error) {

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// Use information_schema for better compatibility and to filter out system dbs if needed
	rows, err := getDB().QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, ListDatabasesOutput{}, fmt.Errorf("ListDatabases failed: %w", err)
	}
	defer rows.Close()

	out := ListDatabasesOutput{Databases: []DatabaseInfo{}}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, ListDatabasesOutput{}, fmt.Errorf("scan failed: %w", err)
		}
		if !databaseAllowed(name) {
			continue
		}
		out.Databases = append(out.Databases, DatabaseInfo{Name: name})
		if len(out.Databases) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListDatabasesOutput{}, fmt.Errorf("row iteration failed: %w", err)
	}

	return nil, out, nil
}

func toolListTables(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListTablesInput,
) (*mcp.CallToolResult, ListTablesOutput, error) {

	if strings.TrimSpace(input.Database) == "" {
		return nil, ListTablesOutput{}, fmt.Errorf("database is required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, ListTablesOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// Fetch enhanced table metadata in a single query
	query := `SELECT TABLE_NAME, ENGINE, TABLE_ROWS, TABLE_COMMENT 
			  FROM information_schema.TABLES 
			  WHERE TABLE_SCHEMA = ?
			  ORDER BY TABLE_NAME`

	rows, err := getDB().QueryContext(ctx, query, input.Database)
	if err != nil {
		return nil, ListTablesOutput{}, fmt.Errorf("ListTables failed: %w", err)
	}
	rowsClosed := false
	defer func() {
		if !rowsClosed {
			_ = rows.Close()
		}
	}()

	out := ListTablesOutput{Tables: []TableInfo{}}
	for rows.Next() {
		var name string
		var engine, comment sql.NullString
		var tableRows sql.NullInt64

		if err := rows.Scan(&name, &engine, &tableRows, &comment); err != nil {
			return nil, ListTablesOutput{}, fmt.Errorf("scan failed: %w", err)
		}

		info := TableInfo{
			Name:    name,
			Engine:  engine.String,
			Comment: comment.String,
		}
		if tableRows.Valid {
			rowsVal := tableRows.Int64
			info.Rows = &rowsVal
		}

		out.Tables = append(out.Tables, info)
		if len(out.Tables) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListTablesOutput{}, fmt.Errorf("ListTables rows iteration: %w", err)
	}

	if len(out.Tables) == 0 {
		if !rowsClosed {
			if err := rows.Close(); err != nil {
				return nil, ListTablesOutput{}, fmt.Errorf("failed to close rows: %w", err)
			}
			rowsClosed = true
		}
		exists, err := schemaExists(ctx, input.Database)
		if err != nil {
			return nil, ListTablesOutput{}, err
		}
		if !exists {
			return nil, ListTablesOutput{}, fmt.Errorf("database not found: %s", input.Database)
		}
	}

	return nil, out, nil
}

func toolDescribeTable(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input DescribeTableInput,
) (*mcp.CallToolResult, DescribeTableOutput, error) {

	if strings.TrimSpace(input.Database) == "" {
		return nil, DescribeTableOutput{}, fmt.Errorf("database is required")
	}
	if strings.TrimSpace(input.Table) == "" {
		return nil, DescribeTableOutput{}, fmt.Errorf("table is required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, DescribeTableOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// Fetch comprehensive column info from information_schema
	query := `SELECT 
				COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, 
				COLUMN_DEFAULT, EXTRA, COLUMN_COMMENT, COLLATION_NAME
			  FROM information_schema.COLUMNS 
			  WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
			  ORDER BY ORDINAL_POSITION`

	rows, err := getDB().QueryContext(ctx, query, input.Database, input.Table)
	if err != nil {
		return nil, DescribeTableOutput{}, fmt.Errorf("DescribeTable failed: %w", err)
	}
	defer rows.Close()

	out := DescribeTableOutput{Columns: []ColumnInfo{}}
	for rows.Next() {
		var name, colType, nullable, key, extra, comment, collation sql.NullString
		var dataDefault sql.NullString // Defaults can be null

		if err := rows.Scan(&name, &colType, &nullable, &key, &dataDefault, &extra, &comment, &collation); err != nil {
			return nil, DescribeTableOutput{}, fmt.Errorf("scan failed: %w", err)
		}

		col := ColumnInfo{
			Name:      name.String,
			Type:      colType.String,
			Null:      nullable.String,
			Key:       key.String,
			Default:   dataDefault.String,
			Extra:     extra.String,
			Comment:   comment.String,
			Collation: collation.String,
		}
		out.Columns = append(out.Columns, col)
		if len(out.Columns) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, DescribeTableOutput{}, fmt.Errorf("row iteration failed: %w", err)
	}

	if len(out.Columns) == 0 {
		exists, err := tableExists(ctx, input.Database, input.Table)
		if err != nil {
			return nil, DescribeTableOutput{}, err
		}
		if !exists {
			schemaOk, err := schemaExists(ctx, input.Database)
			if err != nil {
				return nil, DescribeTableOutput{}, err
			}
			if !schemaOk {
				return nil, DescribeTableOutput{}, fmt.Errorf("database not found: %s", input.Database)
			}
			return nil, DescribeTableOutput{}, fmt.Errorf("table not found: %s.%s", input.Database, input.Table)
		}
		return nil, DescribeTableOutput{}, fmt.Errorf("no columns found for table: %s.%s", input.Database, input.Table)
	}

	return nil, out, nil
}

func schemaExists(ctx context.Context, database string) (bool, error) {
	var found int
	err := getDB().QueryRowContext(
		ctx,
		"SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ? LIMIT 1",
		database,
	).Scan(&found)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("schema existence check failed: %w", err)
}

func tableExists(ctx context.Context, database, table string) (bool, error) {
	var found int
	err := getDB().QueryRowContext(
		ctx,
		"SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? LIMIT 1",
		database,
		table,
	).Scan(&found)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("table existence check failed: %w", err)
}

// scanAndNormalizeRow reads one row from rows and returns normalized cell values.
func scanAndNormalizeRow(rows *sql.Rows, ncols int) ([]interface{}, error) {
	values := make([]interface{}, ncols)
	valuePtrs := make([]interface{}, ncols)
	for i := range values {
		valuePtrs[i] = &values[i]
	}
	if err := rows.Scan(valuePtrs...); err != nil {
		return nil, fmt.Errorf("failed to scan row: %w", err)
	}
	rowValues := make([]interface{}, ncols)
	for i, v := range values {
		rowValues[i] = util.NormalizeValue(v)
	}
	return rowValues, nil
}

// runQueryScan executes finalSQL on a dedicated connection (USE database when set),
// scans rows, and enforces limit. When paginated is true, finalSQL must request at
// most limit+1 rows (server-side); HasMore and NextOffset are derived from the extra row.
// limit must be positive when paginated is true (callers validate).
func runQueryScan(ctx context.Context, db *sql.DB, finalSQL, database string, limit int, paginated bool, pageOffset int) (QueryResult, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return QueryResult{}, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	if database != "" {
		quotedDB, err := util.QuoteIdent(database)
		if err != nil {
			return QueryResult{}, fmt.Errorf("invalid database name: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "USE "+quotedDB); err != nil {
			return QueryResult{}, fmt.Errorf("failed to select database '%s': %w", database, err)
		}
	}

	rows, err := conn.QueryContext(ctx, finalSQL)
	if err != nil {
		return QueryResult{}, fmt.Errorf("query failed: %w", err)
	}
	rowsClosed := false
	defer func() {
		if !rowsClosed {
			_ = rows.Close()
		}
	}()

	out := QueryResult{
		Columns: make([]string, 0),
		Rows:    make([][]interface{}, 0, limit),
	}

	columns, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		rowsClosed = true
		return QueryResult{}, fmt.Errorf("failed to get columns: %w", err)
	}
	out.Columns = columns

	ncols := len(columns)
	for rows.Next() {
		rowValues, err := scanAndNormalizeRow(rows, ncols)
		if err != nil {
			_ = rows.Close()
			rowsClosed = true
			return QueryResult{}, err
		}
		if len(out.Rows) < limit {
			out.Rows = append(out.Rows, rowValues)
			continue
		}
		if paginated {
			out.HasMore = true
			break
		}
		out.Truncated = true
		if err := rows.Close(); err != nil {
			return QueryResult{}, fmt.Errorf("failed to close rows: %w", err)
		}
		rowsClosed = true
		break
	}

	if !rowsClosed {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			rowsClosed = true
			return QueryResult{}, fmt.Errorf("row iteration failed: %w", err)
		}
		if err := rows.Close(); err != nil {
			return QueryResult{}, fmt.Errorf("failed to close rows: %w", err)
		}
		rowsClosed = true
	}

	if out.HasMore {
		next := pageOffset + limit
		out.NextOffset = &next
	}

	return out, nil
}

func toolRunQuery(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input RunQueryInput,
) (*mcp.CallToolResult, QueryResult, error) {
	timer := NewQueryTimer("run_query")

	sqlText := strings.TrimSpace(input.SQL)
	if sqlText == "" {
		return nil, QueryResult{}, fmt.Errorf("sql is required")
	}
	database := strings.TrimSpace(input.Database)
	if accessControlEnabled() {
		if database == "" {
			return nil, QueryResult{}, fmt.Errorf("database is required when MYSQL_MCP_ALLOWED_DATABASES is set")
		}
		if err := requireAllowedDatabase(database); err != nil {
			return nil, QueryResult{}, err
		}
	}
	if err := requireSafeDatabase(database); err != nil {
		return nil, QueryResult{}, err
	}

	// Token estimation (optional)
	inputTokens, _ := estimateTokensForValue(input)
	tokens := &TokenUsage{
		InputEstimated: inputTokens,
		TotalEstimated: inputTokens, // Default to input; updated on success with output
		Model:          tokenModel,
	}

	// Enhanced SQL validation using parser + regex defense-in-depth
	if err := util.ValidateSQLCombined(sqlText); err != nil {
		logWarn("query rejected by validator", map[string]interface{}{
			"error": err.Error(),
			"query": util.TruncateQuery(sqlText, 200),
		})
		if auditLogger != nil {
			auditLogger.Log(&AuditEntry{
				Tool:        "run_query",
				Database:    database,
				Query:       util.TruncateQuery(sqlText, 500),
				InputTokens: inputTokens,
				Success:     false,
				Error:       err.Error(),
			})
		}
		return nil, QueryResult{}, fmt.Errorf("query validation failed: %w", err)
	}
	if err := requireReferencedSchemasInQuery(sqlText); err != nil {
		return nil, QueryResult{}, err
	}

	limit := maxRows
	if input.MaxRows != nil && *input.MaxRows > 0 && *input.MaxRows < maxRows {
		limit = *input.MaxRows
	}
	if limit < 0 {
		limit = 0
	}

	usePagination := input.Offset != nil
	var pageOffset int
	if usePagination {
		pageOffset = *input.Offset
		if pageOffset < 0 {
			return nil, QueryResult{}, fmt.Errorf("offset must be non-negative")
		}
		if limit <= 0 {
			return nil, QueryResult{}, fmt.Errorf(
				"offset pagination requires a positive effective row limit (got %d; check MYSQL_MAX_ROWS and run_query max_rows)",
				limit,
			)
		}
	}

	// Detect SELECT * before rewriting so we can surface a warning.
	hasStar := util.HasSelectStar(sqlText)

	var finalSQL string
	if usePagination {
		var err error
		finalSQL, err = util.InjectLimitWithOffset(sqlText, limit+1, pageOffset)
		if err != nil {
			return nil, QueryResult{}, fmt.Errorf("pagination: %w", err)
		}
	} else {
		// Inject a server-side LIMIT so MySQL stops processing early.
		// This is a best-effort optimization; we still enforce the row cap on
		// the client side below to guard against non-SELECT statements where
		// InjectLimit is a no-op.
		finalSQL = util.InjectLimit(sqlText, limit)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	db := getDB()
	var out QueryResult
	err := dbretry.Do(ctx, db, dbRetryCfg, pingTimeout, func() error {
		var e error
		out, e = runQueryScan(ctx, db, finalSQL, database, limit, usePagination, pageOffset)
		return e
	})
	if err != nil {
		timer.LogError(err, finalSQL, tokens, nil)
		if auditLogger != nil {
			auditLogger.Log(&AuditEntry{
				Tool:        "run_query",
				Database:    database,
				Query:       util.TruncateQuery(finalSQL, 500),
				DurationMs:  timer.ElapsedMs(),
				InputTokens: inputTokens,
				Success:     false,
				Error:       err.Error(),
			})
		}
		return nil, QueryResult{}, err
	}

	// Attach a warning when SELECT * was used so the AI can adjust future queries.
	if hasStar {
		out.Warning = "SELECT * retrieves all columns, which increases payload size. " +
			"Specify only the columns you need for better performance."
	}

	// Apply column masking if configured
	if cfg != nil && len(cfg.MaskColumns) > 0 {
		maskResults(out.Columns, out.Rows, cfg.MaskColumns)
	}

	// Token estimation for output (optional)
	outputTokens, _ := estimateTokensForValue(out)
	tokens.OutputEstimated = outputTokens
	tokens.TotalEstimated = inputTokens + outputTokens

	// Record into global metrics aggregator (when token tracking enabled)
	if tokenTracking {
		globalTokenMetrics.Record("run_query", inputTokens, outputTokens)
	}

	// Calculate efficiency metrics
	eff := CalculateEfficiency(inputTokens, outputTokens, len(out.Rows))

	// Log success
	timer.LogSuccess(len(out.Rows), finalSQL, tokens, eff)
	if auditLogger != nil {
		entry := &AuditEntry{
			Tool:         "run_query",
			Database:     database,
			Query:        util.TruncateQuery(finalSQL, 500),
			DurationMs:   timer.ElapsedMs(),
			RowCount:     len(out.Rows),
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			Success:      true,
		}
		if eff != nil {
			entry.TokensPerRow = eff.TokensPerRow
			entry.IOEfficiency = eff.IOEfficiency
			entry.CostEstimateUSD = eff.CostEstimateUSD
		}
		auditLogger.Log(entry)
	}

	return nil, out, nil
}

func toolPing(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input PingInput,
) (*mcp.CallToolResult, PingOutput, error) {

	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	start := NewQueryTimer("ping")
	db := getDB()
	err := dbretry.Do(ctx, db, dbRetryCfg, pingTimeout, func() error {
		return db.PingContext(ctx)
	})
	latency := start.ElapsedMs()

	if err != nil {
		return nil, PingOutput{
			Success:   false,
			LatencyMs: latency,
			Message:   fmt.Sprintf("ping failed: %v", err),
		}, nil
	}

	return nil, PingOutput{
		Success:   true,
		LatencyMs: latency,
		Message:   "pong",
	}, nil
}

func toolServerInfo(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ServerInfoInput,
) (*mcp.CallToolResult, ServerInfoOutput, error) {

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	out := ServerInfoOutput{}

	// Get version and version comment
	row := getDB().QueryRowContext(ctx, "SELECT VERSION()")
	if err := row.Scan(&out.Version); err != nil {
		return nil, ServerInfoOutput{}, fmt.Errorf("failed to get version: %w", err)
	}

	out.ServerEngine = string(getServerType())

	// Get various server variables in one query
	rows, err := getDB().QueryContext(ctx, `
		SELECT VARIABLE_NAME, VARIABLE_VALUE
		FROM performance_schema.global_variables
		WHERE VARIABLE_NAME IN (
			'version_comment',
			'character_set_server',
			'collation_server',
			'max_connections'
		)
	`)
	if err != nil {
		// Fallback for older MySQL or restricted permissions
		rows, err = getDB().QueryContext(ctx, `
			SHOW VARIABLES WHERE Variable_name IN (
				'version_comment',
				'character_set_server',
				'collation_server',
				'max_connections'
			)
		`)
		if err != nil {
			return nil, ServerInfoOutput{}, fmt.Errorf("failed to get server variables: %w", err)
		}
	}
	defer rows.Close()

	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			continue
		}
		switch strings.ToLower(name) {
		case "version_comment":
			out.VersionComment = value
		case "character_set_server":
			out.CharacterSet = value
		case "collation_server":
			out.Collation = value
		case "max_connections":
			out.MaxConnections, _ = strconv.Atoi(value)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, ServerInfoOutput{}, fmt.Errorf("server variables iteration failed: %w", err)
	}

	// Get uptime and threads connected from status
	statusRows, err := getDB().QueryContext(ctx, `
		SELECT VARIABLE_NAME, VARIABLE_VALUE
		FROM performance_schema.global_status
		WHERE VARIABLE_NAME IN ('Uptime', 'Threads_connected')
	`)
	if err != nil {
		// Fallback for older MySQL or restricted permissions
		statusRows, err = getDB().QueryContext(ctx, `
			SHOW GLOBAL STATUS WHERE Variable_name IN ('Uptime', 'Threads_connected')
		`)
		if err != nil {
			return nil, ServerInfoOutput{}, fmt.Errorf("failed to get server status: %w", err)
		}
	}
	defer statusRows.Close()

	for statusRows.Next() {
		var name, value string
		if err := statusRows.Scan(&name, &value); err != nil {
			continue
		}
		switch strings.ToLower(name) {
		case "uptime":
			out.Uptime, _ = strconv.ParseInt(value, 10, 64)
		case "threads_connected":
			out.ThreadsConnected, _ = strconv.Atoi(value)
		}
	}

	if err := statusRows.Err(); err != nil {
		return nil, ServerInfoOutput{}, fmt.Errorf("server status iteration failed: %w", err)
	}

	// Get current user and database
	row = getDB().QueryRowContext(ctx, "SELECT CURRENT_USER(), IFNULL(DATABASE(), '')")
	if err := row.Scan(&out.CurrentUser, &out.CurrentDatabase); err != nil {
		return nil, ServerInfoOutput{}, fmt.Errorf("failed to get current user/database: %w", err)
	}

	if tokenTracking {
		s := globalTokenMetrics.Snapshot()
		out.TokenMetrics = &ServerTokenSnapshot{
			ToolCalls:         s.QueryCount,
			TotalInputTokens:  s.TotalInputTokens,
			TotalOutputTokens: s.TotalOutputTokens,
			TotalTokens:       s.TotalTokens,
			MetricsUptimeSec:  s.UptimeSeconds,
		}
	}

	if input.Detailed {
		h := &ServerHealthSnapshot{}
		pctx, pcancel := context.WithTimeout(ctx, pingTimeout)
		t0 := time.Now()
		_ = getDB().PingContext(pctx)
		pcancel()
		h.PingLatencyMs = time.Since(t0).Milliseconds()

		keyVars := []string{
			"Threads_running", "Slow_queries", "Questions",
			"Innodb_buffer_pool_read_requests", "Innodb_buffer_pool_reads",
		}
		ph := strings.Repeat("?,", len(keyVars))
		ph = ph[:len(ph)-1]
		q := fmt.Sprintf(`SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME IN (%s)`, ph)
		args := make([]interface{}, len(keyVars))
		for i := range keyVars {
			args[i] = keyVars[i]
		}
		stRows, err := getDB().QueryContext(ctx, q, args...)
		if err != nil {
			stRows, err = getDB().QueryContext(ctx,
				`SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_running','Slow_queries','Questions','Innodb_buffer_pool_read_requests','Innodb_buffer_pool_reads')`)
		}
		if err == nil {
			defer func() { _ = stRows.Close() }()
			var reads, reqs int64
			var gotReads, gotReqs bool
			for stRows.Next() {
				var n, v string
				if err := stRows.Scan(&n, &v); err != nil {
					continue
				}
				iv, _ := strconv.ParseInt(v, 10, 64)
				switch strings.ToLower(n) {
				case "threads_running":
					h.ThreadsRunning = int(iv)
				case "slow_queries":
					h.SlowQueries = iv
				case "questions":
					h.Questions = iv
				case "innodb_buffer_pool_reads":
					reads = iv
					gotReads = true
				case "innodb_buffer_pool_read_requests":
					reqs = iv
					gotReqs = true
				}
			}
			if gotReads && gotReqs {
				br := reads
				h.BufferPoolReads = &br
				rr := reqs
				h.BufferPoolReadRequests = &rr
				if reqs > 0 {
					hit := 100.0 * (1.0 - float64(reads)/float64(reqs))
					if hit < 0 {
						hit = 0
					}
					if hit > 100 {
						hit = 100
					}
					h.BufferPoolHitPercent = &hit
				}
			}
		}
		out.Health = h
	}

	return nil, out, nil
}

// ===== Multi-DSN Tool Handlers =====

func toolListConnections(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListConnectionsInput,
) (*mcp.CallToolResult, ListConnectionsOutput, error) {
	if connManager == nil {
		return nil, ListConnectionsOutput{}, fmt.Errorf("connection manager not initialized")
	}

	configs := connManager.List()
	_, activeName := connManager.GetActive()

	out := ListConnectionsOutput{
		Connections: make([]ConnectionInfo, 0, len(configs)),
		Active:      activeName,
	}

	for _, cfg := range configs {
		out.Connections = append(out.Connections, ConnectionInfo{
			Name:        cfg.Name,
			DSN:         cfg.DSN, // Already masked
			Description: cfg.Description,
			Active:      cfg.Name == activeName,
		})
	}

	return nil, out, nil
}

func toolUseConnection(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input UseConnectionInput,
) (*mcp.CallToolResult, UseConnectionOutput, error) {
	if connManager == nil {
		return nil, UseConnectionOutput{}, fmt.Errorf("connection manager not initialized")
	}

	if input.Name == "" {
		return nil, UseConnectionOutput{}, fmt.Errorf("connection name is required")
	}

	if err := connManager.SetActive(input.Name); err != nil {
		return nil, UseConnectionOutput{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	// Get current database (informational, don't fail if this errors)
	var currentDB sql.NullString
	var dbQueryErr error
	if err := getDB().QueryRowContext(ctx, "SELECT DATABASE()").Scan(&currentDB); err != nil {
		dbQueryErr = err
		logWarn("failed to get current database after connection switch", map[string]interface{}{
			"connection": input.Name,
			"error":      err.Error(),
		})
	}

	logInfo("switched connection", map[string]interface{}{
		"connection": input.Name,
	})

	message := fmt.Sprintf("Switched to connection '%s'", input.Name)
	if dbQueryErr != nil {
		message += " (note: could not determine current database)"
	}

	return nil, UseConnectionOutput{
		Success:  true,
		Active:   input.Name,
		Message:  message,
		Database: currentDB.String,
	}, nil
}

func maskResults(cols []string, rows [][]interface{}, patterns []string) {
	var nonEmpty []string
	for _, p := range patterns {
		if t := strings.TrimSpace(p); t != "" {
			nonEmpty = append(nonEmpty, t)
		}
	}
	if len(nonEmpty) == 0 {
		return
	}

	maskIndices := make(map[int]bool)
	for i, col := range cols {
		lowerCol := strings.ToLower(col)
		for _, p := range nonEmpty {
			if strings.Contains(lowerCol, strings.ToLower(p)) {
				maskIndices[i] = true
				break
			}
		}
	}

	if len(maskIndices) == 0 {
		return
	}

	for _, row := range rows {
		for idx := range maskIndices {
			if idx < len(row) && row[idx] != nil {
				row[idx] = "********"
			}
		}
	}
}

func toolAddConnection(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input AddConnectionInput,
	cm *ConnectionManager,
	cfg *config.Config,
) (*mcp.CallToolResult, AddConnectionOutput, error) {
	if cm == nil {
		return nil, AddConnectionOutput{}, fmt.Errorf("connection manager not initialized")
	}

	name := strings.TrimSpace(input.Name)
	dsn := strings.TrimSpace(input.DSN)
	if name == "" || dsn == "" {
		return nil, AddConnectionOutput{}, fmt.Errorf("name and dsn are required")
	}

	// 1. Safety Check: Reject root user
	mysqlCfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, AddConnectionOutput{}, fmt.Errorf("invalid DSN: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(mysqlCfg.User), "root") {
		return nil, AddConnectionOutput{}, fmt.Errorf("security policy: runtime registration of 'root' user is not allowed")
	}

	// 2. Add connection (brief duplicate check + register after ping; see AddConnectionIfAbsentWithPoolConfig)
	connCfg := config.ConnectionConfig{
		Name:        name,
		DSN:         dsn,
		Description: input.Description,
	}
	if err := cm.AddConnectionIfAbsentWithPoolConfig(ctx, connCfg, cfg); err != nil {
		if errors.Is(err, ErrConnectionAlreadyExists) {
			return nil, AddConnectionOutput{}, fmt.Errorf("connection '%s' already exists", name)
		}
		return nil, AddConnectionOutput{}, fmt.Errorf("failed to add connection: %w", err)
	}

	// 3. Automatically switch to it (process-wide active DSN; see spec)
	if err := cm.SetActive(name); err != nil {
		if rbErr := cm.RemoveConnection(name); rbErr != nil {
			return nil, AddConnectionOutput{}, fmt.Errorf("failed to activate connection: %w (rollback of added connection also failed: %w)", err, rbErr)
		}
		return nil, AddConnectionOutput{}, fmt.Errorf("failed to activate connection: %w", err)
	}

	return nil,
		AddConnectionOutput{
			Success: true,
			Active:  name,
			Message: fmt.Sprintf("Added and switched to connection '%s'", name),
		}, nil
}
