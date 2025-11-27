// cmd/mysql-mcp-server/main.go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMaxRows          = 200
	defaultQueryTimeoutSecs = 30
	defaultMaxOpenConns     = 10
	defaultMaxIdleConns     = 5
	defaultConnMaxLifetimeM = 30
)

// Global DB handle shared by all tools (safe for concurrent use).
var (
	db           *sql.DB
	maxRows      int
	queryTimeout time.Duration
	extendedMode bool
	jsonLogging  bool
	auditLogger  *AuditLogger
)

// ===== Structured Logging =====

type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

func logJSON(level, message string, fields map[string]interface{}) {
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Message:   message,
		Fields:    fields,
	}
	if jsonLogging {
		data, _ := json.Marshal(entry)
		fmt.Fprintln(os.Stderr, string(data))
	} else {
		if len(fields) > 0 {
			log.Printf("[%s] %s %v", level, message, fields)
		} else {
			log.Printf("[%s] %s", level, message)
		}
	}
}

func logInfo(message string, fields map[string]interface{}) {
	logJSON("INFO", message, fields)
}

func logWarn(message string, fields map[string]interface{}) {
	logJSON("WARN", message, fields)
}

func logError(message string, fields map[string]interface{}) {
	logJSON("ERROR", message, fields)
}

// ===== Audit Logging =====

type AuditEntry struct {
	Timestamp  string  `json:"timestamp"`
	Tool       string  `json:"tool"`
	Database   string  `json:"database,omitempty"`
	Query      string  `json:"query,omitempty"`
	DurationMs int64   `json:"duration_ms"`
	RowCount   int     `json:"row_count,omitempty"`
	Success    bool    `json:"success"`
	Error      string  `json:"error,omitempty"`
}

type AuditLogger struct {
	file   *os.File
	mu     sync.Mutex
	enabled bool
}

func NewAuditLogger(path string) (*AuditLogger, error) {
	if path == "" {
		return &AuditLogger{enabled: false}, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log: %w", err)
	}
	return &AuditLogger{file: f, enabled: true}, nil
}

func (a *AuditLogger) Log(entry AuditEntry) {
	if !a.enabled {
		return
	}
	entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	a.mu.Lock()
	defer a.mu.Unlock()
	data, _ := json.Marshal(entry)
	a.file.WriteString(string(data) + "\n")
}

func (a *AuditLogger) Close() {
	if a.file != nil {
		a.file.Close()
	}
}

// ===== Query Timing Helper =====

type QueryTimer struct {
	start time.Time
	tool  string
}

func NewQueryTimer(tool string) *QueryTimer {
	return &QueryTimer{start: time.Now(), tool: tool}
}

func (t *QueryTimer) Elapsed() time.Duration {
	return time.Since(t.start)
}

func (t *QueryTimer) ElapsedMs() int64 {
	return t.Elapsed().Milliseconds()
}

func (t *QueryTimer) LogSuccess(rowCount int, query string) {
	fields := map[string]interface{}{
		"tool":        t.tool,
		"duration_ms": t.ElapsedMs(),
		"row_count":   rowCount,
	}
	if query != "" && len(query) <= 200 {
		fields["query"] = query
	}
	logInfo("query executed", fields)
}

func (t *QueryTimer) LogError(err error, query string) {
	fields := map[string]interface{}{
		"tool":        t.tool,
		"duration_ms": t.ElapsedMs(),
		"error":       err.Error(),
	}
	if query != "" && len(query) <= 200 {
		fields["query"] = query
	}
	logError("query failed", fields)
}

// ===== Tool input / output types =====

type ListDatabasesInput struct{}

type DatabaseInfo struct {
	Name string `json:"name" jsonschema:"database name"`
}

type ListDatabasesOutput struct {
	Databases []DatabaseInfo `json:"databases" jsonschema:"list of accessible databases"`
}

type ListTablesInput struct {
	Database string `json:"database" jsonschema:"database name to list tables from"`
}

type TableInfo struct {
	Name string `json:"name" jsonschema:"table name"`
}

type ListTablesOutput struct {
	Tables []TableInfo `json:"tables" jsonschema:"list of tables in the database"`
}

type DescribeTableInput struct {
	Database string `json:"database" jsonschema:"database name"`
	Table    string `json:"table" jsonschema:"table name"`
}

