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
	extendedMode bool
	jsonLogging  bool

	// Deprecated: Use connManager.GetActiveDB() instead.
	// Kept for backward compatibility during transition.
	db *sql.DB
)

// ===== Main Entry Point =====

func main() {
	// Handle --version flag
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("mysql-mcp-server %s\n", Version)
		fmt.Printf("  Build time: %s\n", BuildTime)
		fmt.Printf("  Git commit: %s\n", GitCommit)
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
