// cmd/mysql-mcp-server/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/askdba/mysql-mcp-server/internal/dbretry"
	"github.com/askdba/mysql-mcp-server/internal/util"
)

// Version information (injected at build time via ldflags).
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// ===== Global State =====

// Global configuration and state shared by all tools.
var (
	cfg         *config.Config
	connManager *ConnectionManager
	auditLogger *AuditLogger

	// Convenience aliases from config (for tool access)
	maxRows        int
	queryTimeout   time.Duration
	pingTimeout    time.Duration
	dbRetryCfg     dbretry.Config
	extendedMode   bool
	jsonLogging    bool
	tokenTracking  bool
	tokenCard      bool
	tokenModel     string
	tokenEstimator TokenEstimator

	// silentMode suppresses INFO and WARN logs (--silent); ERROR still goes to stderr.
	silentMode bool
)

// ===== Argument Parsing =====

// parsedArgs holds the result of command-line argument parsing.
type parsedArgs struct {
	action        string // "", "version", "help", "print-config", "validate-config"
	configPath    string // path from --config or --config=
	validatePath  string // path for --validate-config
	silent        bool   // --silent or -s: suppress INFO/WARN logs
	daemon        bool   // --daemon: fork to background (HTTP mode)
	tokenCardFlag bool   // --token-card: enable live token monitoring UI
	err           error  // parsing error (e.g., unknown flag)
}

// parseArgs parses command-line arguments and returns the result.
// This is separated from main() for testability.
func parseArgs(args []string) parsedArgs {
	var result parsedArgs

	for len(args) > 0 {
		arg := args[0]
		args = args[1:]

		switch arg {
		case "--version", "-v":
			result.action = "version"
			return result
		case "--help", "-h", "help":
			result.action = "help"
			return result
		case "--config", "-c":
			if len(args) < 1 {
				result.err = fmt.Errorf("--config requires a path argument")
				return result
			}
			result.configPath = args[0]
			args = args[1:]
		case "--print-config":
			result.action = "print-config"
		case "--validate-config":
			if len(args) < 1 {
				result.err = fmt.Errorf("--validate-config requires a path argument")
				return result
			}
			result.action = "validate-config"
			result.validatePath = args[0]
			args = args[1:]
		case "--silent", "-s":
			result.silent = true
		case "--daemon", "-d":
			result.daemon = true
		case "--token-card":
			result.tokenCardFlag = true
		default:
			// Check if it's --config=path format
			if len(arg) > 9 && arg[:9] == "--config=" {
				result.configPath = arg[9:]
			} else {
				result.err = fmt.Errorf("unknown flag '%s'", arg)
				return result
			}
		}
	}

	return result
}

// ===== Main Entry Point =====