type ColumnInfo struct {
	Name      string `json:"name" jsonschema:"column name"`
	Type      string `json:"type" jsonschema:"column type"`
	Null      string `json:"null" jsonschema:"YES if nullable, NO otherwise"`
	Key       string `json:"key" jsonschema:"key information (PRI, MUL, etc.)"`
	Default   string `json:"default" jsonschema:"default value, if any"`
	Extra     string `json:"extra" jsonschema:"extra metadata (auto_increment, etc.)"`
	Comment   string `json:"comment" jsonschema:"column comment, if any"`
	Collation string `json:"collation" jsonschema:"column collation, if any"`
}

type DescribeTableOutput struct {
	Columns []ColumnInfo `json:"columns" jsonschema:"detailed column information"`
}

type RunQueryInput struct {
	SQL      string `json:"sql" jsonschema:"SQL query to execute; must start with SELECT, SHOW, DESCRIBE, or EXPLAIN"`
	MaxRows  *int   `json:"max_rows,omitempty" jsonschema:"optional row limit overriding the default max rows"`
	Database string `json:"database,omitempty" jsonschema:"optional database name to USE before running the query"`
}

type QueryResult struct {
	Columns []string        `json:"columns" jsonschema:"column names"`
	Rows    [][]interface{} `json:"rows" jsonschema:"rows of values"`
}

type PingInput struct{}

type PingOutput struct {
	Success   bool   `json:"success" jsonschema:"true if the database is reachable"`
	LatencyMs int64  `json:"latency_ms" jsonschema:"round-trip latency in milliseconds"`
	Message   string `json:"message" jsonschema:"status message"`
}

type ServerInfoInput struct{}

type ServerInfoOutput struct {
	Version          string `json:"version" jsonschema:"MySQL server version"`
	VersionComment   string `json:"version_comment" jsonschema:"MySQL version comment (e.g., MySQL Community Server)"`
	Uptime           int64  `json:"uptime_seconds" jsonschema:"server uptime in seconds"`
	CurrentDatabase  string `json:"current_database" jsonschema:"currently selected database, if any"`
	CurrentUser      string `json:"current_user" jsonschema:"current MySQL user"`
	CharacterSet     string `json:"character_set" jsonschema:"server character set"`
	Collation        string `json:"collation" jsonschema:"server collation"`
	MaxConnections   int    `json:"max_connections" jsonschema:"maximum allowed connections"`
	ThreadsConnected int    `json:"threads_connected" jsonschema:"current number of connected threads"`
}

// ===== Extended Tool Types (MYSQL_MCP_EXTENDED=1) =====

type ListIndexesInput struct {
	Database string `json:"database" jsonschema:"database name"`
	Table    string `json:"table" jsonschema:"table name"`
}

type IndexInfo struct {
	Name      string `json:"name" jsonschema:"index name"`
	Columns   string `json:"columns" jsonschema:"columns in the index"`
	NonUnique bool   `json:"non_unique" jsonschema:"true if index allows duplicates"`
	Type      string `json:"type" jsonschema:"index type (BTREE, HASH, etc.)"`
}

type ListIndexesOutput struct {
	Indexes []IndexInfo `json:"indexes" jsonschema:"list of indexes on the table"`
}

type ShowCreateTableInput struct {
	Database string `json:"database" jsonschema:"database name"`
	Table    string `json:"table" jsonschema:"table name"`
}

type ShowCreateTableOutput struct {
	CreateStatement string `json:"create_statement" jsonschema:"CREATE TABLE statement"`
}

type ExplainQueryInput struct {
	SQL      string `json:"sql" jsonschema:"SELECT query to explain"`
	Database string `json:"database,omitempty" jsonschema:"optional database context"`
	Format   string `json:"format,omitempty" jsonschema:"output format: traditional, json, tree (default: traditional)"`
}

type ExplainQueryOutput struct {
	Plan []map[string]interface{} `json:"plan" jsonschema:"query execution plan"`
}

type ListViewsInput struct {
	Database string `json:"database" jsonschema:"database name"`
}

type ViewInfo struct {
	Name        string `json:"name" jsonschema:"view name"`
	Definer     string `json:"definer" jsonschema:"view definer"`
	Security    string `json:"security" jsonschema:"security type (DEFINER or INVOKER)"`
	IsUpdatable string `json:"is_updatable" jsonschema:"YES if view is updatable"`
}

type ListViewsOutput struct {
	Views []ViewInfo `json:"views" jsonschema:"list of views in the database"`
}

type ListTriggersInput struct {
	Database string `json:"database" jsonschema:"database name"`
}

