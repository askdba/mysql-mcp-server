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
