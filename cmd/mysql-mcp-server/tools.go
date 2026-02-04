// cmd/mysql-mcp-server/tools.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/askdba/mysql-mcp-server/internal/util"
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
	defer rows.Close()

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

	return nil, out, nil
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

	limit := maxRows
	if input.MaxRows != nil && *input.MaxRows > 0 && *input.MaxRows < maxRows {
		limit = *input.MaxRows
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// Use a dedicated connection so USE applies to the query.
	conn, err := getDB().Conn(ctx)
	if err != nil {
		return nil, QueryResult{}, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	if database != "" {
		quotedDB, err := util.QuoteIdent(database)
		if err != nil {
			return nil, QueryResult{}, fmt.Errorf("invalid database name: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "USE "+quotedDB); err != nil {
			return nil, QueryResult{}, fmt.Errorf("failed to select database '%s': %w", database, err)
		}
	}

	rows, err := conn.QueryContext(ctx, sqlText)
	if err != nil {
		timer.LogError(err, sqlText, tokens, nil)
		if auditLogger != nil {
			auditLogger.Log(&AuditEntry{
				Tool:        "run_query",
				Database:    database,
				Query:       util.TruncateQuery(sqlText, 500),
				DurationMs:  timer.ElapsedMs(),
				InputTokens: inputTokens,
				Success:     false,
				Error:       err.Error(),
			})
		}
		return nil, QueryResult{}, fmt.Errorf("query failed: %w", err)
	}
	rowsClosed := false

	out := QueryResult{
		Columns: make([]string, 0),
		Rows:    make([][]interface{}, 0),
	}

	columns, err := rows.Columns()
	if err != nil {
		return nil, QueryResult{}, fmt.Errorf("failed to get columns: %w", err)
	}
	out.Columns = columns

	count := 0
	for rows.Next() {
		// Create a slice of interface{} to hold the values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, QueryResult{}, fmt.Errorf("failed to scan row: %w", err)
		}

		// Normalize values (handle []byte for strings, etc.)
		rowValues := make([]interface{}, len(columns))
		for i, v := range values {
			rowValues[i] = util.NormalizeValue(v)
		}
		out.Rows = append(out.Rows, rowValues)
		count++

		if count >= limit {
			// Close early to avoid leaving unread results on the connection.
			if err := rows.Close(); err != nil {
				return nil, QueryResult{}, fmt.Errorf("failed to close rows: %w", err)
			}
			rowsClosed = true
			break
		}
	}

	if !rowsClosed {
		if err := rows.Err(); err != nil {
			return nil, QueryResult{}, fmt.Errorf("row iteration failed: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, QueryResult{}, fmt.Errorf("failed to close rows: %w", err)
		}
		rowsClosed = true
	}

	// Token estimation for output (optional)
	outputTokens, _ := estimateTokensForValue(out)
	tokens.OutputEstimated = outputTokens
	tokens.TotalEstimated = inputTokens + outputTokens

	// Calculate efficiency metrics
	eff := CalculateEfficiency(inputTokens, outputTokens, len(out.Rows))

	// Log success
	timer.LogSuccess(len(out.Rows), sqlText, tokens, eff)
	if auditLogger != nil {
		entry := &AuditEntry{
			Tool:         "run_query",
			Database:     database,
			Query:        util.TruncateQuery(sqlText, 500),
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
	err := getDB().PingContext(ctx)
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