type TriggerInfo struct {
	Name      string `json:"name" jsonschema:"trigger name"`
	Event     string `json:"event" jsonschema:"triggering event (INSERT, UPDATE, DELETE)"`
	Table     string `json:"table" jsonschema:"table the trigger is on"`
	Timing    string `json:"timing" jsonschema:"BEFORE or AFTER"`
	Statement string `json:"statement" jsonschema:"trigger body (truncated)"`
}

type ListTriggersOutput struct {
	Triggers []TriggerInfo `json:"triggers" jsonschema:"list of triggers"`
}

type ListProceduresInput struct {
	Database string `json:"database" jsonschema:"database name"`
}

type ProcedureInfo struct {
	Name      string `json:"name" jsonschema:"procedure name"`
	Definer   string `json:"definer" jsonschema:"procedure definer"`
	Created   string `json:"created" jsonschema:"creation timestamp"`
	Modified  string `json:"modified" jsonschema:"last modified timestamp"`
	ParamList string `json:"parameters" jsonschema:"parameter list"`
}

type ListProceduresOutput struct {
	Procedures []ProcedureInfo `json:"procedures" jsonschema:"list of stored procedures"`
}

type ListFunctionsInput struct {
	Database string `json:"database" jsonschema:"database name"`
}

type FunctionInfo struct {
	Name    string `json:"name" jsonschema:"function name"`
	Definer string `json:"definer" jsonschema:"function definer"`
	Returns string `json:"returns" jsonschema:"return type"`
	Created string `json:"created" jsonschema:"creation timestamp"`
}

type ListFunctionsOutput struct {
	Functions []FunctionInfo `json:"functions" jsonschema:"list of stored functions"`
}

type ListPartitionsInput struct {
	Database string `json:"database" jsonschema:"database name"`
	Table    string `json:"table" jsonschema:"table name"`
}

type PartitionInfo struct {
	Name        string `json:"name" jsonschema:"partition name"`
	Method      string `json:"method" jsonschema:"partitioning method"`
	Expression  string `json:"expression" jsonschema:"partitioning expression"`
	Description string `json:"description" jsonschema:"partition description/value"`
	TableRows   int64  `json:"table_rows" jsonschema:"approximate row count"`
	DataLength  int64  `json:"data_length" jsonschema:"data size in bytes"`
}

type ListPartitionsOutput struct {
	Partitions []PartitionInfo `json:"partitions" jsonschema:"list of partitions"`
}

type DatabaseSizeInput struct {
	Database string `json:"database,omitempty" jsonschema:"database name (optional, all databases if empty)"`
}

type DatabaseSizeInfo struct {
	Name    string  `json:"name" jsonschema:"database name"`
	SizeMB  float64 `json:"size_mb" jsonschema:"total size in megabytes"`
	DataMB  float64 `json:"data_mb" jsonschema:"data size in megabytes"`
	IndexMB float64 `json:"index_mb" jsonschema:"index size in megabytes"`
	Tables  int     `json:"tables" jsonschema:"number of tables"`
}

type DatabaseSizeOutput struct {
	Databases []DatabaseSizeInfo `json:"databases" jsonschema:"database size information"`
}

type TableSizeInput struct {
	Database string `json:"database" jsonschema:"database name"`
	Table    string `json:"table,omitempty" jsonschema:"table name (optional, all tables if empty)"`
}

type TableSizeInfo struct {
	Name    string  `json:"name" jsonschema:"table name"`
	Rows    int64   `json:"rows" jsonschema:"approximate row count"`
	DataMB  float64 `json:"data_mb" jsonschema:"data size in megabytes"`
	IndexMB float64 `json:"index_mb" jsonschema:"index size in megabytes"`
	TotalMB float64 `json:"total_mb" jsonschema:"total size in megabytes"`
	Engine  string  `json:"engine" jsonschema:"storage engine"`
}

type TableSizeOutput struct {
	Tables []TableSizeInfo `json:"tables" jsonschema:"table size information"`
}

type ForeignKeysInput struct {
	Database string `json:"database" jsonschema:"database name"`
	Table    string `json:"table,omitempty" jsonschema:"table name (optional)"`
}

type ForeignKeyInfo struct {
	Name             string `json:"name" jsonschema:"constraint name"`
	Table            string `json:"table" jsonschema:"table name"`
	Column           string `json:"column" jsonschema:"column name"`
	ReferencedTable  string `json:"referenced_table" jsonschema:"referenced table"`
	ReferencedColumn string `json:"referenced_column" jsonschema:"referenced column"`
	OnUpdate         string `json:"on_update" jsonschema:"ON UPDATE action"`
	OnDelete         string `json:"on_delete" jsonschema:"ON DELETE action"`
}

