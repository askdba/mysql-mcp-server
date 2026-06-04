// cmd/mysql-mcp-server/tools_extended.go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/askdba/mysql-mcp-server/internal/util"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// errExplainFilteredValue marks normalization failures caused by an invalid
// table.filtered value (see docs/superpowers/specs/2026-04-07-issue-104-richer-explain.md §7.6).
var errExplainFilteredValue = errors.New("invalid filtered field in EXPLAIN JSON plan")

// ===== Extended Tool Handlers (MYSQL_MCP_EXTENDED=1) =====

func toolListIndexes(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListIndexesInput,
) (*mcp.CallToolResult, ListIndexesOutput, error) {
	if input.Database == "" || input.Table == "" {
		return nil, ListIndexesOutput{}, fmt.Errorf("database and table are required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, ListIndexesOutput{}, err
	}

	dbName, err := util.QuoteIdent(input.Database)
	if err != nil {
		return nil, ListIndexesOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	tableName, err := util.QuoteIdent(input.Table)
	if err != nil {
		return nil, ListIndexesOutput{}, fmt.Errorf("invalid table name: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := fmt.Sprintf("SHOW INDEX FROM %s.%s", dbName, tableName)
	rows, err := getDB().QueryContext(ctx, query)
	if err != nil {
		return nil, ListIndexesOutput{}, fmt.Errorf("SHOW INDEX failed: %w", err)
	}
	defer rows.Close()

	// Get column count dynamically to handle different MySQL versions
	cols, err := rows.Columns()
	if err != nil {
		return nil, ListIndexesOutput{}, fmt.Errorf("failed to get columns: %w", err)
	}
	colCount := len(cols)

	// Validate column count before processing rows
	if colCount < 11 {
		return nil, ListIndexesOutput{}, fmt.Errorf("unexpected SHOW INDEX output: got %d columns, expected at least 11", colCount)
	}

	// Group columns by index name
	indexCols := make(map[string][]string)
	indexInfo := make(map[string]IndexInfo)

	for rows.Next() {
		values := make([]interface{}, colCount)
		ptrs := make([]interface{}, colCount)
		for i := range values {
			ptrs[i] = &values[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			continue
		}

		keyName := fmt.Sprintf("%v", util.NormalizeValue(values[2]))
		colName := fmt.Sprintf("%v", util.NormalizeValue(values[4]))
		nonUnique := fmt.Sprintf("%v", util.NormalizeValue(values[1])) == "1"
		indexType := fmt.Sprintf("%v", util.NormalizeValue(values[10]))

		indexCols[keyName] = append(indexCols[keyName], colName)
		indexInfo[keyName] = IndexInfo{
			Name:      keyName,
			NonUnique: nonUnique,
			Type:      indexType,
		}
	}

	out := ListIndexesOutput{Indexes: []IndexInfo{}}
	for name, info := range indexInfo {
		info.Columns = strings.Join(indexCols[name], ", ")
		out.Indexes = append(out.Indexes, info)
	}

	return nil, out, nil
}

func toolShowCreateTable(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ShowCreateTableInput,
) (*mcp.CallToolResult, ShowCreateTableOutput, error) {
	if input.Database == "" || input.Table == "" {
		return nil, ShowCreateTableOutput{}, fmt.Errorf("database and table are required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, ShowCreateTableOutput{}, err
	}

	dbName, err := util.QuoteIdent(input.Database)
	if err != nil {
		return nil, ShowCreateTableOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	tableName, err := util.QuoteIdent(input.Table)
	if err != nil {
		return nil, ShowCreateTableOutput{}, fmt.Errorf("invalid table name: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := fmt.Sprintf("SHOW CREATE TABLE %s.%s", dbName, tableName)
	var tbl, createStmt string
	if err := getDB().QueryRowContext(ctx, query).Scan(&tbl, &createStmt); err != nil {
		return nil, ShowCreateTableOutput{}, fmt.Errorf("SHOW CREATE TABLE failed: %w", err)
	}

	return nil, ShowCreateTableOutput{CreateStatement: createStmt}, nil
}

func toolExplainQuery(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ExplainQueryInput,
) (*mcp.CallToolResult, ExplainQueryOutput, error) {
	sqlText := strings.TrimSpace(input.SQL)
	if sqlText == "" {
		return nil, ExplainQueryOutput{}, fmt.Errorf("sql is required")
	}

	// Only allow explaining SELECT statements
	upper := strings.ToUpper(sqlText)
	if !strings.HasPrefix(upper, "SELECT") {
		return nil, ExplainQueryOutput{}, fmt.Errorf("only SELECT statements can be explained")
	}

	database := strings.TrimSpace(input.Database)
	if err := util.ValidateDatabaseAccess(database); err != nil {
		return nil, ExplainQueryOutput{}, fmt.Errorf("database access denied: %w", err)
	}
	if accessControlEnabled() && database == "" {
		return nil, ExplainQueryOutput{}, fmt.Errorf("database is required when MYSQL_MCP_ALLOWED_DATABASES is set")
	}
	if database != "" {
		if err := requireAllowedDatabase(database); err != nil {
			return nil, ExplainQueryOutput{}, err
		}
	}
	if err := requireSafeDatabase(database); err != nil {
		return nil, ExplainQueryOutput{}, err
	}
	// Enhanced SQL validation using parser + regex defense-in-depth, matching
	// run_query. Without this, EXPLAIN bypasses the system-schema guard (and the
	// MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS opt-in): the allowlist check below is a no-op
	// when no allowlist is configured, so EXPLAIN could otherwise reach mysql.* /
	// information_schema.* etc. regardless of the flag.
	if err := util.ValidateSQLCombined(sqlText); err != nil {
		logWarn("query rejected by validator", map[string]interface{}{
			"tool":  "explain_query",
			"error": err.Error(),
			"query": util.TruncateQuery(sqlText, 200),
		})
		return nil, ExplainQueryOutput{}, fmt.Errorf("query validation failed: %w", err)
	}
	if err := requireReferencedSchemasInQuery(sqlText); err != nil {
		return nil, ExplainQueryOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	format := strings.ToLower(strings.TrimSpace(input.Format))
	if format == "" {
		format = "json"
	}

	explainSQL := "EXPLAIN "
	if format == "json" {
		explainSQL = "EXPLAIN FORMAT=JSON "
	} else if format == "tree" {
		explainSQL = "EXPLAIN FORMAT=TREE "
	}
	explainSQL += sqlText
	var rows *sql.Rows
	var err error

	if database != "" {
		var dbName string
		dbName, err = util.QuoteIdent(database)
		if err != nil {
			return nil, ExplainQueryOutput{}, fmt.Errorf("invalid database name: %w", err)
		}
		var conn *sql.Conn
		conn, err = getDB().Conn(ctx)
		if err != nil {
			return nil, ExplainQueryOutput{}, fmt.Errorf("failed to get connection: %w", err)
		}
		defer conn.Close()

		_, err = conn.ExecContext(ctx, "USE "+dbName)
		if err != nil {
			return nil, ExplainQueryOutput{}, fmt.Errorf("failed to switch database: %w", err)
		}
		rows, err = conn.QueryContext(ctx, explainSQL)
	} else {
		rows, err = getDB().QueryContext(ctx, explainSQL)
	}

	if err != nil {
		return nil, ExplainQueryOutput{}, fmt.Errorf("EXPLAIN failed: %w", err)
	}
	defer rows.Close()

	var result interface{}

	if format == "json" {
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return nil, ExplainQueryOutput{}, fmt.Errorf("EXPLAIN FORMAT=JSON: %w", err)
			}
			return nil, ExplainQueryOutput{}, fmt.Errorf("EXPLAIN FORMAT=JSON returned no rows")
		}
		var jsonPlan string
		if err := rows.Scan(&jsonPlan); err != nil {
			return nil, ExplainQueryOutput{}, fmt.Errorf("failed to scan json explain: %w", err)
		}
		if rows.Next() {
			return nil, ExplainQueryOutput{}, fmt.Errorf("EXPLAIN FORMAT=JSON returned multiple rows")
		}
		if err := rows.Err(); err != nil {
			return nil, ExplainQueryOutput{}, fmt.Errorf("EXPLAIN FORMAT=JSON: %w", err)
		}
		unifiedPlan, parseErr := mapRawExplainToUnified(jsonPlan)
		if parseErr != nil {
			if errors.Is(parseErr, errExplainFilteredValue) {
				return nil, ExplainQueryOutput{}, fmt.Errorf("failed to normalize EXPLAIN JSON: %w", parseErr)
			}
			result = jsonPlan
		} else {
			result = unifiedPlan
		}
	} else {
		cols, err := rows.Columns()
		if err != nil {
			return nil, ExplainQueryOutput{}, fmt.Errorf("EXPLAIN columns: %w", err)
		}
		var traditionalPlan []map[string]interface{}
		for rows.Next() {
			values := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return nil, ExplainQueryOutput{}, fmt.Errorf("failed to scan EXPLAIN row: %w", err)
			}
			row := make(map[string]interface{})
			for i, col := range cols {
				row[col] = util.NormalizeValue(values[i])
			}
			traditionalPlan = append(traditionalPlan, row)
		}
		if err := rows.Err(); err != nil {
			return nil, ExplainQueryOutput{}, fmt.Errorf("EXPLAIN rows: %w", err)
		}
		result = traditionalPlan
	}

	out := ExplainQueryOutput{Plan: result}

	if format == "traditional" {
		if tabularPlan, ok := result.([]map[string]interface{}); ok {
			out.Warnings = analyzeExplainPlan(tabularPlan)
		}
	}

	return nil, out, nil
}

// analyzeExplainPlan inspects a traditional EXPLAIN plan and returns actionable
// optimization suggestions. It checks for full-table scans, unused indexes,
// filesort, and temporary-table operations.
func analyzeExplainPlan(plan []map[string]interface{}) []string {
	var warnings []string

	isNullLike := func(s string) bool {
		return s == "" || s == "<nil>" || strings.EqualFold(s, "null")
	}

	for _, row := range plan {
		table := fmt.Sprintf("%v", row["table"])
		accessType := strings.ToUpper(fmt.Sprintf("%v", row["type"]))
		// Missing type becomes "<NIL>" after fmt + ToUpper; do not treat as a known access type.
		accessTypeKnown := accessType != "" && !strings.EqualFold(accessType, "<nil>")
		key := fmt.Sprintf("%v", row["key"])
		possibleKeys := fmt.Sprintf("%v", row["possible_keys"])
		extra := strings.ToLower(fmt.Sprintf("%v", row["Extra"]))

		// Full table scan
		if accessType == "ALL" {
			if isNullLike(possibleKeys) {
				warnings = append(warnings, fmt.Sprintf(
					"Table '%s': full table scan with no candidate indexes — consider adding an index on the columns used in WHERE/JOIN conditions.",
					table,
				))
			} else {
				warnings = append(warnings, fmt.Sprintf(
					"Table '%s': full table scan despite candidate indexes (%s) — verify the WHERE clause matches the index prefix and that column types align.",
					table, possibleKeys,
				))
			}
		}

		// Index available but not used (requires a known non-ALL access type)
		if isNullLike(key) && !isNullLike(possibleKeys) && accessType != "ALL" && accessTypeKnown {
			warnings = append(warnings, fmt.Sprintf(
				"Table '%s': indexes (%s) are available but none were chosen — check for type mismatches or functions wrapping indexed columns.",
				table, possibleKeys,
			))
		}

		// Filesort
		if strings.Contains(extra, "using filesort") {
			warnings = append(warnings, fmt.Sprintf(
				"Table '%s': filesort required — consider a composite index whose column order matches your ORDER BY clause.",
				table,
			))
		}

		// Temporary table
		if strings.Contains(extra, "using temporary") {
			warnings = append(warnings, fmt.Sprintf(
				"Table '%s': temporary table created — consider an index covering the columns used in GROUP BY or DISTINCT.",
				table,
			))
		}
	}

	return warnings
}

func toolListViews(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListViewsInput,
) (*mcp.CallToolResult, ListViewsOutput, error) {
	if input.Database == "" {
		return nil, ListViewsOutput{}, fmt.Errorf("database is required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, ListViewsOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := `SELECT TABLE_NAME, DEFINER, SECURITY_TYPE, IS_UPDATABLE 
		FROM information_schema.VIEWS WHERE TABLE_SCHEMA = ?`
	rows, err := getDB().QueryContext(ctx, query, input.Database)
	if err != nil {
		return nil, ListViewsOutput{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	out := ListViewsOutput{Views: []ViewInfo{}}
	for rows.Next() {
		var v ViewInfo
		if err := rows.Scan(&v.Name, &v.Definer, &v.Security, &v.IsUpdatable); err != nil {
			continue
		}
		out.Views = append(out.Views, v)
		if len(out.Views) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListViewsOutput{}, err
	}

	return nil, out, nil
}

func toolListTriggers(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListTriggersInput,
) (*mcp.CallToolResult, ListTriggersOutput, error) {
	if input.Database == "" {
		return nil, ListTriggersOutput{}, fmt.Errorf("database is required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, ListTriggersOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := `SELECT TRIGGER_NAME, EVENT_MANIPULATION, EVENT_OBJECT_TABLE, ACTION_TIMING, 
		LEFT(ACTION_STATEMENT, 200) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = ?`
	rows, err := getDB().QueryContext(ctx, query, input.Database)
	if err != nil {
		return nil, ListTriggersOutput{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	out := ListTriggersOutput{Triggers: []TriggerInfo{}}
	for rows.Next() {
		var t TriggerInfo
		if err := rows.Scan(&t.Name, &t.Event, &t.Table, &t.Timing, &t.Statement); err != nil {
			continue
		}
		out.Triggers = append(out.Triggers, t)
		if len(out.Triggers) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListTriggersOutput{}, err
	}

	return nil, out, nil
}

func toolListProcedures(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListProceduresInput,
) (*mcp.CallToolResult, ListProceduresOutput, error) {
	if input.Database == "" {
		return nil, ListProceduresOutput{}, fmt.Errorf("database is required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, ListProceduresOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := `SELECT ROUTINE_NAME, DEFINER, CREATED, LAST_ALTERED, 
		IFNULL(PARAMETER_STYLE, '') FROM information_schema.ROUTINES 
		WHERE ROUTINE_SCHEMA = ? AND ROUTINE_TYPE = 'PROCEDURE'`
	rows, err := getDB().QueryContext(ctx, query, input.Database)
	if err != nil {
		return nil, ListProceduresOutput{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	out := ListProceduresOutput{Procedures: []ProcedureInfo{}}
	for rows.Next() {
		var p ProcedureInfo
		if err := rows.Scan(&p.Name, &p.Definer, &p.Created, &p.Modified, &p.ParamList); err != nil {
			continue
		}
		out.Procedures = append(out.Procedures, p)
		if len(out.Procedures) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListProceduresOutput{}, err
	}

	return nil, out, nil
}

func toolListFunctions(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListFunctionsInput,
) (*mcp.CallToolResult, ListFunctionsOutput, error) {
	if input.Database == "" {
		return nil, ListFunctionsOutput{}, fmt.Errorf("database is required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, ListFunctionsOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := `SELECT ROUTINE_NAME, DEFINER, DTD_IDENTIFIER, CREATED 
		FROM information_schema.ROUTINES 
		WHERE ROUTINE_SCHEMA = ? AND ROUTINE_TYPE = 'FUNCTION'`
	rows, err := getDB().QueryContext(ctx, query, input.Database)
	if err != nil {
		return nil, ListFunctionsOutput{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	out := ListFunctionsOutput{Functions: []FunctionInfo{}}
	for rows.Next() {
		var f FunctionInfo
		if err := rows.Scan(&f.Name, &f.Definer, &f.Returns, &f.Created); err != nil {
			continue
		}
		out.Functions = append(out.Functions, f)
		if len(out.Functions) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListFunctionsOutput{}, err
	}

	return nil, out, nil
}

func toolListPartitions(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListPartitionsInput,
) (*mcp.CallToolResult, ListPartitionsOutput, error) {
	if input.Database == "" || input.Table == "" {
		return nil, ListPartitionsOutput{}, fmt.Errorf("database and table are required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, ListPartitionsOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := `SELECT PARTITION_NAME, PARTITION_METHOD, PARTITION_EXPRESSION, 
		PARTITION_DESCRIPTION, TABLE_ROWS, DATA_LENGTH 
		FROM information_schema.PARTITIONS 
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND PARTITION_NAME IS NOT NULL`
	rows, err := getDB().QueryContext(ctx, query, input.Database, input.Table)
	if err != nil {
		return nil, ListPartitionsOutput{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	out := ListPartitionsOutput{Partitions: []PartitionInfo{}}
	for rows.Next() {
		var p PartitionInfo
		var name, method, expr, desc sql.NullString
		if err := rows.Scan(&name, &method, &expr, &desc, &p.TableRows, &p.DataLength); err != nil {
			continue
		}
		p.Name = name.String
		p.Method = method.String
		p.Expression = expr.String
		p.Description = desc.String
		out.Partitions = append(out.Partitions, p)
		if len(out.Partitions) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListPartitionsOutput{}, err
	}

	return nil, out, nil
}

func toolDatabaseSize(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input DatabaseSizeInput,
) (*mcp.CallToolResult, DatabaseSizeOutput, error) {
	database := strings.TrimSpace(input.Database)
	if database != "" {
		if err := requireAllowedDatabase(database); err != nil {
			return nil, DatabaseSizeOutput{}, err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := `SELECT 
		TABLE_SCHEMA, 
		ROUND(SUM(DATA_LENGTH + INDEX_LENGTH) / 1024 / 1024, 2) as size_mb,
		ROUND(SUM(DATA_LENGTH) / 1024 / 1024, 2) as data_mb,
		ROUND(SUM(INDEX_LENGTH) / 1024 / 1024, 2) as index_mb,
		COUNT(*) as tables
		FROM information_schema.TABLES`

	if database != "" {
		query += " WHERE TABLE_SCHEMA = ?"
	} else {
		query += " WHERE TABLE_SCHEMA NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')"
	}
	query += " GROUP BY TABLE_SCHEMA ORDER BY size_mb DESC"

	var rows *sql.Rows
	var err error
	if database != "" {
		rows, err = getDB().QueryContext(ctx, query, database)
	} else {
		rows, err = getDB().QueryContext(ctx, query)
	}
	if err != nil {
		return nil, DatabaseSizeOutput{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	out := DatabaseSizeOutput{Databases: []DatabaseSizeInfo{}}
	for rows.Next() {
		var d DatabaseSizeInfo
		if err := rows.Scan(&d.Name, &d.SizeMB, &d.DataMB, &d.IndexMB, &d.Tables); err != nil {
			continue
		}
		if accessControlEnabled() && !databaseAllowed(d.Name) {
			continue
		}
		out.Databases = append(out.Databases, d)
		if len(out.Databases) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, DatabaseSizeOutput{}, err
	}

	return nil, out, nil
}

func toolTableSize(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input TableSizeInput,
) (*mcp.CallToolResult, TableSizeOutput, error) {
	if input.Database == "" {
		return nil, TableSizeOutput{}, fmt.Errorf("database is required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, TableSizeOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := `SELECT 
		TABLE_NAME,
		TABLE_ROWS,
		ROUND(DATA_LENGTH / 1024 / 1024, 2) as data_mb,
		ROUND(INDEX_LENGTH / 1024 / 1024, 2) as index_mb,
		ROUND((DATA_LENGTH + INDEX_LENGTH) / 1024 / 1024, 2) as total_mb,
		ENGINE
		FROM information_schema.TABLES 
		WHERE TABLE_SCHEMA = ?`

	args := []interface{}{input.Database}
	if input.Table != "" {
		query += " AND TABLE_NAME = ?"
		args = append(args, input.Table)
	}
	query += " ORDER BY total_mb DESC"

	rows, err := getDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, TableSizeOutput{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	out := TableSizeOutput{Tables: []TableSizeInfo{}}
	for rows.Next() {
		var t TableSizeInfo
		var tableRows sql.NullInt64
		var dataMB, indexMB, totalMB sql.NullFloat64
		var engine sql.NullString
		if err := rows.Scan(&t.Name, &tableRows, &dataMB, &indexMB, &totalMB, &engine); err != nil {
			continue
		}
		t.Rows = tableRows.Int64
		t.DataMB = dataMB.Float64
		t.IndexMB = indexMB.Float64
		t.TotalMB = totalMB.Float64
		t.Engine = engine.String
		out.Tables = append(out.Tables, t)
		if len(out.Tables) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, TableSizeOutput{}, err
	}

	return nil, out, nil
}

func toolForeignKeys(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ForeignKeysInput,
) (*mcp.CallToolResult, ForeignKeysOutput, error) {
	if input.Database == "" {
		return nil, ForeignKeysOutput{}, fmt.Errorf("database is required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, ForeignKeysOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := `SELECT 
		CONSTRAINT_NAME, TABLE_NAME, COLUMN_NAME, 
		REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME,
		(SELECT UPDATE_RULE FROM information_schema.REFERENTIAL_CONSTRAINTS rc 
		 WHERE rc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA 
		 AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME) as on_update,
		(SELECT DELETE_RULE FROM information_schema.REFERENTIAL_CONSTRAINTS rc 
		 WHERE rc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA 
		 AND rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME) as on_delete
		FROM information_schema.KEY_COLUMN_USAGE kcu
		WHERE CONSTRAINT_SCHEMA = ? AND REFERENCED_TABLE_NAME IS NOT NULL`

	args := []interface{}{input.Database}
	if input.Table != "" {
		query += " AND TABLE_NAME = ?"
		args = append(args, input.Table)
	}

	rows, err := getDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, ForeignKeysOutput{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	out := ForeignKeysOutput{ForeignKeys: []ForeignKeyInfo{}}
	for rows.Next() {
		var fk ForeignKeyInfo
		var onUpdate, onDelete sql.NullString
		if err := rows.Scan(&fk.Name, &fk.Table, &fk.Column, &fk.ReferencedTable, &fk.ReferencedColumn, &onUpdate, &onDelete); err != nil {
			continue
		}
		fk.OnUpdate = onUpdate.String
		fk.OnDelete = onDelete.String
		out.ForeignKeys = append(out.ForeignKeys, fk)
		if len(out.ForeignKeys) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ForeignKeysOutput{}, err
	}

	return nil, out, nil
}

func toolListStatus(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListStatusInput,
) (*mcp.CallToolResult, ListStatusOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var rows *sql.Rows
	var err error

	// Use performance_schema for better performance and flexibility
	if input.Pattern != "" {
		rows, err = getDB().QueryContext(ctx,
			"SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME LIKE ? ORDER BY VARIABLE_NAME",
			input.Pattern)
	} else {
		rows, err = getDB().QueryContext(ctx,
			"SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status ORDER BY VARIABLE_NAME")
	}
	if err != nil {
		// Fallback to SHOW GLOBAL STATUS for restricted environments or older versions
		if input.Pattern != "" {
			rows, err = getDB().QueryContext(ctx, "SHOW GLOBAL STATUS LIKE ?", input.Pattern)
		} else {
			rows, err = getDB().QueryContext(ctx, "SHOW GLOBAL STATUS")
		}
		if err != nil {
			return nil, ListStatusOutput{}, fmt.Errorf("query status failed: %w", err)
		}
	}
	defer rows.Close()

	out := ListStatusOutput{Variables: []StatusVariable{}}
	for rows.Next() {
		var v StatusVariable
		if err := rows.Scan(&v.Name, &v.Value); err != nil {
			continue
		}
		out.Variables = append(out.Variables, v)
		if len(out.Variables) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListStatusOutput{}, err
	}

	return nil, out, nil
}

func toolListVariables(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListVariablesInput,
) (*mcp.CallToolResult, ListVariablesOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var rows *sql.Rows
	var err error

	// Prefer SHOW GLOBAL VARIABLES first: it is the most compatible path across managed
	// MySQL/MariaDB deployments. Some environments stall when selecting from
	// performance_schema.global_variables; use that only as a fallback.
	if input.Pattern != "" {
		rows, err = getDB().QueryContext(ctx, "SHOW GLOBAL VARIABLES LIKE ?", input.Pattern)
	} else {
		rows, err = getDB().QueryContext(ctx, "SHOW GLOBAL VARIABLES")
	}
	if err != nil {
		if input.Pattern != "" {
			rows, err = getDB().QueryContext(ctx,
				"SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_variables WHERE VARIABLE_NAME LIKE ? ORDER BY VARIABLE_NAME",
				input.Pattern)
		} else {
			rows, err = getDB().QueryContext(ctx,
				"SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_variables ORDER BY VARIABLE_NAME")
		}
		if err != nil {
			return nil, ListVariablesOutput{}, fmt.Errorf("query variables failed: %w", err)
		}
	}
	defer rows.Close()

	out := ListVariablesOutput{Variables: []ServerVariable{}}
	for rows.Next() {
		var v ServerVariable
		if err := rows.Scan(&v.Name, &v.Value); err != nil {
			continue
		}
		out.Variables = append(out.Variables, v)
		if len(out.Variables) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, ListVariablesOutput{}, err
	}

	return nil, out, nil
}

func toolSearchSchema(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input SearchSchemaInput,
) (*mcp.CallToolResult, SearchSchemaOutput, error) {
	if input.Pattern == "" {
		return nil, SearchSchemaOutput{}, fmt.Errorf("pattern is required")
	}
	if input.Database != "" {
		if err := requireAllowedDatabase(input.Database); err != nil {
			return nil, SearchSchemaOutput{}, err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	out := SearchSchemaOutput{Matches: []SchemaMatch{}}

	// 1. Search for matching tables
	tableQuery := `SELECT TABLE_SCHEMA, TABLE_NAME
		FROM information_schema.TABLES
		WHERE TABLE_NAME LIKE ?`
	var tableArgs []interface{}
	tableArgs = append(tableArgs, input.Pattern)

	if input.Database != "" {
		tableQuery += " AND TABLE_SCHEMA = ?"
		tableArgs = append(tableArgs, input.Database)
	} else if accessControlEnabled() {
		allowed := allowedDatabasesLower()
		if len(allowed) == 0 {
			return nil, SearchSchemaOutput{}, fmt.Errorf("MYSQL_MCP_ALLOWED_DATABASES is set but empty; cannot run search_schema without a database filter")
		}
		ph := strings.Repeat("?,", len(allowed))
		ph = ph[:len(ph)-1]
		tableQuery += " AND LOWER(TABLE_SCHEMA) IN (" + ph + ")"
		for _, db := range allowed {
			tableArgs = append(tableArgs, db)
		}
	} else {
		tableQuery += " AND TABLE_SCHEMA NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')"
	}
	tableQuery += " LIMIT ?"
	tableArgs = append(tableArgs, maxRows)

	rows, err := getDB().QueryContext(ctx, tableQuery, tableArgs...)
	if err != nil {
		return nil, SearchSchemaOutput{}, fmt.Errorf("table search failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var m SchemaMatch
		if err := rows.Scan(&m.Database, &m.Table); err != nil {
			return nil, SearchSchemaOutput{}, fmt.Errorf("scan table row: %w", err)
		}
		m.Type = "TABLE"
		out.Matches = append(out.Matches, m)
	}
	if err := rows.Err(); err != nil {
		return nil, SearchSchemaOutput{}, err
	}

	// 2. Search for matching columns
	colQuery := `SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME
		FROM information_schema.COLUMNS
		WHERE COLUMN_NAME LIKE ?`
	var colArgs []interface{}
	colArgs = append(colArgs, input.Pattern)

	if input.Database != "" {
		colQuery += " AND TABLE_SCHEMA = ?"
		colArgs = append(colArgs, input.Database)
	} else if accessControlEnabled() {
		allowed := allowedDatabasesLower()
		if len(allowed) == 0 {
			return nil, SearchSchemaOutput{}, fmt.Errorf("MYSQL_MCP_ALLOWED_DATABASES is set but empty; cannot run search_schema without a database filter")
		}
		ph := strings.Repeat("?,", len(allowed))
		ph = ph[:len(ph)-1]
		colQuery += " AND LOWER(TABLE_SCHEMA) IN (" + ph + ")"
		for _, db := range allowed {
			colArgs = append(colArgs, db)
		}
	} else {
		colQuery += " AND TABLE_SCHEMA NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')"
	}
	colQuery += " LIMIT ?"
	colArgs = append(colArgs, maxRows-len(out.Matches))

	if len(out.Matches) < maxRows {
		crows, err := getDB().QueryContext(ctx, colQuery, colArgs...)
		if err != nil {
			return nil, SearchSchemaOutput{}, fmt.Errorf("column search failed: %w", err)
		}
		defer crows.Close()

		for crows.Next() {
			var m SchemaMatch
			if err := crows.Scan(&m.Database, &m.Table, &m.Column); err != nil {
				return nil, SearchSchemaOutput{}, fmt.Errorf("scan column row: %w", err)
			}
			m.Type = "COLUMN"
			out.Matches = append(out.Matches, m)
		}
		if err := crows.Err(); err != nil {
			return nil, SearchSchemaOutput{}, err
		}
	}

	return nil, out, nil
}

func toolSchemaDiff(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input SchemaDiffInput,
) (*mcp.CallToolResult, SchemaDiffOutput, error) {
	if input.SourceDatabase == "" || input.TargetDatabase == "" {
		return nil, SchemaDiffOutput{}, fmt.Errorf("source_database and target_database are required")
	}
	if err := requireAllowedDatabase(input.SourceDatabase); err != nil {
		return nil, SchemaDiffOutput{}, err
	}
	if err := requireAllowedDatabase(input.TargetDatabase); err != nil {
		return nil, SchemaDiffOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	out := SchemaDiffOutput{
		SourceDatabase: input.SourceDatabase,
		TargetDatabase: input.TargetDatabase,
		Diffs:          []DiffResult{},
	}

	// Get tables from source
	sourceTables := make(map[string]bool)
	sourceRows, err := getDB().QueryContext(ctx, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?", input.SourceDatabase)
	if err != nil {
		return nil, SchemaDiffOutput{}, fmt.Errorf("failed to list source tables: %w", err)
	}
	defer sourceRows.Close()
	for sourceRows.Next() {
		var name string
		if err := sourceRows.Scan(&name); err != nil {
			sourceRows.Close()
			return nil, SchemaDiffOutput{}, fmt.Errorf("scan source table name: %w", err)
		}
		sourceTables[name] = true
	}
	if err := sourceRows.Err(); err != nil {
		return nil, SchemaDiffOutput{}, fmt.Errorf("source tables iteration failed: %w", err)
	}

	// Get tables from target
	targetTables := make(map[string]bool)
	targetRows, err := getDB().QueryContext(ctx, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?", input.TargetDatabase)
	if err != nil {
		return nil, SchemaDiffOutput{}, fmt.Errorf("failed to list target tables: %w", err)
	}
	defer targetRows.Close()
	for targetRows.Next() {
		var name string
		if err := targetRows.Scan(&name); err != nil {
			targetRows.Close()
			return nil, SchemaDiffOutput{}, fmt.Errorf("scan target table name: %w", err)
		}
		targetTables[name] = true
	}
	if err := targetRows.Err(); err != nil {
		return nil, SchemaDiffOutput{}, fmt.Errorf("target tables iteration failed: %w", err)
	}

	// Compare tables
	for name := range sourceTables {
		if !targetTables[name] {
			out.Diffs = append(out.Diffs, DiffResult{
				Table:   name,
				Status:  "MISSING",
				Details: fmt.Sprintf("Table exists in %s but missing in %s", input.SourceDatabase, input.TargetDatabase),
			})
		}
	}

	for name := range targetTables {
		if !sourceTables[name] {
			out.Diffs = append(out.Diffs, DiffResult{
				Table:   name,
				Status:  "EXTRA",
				Details: fmt.Sprintf("Table exists in %s but missing in %s", input.TargetDatabase, input.SourceDatabase),
			})
		} else {
			diff, err := compareTableSchema(ctx, input.SourceDatabase, input.TargetDatabase, name)
			if err != nil {
				return nil, SchemaDiffOutput{}, err
			}
			if diff != "" {
				out.Diffs = append(out.Diffs, DiffResult{
					Table:   name,
					Status:  "CHANGED",
					Details: diff,
				})
			}
		}
	}

	return nil, out, nil
}

func compareTableSchema(ctx context.Context, sourceDB, targetDB, table string) (string, error) {
	query := `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT 
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? 
		ORDER BY COLUMN_NAME`

	getSourceCols := func(dbName string) (map[string]string, error) {
		rows, err := getDB().QueryContext(ctx, query, dbName, table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		cols := make(map[string]string)
		for rows.Next() {
			var name, ctype, nullable, def sql.NullString
			if err := rows.Scan(&name, &ctype, &nullable, &def); err != nil {
				return nil, fmt.Errorf("scan column metadata: %w", err)
			}
			defVal := "<NULL>"
			if def.Valid {
				defVal = def.String
			}
			cols[name.String] = fmt.Sprintf("%s, Null:%s, Def:%s", ctype.String, nullable.String, defVal)
		}
		return cols, rows.Err()
	}

	sCols, err := getSourceCols(sourceDB)
	if err != nil {
		return "", err
	}
	tCols, err := getSourceCols(targetDB)
	if err != nil {
		return "", err
	}

	var diffs []string
	for name, sDef := range sCols {
		tDef, exists := tCols[name]
		if !exists {
			diffs = append(diffs, fmt.Sprintf("Column %s missing in %s", name, targetDB))
		} else if sDef != tDef {
			diffs = append(diffs, fmt.Sprintf("Column %s differs: %s (src) vs %s (tgt)", name, sDef, tDef))
		}
	}

	for name := range tCols {
		if _, exists := sCols[name]; !exists {
			diffs = append(diffs, fmt.Sprintf("Column %s extra in %s", name, targetDB))
		}
	}

	if len(diffs) == 0 {
		return "", nil
	}
	return strings.Join(diffs, "; "), nil
}

// ===== Vector Tool Handlers (MySQL 9.0+) =====

func toolVectorSearch(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input VectorSearchInput,
) (*mcp.CallToolResult, VectorSearchOutput, error) {
	if input.Database == "" || input.Table == "" || input.Column == "" {
		return nil, VectorSearchOutput{}, fmt.Errorf("database, table, and column are required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, VectorSearchOutput{}, err
	}
	if len(input.Query) == 0 {
		return nil, VectorSearchOutput{}, fmt.Errorf("query vector is required")
	}

	dbName, err := util.QuoteIdent(input.Database)
	if err != nil {
		return nil, VectorSearchOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	tableName, err := util.QuoteIdent(input.Table)
	if err != nil {
		return nil, VectorSearchOutput{}, fmt.Errorf("invalid table name: %w", err)
	}
	colName, err := util.QuoteIdent(input.Column)
	if err != nil {
		return nil, VectorSearchOutput{}, fmt.Errorf("invalid column name: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// Set default limit, cap to maxRows for safety
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > maxRows {
		limit = maxRows
	}

	// Build vector string for MySQL
	vectorStr := buildVectorString(input.Query)

	// Determine distance function
	distFunc := "COSINE"
	switch strings.ToLower(input.DistanceFunc) {
	case "euclidean", "l2":
		distFunc = "EUCLIDEAN"
	case "dot", "inner_product":
		distFunc = "DOT"
	}

	// Build SELECT columns with validation
	selectCols := "*"
	if input.Select != "" {
		validatedCols, err := util.ValidateSelectColumns(input.Select)
		if err != nil {
			return nil, VectorSearchOutput{}, fmt.Errorf("invalid select columns: %w", err)
		}
		selectCols = validatedCols
	}

	// Build query with vector distance
	query := fmt.Sprintf(`
		SELECT %s, 
			DISTANCE(%s, STRING_TO_VECTOR('%s'), '%s') AS _distance
		FROM %s.%s
	`, selectCols, colName, vectorStr, distFunc, dbName, tableName)

	// Validate WHERE clause if provided
	if input.Where != "" {
		if err := util.ValidateWhereClause(input.Where); err != nil {
			return nil, VectorSearchOutput{}, fmt.Errorf("invalid where clause: %w", err)
		}
		query += " WHERE " + input.Where
	}

	query += fmt.Sprintf(" ORDER BY _distance ASC LIMIT %d", limit)

	rows, err := getDB().QueryContext(ctx, query)
	if err != nil {
		if strings.Contains(err.Error(), "DISTANCE") || strings.Contains(err.Error(), "STRING_TO_VECTOR") {
			return nil, VectorSearchOutput{}, fmt.Errorf("vector search failed (MySQL 9.0+ required): %w", err)
		}
		return nil, VectorSearchOutput{}, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, VectorSearchOutput{}, fmt.Errorf("failed to get columns: %w", err)
	}

	out := VectorSearchOutput{Results: []VectorSearchResult{}}

	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			continue
		}

		result := VectorSearchResult{
			Data: make(map[string]interface{}),
		}

		for i, col := range cols {
			if col == "_distance" {
				if dist, ok := values[i].(float64); ok {
					result.Distance = dist
				}
			} else {
				result.Data[col] = util.NormalizeValue(values[i])
			}
		}

		out.Results = append(out.Results, result)
	}

	out.Count = len(out.Results)
	return nil, out, nil
}

func toolVectorInfo(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input VectorInfoInput,
) (*mcp.CallToolResult, VectorInfoOutput, error) {
	if input.Database == "" {
		return nil, VectorInfoOutput{}, fmt.Errorf("database is required")
	}
	if err := requireAllowedDatabase(input.Database); err != nil {
		return nil, VectorInfoOutput{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	out := VectorInfoOutput{Columns: []VectorColumnInfo{}}

	// Check MySQL version for vector support
	var version string
	if err := getDB().QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		return nil, VectorInfoOutput{}, fmt.Errorf("failed to get version: %w", err)
	}
	out.MySQLVersion = version

	// Check if VECTOR type is supported (MySQL 9.0+)
	out.VectorSupport = isVectorSupported(version)

	if !out.VectorSupport {
		return nil, out, nil
	}

	// Query for VECTOR columns from information_schema
	query := `
		SELECT TABLE_NAME, COLUMN_NAME, COLUMN_TYPE
		FROM information_schema.COLUMNS 
		WHERE TABLE_SCHEMA = ? 
		AND COLUMN_TYPE LIKE 'vector%'
	`
	args := []interface{}{input.Database}

	if input.Table != "" {
		query += " AND TABLE_NAME = ?"
		args = append(args, input.Table)
	}

	rows, err := getDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, VectorInfoOutput{}, fmt.Errorf("failed to query vector columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tableName, colName, colType string
		if err := rows.Scan(&tableName, &colName, &colType); err != nil {
			continue
		}

		info := VectorColumnInfo{
			Table:  tableName,
			Column: colName,
		}

		// Extract dimensions from type like "vector(768)"
		if matches := vectorDimensionsRegex.FindStringSubmatch(colType); len(matches) > 1 {
			info.Dimensions, _ = strconv.Atoi(matches[1])
		}

		// Check for vector index
		const indexQuery = `
			SELECT INDEX_NAME, INDEX_TYPE 
			FROM information_schema.STATISTICS 
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ?
		`
		var indexName, indexType sql.NullString
		_ = getDB().QueryRowContext(ctx, indexQuery, input.Database, tableName, colName).Scan(&indexName, &indexType)
		info.IndexName = indexName.String
		info.IndexType = indexType.String

		out.Columns = append(out.Columns, info)
	}

	return nil, out, nil
}

// buildVectorString converts a float64 slice to MySQL vector string format.
func buildVectorString(vec []float64) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = fmt.Sprintf("%f", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// isVectorSupported checks if MySQL version supports VECTOR type (9.0+).
// Returns false for MariaDB and unknown server types to avoid incorrectly
// enabling MySQL-specific features.
func isVectorSupported(version string) bool {
	serverType := getServerType()
	if serverType == ServerTypeMariaDB || serverType == ServerTypeUnknown {
		return false
	}
	parts := strings.Split(version, ".")
	if len(parts) < 1 {
		return false
	}
	major, err := strconv.Atoi(strings.TrimLeft(parts[0], "0"))
	if err != nil {
		return false
	}
	return major >= 9
}

func mapRawExplainToUnified(rawJSON string) (UnifiedExplainPlan, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		return UnifiedExplainPlan{}, err
	}

	plan := UnifiedExplainPlan{}

	if qb, ok := raw["query_block"].(map[string]interface{}); ok {
		costFromInfo := false
		if ci, ok := qb["cost_info"].(map[string]interface{}); ok {
			if costStr, ok := ci["query_cost"].(string); ok {
				if v, err := strconv.ParseFloat(costStr, 64); err == nil {
					plan.QueryCost = v
					costFromInfo = true
				}
			}
		}
		if !costFromInfo {
			if v, ok := float64FromExplainJSONNumber(qb["cost"]); ok {
				plan.QueryCost = v
			}
		}
		if tables, ok := qb["table"].(map[string]interface{}); ok {
			op, err := extractUnifiedOp(tables)
			if err != nil {
				return UnifiedExplainPlan{}, err
			}
			plan.Operations = append(plan.Operations, op)
		} else if tablesList, ok := qb["table"].([]interface{}); ok {
			for _, t := range tablesList {
				if tMap, ok := t.(map[string]interface{}); ok {
					op, err := extractUnifiedOp(tMap)
					if err != nil {
						return UnifiedExplainPlan{}, err
					}
					plan.Operations = append(plan.Operations, op)
				}
			}
		} else if nestedOps, ok := qb["nested_loop"].([]interface{}); ok {
			for _, nl := range nestedOps {
				if nlMap, ok := nl.(map[string]interface{}); ok {
					if tMap, ok := tableMapFromNestedLoopStep(nlMap); ok {
						op, err := extractUnifiedOp(tMap)
						if err != nil {
							return UnifiedExplainPlan{}, err
						}
						plan.Operations = append(plan.Operations, op)
					}
				}
			}
		}
	}

	return plan, nil
}

// tableMapFromNestedLoopStep extracts a per-step table map from a nested_loop element.
// It handles a direct "table" object and MariaDB's "block-nl-join" wrapper. For block-nl-join,
// the wrapper's attached_condition MUST be merged into a copy of block-nl-join.table when
// that table object has no attached_condition (see spec: deterministic merge rule).
func tableMapFromNestedLoopStep(nlMap map[string]interface{}) (map[string]interface{}, bool) {
	if tMap, ok := nlMap["table"].(map[string]interface{}); ok {
		return tMap, true
	}
	bnl, ok := nlMap["block-nl-join"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	tMap, ok := bnl["table"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	if _, has := tMap["attached_condition"]; has {
		return tMap, true
	}
	wrapCond, ok := bnl["attached_condition"].(string)
	if !ok || wrapCond == "" {
		return tMap, true
	}
	merged := make(map[string]interface{}, len(tMap)+1)
	for k, v := range tMap {
		merged[k] = v
	}
	merged["attached_condition"] = wrapCond
	return merged, true
}

// float64FromExplainJSONNumber coerces EXPLAIN FORMAT=JSON numeric fields from
// map[string]interface{} (float64 from encoding/json, json.Number with UseNumber, or integer types).
func float64FromExplainJSONNumber(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// parseExplainFilteredField implements §7.6 rule 4 (filtered) for EXPLAIN JSON table maps.
func parseExplainFilteredField(v interface{}) (float64, error) {
	if v == nil {
		return 0, nil
	}
	if f, ok := float64FromExplainJSONNumber(v); ok {
		return f, nil
	}
	s, ok := v.(string)
	if !ok {
		return 0, fmt.Errorf("%w: unsupported type %T (allowed: JSON numbers and numeric strings)", errExplainFilteredValue, v)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("%w: filtered is empty string", errExplainFilteredValue)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: cannot parse %q as numeric string: %v", errExplainFilteredValue, s, err)
	}
	return f, nil
}

func extractUnifiedOp(table map[string]interface{}) (UnifiedOp, error) {
	op := UnifiedOp{}
	if name, ok := table["table_name"].(string); ok {
		op.TableName = name
	}
	if access, ok := table["access_type"].(string); ok {
		op.AccessType = access
	}
	if key, ok := table["key"].(string); ok {
		op.Key = key
	}
	if keyLen, ok := table["key_length"].(string); ok {
		op.KeyLength = keyLen
	}

	if rows, ok := table["rows_examined_per_scan"].(float64); ok {
		op.RowsExamined = int64(rows)
	} else if rows, ok := table["rows"].(float64); ok {
		op.RowsExamined = int64(rows)
	}

	if v, ok := table["filtered"]; ok {
		f, err := parseExplainFilteredField(v)
		if err != nil {
			return UnifiedOp{}, err
		}
		op.Filtered = f
	}

	if msg, ok := table["message"].(string); ok {
		op.Message = msg
	}
	if extra, ok := table["Extra"].(string); ok {
		op.Message = extra
	}

	if cond, ok := table["attached_condition"].(string); ok {
		op.AttachedCondition = cond
	}

	if ci, ok := table["cost_info"].(map[string]interface{}); ok {
		if rcStr, ok := ci["read_cost"].(string); ok {
			if v, err := strconv.ParseFloat(rcStr, 64); err == nil {
				op.CostInfo.ReadCost = v
			}
		}
		if ecStr, ok := ci["eval_cost"].(string); ok {
			if v, err := strconv.ParseFloat(ecStr, 64); err == nil {
				op.CostInfo.EvalCost = v
			}
		}
		if pcStr, ok := ci["prefix_cost"].(string); ok {
			if v, err := strconv.ParseFloat(pcStr, 64); err == nil {
				op.CostInfo.PrefixCost = v
			}
		}
		if dStr, ok := ci["data_read_per_join"].(string); ok {
			op.CostInfo.DataReadPerJoin = dStr
		}
	}

	if pkList, ok := table["possible_keys"].([]interface{}); ok {
		for _, pk := range pkList {
			if str, ok := pk.(string); ok {
				op.PossibleKeys = append(op.PossibleKeys, str)
			}
		}
	}
	return op, nil
}
