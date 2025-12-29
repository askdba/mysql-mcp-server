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

	rows, err := getDB().QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, ListDatabasesOutput{}, fmt.Errorf("SHOW DATABASES failed: %w", err)
	}
	defer rows.Close()

	out := ListDatabasesOutput{Databases: []DatabaseInfo{}}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, ListDatabasesOutput{}, fmt.Errorf("scan database name failed: %w", err)
		}
		out.Databases = append(out.Databases, DatabaseInfo{Name: name})
		if len(out.Databases) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListDatabasesOutput{}, err
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

	dbName, err := util.QuoteIdent(input.Database)
	if err != nil {
		return nil, ListTablesOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	query := fmt.Sprintf("SHOW TABLES FROM %s", dbName)

	rows, err := getDB().QueryContext(ctx, query)
	if err != nil {
		return nil, ListTablesOutput{}, fmt.Errorf("SHOW TABLES failed: %w", err)
	}
	defer rows.Close()

	out := ListTablesOutput{Tables: []TableInfo{}}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, ListTablesOutput{}, fmt.Errorf("scan table name failed: %w", err)
		}
		out.Tables = append(out.Tables, TableInfo{Name: name})
		if len(out.Tables) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListTablesOutput{}, err
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

	dbName, err := util.QuoteIdent(input.Database)
	if err != nil {
		return nil, DescribeTableOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	tableName, err := util.QuoteIdent(input.Table)
	if err != nil {
		return nil, DescribeTableOutput{}, fmt.Errorf("invalid table name: %w", err)
	}

	// Using SHOW FULL COLUMNS to get richer metadata.
	query := fmt.Sprintf("SHOW FULL COLUMNS FROM %s.%s", dbName, tableName)

	rows, err := getDB().QueryContext(ctx, query)
	if err != nil {
		return nil, DescribeTableOutput{}, fmt.Errorf("SHOW FULL COLUMNS failed: %w", err)
	}
	defer rows.Close()

	out := DescribeTableOutput{Columns: []ColumnInfo{}}
	for rows.Next() {
		var col ColumnInfo
		var dummyPrivileges string
		// Use sql.NullString for columns that can be NULL
		var collation, null, key, defaultVal, extra, comment sql.NullString

		// SHOW FULL COLUMNS FROM db.table returns:
		// Field, Type, Collation, Null, Key, Default, Extra, Privileges, Comment
		if err := rows.Scan(
			&col.Name,
			&col.Type,
			&collation,
			&null,
			&key,
			&defaultVal,
			&extra,
			&dummyPrivileges,
			&comment,
		); err != nil {
			return nil, DescribeTableOutput{}, fmt.Errorf("scan column failed: %w", err)
		}
		// Convert NullString to string (empty string if NULL)
		col.Collation = collation.String
		col.Null = null.String
		col.Key = key.String
		col.Default = defaultVal.String
		col.Extra = extra.String
		col.Comment = comment.String

		out.Columns = append(out.Columns, col)
		if len(out.Columns) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, DescribeTableOutput{}, err
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

	// Switch to the specified database if provided
	database := strings.TrimSpace(input.Database)
	var rows *sql.Rows
	var err error

	if database != "" {
		var dbName string
		dbName, err = util.QuoteIdent(database)
		if err != nil {
			return nil, QueryResult{}, fmt.Errorf("invalid database name: %w", err)
		}
		// Use a single connection to ensure USE affects the query
		var conn *sql.Conn
		conn, err = getDB().Conn(ctx)
		if err != nil {
			return nil, QueryResult{}, fmt.Errorf("failed to get connection: %w", err)
		}
		defer conn.Close()

		_, err = conn.ExecContext(ctx, "USE "+dbName)
		if err != nil {
			return nil, QueryResult{}, fmt.Errorf("failed to switch database: %w", err)
		}
		rows, err = conn.QueryContext(ctx, sqlText)
	} else {
		rows, err = getDB().QueryContext(ctx, sqlText)
	}

	if err != nil {
		timer.LogError(err, sqlText, tokens)
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
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, QueryResult{}, fmt.Errorf("get columns failed: %w", err)
	}

	result := QueryResult{
		Columns: cols,
		Rows:    make([][]interface{}, 0),
	}

	for rows.Next() {
		raw := make([]interface{}, len(cols))
		dest := make([]interface{}, len(cols))
		for i := range raw {
			dest[i] = &raw[i]
		}

		if err := rows.Scan(dest...); err != nil {
			return nil, QueryResult{}, fmt.Errorf("scan row failed: %w", err)
		}

		rowVals := make([]interface{}, len(cols))
		for i, v := range raw {
			rowVals[i] = util.NormalizeValue(v)
		}
		result.Rows = append(result.Rows, rowVals)

		if len(result.Rows) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, QueryResult{}, err
	}

	// Token estimation for output (optional)
	outputTokens, _ := estimateTokensForValue(result)
	tokens.OutputEstimated = outputTokens
	tokens.TotalEstimated = inputTokens + outputTokens

	// Log success
	timer.LogSuccess(len(result.Rows), sqlText, tokens)
	if auditLogger != nil {
		auditLogger.Log(&AuditEntry{
			Tool:         "run_query",
			Database:     database,
			Query:        util.TruncateQuery(sqlText, 500),
			DurationMs:   timer.ElapsedMs(),
			RowCount:     len(result.Rows),
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			Success:      true,
		})
	}

	return nil, result, nil
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