type ForeignKeysOutput struct {
	ForeignKeys []ForeignKeyInfo `json:"foreign_keys" jsonschema:"list of foreign key constraints"`
}

type ListStatusInput struct {
	Pattern string `json:"pattern,omitempty" jsonschema:"optional LIKE pattern to filter status variables"`
}

type StatusVariable struct {
	Name  string `json:"name" jsonschema:"variable name"`
	Value string `json:"value" jsonschema:"variable value"`
}

type ListStatusOutput struct {
	Variables []StatusVariable `json:"variables" jsonschema:"server status variables"`
}

type ListVariablesInput struct {
	Pattern string `json:"pattern,omitempty" jsonschema:"optional LIKE pattern to filter variables"`
}

type ServerVariable struct {
	Name  string `json:"name" jsonschema:"variable name"`
	Value string `json:"value" jsonschema:"variable value"`
}

type ListVariablesOutput struct {
	Variables []ServerVariable `json:"variables" jsonschema:"server configuration variables"`
}

// ===== Utility: env config & helpers =====

func getEnvInt(key string, def int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// quoteIdent safely quotes a MySQL identifier, returning an error if the name
// contains potentially dangerous characters.
func quoteIdent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("identifier cannot be empty")
	}
	// Reject identifiers with dangerous characters that could enable SQL injection
	if strings.ContainsAny(name, " \t\n\r;`\\") {
		return "", fmt.Errorf("identifier contains invalid characters: %q", name)
	}
	// Additional check: reject identifiers that are too long (MySQL limit is 64)
	if len(name) > 64 {
		return "", fmt.Errorf("identifier too long: %d characters (max 64)", len(name))
	}
	return "`" + name + "`", nil
}

// convert raw DB value into something JSON-friendly.
func normalizeValue(v interface{}) interface{} {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(x)
	default:
		return x
	}
}

// ===== SQL Safety Validator (Paranoid Mode) =====

// SQLValidationError contains details about why a query was rejected
type SQLValidationError struct {
	Reason  string
	Pattern string
}

func (e *SQLValidationError) Error() string {
	if e.Pattern != "" {
		return fmt.Sprintf("%s: %s", e.Reason, e.Pattern)
	}
	return e.Reason
}

// Blocked SQL patterns - these are dangerous even in SELECT statements
var blockedPatterns = []*regexp.Regexp{
	// File operations
	regexp.MustCompile(`(?i)\bLOAD_FILE\s*\(`),
	regexp.MustCompile(`(?i)\bINTO\s+OUTFILE\b`),
	regexp.MustCompile(`(?i)\bINTO\s+DUMPFILE\b`),
	regexp.MustCompile(`(?i)\bLOAD\s+DATA\b`),

	// DDL statements that might slip through
	regexp.MustCompile(`(?i)^\s*CREATE\b`),
	regexp.MustCompile(`(?i)^\s*ALTER\b`),
	regexp.MustCompile(`(?i)^\s*DROP\b`),
	regexp.MustCompile(`(?i)^\s*TRUNCATE\b`),
	regexp.MustCompile(`(?i)^\s*RENAME\b`),

	// DML statements
	regexp.MustCompile(`(?i)^\s*INSERT\b`),
	regexp.MustCompile(`(?i)^\s*UPDATE\b`),
	regexp.MustCompile(`(?i)^\s*DELETE\b`),
	regexp.MustCompile(`(?i)^\s*REPLACE\b`),

	// Administrative commands
	regexp.MustCompile(`(?i)^\s*GRANT\b`),
	regexp.MustCompile(`(?i)^\s*REVOKE\b`),
	regexp.MustCompile(`(?i)^\s*SET\s+(GLOBAL|SESSION|@@)`),
	regexp.MustCompile(`(?i)^\s*FLUSH\b`),
	regexp.MustCompile(`(?i)^\s*RESET\b`),
	regexp.MustCompile(`(?i)^\s*KILL\b`),
	regexp.MustCompile(`(?i)^\s*SHUTDOWN\b`),

	// Locking
	regexp.MustCompile(`(?i)^\s*LOCK\s+TABLES\b`),
	regexp.MustCompile(`(?i)^\s*UNLOCK\s+TABLES\b`),

	// Transactions (should not be user-controlled)
	regexp.MustCompile(`(?i)^\s*START\s+TRANSACTION\b`),
	regexp.MustCompile(`(?i)^\s*BEGIN\b`),
	regexp.MustCompile(`(?i)^\s*COMMIT\b`),
	regexp.MustCompile(`(?i)^\s*ROLLBACK\b`),
	regexp.MustCompile(`(?i)^\s*SAVEPOINT\b`),

	// Prepared statements (could be abused)
	regexp.MustCompile(`(?i)^\s*PREPARE\b`),
	regexp.MustCompile(`(?i)^\s*EXECUTE\b`),
	regexp.MustCompile(`(?i)^\s*DEALLOCATE\b`),

	// Stored procedure calls
	regexp.MustCompile(`(?i)^\s*CALL\b`),

	// User-defined functions that might be dangerous
	regexp.MustCompile(`(?i)\bSLEEP\s*\(`),
	regexp.MustCompile(`(?i)\bBENCHMARK\s*\(`),
	regexp.MustCompile(`(?i)\bGET_LOCK\s*\(`),
	regexp.MustCompile(`(?i)\bRELEASE_LOCK\s*\(`),
	regexp.MustCompile(`(?i)\bIS_FREE_LOCK\s*\(`),
	regexp.MustCompile(`(?i)\bIS_USED_LOCK\s*\(`),
}

