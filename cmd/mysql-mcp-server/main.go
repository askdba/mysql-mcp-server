// cmd/mysql-mcp-server/main.go
package main

import (
	"context"
	"database/sql"
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

	// Deprecated: Use connManager.GetActiveDB() instead.
	// Kept for backward compatibility during transition.
	db *sql.DB
)

// ===== Main Entry Point =====

func main() {
	// Handle command-line flags before loading configuration
	if len(os.Args) > 1 {
		arg := os.Args[1]
		switch arg {
		case "--version", "-v":
			fmt.Printf("mysql-mcp-server %s\n", Version)
			fmt.Printf("  Build time: %s\n", BuildTime)
			fmt.Printf("  Git commit: %s\n", GitCommit)
			os.Exit(0)
		case "--help", "-h", "help":
			printHelp()
			os.Exit(0)
		default:
			// Unknown flag
			fmt.Fprintf(os.Stderr, "Error: unknown flag '%s'\n\n", arg)
			printHelp()
			os.Exit(1)
		}
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

	// Initialize audit logger
	auditLogger, err = NewAuditLogger(cfg.AuditLogPath)
	if err != nil {
		log.Fatalf("audit log init error: %v", err)
	}
	if auditLogger.enabled {
		defer auditLogger.Close()
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
	db = connManager.GetActiveDB()
	if db == nil {
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
}

func registerConnectionTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_connections",
		Description: "List all configured MySQL connections and show which is active",
	}, toolListConnections)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "use_connection",
		Description: "Switch to a different MySQL connection by name",
	}, toolUseConnection)
}

func registerVectorTools(server *mcp.Server) {
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

func registerExtendedTools(server *mcp.Server) {
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

// ===== Help and Usage =====

func printHelp() {
	fmt.Printf(`mysql-mcp-server - MySQL Server for Model Context Protocol (MCP)

USAGE:
    mysql-mcp-server [OPTIONS]

OPTIONS:
    -h, --help      Show this help message
    -v, --version   Show version information

DESCRIPTION:
    A fast, read-only MySQL Server for the Model Context Protocol (MCP).
    Exposes safe MySQL introspection tools to Claude Desktop via MCP.

CONFIGURATION:
    All configuration is done via environment variables.

    Required:
        MYSQL_DSN                    MySQL DSN (e.g., user:pass@tcp(localhost:3306)/db)

    Optional:
        MYSQL_MAX_ROWS               Max rows returned (default: 200)
        MYSQL_QUERY_TIMEOUT_SECONDS  Query timeout in seconds (default: 30)
        MYSQL_MCP_EXTENDED           Enable extended tools (set to 1)
        MYSQL_MCP_JSON_LOGS          Enable JSON structured logging (set to 1)
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
