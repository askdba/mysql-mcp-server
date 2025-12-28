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
	maxRows      int
	queryTimeout time.Duration
	pingTimeout  time.Duration
	extendedMode bool
	jsonLogging  bool
	tokenTracking bool
	tokenModel    string
	tokenEstimator TokenEstimator
)

// ===== Argument Parsing =====

// parsedArgs holds the result of command-line argument parsing.
type parsedArgs struct {
	action       string // "", "version", "help", "print-config", "validate-config"
	configPath   string // path from --config or --config=
	validatePath string // path for --validate-config
	err          error  // parsing error (e.g., unknown flag)
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

	// Set convenience aliases
	maxRows = cfg.MaxRows
	queryTimeout = cfg.QueryTimeout
	pingTimeout = cfg.PingTimeout
	extendedMode = cfg.ExtendedMode
	jsonLogging = cfg.JSONLogging
	tokenTracking = cfg.TokenTracking
	tokenModel = cfg.TokenModel

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
			log.Printf("Warning: failed to add connection '%s': %v", connCfg.Name, err)
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
		"httpPort":         cfg.HTTPPort,
		"jsonLogging":      jsonLogging,
		"auditLogEnabled":  auditLogger.enabled,
		"tokenTracking":    tokenTracking,
		"tokenModel":       tokenModel,
		"connections":      len(cfg.Connections),
		"activeConnection": activeName,
	})

	// If HTTP mode is enabled, start REST API server instead of MCP
	if cfg.HTTPMode {
		startHTTPServer(cfg.HTTPPort, cfg.VectorMode)
		return
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
		Name:        "run_query",
		Description: "Execute a read-only SQL query (SELECT/SHOW/DESCRIBE/EXPLAIN only)",
	}, toolRunQueryWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ping",
		Description: "Test database connectivity and measure latency",
	}, toolPingWrapped)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "server_info",
		Description: "Get MySQL server version, uptime, and configuration details",
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
	log.Printf("Registering extended MySQL tools...")

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
        MYSQL_MAX_ROWS               Max rows returned (default: 200)
        MYSQL_QUERY_TIMEOUT_SECONDS  Query timeout in seconds (default: 30)
        MYSQL_MCP_EXTENDED           Enable extended tools (set to 1)
        MYSQL_MCP_JSON_LOGS          Enable JSON structured logging (set to 1)
        MYSQL_MCP_TOKEN_TRACKING     Enable token usage estimation (set to 1)
        MYSQL_MCP_TOKEN_MODEL        Tokenizer encoding to use (default: cl100k_base)
        MYSQL_MCP_AUDIT_LOG          Path to audit log file
        MYSQL_MCP_VECTOR             Enable vector tools for MySQL 9.0+ (set to 1)
        MYSQL_MCP_HTTP               Enable REST API mode (set to 1)
        MYSQL_HTTP_PORT              HTTP port for REST API mode (default: 9306)
        MYSQL_HTTP_RATE_LIMIT        Enable rate limiting for HTTP mode (set to 1)
        MYSQL_HTTP_RATE_LIMIT_RPS    Rate limit: requests per second (default: 100)
        MYSQL_HTTP_RATE_LIMIT_BURST  Rate limit: burst size (default: 200)
        MYSQL_MAX_OPEN_CONNS         Max open database connections (default: 10)
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