// Allowed query prefixes (read-only operations)
var allowedPrefixes = []string{
	"SELECT",
	"SHOW",
	"DESCRIBE",
	"DESC",
	"EXPLAIN",
}

// validateSQL performs comprehensive SQL safety validation
func validateSQL(sqlText string) error {
	s := strings.TrimSpace(sqlText)
	if s == "" {
		return &SQLValidationError{Reason: "empty query"}
	}

	// Check for multi-statement attacks (semicolon-separated queries)
	// Allow semicolon only at the very end (single statement)
	cleaned := strings.TrimRight(s, "; \t\n\r")
	if strings.Contains(cleaned, ";") {
		return &SQLValidationError{
			Reason:  "multi-statement queries are not allowed",
			Pattern: ";",
		}
	}

	// Check against blocked patterns
	for _, pattern := range blockedPatterns {
		if pattern.MatchString(s) {
			return &SQLValidationError{
				Reason:  "query contains blocked pattern",
				Pattern: pattern.String(),
			}
		}
	}

	// Verify query starts with an allowed prefix
	upper := strings.ToUpper(s)
	allowed := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			allowed = true
			break
		}
	}

	if !allowed {
		return &SQLValidationError{
			Reason: "only SELECT, SHOW, DESCRIBE, and EXPLAIN queries are allowed",
		}
	}

	// Additional check: look for suspicious subqueries that modify data
	// (SELECT with subquery that does INSERT/UPDATE/DELETE)
	if strings.Contains(upper, "INSERT") || strings.Contains(upper, "UPDATE") ||
		strings.Contains(upper, "DELETE") {
		// Allow these words only if they appear in string literals or comments
		// Simple heuristic: if they appear as standalone words, block
		dangerousKeywords := regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE)\b`)
		if dangerousKeywords.MatchString(s) {
			// Check if it's likely in a string by looking for quotes around it
			// This is a simplified check - real parsing would be better
			if !isLikelyInString(s, "INSERT") && !isLikelyInString(s, "UPDATE") &&
				!isLikelyInString(s, "DELETE") {
				return &SQLValidationError{
					Reason: "query may contain data modification keywords",
				}
			}
		}
	}

	return nil
}

// isLikelyInString checks if a keyword is likely within a string literal
// This is a heuristic, not a full SQL parser
func isLikelyInString(sql, keyword string) bool {
	// Count quotes before and after the keyword
	upper := strings.ToUpper(sql)
	idx := strings.Index(upper, keyword)
	if idx == -1 {
		return false
	}

	before := sql[:idx]
	singleQuotes := strings.Count(before, "'") - strings.Count(before, "\\'")
	doubleQuotes := strings.Count(before, "\"") - strings.Count(before, "\\\"")

	// If odd number of quotes, we're likely inside a string
	return singleQuotes%2 == 1 || doubleQuotes%2 == 1
}

// Legacy compatibility wrapper
func isReadOnlySQL(sqlText string) bool {
	return validateSQL(sqlText) == nil
}

func StringsHasPrefixAny(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// ===== Tool handlers =====

func toolListDatabases(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListDatabasesInput,
) (*mcp.CallToolResult, ListDatabasesOutput, error) {

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, "SHOW DATABASES")
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

	dbName, err := quoteIdent(input.Database)
	if err != nil {
		return nil, ListTablesOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	query := fmt.Sprintf("SHOW TABLES FROM %s", dbName)

	rows, err := db.QueryContext(ctx, query)
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

	dbName, err := quoteIdent(input.Database)
	if err != nil {
		return nil, DescribeTableOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	tableName, err := quoteIdent(input.Table)
	if err != nil {
		return nil, DescribeTableOutput{}, fmt.Errorf("invalid table name: %w", err)
	}

	// Using SHOW FULL COLUMNS to get richer metadata.
	query := fmt.Sprintf("SHOW FULL COLUMNS FROM %s.%s", dbName, tableName)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, DescribeTableOutput{}, fmt.Errorf("SHOW FULL COLUMNS failed: %w", err)
	}
	defer rows.Close()

	out := DescribeTableOutput{Columns: []ColumnInfo{}}
	for rows.Next() {
		var col ColumnInfo
		var dummyPrivileges string

		// SHOW FULL COLUMNS FROM db.table returns:
		// Field, Type, Collation, Null, Key, Default, Extra, Privileges, Comment
		if err := rows.Scan(
			&col.Name,
			&col.Type,
			&col.Collation,
			&col.Null,
			&col.Key,
			&col.Default,
			&col.Extra,
			&dummyPrivileges,
			&col.Comment,
		); err != nil {
			return nil, DescribeTableOutput{}, fmt.Errorf("scan column failed: %w", err)
		}
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

	// Enhanced SQL validation
	if err := validateSQL(sqlText); err != nil {
		logWarn("query rejected by validator", map[string]interface{}{
			"error": err.Error(),
			"query": truncateQuery(sqlText, 200),
		})
		if auditLogger != nil {
			auditLogger.Log(AuditEntry{
				Tool:    "run_query",
				Query:   truncateQuery(sqlText, 500),
				Success: false,
				Error:   err.Error(),
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
	if database != "" {
		dbName, err := quoteIdent(database)
		if err != nil {
			return nil, QueryResult{}, fmt.Errorf("invalid database name: %w", err)
		}
		if _, err := db.ExecContext(ctx, "USE "+dbName); err != nil {
			return nil, QueryResult{}, fmt.Errorf("failed to switch database: %w", err)
		}
	}

	rows, err := db.QueryContext(ctx, sqlText)
	if err != nil {
		timer.LogError(err, sqlText)
		if auditLogger != nil {
			auditLogger.Log(AuditEntry{
				Tool:       "run_query",
				Database:   database,
				Query:      truncateQuery(sqlText, 500),
				DurationMs: timer.ElapsedMs(),
				Success:    false,
				Error:      err.Error(),
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
			rowVals[i] = normalizeValue(v)
		}
		result.Rows = append(result.Rows, rowVals)

		if len(result.Rows) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, QueryResult{}, err
	}

	// Log success
	timer.LogSuccess(len(result.Rows), sqlText)
	if auditLogger != nil {
		auditLogger.Log(AuditEntry{
			Tool:       "run_query",
			Database:   database,
			Query:      truncateQuery(sqlText, 500),
			DurationMs: timer.ElapsedMs(),
			RowCount:   len(result.Rows),
			Success:    true,
		})
	}

	return nil, result, nil
}

// truncateQuery truncates a query string to maxLen characters
func truncateQuery(query string, maxLen int) string {
	if len(query) <= maxLen {
		return query
	}
	return query[:maxLen] + "..."
}

func toolPing(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input PingInput,
) (*mcp.CallToolResult, PingOutput, error) {

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	start := time.Now()
	err := db.PingContext(ctx)
	latency := time.Since(start).Milliseconds()

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
	row := db.QueryRowContext(ctx, "SELECT VERSION()")
	if err := row.Scan(&out.Version); err != nil {
		return nil, ServerInfoOutput{}, fmt.Errorf("failed to get version: %w", err)
	}

	// Get various server variables in one query
	rows, err := db.QueryContext(ctx, `
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
		rows, err = db.QueryContext(ctx, `
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
	statusRows, err := db.QueryContext(ctx, `
		SELECT VARIABLE_NAME, VARIABLE_VALUE 
		FROM performance_schema.global_status 
		WHERE VARIABLE_NAME IN ('Uptime', 'Threads_connected')
	`)
	if err != nil {
		// Fallback for older MySQL or restricted permissions
		statusRows, err = db.QueryContext(ctx, `
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
	row = db.QueryRowContext(ctx, "SELECT CURRENT_USER(), IFNULL(DATABASE(), '')")
	if err := row.Scan(&out.CurrentUser, &out.CurrentDatabase); err != nil {
		return nil, ServerInfoOutput{}, fmt.Errorf("failed to get current user/database: %w", err)
	}

	return nil, out, nil
}

