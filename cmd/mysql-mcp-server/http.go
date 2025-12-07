// cmd/mysql-mcp-server/http.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/api"
)

// httpContext returns a context with timeout for HTTP handlers.
// Uses the request's context as parent to properly handle client disconnects.
func httpContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), cfg.HTTPRequestTimeout)
}

// ===== Core HTTP Handlers =====

// httpListDatabases handles GET /api/databases
func httpListDatabases(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListDatabases(ctx, nil, ListDatabasesInput{})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpListTables handles GET /api/tables?database=xxx
func httpListTables(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListTables(ctx, nil, ListTablesInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpDescribeTable handles GET /api/describe?database=xxx&table=yyy
func httpDescribeTable(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolDescribeTable(ctx, nil, DescribeTableInput{Database: database, Table: table})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpRunQuery handles POST /api/query with JSON body {"sql": "...", "database": "...", "max_rows": N}
func httpRunQuery(w http.ResponseWriter, r *http.Request) {
	var input RunQueryInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		api.WriteBadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if input.SQL == "" {
		api.WriteBadRequest(w, "sql field is required")
		return
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolRunQuery(ctx, nil, input)
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpPing handles GET /api/ping
func httpPing(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolPing(ctx, nil, PingInput{})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpServerInfo handles GET /api/server-info
func httpServerInfo(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolServerInfo(ctx, nil, ServerInfoInput{})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpListConnections handles GET /api/connections
func httpListConnections(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListConnections(ctx, nil, ListConnectionsInput{})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpUseConnection handles POST /api/connections/use with JSON body {"name": "..."}
func httpUseConnection(w http.ResponseWriter, r *http.Request) {
	var input UseConnectionInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		api.WriteBadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if input.Name == "" {
		api.WriteBadRequest(w, "name field is required")
		return
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolUseConnection(ctx, nil, input)
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// ===== Extended HTTP Handlers =====

// httpListIndexes handles GET /api/indexes?database=xxx&table=yyy
func httpListIndexes(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListIndexes(ctx, nil, ListIndexesInput{Database: database, Table: table})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpShowCreateTable handles GET /api/create-table?database=xxx&table=yyy
func httpShowCreateTable(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolShowCreateTable(ctx, nil, ShowCreateTableInput{Database: database, Table: table})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpExplainQuery handles POST /api/explain with JSON body {"sql": "...", "database": "..."}
func httpExplainQuery(w http.ResponseWriter, r *http.Request) {
	var input ExplainQueryInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		api.WriteBadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if input.SQL == "" {
		api.WriteBadRequest(w, "sql field is required")
		return
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolExplainQuery(ctx, nil, input)
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpListViews handles GET /api/views?database=xxx
func httpListViews(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListViews(ctx, nil, ListViewsInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpListTriggers handles GET /api/triggers?database=xxx
func httpListTriggers(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListTriggers(ctx, nil, ListTriggersInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpListProcedures handles GET /api/procedures?database=xxx
func httpListProcedures(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListProcedures(ctx, nil, ListProceduresInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpListFunctions handles GET /api/functions?database=xxx
func httpListFunctions(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListFunctions(ctx, nil, ListFunctionsInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpDatabaseSize handles GET /api/size/database?database=xxx (optional)
func httpDatabaseSize(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolDatabaseSize(ctx, nil, DatabaseSizeInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpTableSize handles GET /api/size/tables?database=xxx
func httpTableSize(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolTableSize(ctx, nil, TableSizeInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpForeignKeys handles GET /api/foreign-keys?database=xxx&table=yyy (table optional)
func httpForeignKeys(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolForeignKeys(ctx, nil, ForeignKeysInput{Database: database, Table: table})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpListStatus handles GET /api/status?pattern=xxx (pattern optional)
func httpListStatus(w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("pattern")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListStatus(ctx, nil, ListStatusInput{Pattern: pattern})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpListVariables handles GET /api/variables?pattern=xxx (pattern optional)
func httpListVariables(w http.ResponseWriter, r *http.Request) {
	pattern := r.URL.Query().Get("pattern")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListVariables(ctx, nil, ListVariablesInput{Pattern: pattern})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// ===== Vector HTTP Handlers =====

// httpVectorSearch handles POST /api/vector/search
func httpVectorSearch(w http.ResponseWriter, r *http.Request) {
	var input VectorSearchInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		api.WriteBadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolVectorSearch(ctx, nil, input)
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpVectorInfo handles GET /api/vector/info?database=xxx
func httpVectorInfo(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolVectorInfo(ctx, nil, VectorInfoInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// ===== Utility HTTP Handlers =====

// httpHealth handles GET /health
func httpHealth(w http.ResponseWriter, r *http.Request) {
	api.WriteSuccess(w, map[string]interface{}{
		"status":  "healthy",
		"service": "mysql-mcp-server",
	})
}

// httpAPIIndex handles GET /api
func httpAPIIndex(w http.ResponseWriter, r *http.Request) {
	endpoints := map[string]interface{}{
		"service": "mysql-mcp-server REST API",
		"version": Version,
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
	api.WriteSuccess(w, endpoints)
}

// ===== HTTP Server Setup =====

// httpLogger logs HTTP requests using the application's structured logging.
func httpLogger(method, path string, status int, duration time.Duration) {
	logInfo("http request", map[string]interface{}{
		"method":      method,
		"path":        path,
		"status":      status,
		"duration_ms": duration.Milliseconds(),
	})
}

// startHTTPServer starts the REST API server with graceful shutdown support.
func startHTTPServer(port int, vectorMode bool) {
	mux := http.NewServeMux()

	// Create logging middleware
	withLog := api.WithLogging(httpLogger)

	// Health and index
	mux.HandleFunc("/health", api.WithCORS(httpHealth))
	mux.HandleFunc("/api", api.WithCORS(httpAPIIndex))
	mux.HandleFunc("/api/", api.WithCORS(httpAPIIndex))

	// Core endpoints
	mux.HandleFunc("/api/databases", api.WithCORS(httpListDatabases))
	mux.HandleFunc("/api/tables", api.Chain(httpListTables, api.WithCORS, api.RequireQueryParam("database")))
	mux.HandleFunc("/api/describe", api.Chain(httpDescribeTable, api.WithCORS, api.RequireQueryParams([]string{"database", "table"})))
	mux.HandleFunc("/api/query", api.Chain(httpRunQuery, api.WithCORS, api.RequirePOST))
	mux.HandleFunc("/api/ping", api.WithCORS(httpPing))
	mux.HandleFunc("/api/server-info", api.WithCORS(httpServerInfo))
	mux.HandleFunc("/api/connections", api.WithCORS(httpListConnections))
	mux.HandleFunc("/api/connections/use", api.Chain(httpUseConnection, api.WithCORS, api.RequirePOST))

	// Extended endpoints
	extendedFeature := func(next http.HandlerFunc) http.HandlerFunc {
		return api.RequireFeature(extendedMode, "extended mode (set MYSQL_MCP_EXTENDED=1)", next)
	}
	mux.HandleFunc("/api/indexes", api.Chain(httpListIndexes, api.WithCORS, extendedFeature, api.RequireQueryParams([]string{"database", "table"})))
	mux.HandleFunc("/api/create-table", api.Chain(httpShowCreateTable, api.WithCORS, extendedFeature, api.RequireQueryParams([]string{"database", "table"})))
	mux.HandleFunc("/api/explain", api.Chain(httpExplainQuery, api.WithCORS, extendedFeature, api.RequirePOST))
	mux.HandleFunc("/api/views", api.Chain(httpListViews, api.WithCORS, extendedFeature, api.RequireQueryParam("database")))
	mux.HandleFunc("/api/triggers", api.Chain(httpListTriggers, api.WithCORS, extendedFeature, api.RequireQueryParam("database")))
	mux.HandleFunc("/api/procedures", api.Chain(httpListProcedures, api.WithCORS, extendedFeature, api.RequireQueryParam("database")))
	mux.HandleFunc("/api/functions", api.Chain(httpListFunctions, api.WithCORS, extendedFeature, api.RequireQueryParam("database")))
	mux.HandleFunc("/api/size/database", api.Chain(httpDatabaseSize, api.WithCORS, extendedFeature))
	mux.HandleFunc("/api/size/tables", api.Chain(httpTableSize, api.WithCORS, extendedFeature, api.RequireQueryParam("database")))
	mux.HandleFunc("/api/foreign-keys", api.Chain(httpForeignKeys, api.WithCORS, extendedFeature, api.RequireQueryParam("database")))
	mux.HandleFunc("/api/status", api.Chain(httpListStatus, api.WithCORS, extendedFeature))
	mux.HandleFunc("/api/variables", api.Chain(httpListVariables, api.WithCORS, extendedFeature))

	// Vector endpoints
	vectorFeature := func(next http.HandlerFunc) http.HandlerFunc {
		return api.RequireFeature(vectorMode, "vector mode (set MYSQL_MCP_VECTOR=1)", next)
	}
	mux.HandleFunc("/api/vector/search", api.Chain(httpVectorSearch, api.WithCORS, vectorFeature, api.RequirePOST))
	mux.HandleFunc("/api/vector/info", api.Chain(httpVectorInfo, api.WithCORS, vectorFeature, api.RequireQueryParam("database")))

	addr := fmt.Sprintf(":%d", port)

	// Create server with timeouts and logging
	server := &http.Server{
		Addr:         addr,
		Handler:      withLog(mux.ServeHTTP),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: cfg.HTTPRequestTimeout + 5*time.Second, // Slightly longer than request timeout
		IdleTimeout:  120 * time.Second,
	}

	// Channel to listen for shutdown signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		logInfo("HTTP REST API server starting", map[string]interface{}{
			"port":         port,
			"address":      "http://localhost" + addr,
			"extendedMode": extendedMode,
			"vectorMode":   vectorMode,
			"version":      Version,
		})

		log.Printf("REST API endpoints available at http://localhost:%d/api", port)
		log.Printf("Health check at http://localhost:%d/health", port)
		log.Printf("Press Ctrl+C to stop the server")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-stop
	logInfo("Shutdown signal received, stopping server...", nil)

	// Create context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	} else {
		logInfo("Server stopped gracefully", nil)
	}
}
