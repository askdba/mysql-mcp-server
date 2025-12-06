// cmd/mysql-mcp-server/main.go
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	defaultHTTPPort         = 9306
)

// Global DB handle shared by all tools (safe for concurrent use).
var (
	db           *sql.DB
	maxRows      int
	queryTimeout time.Duration
	extendedMode bool
	jsonLogging  bool
	auditLogger  *AuditLogger
	connManager  *ConnectionManager
)

// ===== Multi-DSN Connection Manager =====

type ConnectionConfig struct {
	Name        string `json:"name"`
	DSN         string `json:"dsn"`
	Description string `json:"description,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
}

type ConnectionManager struct {
	connections map[string]*sql.DB
	configs     map[string]ConnectionConfig
	activeConn  string
	mu          sync.RWMutex
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*sql.DB),
		configs:     make(map[string]ConnectionConfig),
	}
}

func (cm *ConnectionManager) AddConnection(cfg ConnectionConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conn, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open connection %s: %w", cfg.Name, err)
	}

	// Configure connection pool
	conn.SetMaxOpenConns(defaultMaxOpenConns)
	conn.SetMaxIdleConns(defaultMaxIdleConns)
	conn.SetConnMaxLifetime(time.Duration(defaultConnMaxLifetimeM) * time.Minute)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return fmt.Errorf("failed to ping connection %s: %w", cfg.Name, err)
	}

	cm.connections[cfg.Name] = conn
	cm.configs[cfg.Name] = cfg

	// Set as active if it's the first connection
	if cm.activeConn == "" {
		cm.activeConn = cfg.Name
	}

	return nil
}

func (cm *ConnectionManager) GetActive() (*sql.DB, string) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.connections[cm.activeConn], cm.activeConn
}

func (cm *ConnectionManager) SetActive(name string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.connections[name]; !exists {
		return fmt.Errorf("connection '%s' not found", name)
	}
	cm.activeConn = name
	return nil
}

func (cm *ConnectionManager) List() []ConnectionConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var list []ConnectionConfig
	for _, cfg := range cm.configs {
		// Mask DSN for security
		maskedCfg := cfg
		maskedCfg.DSN = maskDSN(cfg.DSN)
		list = append(list, maskedCfg)
	}
	return list
}

func (cm *ConnectionManager) GetActiveDB() *sql.DB {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.connections[cm.activeConn]
}

func (cm *ConnectionManager) Close() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for _, conn := range cm.connections {
		conn.Close()
	}
}

// getDB returns the active database connection in a thread-safe manner.
// This should be used instead of accessing the global db variable directly
// to avoid data races when connections are switched.
func getDB() *sql.DB {
	if connManager != nil {
		return connManager.GetActiveDB()
	}
	return db
}

// maskDSN hides password in DSN for display
func maskDSN(dsn string) string {
	// DSN format: user:password@tcp(host:port)/database
	atIdx := strings.Index(dsn, "@")
	if atIdx == -1 {
		return dsn
	}
	colonIdx := strings.Index(dsn, ":")
	if colonIdx == -1 || colonIdx > atIdx {
		return dsn
	}
	return dsn[:colonIdx+1] + "****" + dsn[atIdx:]
}

// loadConnectionsFromEnv loads DSN configurations from environment variables
func loadConnectionsFromEnv() ([]ConnectionConfig, error) {
	var configs []ConnectionConfig

	// Check for JSON-based configuration first
	if jsonConfig := os.Getenv("MYSQL_CONNECTIONS"); jsonConfig != "" {
		if err := json.Unmarshal([]byte(jsonConfig), &configs); err != nil {
			return nil, fmt.Errorf("failed to parse MYSQL_CONNECTIONS: %w", err)
		}
		return configs, nil
	}

	// Fall back to numbered DSN environment variables
	// MYSQL_DSN (default), MYSQL_DSN_1, MYSQL_DSN_2, etc.
	if dsn := os.Getenv("MYSQL_DSN"); dsn != "" {
		configs = append(configs, ConnectionConfig{
			Name:        "default",
			DSN:         dsn,
			Description: "Default connection",
		})
	}

	for i := 1; i <= 10; i++ {
		dsnKey := fmt.Sprintf("MYSQL_DSN_%d", i)
		nameKey := fmt.Sprintf("MYSQL_DSN_%d_NAME", i)
		descKey := fmt.Sprintf("MYSQL_DSN_%d_DESC", i)

		dsn := os.Getenv(dsnKey)
		if dsn == "" {
			continue
		}

		name := os.Getenv(nameKey)
		if name == "" {
			name = fmt.Sprintf("connection_%d", i)
		}

		configs = append(configs, ConnectionConfig{
			Name:        name,
			DSN:         dsn,
			Description: os.Getenv(descKey),
		})
	}

	return configs, nil
}

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
	Timestamp  string `json:"timestamp"`
	Tool       string `json:"tool"`
	Database   string `json:"database,omitempty"`
	Query      string `json:"query,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	RowCount   int    `json:"row_count,omitempty"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

type AuditLogger struct {
	file    *os.File
	mu      sync.Mutex
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

// ===== Multi-DSN Tool Types =====

type ListConnectionsInput struct{}

type ConnectionInfo struct {
	Name        string `json:"name" jsonschema:"connection name"`
	DSN         string `json:"dsn" jsonschema:"masked DSN (password hidden)"`
	Description string `json:"description,omitempty" jsonschema:"connection description"`
	Active      bool   `json:"active" jsonschema:"true if this is the active connection"`
}

type ListConnectionsOutput struct {
	Connections []ConnectionInfo `json:"connections" jsonschema:"list of available connections"`
	Active      string           `json:"active" jsonschema:"name of the currently active connection"`
}

type UseConnectionInput struct {
	Name string `json:"name" jsonschema:"name of the connection to switch to"`
}

type UseConnectionOutput struct {
	Success  bool   `json:"success" jsonschema:"true if switch was successful"`
	Active   string `json:"active" jsonschema:"name of the now-active connection"`
	Message  string `json:"message" jsonschema:"status message"`
	Database string `json:"database,omitempty" jsonschema:"current database of the connection"`
}

// ===== Vector Tool Types (MySQL 9.0+) =====

type VectorSearchInput struct {
	Database     string    `json:"database" jsonschema:"database name"`
	Table        string    `json:"table" jsonschema:"table name containing vector column"`
	Column       string    `json:"column" jsonschema:"name of the vector column"`
	Query        []float64 `json:"query" jsonschema:"query vector for similarity search"`
	Limit        int       `json:"limit,omitempty" jsonschema:"max results to return (default: 10)"`
	Select       string    `json:"select,omitempty" jsonschema:"additional columns to select (comma-separated)"`
	Where        string    `json:"where,omitempty" jsonschema:"additional WHERE conditions"`
	DistanceFunc string    `json:"distance_func,omitempty" jsonschema:"distance function: cosine, euclidean, dot (default: cosine)"`
}

type VectorSearchResult struct {
	Distance float64                `json:"distance" jsonschema:"distance/similarity score"`
	Data     map[string]interface{} `json:"data" jsonschema:"row data"`
}

type VectorSearchOutput struct {
	Results []VectorSearchResult `json:"results" jsonschema:"search results ordered by similarity"`
	Count   int                  `json:"count" jsonschema:"number of results"`
}

type VectorInfoInput struct {
	Database string `json:"database" jsonschema:"database name"`
	Table    string `json:"table,omitempty" jsonschema:"table name (optional, lists all if empty)"`
}

type VectorColumnInfo struct {
	Table      string `json:"table" jsonschema:"table name"`
	Column     string `json:"column" jsonschema:"column name"`
	Dimensions int    `json:"dimensions" jsonschema:"vector dimensions"`
	IndexName  string `json:"index_name,omitempty" jsonschema:"vector index name if exists"`
	IndexType  string `json:"index_type,omitempty" jsonschema:"vector index type"`
}

type VectorInfoOutput struct {
	Columns       []VectorColumnInfo `json:"columns" jsonschema:"vector columns found"`
	VectorSupport bool               `json:"vector_support" jsonschema:"true if MySQL supports VECTOR type"`
	MySQLVersion  string             `json:"mysql_version" jsonschema:"MySQL version"`
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

// validateSelectColumns validates and quotes column names in a SELECT list.
// Accepts: "col1, col2, col3" or "col1 AS alias, col2"
// Returns quoted column string or error if invalid.
func validateSelectColumns(selectStr string) (string, error) {
	if selectStr == "" {
		return "*", nil
	}

	// Block dangerous patterns in select
	dangerousPatterns := []string{
		"(", ")", ";", "--", "/*", "*/", "@@", "SLEEP", "BENCHMARK",
		"LOAD_FILE", "INTO", "OUTFILE", "DUMPFILE", "UNION", "INFORMATION_SCHEMA",
	}
	upperSelect := strings.ToUpper(selectStr)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(upperSelect, pattern) {
			return "", fmt.Errorf("select contains forbidden pattern: %s", pattern)
		}
	}

	// Split by comma and validate each column
	parts := strings.Split(selectStr, ",")
	var quotedCols []string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Handle "column AS alias" syntax
		var colName, alias string
		if idx := strings.Index(strings.ToUpper(part), " AS "); idx != -1 {
			colName = strings.TrimSpace(part[:idx])
			alias = strings.TrimSpace(part[idx+4:])
		} else {
			colName = part
		}

		// Allow * as a special case
		if colName == "*" {
			quotedCols = append(quotedCols, "*")
			continue
		}

		// Handle table.column syntax
		if strings.Contains(colName, ".") {
			dotParts := strings.Split(colName, ".")
			if len(dotParts) != 2 {
				return "", fmt.Errorf("invalid column reference: %s", colName)
			}
			tablePart, err := quoteIdent(strings.TrimSpace(dotParts[0]))
			if err != nil {
				return "", fmt.Errorf("invalid table in column reference: %w", err)
			}
			colPart, err := quoteIdent(strings.TrimSpace(dotParts[1]))
			if err != nil {
				return "", fmt.Errorf("invalid column in reference: %w", err)
			}
			colName = tablePart + "." + colPart
		} else {
			quoted, err := quoteIdent(colName)
			if err != nil {
				return "", fmt.Errorf("invalid column name: %w", err)
			}
			colName = quoted
		}

		// Quote alias if present
		if alias != "" {
			quotedAlias, err := quoteIdent(alias)
			if err != nil {
				return "", fmt.Errorf("invalid alias: %w", err)
			}
			quotedCols = append(quotedCols, colName+" AS "+quotedAlias)
		} else {
			quotedCols = append(quotedCols, colName)
		}
	}

	if len(quotedCols) == 0 {
		return "*", nil
	}

	return strings.Join(quotedCols, ", "), nil
}

// validateWhereClause checks a WHERE clause for SQL injection attempts.
// This is a defense-in-depth measure - the primary protection is the read-only
// MySQL user, but we still block obvious injection patterns.
func validateWhereClause(where string) error {
	if where == "" {
		return nil
	}

	// Block dangerous patterns that have no legitimate use in WHERE clauses
	dangerousPatterns := []struct {
		pattern *regexp.Regexp
		reason  string
	}{
		{regexp.MustCompile(`(?i);\s*`), "semicolon (multi-statement)"},
		{regexp.MustCompile(`(?i)--`), "SQL comment"},
		{regexp.MustCompile(`(?i)/\*`), "SQL block comment"},
		{regexp.MustCompile(`(?i)\bUNION\b`), "UNION keyword"},
		{regexp.MustCompile(`(?i)\bINTO\b`), "INTO keyword"},
		{regexp.MustCompile(`(?i)\bLOAD_FILE\s*\(`), "LOAD_FILE function"},
		{regexp.MustCompile(`(?i)\bSLEEP\s*\(`), "SLEEP function"},
		{regexp.MustCompile(`(?i)\bBENCHMARK\s*\(`), "BENCHMARK function"},
		{regexp.MustCompile(`(?i)\bGET_LOCK\s*\(`), "GET_LOCK function"},
		{regexp.MustCompile(`(?i)\bRELEASE_LOCK\s*\(`), "RELEASE_LOCK function"},
		{regexp.MustCompile(`(?i)@@`), "system variable access"},
		{regexp.MustCompile(`(?i)\bINFORMATION_SCHEMA\b`), "INFORMATION_SCHEMA access"},
		{regexp.MustCompile(`(?i)\bPERFORMANCE_SCHEMA\b`), "PERFORMANCE_SCHEMA access"},
		{regexp.MustCompile(`(?i)\bMYSQL\s*\.\b`), "mysql system database access"},
		{regexp.MustCompile(`(?i)\bSYS\s*\.\b`), "sys database access"},
		{regexp.MustCompile(`(?i)\bEXEC\s*\(`), "EXEC function"},
		{regexp.MustCompile(`(?i)\bSHUTDOWN\b`), "SHUTDOWN command"},
		{regexp.MustCompile(`(?i)0x[0-9a-fA-F]{10,}`), "long hex string (possible injection)"},
	}

	for _, dp := range dangerousPatterns {
		if dp.pattern.MatchString(where) {
			return fmt.Errorf("forbidden pattern detected: %s", dp.reason)
		}
	}

	// Check for balanced parentheses (basic sanity check)
	openParens := strings.Count(where, "(")
	closeParens := strings.Count(where, ")")
	if openParens != closeParens {
		return fmt.Errorf("unbalanced parentheses in WHERE clause")
	}

	// Limit length to prevent abuse
	if len(where) > 1000 {
		return fmt.Errorf("WHERE clause too long (max 1000 characters)")
	}

	return nil
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

	// Note: We intentionally do NOT block queries containing INSERT/UPDATE/DELETE
	// as standalone words because:
	// 1. They can be valid column/table names (e.g., SELECT update_time FROM logs)
	// 2. The blocked patterns regex already catches actual DML statements at the start
	// 3. Subqueries cannot perform DML in MySQL SELECT statements anyway

	return nil
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

	dbName, err := quoteIdent(input.Database)
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

	rows, err := getDB().QueryContext(ctx, query)
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
	// Use a single connection to ensure USE and query run on the same connection
	database := strings.TrimSpace(input.Database)
	var rows *sql.Rows
	var err error

	if database != "" {
		var dbName string
		dbName, err = quoteIdent(database)
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
	err := getDB().PingContext(ctx)
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

	// Note: No need to update global db - getDB() safely retrieves the active connection

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

	dbName, err := quoteIdent(input.Database)
	if err != nil {
		return nil, VectorSearchOutput{}, fmt.Errorf("invalid database name: %w", err)
	}
	tableName, err := quoteIdent(input.Table)
	if err != nil {
		return nil, VectorSearchOutput{}, fmt.Errorf("invalid table name: %w", err)
	}
	colName, err := quoteIdent(input.Column)
	if err != nil {
		return nil, VectorSearchOutput{}, fmt.Errorf("invalid column name: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// Set default limit, cap to maxRows for safety
	limit := input.Limit
	if limit <= 0 {
		limit = 10 // sensible default
	}
	if limit > maxRows {
		limit = maxRows // cap to configured maximum
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
		// Validate and quote each column in the select list
		validatedCols, err := validateSelectColumns(input.Select)
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
		if err := validateWhereClause(input.Where); err != nil {
			return nil, VectorSearchOutput{}, fmt.Errorf("invalid where clause: %w", err)
		}
		query += " WHERE " + input.Where
	}

	query += fmt.Sprintf(" ORDER BY _distance ASC LIMIT %d", limit)

	rows, err := getDB().QueryContext(ctx, query)
	if err != nil {
		// Check if it's a vector support error
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
				result.Data[col] = normalizeValue(values[i])
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
		if matches := regexp.MustCompile(`vector\((\d+)\)`).FindStringSubmatch(colType); len(matches) > 1 {
			info.Dimensions, _ = strconv.Atoi(matches[1])
		}

		// Check for vector index
		indexQuery := fmt.Sprintf(`
			SELECT INDEX_NAME, INDEX_TYPE 
			FROM information_schema.STATISTICS 
			WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ?
		`)
		var indexName, indexType sql.NullString
		getDB().QueryRowContext(ctx, indexQuery, input.Database, tableName, colName).Scan(&indexName, &indexType)
		info.IndexName = indexName.String
		info.IndexType = indexType.String

		out.Columns = append(out.Columns, info)
	}

	return nil, out, nil
}

// buildVectorString converts a float64 slice to MySQL vector string format
func buildVectorString(vec []float64) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = fmt.Sprintf("%f", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// isVectorSupported checks if MySQL version supports VECTOR type (9.0+)
func isVectorSupported(version string) bool {
	// Extract major version
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

	// Column positions (standard SHOW INDEX output):
	// 0:Table, 1:Non_unique, 2:Key_name, 3:Seq_in_index, 4:Column_name,
	// 5:Collation, 6:Cardinality, 7:Sub_part, 8:Packed, 9:Null, 10:Index_type
	// Newer versions may have additional columns (Comment, Index_comment, Visible, Expression)

	// Validate column count before processing rows
	// We need at least columns 0-10 (11 columns) for the standard SHOW INDEX output
	if colCount < 11 {
		return nil, ListIndexesOutput{}, fmt.Errorf("unexpected SHOW INDEX output: got %d columns, expected at least 11", colCount)
	}

	// Group columns by index name
	indexCols := make(map[string][]string)
	indexInfo := make(map[string]IndexInfo)

	for rows.Next() {
		// Create slice to hold all columns dynamically
		values := make([]interface{}, colCount)
		ptrs := make([]interface{}, colCount)
		for i := range values {
			ptrs[i] = &values[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			continue
		}

		// Extract the values we need (positions 1, 2, 4, 10)
		keyName := fmt.Sprintf("%v", normalizeValue(values[2]))
		colName := fmt.Sprintf("%v", normalizeValue(values[4]))
		nonUnique := fmt.Sprintf("%v", normalizeValue(values[1])) == "1"
		indexType := fmt.Sprintf("%v", normalizeValue(values[10]))

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

	// Switch database if specified
	// Use a single connection to ensure USE affects the query
	database := strings.TrimSpace(input.Database)
	explainSQL := "EXPLAIN " + sqlText
	var rows *sql.Rows
	var err error

	if database != "" {
		var dbName string
		dbName, err = quoteIdent(database)
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

	return nil, out, nil
}

// ===== HTTP REST API Server =====

// APIResponse is the standard JSON response structure
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeSuccess writes a successful JSON response
func writeSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: data})
}

// writeError writes an error JSON response
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, APIResponse{Success: false, Error: message})
}

// httpListDatabases handles GET /api/databases
func httpListDatabases(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	ctx := context.Background()
	_, out, err := toolListDatabases(ctx, nil, ListDatabasesInput{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpListTables handles GET /api/tables?database=xxx
func httpListTables(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	if database == "" {
		writeError(w, http.StatusBadRequest, "database parameter is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolListTables(ctx, nil, ListTablesInput{Database: database})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpDescribeTable handles GET /api/describe?database=xxx&table=yyy
func httpDescribeTable(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	if database == "" || table == "" {
		writeError(w, http.StatusBadRequest, "database and table parameters are required")
		return
	}
	ctx := context.Background()
	_, out, err := toolDescribeTable(ctx, nil, DescribeTableInput{Database: database, Table: table})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpRunQuery handles POST /api/query with JSON body {"sql": "...", "database": "...", "max_rows": N}
func httpRunQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "POST method required")
		return
	}
	var input RunQueryInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if input.SQL == "" {
		writeError(w, http.StatusBadRequest, "sql field is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolRunQuery(ctx, nil, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpPing handles GET /api/ping
func httpPing(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	ctx := context.Background()
	_, out, err := toolPing(ctx, nil, PingInput{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpServerInfo handles GET /api/server-info
func httpServerInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	ctx := context.Background()
	_, out, err := toolServerInfo(ctx, nil, ServerInfoInput{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpListConnections handles GET /api/connections
func httpListConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	ctx := context.Background()
	_, out, err := toolListConnections(ctx, nil, ListConnectionsInput{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpUseConnection handles POST /api/connections/use with JSON body {"name": "..."}
func httpUseConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "POST method required")
		return
	}
	var input UseConnectionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if input.Name == "" {
		writeError(w, http.StatusBadRequest, "name field is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolUseConnection(ctx, nil, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpListIndexes handles GET /api/indexes?database=xxx&table=yyy
func httpListIndexes(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	if database == "" || table == "" {
		writeError(w, http.StatusBadRequest, "database and table parameters are required")
		return
	}
	ctx := context.Background()
	_, out, err := toolListIndexes(ctx, nil, ListIndexesInput{Database: database, Table: table})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpShowCreateTable handles GET /api/create-table?database=xxx&table=yyy
func httpShowCreateTable(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	if database == "" || table == "" {
		writeError(w, http.StatusBadRequest, "database and table parameters are required")
		return
	}
	ctx := context.Background()
	_, out, err := toolShowCreateTable(ctx, nil, ShowCreateTableInput{Database: database, Table: table})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpExplainQuery handles POST /api/explain with JSON body {"sql": "...", "database": "..."}
func httpExplainQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "POST method required")
		return
	}
	var input ExplainQueryInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if input.SQL == "" {
		writeError(w, http.StatusBadRequest, "sql field is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolExplainQuery(ctx, nil, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpListViews handles GET /api/views?database=xxx
func httpListViews(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	if database == "" {
		writeError(w, http.StatusBadRequest, "database parameter is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolListViews(ctx, nil, ListViewsInput{Database: database})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpListTriggers handles GET /api/triggers?database=xxx
func httpListTriggers(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	if database == "" {
		writeError(w, http.StatusBadRequest, "database parameter is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolListTriggers(ctx, nil, ListTriggersInput{Database: database})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpListProcedures handles GET /api/procedures?database=xxx
func httpListProcedures(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	if database == "" {
		writeError(w, http.StatusBadRequest, "database parameter is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolListProcedures(ctx, nil, ListProceduresInput{Database: database})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpListFunctions handles GET /api/functions?database=xxx
func httpListFunctions(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	if database == "" {
		writeError(w, http.StatusBadRequest, "database parameter is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolListFunctions(ctx, nil, ListFunctionsInput{Database: database})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpDatabaseSize handles GET /api/size/database?database=xxx (optional)
func httpDatabaseSize(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	ctx := context.Background()
	_, out, err := toolDatabaseSize(ctx, nil, DatabaseSizeInput{Database: database})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpTableSize handles GET /api/size/tables?database=xxx
func httpTableSize(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	if database == "" {
		writeError(w, http.StatusBadRequest, "database parameter is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolTableSize(ctx, nil, TableSizeInput{Database: database})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpForeignKeys handles GET /api/foreign-keys?database=xxx&table=yyy (table optional)
func httpForeignKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	if database == "" {
		writeError(w, http.StatusBadRequest, "database parameter is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolForeignKeys(ctx, nil, ForeignKeysInput{Database: database, Table: table})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpListStatus handles GET /api/status?pattern=xxx (pattern optional)
func httpListStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	pattern := r.URL.Query().Get("pattern")
	ctx := context.Background()
	_, out, err := toolListStatus(ctx, nil, ListStatusInput{Pattern: pattern})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpListVariables handles GET /api/variables?pattern=xxx (pattern optional)
func httpListVariables(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	pattern := r.URL.Query().Get("pattern")
	ctx := context.Background()
	_, out, err := toolListVariables(ctx, nil, ListVariablesInput{Pattern: pattern})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpVectorSearch handles POST /api/vector/search
func httpVectorSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "POST method required")
		return
	}
	var input VectorSearchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	ctx := context.Background()
	_, out, err := toolVectorSearch(ctx, nil, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpVectorInfo handles GET /api/vector/info?database=xxx
func httpVectorInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	database := r.URL.Query().Get("database")
	if database == "" {
		writeError(w, http.StatusBadRequest, "database parameter is required")
		return
	}
	ctx := context.Background()
	_, out, err := toolVectorInfo(ctx, nil, VectorInfoInput{Database: database})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSuccess(w, out)
}

// httpHealth handles GET /health
func httpHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeSuccess(w, map[string]interface{}{
		"status":  "healthy",
		"service": "mysql-mcp-server",
	})
}

// httpAPIIndex handles GET /api
func httpAPIIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	endpoints := map[string]interface{}{
		"service": "mysql-mcp-server REST API",
		"version": "1.1.0",
		"endpoints": map[string]string{
			"GET  /health":              "Health check",
			"GET  /api":                 "API index (this page)",
			"GET  /api/databases":       "List databases",
			"GET  /api/tables":          "List tables (requires ?database=)",
			"GET  /api/describe":        "Describe table (requires ?database=&table=)",
			"POST /api/query":           "Run SQL query (body: {sql, database?, max_rows?})",
			"GET  /api/ping":            "Ping database",
			"GET  /api/server-info":     "Get server info",
			"GET  /api/connections":     "List connections",
			"POST /api/connections/use": "Switch connection (body: {name})",
			"GET  /api/indexes":         "List indexes (requires ?database=&table=) [extended]",
			"GET  /api/create-table":    "Show CREATE TABLE (requires ?database=&table=) [extended]",
			"POST /api/explain":         "Explain query (body: {sql, database?}) [extended]",
			"GET  /api/views":           "List views (requires ?database=) [extended]",
			"GET  /api/triggers":        "List triggers (requires ?database=) [extended]",
			"GET  /api/procedures":      "List procedures (requires ?database=) [extended]",
			"GET  /api/functions":       "List functions (requires ?database=) [extended]",
			"GET  /api/size/database":   "Database size (optional ?database=) [extended]",
			"GET  /api/size/tables":     "Table sizes (requires ?database=) [extended]",
			"GET  /api/foreign-keys":    "Foreign keys (requires ?database=, optional &table=) [extended]",
			"GET  /api/status":          "Server status (optional ?pattern=) [extended]",
			"GET  /api/variables":       "Server variables (optional ?pattern=) [extended]",
			"POST /api/vector/search":   "Vector search (body: {...}) [vector]",
			"GET  /api/vector/info":     "Vector info (requires ?database=) [vector]",
		},
		"modes": map[string]bool{
			"extended": extendedMode,
			"vector":   os.Getenv("MYSQL_MCP_VECTOR") == "1",
		},
	}
	writeSuccess(w, endpoints)
}

// startHTTPServer starts the REST API server
func startHTTPServer(port int, vectorMode bool) {
	mux := http.NewServeMux()

	// Health and index
	mux.HandleFunc("/health", httpHealth)
	mux.HandleFunc("/api", httpAPIIndex)
	mux.HandleFunc("/api/", httpAPIIndex)

	// Core endpoints
	mux.HandleFunc("/api/databases", httpListDatabases)
	mux.HandleFunc("/api/tables", httpListTables)
	mux.HandleFunc("/api/describe", httpDescribeTable)
	mux.HandleFunc("/api/query", httpRunQuery)
	mux.HandleFunc("/api/ping", httpPing)
	mux.HandleFunc("/api/server-info", httpServerInfo)
	mux.HandleFunc("/api/connections", httpListConnections)
	mux.HandleFunc("/api/connections/use", httpUseConnection)

	// Extended endpoints (always registered, but handlers check extendedMode)
	mux.HandleFunc("/api/indexes", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpListIndexes(w, r)
	})
	mux.HandleFunc("/api/create-table", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpShowCreateTable(w, r)
	})
	mux.HandleFunc("/api/explain", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpExplainQuery(w, r)
	})
	mux.HandleFunc("/api/views", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpListViews(w, r)
	})
	mux.HandleFunc("/api/triggers", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpListTriggers(w, r)
	})
	mux.HandleFunc("/api/procedures", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpListProcedures(w, r)
	})
	mux.HandleFunc("/api/functions", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpListFunctions(w, r)
	})
	mux.HandleFunc("/api/size/database", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpDatabaseSize(w, r)
	})
	mux.HandleFunc("/api/size/tables", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpTableSize(w, r)
	})
	mux.HandleFunc("/api/foreign-keys", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpForeignKeys(w, r)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpListStatus(w, r)
	})
	mux.HandleFunc("/api/variables", func(w http.ResponseWriter, r *http.Request) {
		if !extendedMode {
			writeError(w, http.StatusNotFound, "extended mode not enabled (set MYSQL_MCP_EXTENDED=1)")
			return
		}
		httpListVariables(w, r)
	})

	// Vector endpoints
	mux.HandleFunc("/api/vector/search", func(w http.ResponseWriter, r *http.Request) {
		if !vectorMode {
			writeError(w, http.StatusNotFound, "vector mode not enabled (set MYSQL_MCP_VECTOR=1)")
			return
		}
		httpVectorSearch(w, r)
	})
	mux.HandleFunc("/api/vector/info", func(w http.ResponseWriter, r *http.Request) {
		if !vectorMode {
			writeError(w, http.StatusNotFound, "vector mode not enabled (set MYSQL_MCP_VECTOR=1)")
			return
		}
		httpVectorInfo(w, r)
	})

	addr := fmt.Sprintf(":%d", port)
	logInfo("HTTP REST API server starting", map[string]interface{}{
		"port":         port,
		"address":      "http://localhost" + addr,
		"extendedMode": extendedMode,
		"vectorMode":   vectorMode,
	})

	log.Printf("REST API endpoints available at http://localhost:%d/api", port)
	log.Printf("Health check at http://localhost:%d/health", port)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("HTTP server error: %v", err)
	}
}

// ===== main =====

func main() {
	// ---- Load config from env ----
	maxRows = getEnvInt("MYSQL_MAX_ROWS", defaultMaxRows)
	qTimeoutSecs := getEnvInt("MYSQL_QUERY_TIMEOUT_SECONDS", defaultQueryTimeoutSecs)
	queryTimeout = time.Duration(qTimeoutSecs) * time.Second

	// Check for extended mode
	extendedMode = os.Getenv("MYSQL_MCP_EXTENDED") == "1"

	// Check for JSON logging
	jsonLogging = os.Getenv("MYSQL_MCP_JSON_LOGS") == "1"

	// Check for vector tools (MySQL 9.0+)
	vectorMode := os.Getenv("MYSQL_MCP_VECTOR") == "1"

	// Check for HTTP REST API mode
	httpMode := os.Getenv("MYSQL_MCP_HTTP") == "1"
	httpPort := getEnvInt("MYSQL_HTTP_PORT", defaultHTTPPort)

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

	// ---- Initialize Connection Manager ----
	connManager = NewConnectionManager()
	defer connManager.Close()

	// Load connections from environment
	connConfigs, err := loadConnectionsFromEnv()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	if len(connConfigs) == 0 {
		log.Fatalf("config error: no MySQL connections configured. Set MYSQL_DSN or MYSQL_CONNECTIONS")
	}

	// Add all connections to the manager
	for _, cfg := range connConfigs {
		if err := connManager.AddConnection(cfg); err != nil {
			log.Printf("Warning: failed to add connection '%s': %v", cfg.Name, err)
		} else {
			logInfo("connection added", map[string]interface{}{
				"name": cfg.Name,
				"dsn":  maskDSN(cfg.DSN),
			})
		}
	}

	// Set the global db to the active connection
	db = connManager.GetActiveDB()
	if db == nil {
		log.Fatalf("config error: no valid MySQL connections available")
	}

	_, activeName := connManager.GetActive()

	// Log startup configuration
	logInfo("mysql-mcp-server started", map[string]interface{}{
		"maxRows":          maxRows,
		"queryTimeout":     queryTimeout.String(),
		"extendedMode":     extendedMode,
		"vectorMode":       vectorMode,
		"httpMode":         httpMode,
		"httpPort":         httpPort,
		"jsonLogging":      jsonLogging,
		"auditLogEnabled":  auditLogger.enabled,
		"connections":      len(connConfigs),
		"activeConnection": activeName,
	})

	// If HTTP mode is enabled, start REST API server instead of MCP
	if httpMode {
		startHTTPServer(httpPort, vectorMode)
		return
	}

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

	// ---- Register multi-DSN tools ----
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_connections",
		Description: "List all configured MySQL connections and show which is active",
	}, toolListConnections)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "use_connection",
		Description: "Switch to a different MySQL connection by name",
	}, toolUseConnection)

	// ---- Register vector tools (MYSQL_MCP_VECTOR=1) ----
	if vectorMode {
		logInfo("Registering MySQL vector tools (MySQL 9.0+ required)...", nil)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "vector_search",
			Description: "Perform similarity search on vector columns (MySQL 9.0+ required)",
		}, toolVectorSearch)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "vector_info",
			Description: "List vector columns and their properties in a database",
		}, toolVectorInfo)
	}

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