// ===== Extended Tool Handlers (MYSQL_MCP_EXTENDED=1) =====

func toolListIndexes(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ListIndexesInput,
) (*mcp.CallToolResult, ListIndexesOutput, error) {
	if input.Database == "" || input.Table == "" {
		return nil, ListIndexesOutput{}, fmt.Errorf("database and table are required")
	}

	dbName, err := quoteIdent(input.Database)
	if err != nil {
		return nil, ListIndexesOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	tableName, err := quoteIdent(input.Table)
	if err != nil {
		return nil, ListIndexesOutput{}, fmt.Errorf("invalid table name: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := fmt.Sprintf("SHOW INDEX FROM %s.%s", dbName, tableName)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, ListIndexesOutput{}, fmt.Errorf("SHOW INDEX failed: %w", err)
	}
	defer rows.Close()

	// Group columns by index name
	indexCols := make(map[string][]string)
	indexInfo := make(map[string]IndexInfo)

	for rows.Next() {
		var table, nonUnique, keyName, seqInIndex, colName, collation, cardinality, subPart, packed, null, indexType, comment, indexComment, visible, expression interface{}
		if err := rows.Scan(&table, &nonUnique, &keyName, &seqInIndex, &colName, &collation, &cardinality, &subPart, &packed, &null, &indexType, &comment, &indexComment, &visible, &expression); err != nil {
			// Try simpler scan for older MySQL versions
			var t, nu, kn, si, cn, coll, card, sp, pk, nl, it, cm, ic interface{}
			rows2, _ := db.QueryContext(ctx, query)
			defer rows2.Close()
			for rows2.Next() {
				if err := rows2.Scan(&t, &nu, &kn, &si, &cn, &coll, &card, &sp, &pk, &nl, &it, &cm, &ic); err != nil {
					continue
				}
				name := fmt.Sprintf("%v", kn)
				indexCols[name] = append(indexCols[name], fmt.Sprintf("%v", cn))
				indexInfo[name] = IndexInfo{
					Name:      name,
					NonUnique: fmt.Sprintf("%v", nu) == "1",
					Type:      fmt.Sprintf("%v", it),
				}
			}
			break
		}
		name := fmt.Sprintf("%v", keyName)
		indexCols[name] = append(indexCols[name], fmt.Sprintf("%v", colName))
		indexInfo[name] = IndexInfo{
			Name:      name,
			NonUnique: fmt.Sprintf("%v", nonUnique) == "1",
			Type:      fmt.Sprintf("%v", indexType),
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

	dbName, err := quoteIdent(input.Database)
	if err != nil {
		return nil, ShowCreateTableOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	tableName, err := quoteIdent(input.Table)
	if err != nil {
		return nil, ShowCreateTableOutput{}, fmt.Errorf("invalid table name: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	query := fmt.Sprintf("SHOW CREATE TABLE %s.%s", dbName, tableName)
	var tbl, createStmt string
	if err := db.QueryRowContext(ctx, query).Scan(&tbl, &createStmt); err != nil {
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

	// Switch database if specified
	if database := strings.TrimSpace(input.Database); database != "" {
		dbName, err := quoteIdent(database)
		if err != nil {
			return nil, ExplainQueryOutput{}, fmt.Errorf("invalid database name: %w", err)
		}
		if _, err := db.ExecContext(ctx, "USE "+dbName); err != nil {
			return nil, ExplainQueryOutput{}, fmt.Errorf("failed to switch database: %w", err)
		}
	}

	explainSQL := "EXPLAIN " + sqlText
	rows, err := db.QueryContext(ctx, explainSQL)
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
			row[col] = normalizeValue(values[i])
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
	rows, err := db.QueryContext(ctx, query, input.Database)
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
	rows, err := db.QueryContext(ctx, query, input.Database)
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
	rows, err := db.QueryContext(ctx, query, input.Database)
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
	rows, err := db.QueryContext(ctx, query, input.Database)
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
	rows, err := db.QueryContext(ctx, query, input.Database, input.Table)
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
		rows, err = db.QueryContext(ctx, query, input.Database)
	} else {
		rows, err = db.QueryContext(ctx, query)
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

	rows, err := db.QueryContext(ctx, query, args...)
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

	rows, err := db.QueryContext(ctx, query, args...)
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
		rows, err = db.QueryContext(ctx, query, input.Pattern)
	} else {
		rows, err = db.QueryContext(ctx, query)
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
		rows, err = db.QueryContext(ctx, query, input.Pattern)
	} else {
		rows, err = db.QueryContext(ctx, query)
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

	return nil, out, nil
}

// ===== main =====

func main() {
	// ---- Load config from env ----
	dsn := strings.TrimSpace(os.Getenv("MYSQL_DSN"))
	if dsn == "" {
		log.Fatalf("config error: MYSQL_DSN env var is required, e.g. user:pass@tcp(127.0.0.1:3306)/dbname?parseTime=true")
	}

	maxRows = getEnvInt("MYSQL_MAX_ROWS", defaultMaxRows)
	qTimeoutSecs := getEnvInt("MYSQL_QUERY_TIMEOUT_SECONDS", defaultQueryTimeoutSecs)
	queryTimeout = time.Duration(qTimeoutSecs) * time.Second

	// Check for extended mode
	extendedMode = os.Getenv("MYSQL_MCP_EXTENDED") == "1"

	// Check for JSON logging
	jsonLogging = os.Getenv("MYSQL_MCP_JSON_LOGS") == "1"

	// Initialize audit logger if path is specified
	auditLogPath := strings.TrimSpace(os.Getenv("MYSQL_MCP_AUDIT_LOG"))
	var err error
	auditLogger, err = NewAuditLogger(auditLogPath)
	if err != nil {
		log.Fatalf("audit log init error: %v", err)
	}
	if auditLogger.enabled {
		defer auditLogger.Close()
	}

	// Connection pool settings
	maxOpenConns := getEnvInt("MYSQL_MAX_OPEN_CONNS", defaultMaxOpenConns)
	maxIdleConns := getEnvInt("MYSQL_MAX_IDLE_CONNS", defaultMaxIdleConns)
	connMaxLifetimeMin := getEnvInt("MYSQL_CONN_MAX_LIFETIME_MINUTES", defaultConnMaxLifetimeM)

	// ---- Init DB ----
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("db init error: %v", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(time.Duration(connMaxLifetimeMin) * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("db init error: %v", err)
	}

	// Log startup configuration
	logInfo("mysql-mcp-server started", map[string]interface{}{
		"maxRows":         maxRows,
		"queryTimeout":    queryTimeout.String(),
		"extendedMode":    extendedMode,
		"jsonLogging":     jsonLogging,
		"auditLogEnabled": auditLogger.enabled,
		"maxOpenConns":    maxOpenConns,
		"maxIdleConns":    maxIdleConns,
		"connMaxLifetime": fmt.Sprintf("%dm", connMaxLifetimeMin),
	})

	// ---- Build MCP server ----
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "mysql-mcp-server",
			Version: "0.1.0",
		},
		nil,
	)

	// Register tools.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_databases",
		Description: "List accessible databases in the configured MySQL server",
	}, toolListDatabases)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tables",
		Description: "List tables in a given database",
	}, toolListTables)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_table",
		Description: "Describe columns of a given table",
	}, toolDescribeTable)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_query",
		Description: "Execute a read-only SQL query (SELECT/SHOW/DESCRIBE/EXPLAIN only)",
	}, toolRunQuery)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ping",
		Description: "Test database connectivity and measure latency",
	}, toolPing)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "server_info",
		Description: "Get MySQL server version, uptime, and configuration details",
	}, toolServerInfo)

	// ---- Register extended tools (MYSQL_MCP_EXTENDED=1) ----
	if extendedMode {
		log.Printf("Registering extended MySQL tools...")

		mcp.AddTool(server, &mcp.Tool{
			Name:        "list_indexes",
			Description: "List indexes on a table",
		}, toolListIndexes)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "show_create_table",
			Description: "Show the CREATE TABLE statement for a table",
		}, toolShowCreateTable)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "explain_query",
			Description: "Get the execution plan for a SELECT query",
		}, toolExplainQuery)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "list_views",
			Description: "List views in a database",
		}, toolListViews)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "list_triggers",
			Description: "List triggers in a database",
		}, toolListTriggers)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "list_procedures",
			Description: "List stored procedures in a database",
		}, toolListProcedures)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "list_functions",
			Description: "List stored functions in a database",
		}, toolListFunctions)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "list_partitions",
			Description: "List partitions of a table",
		}, toolListPartitions)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "database_size",
			Description: "Get size information for databases",
		}, toolDatabaseSize)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "table_size",
			Description: "Get size information for tables",
		}, toolTableSize)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "foreign_keys",
			Description: "List foreign key constraints",
		}, toolForeignKeys)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "list_status",
			Description: "List MySQL server status variables",
		}, toolListStatus)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "list_variables",
			Description: "List MySQL server configuration variables",
		}, toolListVariables)
	}

	// ---- Run over stdio ----
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
