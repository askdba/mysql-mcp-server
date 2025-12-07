// cmd/mysql-mcp-server/tools_extended.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/askdba/mysql-mcp-server/internal/util"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ===== Extended Tool Handlers (MYSQL_MCP_EXTENDED=1) =====

func toolListIndexes(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListIndexesInput,
) (*mcp.CallToolResult, ListIndexesOutput, error) {
	if input.Database == "" || input.Table == "" {
		return nil, ListIndexesOutput{}, fmt.Errorf("database and table are required")
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

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	database := strings.TrimSpace(input.Database)
	explainSQL := "EXPLAIN " + sqlText
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

	cols, _ := rows.Columns()
	out := ExplainQueryOutput{Plan: []map[string]interface{}{}}

	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(map[string]interface{})
		for i, col := range cols {
			row[col] = util.NormalizeValue(values[i])
		}
		out.Plan = append(out.Plan, row)
	}

	return nil, out, nil
}

func toolListViews(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListViewsInput,
) (*mcp.CallToolResult, ListViewsOutput, error) {
	if input.Database == "" {
		return nil, ListViewsOutput{}, fmt.Errorf("database is required")
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
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := `SELECT 
		TABLE_SCHEMA, 
		ROUND(SUM(DATA_LENGTH + INDEX_LENGTH) / 1024 / 1024, 2) as size_mb,
		ROUND(SUM(DATA_LENGTH) / 1024 / 1024, 2) as data_mb,
		ROUND(SUM(INDEX_LENGTH) / 1024 / 1024, 2) as index_mb,
		COUNT(*) as tables
		FROM information_schema.TABLES`

	if input.Database != "" {
		query += " WHERE TABLE_SCHEMA = ?"
	} else {
		query += " WHERE TABLE_SCHEMA NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')"
	}
	query += " GROUP BY TABLE_SCHEMA ORDER BY size_mb DESC"

	var rows *sql.Rows
	var err error
	if input.Database != "" {
		rows, err = getDB().QueryContext(ctx, query, input.Database)
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

	query := "SHOW GLOBAL STATUS"
	if input.Pattern != "" {
		query += " LIKE ?"
	}

	var rows *sql.Rows
	var err error
	if input.Pattern != "" {
		rows, err = getDB().QueryContext(ctx, query, input.Pattern)
	} else {
		rows, err = getDB().QueryContext(ctx, query)
	}
	if err != nil {
		return nil, ListStatusOutput{}, fmt.Errorf("SHOW STATUS failed: %w", err)
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

	query := "SHOW GLOBAL VARIABLES"
	if input.Pattern != "" {
		query += " LIKE ?"
	}

	var rows *sql.Rows
	var err error
	if input.Pattern != "" {
		rows, err = getDB().QueryContext(ctx, query, input.Pattern)
	} else {
		rows, err = getDB().QueryContext(ctx, query)
	}
	if err != nil {
		return nil, ListVariablesOutput{}, fmt.Errorf("SHOW VARIABLES failed: %w", err)
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

// ===== Vector Tool Handlers (MySQL 9.0+) =====

func toolVectorSearch(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input VectorSearchInput,
) (*mcp.CallToolResult, VectorSearchOutput, error) {
	if input.Database == "" || input.Table == "" || input.Column == "" {
		return nil, VectorSearchOutput{}, fmt.Errorf("database, table, and column are required")
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
		getDB().QueryRowContext(ctx, indexQuery, input.Database, tableName, colName).Scan(&indexName, &indexType)
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
func isVectorSupported(version string) bool {
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