func main() {
	// Parse command-line arguments
	parsed := parseArgs(os.Args[1:])

	// Handle parsing errors
	if parsed.err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", parsed.err)
		printHelp()
		os.Exit(1)
	}

	// Set config path if specified
	if parsed.configPath != "" {
		config.ConfigFilePath = parsed.configPath
	}
	silentMode = parsed.silent

	// Handle immediate actions
	switch parsed.action {
	case "version":
		fmt.Printf("mysql-mcp-server %s\n", Version)
		fmt.Printf("  Build time: %s\n", BuildTime)
		fmt.Printf("  Git commit: %s\n", GitCommit)
		os.Exit(0)
	case "help":
		printHelp()
		os.Exit(0)
	case "print-config":
		handlePrintConfig()
		os.Exit(0)
	case "validate-config":
		handleValidateConfig(parsed.validatePath)
		os.Exit(0)
	}

	var err error

	// ---- Load configuration ----
	cfg, err = config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	initAccessControl(cfg.AllowedDatabases)

	// Daemon mode requires HTTP mode; defer until after config load so we can check.
	if parsed.daemon {
		if !cfg.HTTPMode {
			fmt.Fprintf(os.Stderr, "Error: daemon mode requires HTTP mode (set MYSQL_MCP_HTTP=1 or http.enabled: true in config)\n")
			os.Exit(1)
		}
		maybeDaemonize(parsed)
	}

	// Set convenience aliases
	maxRows = cfg.MaxRows
	queryTimeout = cfg.QueryTimeout
	pingTimeout = cfg.PingTimeout
	dbRetryCfg = dbretry.Config{
		MaxRetries:  cfg.DBRetryMaxRetries,
		MaxInterval: cfg.DBRetryMaxInterval,
	}
	if dbRetryCfg.MaxInterval <= 0 {
		dbRetryCfg.MaxInterval = 10 * time.Second
	}
	extendedMode = cfg.ExtendedMode
	jsonLogging = cfg.JSONLogging
	tokenTracking = cfg.TokenTracking
	tokenModel = cfg.TokenModel
	// CLI --token-card overrides config (OR with config value)
	tokenCard = cfg.TokenCard || parsed.tokenCardFlag

	// Initialize audit logger
	auditLogger, err = NewAuditLogger(cfg.AuditLogPath)
	if err != nil {
		log.Fatalf("audit log init error: %v", err)
	}
	if auditLogger.enabled {
		defer auditLogger.Close()
	}

	// Initialize token estimator (optional)
	if tokenTracking {
		tokenEstimator, err = NewTokenEstimator(tokenModel)
		if err != nil {
			logWarn("token tracking requested but tokenizer init failed; disabling token tracking", map[string]interface{}{
				"error": err.Error(),
				"model": tokenModel,
			})
			tokenTracking = false
			tokenEstimator = nil
		}
	}

	// ---- Initialize Connection Manager ----
	connManager = NewConnectionManager()
	defer connManager.Close()

	// Add all connections from config
	for _, connCfg := range cfg.Connections {
		if err := connManager.AddConnectionWithPoolConfig(connCfg, cfg); err != nil {
			logWarn("failed to add connection", map[string]interface{}{"name": connCfg.Name, "error": err.Error()})
		} else {
			logInfo("connection added", map[string]interface{}{
				"name": connCfg.Name,
				"dsn":  util.MaskDSN(connCfg.DSN),
			})
		}
	}

	// Verify we have at least one valid connection
	if connManager.GetActiveDB() == nil {
		connManager.Close() // Clean up before exit
		log.Fatalf("config error: no valid MySQL connections available")
	}

	_, activeName := connManager.GetActive()

	// Log startup configuration
	logInfo("mysql-mcp-server started", map[string]interface{}{
		"version":          Version,
		"buildTime":        BuildTime,
		"maxRows":          maxRows,
		"queryTimeout":     queryTimeout.String(),
		"extendedMode":     extendedMode,
		"vectorMode":       cfg.VectorMode,
		"httpMode":         cfg.HTTPMode,
		"metricsHTTP":      cfg.MetricsHTTP,
		"httpPort":         cfg.HTTPPort,
		"jsonLogging":      jsonLogging,
		"auditLogEnabled":  auditLogger.enabled,
		"tokenTracking":    tokenTracking,
		"tokenCard":        tokenCard,
		"tokenModel":       tokenModel,
		"connections":      len(cfg.Connections),
		"activeConnection": activeName,
	})

	// If HTTP mode is enabled, start REST API server instead of MCP
	if cfg.HTTPMode {
		startHTTPServer(cfg.HTTPPort, cfg.VectorMode, tokenCard)
		return
	}

	// Optional: token metrics + /status on HTTP while MCP uses stdio (Claude Desktop, Cursor)
	if cfg.MetricsHTTP {
		go startTokenMetricsHTTPServer(cfg.HTTPPort, tokenCard)
	}

	// ---- Build MCP server ----
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "mysql-mcp-server",
			Version: Version,
		},
		nil,
	)

	// Register core tools
	registerCoreTools(server)

	// Register multi-DSN tools
	registerConnectionTools(server)

	// Register prompts (multi-step workflows that gather live data for Claude)
	registerPrompts(server)

	// Register vector tools (MYSQL_MCP_VECTOR=1)
	if cfg.VectorMode {
		registerVectorTools(server)
	}

	// Register extended tools (MYSQL_MCP_EXTENDED=1)
	if extendedMode {
		registerExtendedTools(server)
	}

	// ---- Run over stdio ----
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

// ===== Tool Registration =====

func registerCoreTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_databases",
		Description: "List accessible databases in the configured MySQL server",
	}, toolListDatabasesWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tables",
		Description: "List tables in a given database",
	}, toolListTablesWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_table",
		Description: "Describe columns of a given table",
	}, toolDescribeTableWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name: "run_query",
		Description: "Execute a read-only SQL query (SELECT/SHOW/DESCRIBE/EXPLAIN only). " +
			"IMPORTANT: Always specify only the columns you need instead of SELECT * to reduce " +
			"payload size and improve performance. Results are automatically capped at the " +
			"configured row limit (default: 200 rows); include a LIMIT clause in your query " +
			"to request fewer rows. For large SELECT result sets, use the offset field with " +
			"max_rows as page size (omit LIMIT from SQL; the server injects LIMIT/OFFSET). " +
			"The response includes has_more and next_offset when another page may exist. " +
			"Apply MySQL optimization guidelines (e.g., filter early, use indexed columns, " +
			"avoid functions on indexed columns, use EXPLAIN) before executing.",
	}, toolRunQueryWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ping",
		Description: "Test database connectivity and measure latency",
	}, toolPingWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "server_info",
		Description: "Get MySQL server version, uptime, and configuration details. Pass detailed=true for health metrics (ping ms, threads_running, slow_queries, buffer pool hit rate). When MYSQL_MCP_TOKEN_TRACKING=1, includes token usage totals.",
	}, toolServerInfoWrapped)
}

func registerConnectionTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_connections",
		Description: "List all configured MySQL connections and show which is active",
	}, toolListConnectionsWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "use_connection",
		Description: "Switch to a different MySQL connection by name",
	}, toolUseConnectionWrapped)
}

func registerVectorTools(server *mcp.Server) {
	logInfo("Registering MySQL vector tools (MySQL 9.0+ required)...", nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "vector_search",
		Description: "Perform similarity search on vector columns (MySQL 9.0+ required)",
	}, toolVectorSearchWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "vector_info",
		Description: "List vector columns and their properties in a database",
	}, toolVectorInfoWrapped)
}

func registerExtendedTools(server *mcp.Server) {
	logInfo("Registering extended MySQL tools...", nil)

	if cfg.ProcessAdmin {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "process_list",
			Description: "Show active server threads (SHOW PROCESSLIST). Requires MYSQL_MCP_PROCESS_ADMIN=1 and PROCESS privilege.",
		}, toolProcessListWrapped)
		mcp.AddTool(server, &mcp.Tool{
			Name:        "kill_query",
			Description: "Cancel the currently executing statement for a connection using id from process_list (KILL QUERY; does not disconnect the client). Requires MYSQL_MCP_PROCESS_ADMIN=1.",
		}, toolKillQueryWrapped)
	}

	if cfg.ReadAuditTool && auditLogger != nil && auditLogger.enabled && cfg.AuditLogPath != "" {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "read_audit_log",
			Description: "Return the last lines of the configured MYSQL_MCP_AUDIT_LOG file (read-only). Requires MYSQL_MCP_READ_AUDIT_TOOL=1.",
		}, toolReadAuditLogWrapped)
	}

	if cfg.SlowQueryTool {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "slow_query_log",
			Description: "Read recent rows from mysql.slow_log when slow_query_log uses TABLE output; otherwise summarize settings. Requires MYSQL_MCP_SLOW_QUERY_TOOL=1.",
		}, toolSlowQueryLogWrapped)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_indexes",
		Description: "List indexes on a table",
	}, toolListIndexesWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "show_create_table",
		Description: "Show the CREATE TABLE statement for a table",
	}, toolShowCreateTableWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "explain_query",
		Description: "Get the execution plan for a SELECT query",
	}, toolExplainQueryWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_views",
		Description: "List views in a database",
	}, toolListViewsWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_triggers",
		Description: "List triggers in a database",
	}, toolListTriggersWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_procedures",
		Description: "List stored procedures in a database",
	}, toolListProceduresWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_functions",
		Description: "List stored functions in a database",
	}, toolListFunctionsWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_partitions",
		Description: "List partitions of a table",
	}, toolListPartitionsWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "database_size",
		Description: "Get size information for databases",
	}, toolDatabaseSizeWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "table_size",
		Description: "Get size information for tables",
	}, toolTableSizeWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "foreign_keys",
		Description: "List foreign key constraints",
	}, toolForeignKeysWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_status",
		Description: "List MySQL server status variables",
	}, toolListStatusWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_variables",
		Description: "List MySQL server configuration variables",
	}, toolListVariablesWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_schema",
		Description: "Find tables and columns matching a pattern across databases",
	}, toolSearchSchemaWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "schema_diff",
		Description: "Compare the schema between two databases",
	}, toolSchemaDiffWrapped)

	if os.Getenv("MYSQL_MCP_ENABLE_ADD_CONNECTION") == "1" {
		mcp.AddTool(server, &mcp.Tool{
			Name: "add_connection",
			Description: "Register and switch to a new MySQL connection at runtime. " +
				"Requires extended mode (MYSQL_MCP_EXTENDED=1) and MYSQL_MCP_ENABLE_ADD_CONNECTION=1.",
		}, wrapTool("add_connection", wrapAddConnection(connManager, cfg)))
	}
}

