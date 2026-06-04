// cmd/mysql-mcp-server/types.go
package main

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
	Name    string `json:"name" jsonschema:"table name"`
	Engine  string `json:"engine,omitempty" jsonschema:"storage engine (e.g. InnoDB, MyISAM)"`
	Rows    *int64 `json:"rows,omitempty" jsonschema:"estimated number of rows"`
	Comment string `json:"comment,omitempty" jsonschema:"table comment"`
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
	SQL      string `json:"sql" jsonschema:"SQL query to execute; must start with SELECT, SHOW, DESCRIBE, or EXPLAIN. For complex queries, use the query_advisor prompt which runs EXPLAIN and surfaces optimization guidance from the registered MCP resources."`
	MaxRows  *int   `json:"max_rows,omitempty" jsonschema:"optional row limit overriding the default max rows"`
	Offset   *int   `json:"offset,omitempty" jsonschema:"optional zero-based row offset for SELECT/UNION pagination; do not add LIMIT to the SQL when using this"`
	Database string `json:"database,omitempty" jsonschema:"optional database name to USE before running the query"`
}

type QueryResult struct {
	Columns    []string        `json:"columns" jsonschema:"column names"`
	Rows       [][]interface{} `json:"rows" jsonschema:"rows of values"`
	Truncated  bool            `json:"truncated,omitempty" jsonschema:"true if more rows existed beyond the row limit (not set when the result size exactly equals the limit)"`
	HasMore    bool            `json:"has_more,omitempty" jsonschema:"true when offset pagination indicates another page may exist"`
	NextOffset *int            `json:"next_offset,omitempty" jsonschema:"pass as offset to retrieve the next page when has_more is true"`
	Warning    string          `json:"warning,omitempty" jsonschema:"performance or usage warning, if any"`
}

type PingInput struct{}

type PingOutput struct {
	Success   bool   `json:"success" jsonschema:"true if the database is reachable"`
	LatencyMs int64  `json:"latency_ms" jsonschema:"round-trip latency in milliseconds"`
	Message   string `json:"message" jsonschema:"status message"`
}

type ServerInfoInput struct {
	Detailed bool `json:"detailed,omitempty" jsonschema:"when true, include health metrics (threads_running, slow_queries, buffer pool hit rate, ping latency)"`
}

// ServerHealthSnapshot is returned when server_info is called with detailed=true.
type ServerHealthSnapshot struct {
	PingLatencyMs          int64    `json:"ping_latency_ms,omitempty" jsonschema:"round-trip ping in milliseconds"`
	ThreadsRunning         int      `json:"threads_running,omitempty" jsonschema:"Threads_running status"`
	SlowQueries            int64    `json:"slow_queries,omitempty" jsonschema:"Slow_queries counter since startup"`
	Questions              int64    `json:"questions,omitempty" jsonschema:"Questions counter since startup"`
	BufferPoolHitPercent   *float64 `json:"buffer_pool_hit_percent,omitempty" jsonschema:"InnoDB buffer pool hit ratio 0-100 when available"`
	BufferPoolReadRequests *int64   `json:"buffer_pool_read_requests,omitempty"`
	BufferPoolReads        *int64   `json:"buffer_pool_reads,omitempty"`
}

// ServerTokenSnapshot summarizes token metrics when MYSQL_MCP_TOKEN_TRACKING=1.
type ServerTokenSnapshot struct {
	ToolCalls         int `json:"tool_calls,omitempty"`
	TotalInputTokens  int `json:"total_input_tokens,omitempty"`
	TotalOutputTokens int `json:"total_output_tokens,omitempty"`
	TotalTokens       int `json:"total_tokens,omitempty"`
	MetricsUptimeSec  int `json:"metrics_uptime_seconds,omitempty"`
}