// ===== Config File Commands =====

func handlePrintConfig() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(config.PrintConfig(cfg))
}

func handleValidateConfig(path string) {
	if err := config.ValidateConfigFile(path); err != nil {
		fmt.Fprintf(os.Stderr, "Config validation failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Config file %s is valid\n", path)
}

// ===== Help and Usage =====

func printHelp() {
	fmt.Printf(`mysql-mcp-server - MySQL Server for Model Context Protocol (MCP)

USAGE:
    mysql-mcp-server [OPTIONS]

OPTIONS:
    -h, --help                  Show this help message
    -v, --version               Show version information
    -c, --config PATH           Use config file at PATH
    -s, --silent                Suppress INFO and WARN logs (ERROR still printed)
    -d, --daemon                Run in background (fork and detach; use with MYSQL_MCP_HTTP=1)
    --token-card                Enable live token monitoring UI at /status (HTTP mode)
    --print-config              Print current configuration as YAML
    --validate-config PATH      Validate config file at PATH

DESCRIPTION:
    A fast, read-only MySQL Server for the Model Context Protocol (MCP).
    Exposes safe MySQL introspection tools to Claude Desktop via MCP.

CONFIGURATION:
    Configuration can be provided via config file or environment variables.
    Environment variables take precedence over config file values.

    Config file search order:
        1. --config flag or MYSQL_MCP_CONFIG env var
        2. ./mysql-mcp-server.yaml (current directory)
        3. ~/.config/mysql-mcp-server/config.yaml (user config)
        4. /etc/mysql-mcp-server/config.yaml (system config)

    Required (via env var or config file):
        MYSQL_DSN                    MySQL DSN (e.g., user:pass@tcp(localhost:3306)/db)

    Optional:
        MYSQL_MAX_ROWS               Max rows returned per query (default: 200)
        MYSQL_QUERY_TIMEOUT_SECONDS  Query timeout in seconds (default: 30)
        MYSQL_QUERY_TIMEOUT          Query timeout in milliseconds (e.g. 30000); overridden by MYSQL_QUERY_TIMEOUT_SECONDS
        MYSQL_MCP_EXTENDED           Enable extended tools (set to 1)
        MYSQL_MCP_JSON_LOGS          Enable JSON structured logging (set to 1)
        MYSQL_MCP_TOKEN_TRACKING     Enable token usage estimation (set to 1)
        MYSQL_MCP_TOKEN_MODEL        Tokenizer encoding to use (default: cl100k_base)
        MYSQL_MCP_TOKEN_CARD         Live token UI at /status: on by default in HTTP mode; set to 0 to disable
        MYSQL_MCP_AUDIT_LOG          Path to audit log file
        MYSQL_MCP_ALLOWED_DATABASES Comma-separated schema allowlist (optional)
        MYSQL_MCP_STRICT_READ_ONLY   Set 1 for transaction_read_only=ON on connections
        MYSQL_MCP_PROCESS_ADMIN      Set 1 for process_list / kill_query tools (extended)
        MYSQL_MCP_READ_AUDIT_TOOL    Set 1 for read_audit_log when audit path set
        MYSQL_MCP_SLOW_QUERY_TOOL    Set 1 for slow_query_log tool (extended)
        MYSQL_MCP_ENABLE_ADD_CONNECTION  Set 1 to register add_connection (runtime DSN registration; extended tools only)
        MYSQL_MCP_VECTOR             Enable vector tools for MySQL 9.0+ (set to 1)
        MYSQL_MCP_HTTP               Enable REST API mode (set to 1)
        MYSQL_MCP_METRICS_HTTP       With stdio MCP only: serve /status and /api/metrics/tokens on MYSQL_HTTP_PORT (set to 1); not used when MYSQL_MCP_HTTP=1
        MYSQL_HTTP_PORT              HTTP port for REST API or metrics sidecar (default: 9306)
        MYSQL_HTTP_RATE_LIMIT        Enable rate limiting for HTTP mode (set to 1)
        MYSQL_HTTP_RATE_LIMIT_RPS    Rate limit: requests per second (default: 100)
        MYSQL_HTTP_RATE_LIMIT_BURST  Rate limit: burst size (default: 200)
        MYSQL_POOL_SIZE              Connection pool size / max open connections (default: 10); alias for MYSQL_MAX_OPEN_CONNS
        MYSQL_MAX_OPEN_CONNS         Max open database connections (default: 10); overrides MYSQL_POOL_SIZE
        MYSQL_MAX_IDLE_CONNS         Max idle database connections (default: 5)
        MYSQL_CONN_MAX_LIFETIME_MINUTES  Connection max lifetime in minutes (default: 30)

MULTI-DSN CONFIGURATION:
    Configure multiple MySQL connections using numbered environment variables:

        MYSQL_DSN_1                  Additional connection DSN
        MYSQL_DSN_1_NAME             Connection name (default: connection_1)
        MYSQL_DSN_1_DESC             Connection description

    Or use JSON configuration:

        MYSQL_CONNECTIONS='[
          {"name": "production", "dsn": "user:pass@tcp(prod:3306)/db", "description": "Production"},
          {"name": "staging", "dsn": "user:pass@tcp(staging:3306)/db", "description": "Staging"}
        ]'

EXAMPLES:
    # Basic usage with single connection
    export MYSQL_DSN="root:password@tcp(127.0.0.1:3306)/mysql?parseTime=true"
    mysql-mcp-server

    # With config file
    mysql-mcp-server --config /path/to/config.yaml

    # Validate a config file
    mysql-mcp-server --validate-config /path/to/config.yaml

    # Print current configuration
    mysql-mcp-server --print-config

    # With extended tools enabled
    export MYSQL_DSN="user:pass@tcp(localhost:3306)/mydb"
    export MYSQL_MCP_EXTENDED=1
    mysql-mcp-server

    # HTTP REST API mode
    export MYSQL_DSN="user:pass@tcp(localhost:3306)/mydb"
    export MYSQL_MCP_HTTP=1
    export MYSQL_HTTP_PORT=9306
    mysql-mcp-server

    # Claude Desktop: stdio MCP + token dashboard on http://127.0.0.1:9306/status (same process)
    export MYSQL_DSN="user:pass@tcp(127.0.0.1:3306)/db?parseTime=true"
    export MYSQL_MCP_TOKEN_TRACKING=1
    export MYSQL_MCP_METRICS_HTTP=1
    export MYSQL_HTTP_PORT=9306
    mysql-mcp-server

    # Silent mode (production; only errors to stderr)
    mysql-mcp-server --silent --config /etc/mysql-mcp-server/config.yaml

    # Run HTTP server as daemon (Unix)
    MYSQL_MCP_HTTP=1 mysql-mcp-server --daemon --config /path/to/config.yaml

FEATURES:
    - Fully read-only (blocks all non-SELECT/SHOW/DESCRIBE/EXPLAIN)
    - Multi-DSN support (connect to multiple MySQL instances)
    - Vector search (MySQL 9.0+)
    - Query timeouts and row limits
    - Structured logging and audit logs
    - REST API mode for HTTP clients

MCP TOOLS:
    Core: list_databases, list_tables, describe_table, run_query, ping, server_info
    Connections: list_connections, use_connection
    Extended: list_indexes, show_create_table, explain_query, list_views, etc.
    Optional extended: add_connection (requires MYSQL_MCP_ENABLE_ADD_CONNECTION=1)
    Vector: vector_search, vector_info (MySQL 9.0+)

SECURITY:
    - SQL validation blocks dangerous operations
    - Read-only enforcement
    - Multi-statement prevention
    - Recommended: Use a read-only MySQL user

    CREATE USER 'mcp'@'localhost' IDENTIFIED BY 'strongpass';
    GRANT SELECT ON *.* TO 'mcp'@'localhost';

DOCUMENTATION:
    Full documentation: https://github.com/askdba/mysql-mcp-server

`)
}