type ServerInfoOutput struct {
	Version          string                `json:"version" jsonschema:"MySQL server version"`
	ServerEngine     string                `json:"server_engine" jsonschema:"Server engine (mysql or mariadb)"`
	VersionComment   string                `json:"version_comment" jsonschema:"MySQL version comment (e.g., MySQL Community Server)"`
	Uptime           int64                 `json:"uptime_seconds" jsonschema:"server uptime in seconds"`
	CurrentDatabase  string                `json:"current_database" jsonschema:"currently selected database, if any"`
	CurrentUser      string                `json:"current_user" jsonschema:"current MySQL user"`
	CharacterSet     string                `json:"character_set" jsonschema:"server character set"`
	Collation        string                `json:"collation" jsonschema:"server collation"`
	MaxConnections   int                   `json:"max_connections" jsonschema:"maximum allowed connections"`
	ThreadsConnected int                   `json:"threads_connected" jsonschema:"current number of connected threads"`
	Health           *ServerHealthSnapshot `json:"health,omitempty" jsonschema:"present when detailed=true"`
	TokenMetrics     *ServerTokenSnapshot  `json:"token_metrics,omitempty" jsonschema:"present when token tracking is enabled"`
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

// ===== Diagnostic / admin tools (extended, gated by config) =====

type ProcessListInput struct{}

type ProcessRow struct {
	ID      int64  `json:"id" jsonschema:"connection / thread id"`
	User    string `json:"user" jsonschema:"user@host"`
	Host    string `json:"host" jsonschema:"client host"`
	DB      string `json:"db,omitempty" jsonschema:"default database"`
	Command string `json:"command" jsonschema:"thread command"`
	Time    int    `json:"time" jsonschema:"seconds in current state"`
	State   string `json:"state,omitempty"`
	Info    string `json:"info,omitempty" jsonschema:"statement (truncated)"`
}

type ProcessListOutput struct {
	Processes []ProcessRow `json:"processes" jsonschema:"active server threads"`
	Note      string       `json:"note,omitempty" jsonschema:"privilege or compatibility note"`
}

type KillQueryInput struct {
	ID int64 `json:"id" jsonschema:"connection/thread id from process_list (KILL QUERY target)"`
}

type KillQueryOutput struct {
	OK      bool   `json:"ok" jsonschema:"true if KILL QUERY executed"`
	Message string `json:"message" jsonschema:"result message"`
}

type ReadAuditLogInput struct {
	Lines int `json:"lines,omitempty" jsonschema:"max lines from end of file (default 50, max 500)"`
}

type ReadAuditLogOutput struct {
	Path      string   `json:"path" jsonschema:"audit file path"`
	Lines     []string `json:"lines" jsonschema:"recent log lines (JSON entries)"`
	Truncated bool     `json:"truncated,omitempty" jsonschema:"true if byte limit hit before reading full tail"`
}

type SlowQueryLogInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"max rows from mysql.slow_log (default 20, max 200)"`
}

type SlowQueryLogRow struct {
	StartTime    string `json:"start_time,omitempty"`
	UserHost     string `json:"user_host,omitempty"`
	QueryTime    string `json:"query_time,omitempty"`
	LockTime     string `json:"lock_time,omitempty"`
	RowsSent     int64  `json:"rows_sent,omitempty"`
	RowsExamined int64  `json:"rows_examined,omitempty"`
	Database     string `json:"db,omitempty"`
	LastInsertID int64  `json:"last_insert_id,omitempty"`
	InsertID     int64  `json:"insert_id,omitempty"`
	Query        string `json:"query,omitempty" jsonschema:"truncated SQL text"`
}

type SlowQueryLogOutput struct {
	Mode        string            `json:"mode" jsonschema:"table_rows, file, or disabled"`
	Message     string            `json:"message,omitempty"`
	Settings    map[string]string `json:"settings,omitempty" jsonschema:"relevant @@variables when not table mode"`
	Rows        []SlowQueryLogRow `json:"rows,omitempty"`
	SlowQueries int64             `json:"slow_queries_status,omitempty" jsonschema:"Slow_queries counter"`
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
	Format   string `json:"format,omitempty" jsonschema:"output format: traditional, json, tree (default: json)"`
}

type ExplainQueryOutput struct {
	Plan     interface{} `json:"plan" jsonschema:"query execution plan; shape depends on format: for format=json (default) this is typically UnifiedExplainPlan, or a raw JSON string if normalization fails; for format=traditional this is an array of row maps from EXPLAIN; for other formats (e.g. tree) this is an array of row maps from the server"`
	Warnings []string    `json:"warnings,omitempty" jsonschema:"actionable optimization hints derived from a traditional tabular EXPLAIN; populated only when format=traditional, omitted otherwise"`
}

// OpCostInfo holds per-operation cost details extracted from EXPLAIN FORMAT=JSON.
type OpCostInfo struct {
	ReadCost        float64 `json:"read_cost,omitempty" jsonschema:"estimated cost to read data for this operation"`
	EvalCost        float64 `json:"eval_cost,omitempty" jsonschema:"estimated cost to evaluate conditions for this operation"`
	PrefixCost      float64 `json:"prefix_cost,omitempty" jsonschema:"cumulative estimated cost up to and including this operation"`
	DataReadPerJoin string  `json:"data_read_per_join,omitempty" jsonschema:"estimated amount of data read per join for this operation"`
}

// UnifiedOp represents a single operation (table access) in a normalized EXPLAIN plan.
type UnifiedOp struct {
	TableName         string     `json:"table_name,omitempty" jsonschema:"table accessed by this operation"`
	AccessType        string     `json:"access_type,omitempty" jsonschema:"join or access method (e.g. ALL, ref, range, index)"`
	Key               string     `json:"key,omitempty" jsonschema:"index chosen for this operation"`
	KeyLength         string     `json:"key_length,omitempty" jsonschema:"length of the index key used"`
	RowsExamined      int64      `json:"rows_examined,omitempty" jsonschema:"estimated number of rows examined"`
	Filtered          float64    `json:"filtered,omitempty" jsonschema:"estimated percentage of rows that pass the table filter (matches EXPLAIN JSON key filtered)"`
	Message           string     `json:"message,omitempty" jsonschema:"additional optimizer or execution details for this operation"`
	AttachedCondition string     `json:"attached_condition,omitempty" jsonschema:"condition attached to this operation"`
	PossibleKeys      []string   `json:"possible_keys,omitempty" jsonschema:"indexes considered by the optimizer"`
	CostInfo          OpCostInfo `json:"cost_info,omitempty" jsonschema:"per-operation cost details"`
}

// UnifiedExplainPlan is a normalized representation of an EXPLAIN FORMAT=JSON result
// that works across MySQL and MariaDB variants.
type UnifiedExplainPlan struct {
	QueryCost  float64     `json:"query_cost,omitempty" jsonschema:"total estimated query cost"`
	Operations []UnifiedOp `json:"operations,omitempty" jsonschema:"normalized list of operations in execution order"`
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

type SearchSchemaInput struct {
	Pattern  string `json:"pattern" jsonschema:"search pattern for table or column names (uses SQL LIKE syntax, e.g., %user%)"`
	Database string `json:"database,omitempty" jsonschema:"optional database name to restrict search"`
}

type SchemaMatch struct {
	Database string `json:"database" jsonschema:"database name"`
	Table    string `json:"table" jsonschema:"table name"`
	Column   string `json:"column,omitempty" jsonschema:"column name (if matched)"`
	Type     string `json:"type" jsonschema:"match type: TABLE or COLUMN"`
}

type SearchSchemaOutput struct {
	Matches []SchemaMatch `json:"matches" jsonschema:"list of schema matches"`
}

type SchemaDiffInput struct {
	SourceDatabase string `json:"source_database" jsonschema:"source database name"`
	TargetDatabase string `json:"target_database" jsonschema:"target database name"`
}

type DiffResult struct {
	Table   string `json:"table" jsonschema:"table name"`
	Status  string `json:"status" jsonschema:"diff status (MISSING, EXTRA, CHANGED)"`
	Details string `json:"details,omitempty" jsonschema:"details about the difference"`
}

type SchemaDiffOutput struct {
	SourceDatabase string       `json:"source_database" jsonschema:"source database name"`
	TargetDatabase string       `json:"target_database" jsonschema:"target database name"`
	Diffs          []DiffResult `json:"diffs" jsonschema:"list of differences between schemas"`
}

type AddConnectionInput struct {
	Name        string `json:"name" jsonschema:"unique name for the new connection"`
	DSN         string `json:"dsn" jsonschema:"MySQL DSN (user:pass@tcp(host:port)/db)"`
	Description string `json:"description,omitempty" jsonschema:"optional description of the connection"`
}

type AddConnectionOutput struct {
	Success bool   `json:"success" jsonschema:"true if connection was added and activated"`
	Active  string `json:"active" jsonschema:"name of the now-active connection"`
	Message string `json:"message" jsonschema:"status message"`
}
