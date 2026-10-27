// cmd/mysql-mcp-server/http.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/api"
)

const maxJSONRequestBodyBytes int64 = 1 << 20 // 1 MiB

// httpContext returns a context with timeout for HTTP handlers.
// Uses the request's context as parent to properly handle client disconnects.
func httpContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), cfg.HTTPRequestTimeout)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return err
	}

	// Reject trailing data (helps avoid JSON request smuggling / ambiguity)
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain a single JSON object")
		}
		return err
	}

	return nil
}

// ===== Core HTTP Handlers =====

// httpListDatabases handles GET /api/databases
func httpListDatabases(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListDatabasesWrapped(ctx, nil, ListDatabasesInput{})
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
	_, out, err := toolListTablesWrapped(ctx, nil, ListTablesInput{Database: database})
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
	_, out, err := toolDescribeTableWrapped(ctx, nil, DescribeTableInput{Database: database, Table: table})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpRunQuery handles POST /api/query with JSON body {"sql": "...", "database": "...", "max_rows": N}
func httpRunQuery(w http.ResponseWriter, r *http.Request) {
	var input RunQueryInput
	if err := decodeJSONBody(w, r, &input); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			api.WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		api.WriteBadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if input.SQL == "" {
		api.WriteBadRequest(w, "sql field is required")
		return
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolRunQueryWrapped(ctx, nil, input)
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
	_, out, err := toolPingWrapped(ctx, nil, PingInput{})
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
	detailed := r.URL.Query().Get("detailed") == "1" || strings.EqualFold(r.URL.Query().Get("detailed"), "true")
	_, out, err := toolServerInfoWrapped(ctx, nil, ServerInfoInput{Detailed: detailed})
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
	_, out, err := toolListConnectionsWrapped(ctx, nil, ListConnectionsInput{})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpUseConnection handles POST /api/connections/use with JSON body {"name": "..."}
func httpUseConnection(w http.ResponseWriter, r *http.Request) {
	var input UseConnectionInput
	if err := decodeJSONBody(w, r, &input); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			api.WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		api.WriteBadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if input.Name == "" {
		api.WriteBadRequest(w, "name field is required")
		return
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolUseConnectionWrapped(ctx, nil, input)
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
	_, out, err := toolListIndexesWrapped(ctx, nil, ListIndexesInput{Database: database, Table: table})
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
	_, out, err := toolShowCreateTableWrapped(ctx, nil, ShowCreateTableInput{Database: database, Table: table})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpExplainQuery handles POST /api/explain with JSON body {"sql": "...", "database": "..."}
func httpExplainQuery(w http.ResponseWriter, r *http.Request) {
	var input ExplainQueryInput
	if err := decodeJSONBody(w, r, &input); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			api.WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		api.WriteBadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if input.SQL == "" {
		api.WriteBadRequest(w, "sql field is required")
		return
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolExplainQueryWrapped(ctx, nil, input)
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
	_, out, err := toolListViewsWrapped(ctx, nil, ListViewsInput{Database: database})
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
	_, out, err := toolListTriggersWrapped(ctx, nil, ListTriggersInput{Database: database})
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
	_, out, err := toolListProceduresWrapped(ctx, nil, ListProceduresInput{Database: database})
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
	_, out, err := toolListFunctionsWrapped(ctx, nil, ListFunctionsInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpListPartitions handles GET /api/partitions?database=xxx&table=yyy
func httpListPartitions(w http.ResponseWriter, r *http.Request) {
	database := r.URL.Query().Get("database")
	table := r.URL.Query().Get("table")
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolListPartitionsWrapped(ctx, nil, ListPartitionsInput{Database: database, Table: table})
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
	_, out, err := toolDatabaseSizeWrapped(ctx, nil, DatabaseSizeInput{Database: database})
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
	_, out, err := toolTableSizeWrapped(ctx, nil, TableSizeInput{Database: database})
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
	_, out, err := toolForeignKeysWrapped(ctx, nil, ForeignKeysInput{Database: database, Table: table})
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
	_, out, err := toolListStatusWrapped(ctx, nil, ListStatusInput{Pattern: pattern})
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
	_, out, err := toolListVariablesWrapped(ctx, nil, ListVariablesInput{Pattern: pattern})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpProcessList handles GET /api/processlist
func httpProcessList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolProcessListWrapped(ctx, nil, ProcessListInput{})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpKillQuery handles POST /api/kill body {"id": 123} (KILL QUERY).
func httpKillQuery(w http.ResponseWriter, r *http.Request) {
	var input KillQueryInput
	if err := decodeJSONBody(w, r, &input); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			api.WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		api.WriteBadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if input.ID <= 0 {
		api.WriteBadRequest(w, "id must be a positive integer")
		return
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolKillQueryWrapped(ctx, nil, input)
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpReadAuditLog handles GET /api/audit-log?lines=50
func httpReadAuditLog(w http.ResponseWriter, r *http.Request) {
	var n int
	if s := r.URL.Query().Get("lines"); s != "" {
		var err error
		n, err = strconv.Atoi(s)
		if err != nil {
			api.WriteBadRequest(w, "invalid lines parameter")
			return
		}
		if n <= 0 {
			api.WriteBadRequest(w, "lines must be a positive integer")
			return
		}
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolReadAuditLogWrapped(ctx, nil, ReadAuditLogInput{Lines: n})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// httpSlowQueryLog handles GET /api/slow-log?limit=20
func httpSlowQueryLog(w http.ResponseWriter, r *http.Request) {
	var n int
	if s := r.URL.Query().Get("limit"); s != "" {
		var err error
		n, err = strconv.Atoi(s)
		if err != nil {
			api.WriteBadRequest(w, "invalid limit parameter")
			return
		}
		if n <= 0 {
			api.WriteBadRequest(w, "limit must be a positive integer")
			return
		}
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolSlowQueryLogWrapped(ctx, nil, SlowQueryLogInput{Limit: n})
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
	if err := decodeJSONBody(w, r, &input); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			api.WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		api.WriteBadRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	ctx, cancel := httpContext(r)
	defer cancel()
	_, out, err := toolVectorSearchWrapped(ctx, nil, input)
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
	_, out, err := toolVectorInfoWrapped(ctx, nil, VectorInfoInput{Database: database})
	if err != nil {
		api.WriteInternalError(w, err.Error())
		return
	}
	api.WriteSuccess(w, out)
}

// ===== Utility HTTP Handlers =====

// httpHealth handles GET /health
func httpHealth(w http.ResponseWriter, r *http.Request) {
	api.WriteSuccess(w, healthStatus())
}

// healthStatus is the body of GET /health on the REST API and the MCP HTTP transport.
func healthStatus() map[string]interface{} {
	return map[string]interface{}{
		"status":  "healthy",
		"service": "mysql-mcp-server",
	}
}

// httpAPIIndex handles GET /api
func httpAPIIndex(w http.ResponseWriter, r *http.Request) {
	endpoints := map[string]string{
		"GET  /health":              "Health check",
		"GET  /api":                 "API index (this page)",
		"GET  /api/databases":       "List databases",
		"GET  /api/tables":          "List tables (requires ?database=)",
		"GET  /api/describe":        "Describe table (requires ?database=&table=)",
		"POST /api/query":           "Run SQL query (body: {sql, database?, max_rows?})",
		"GET  /api/ping":            "Ping database",
		"GET  /api/server-info":     "Get server info (optional ?detailed=1 for health metrics)",
		"GET  /api/connections":     "List connections",
		"POST /api/connections/use": "Switch connection (body: {name})",
		"GET  /api/metrics/tokens":  "Live token usage metrics (cumulative since startup)",
	}
	if extendedMode {
		endpoints["GET  /api/indexes"] = "List indexes (requires ?database=&table=) [extended]"
		endpoints["GET  /api/create-table"] = "Show CREATE TABLE (requires ?database=&table=) [extended]"
		endpoints["POST /api/explain"] = "Explain query (body: {sql, database?}) [extended]"
		endpoints["GET  /api/views"] = "List views (requires ?database=) [extended]"
		endpoints["GET  /api/triggers"] = "List triggers (requires ?database=) [extended]"
		endpoints["GET  /api/procedures"] = "List procedures (requires ?database=) [extended]"
		endpoints["GET  /api/functions"] = "List functions (requires ?database=) [extended]"
		endpoints["GET  /api/partitions"] = "List table partitions (requires ?database=&table=) [extended]"
		endpoints["GET  /api/size/database"] = "Database size (optional ?database=) [extended]"
		endpoints["GET  /api/size/tables"] = "Table sizes (requires ?database=) [extended]"
		endpoints["GET  /api/foreign-keys"] = "Foreign keys (requires ?database=, optional &table=) [extended]"
		endpoints["GET  /api/status"] = "Server status (optional ?pattern=) [extended]"
		endpoints["GET  /api/variables"] = "Server variables (optional ?pattern=) [extended]"
		if cfg.ProcessAdmin {
			endpoints["GET  /api/processlist"] = "Active threads [extended + MYSQL_MCP_PROCESS_ADMIN]"
			endpoints["POST /api/kill"] = "KILL QUERY for thread id (body: {id}) [extended + admin]"
		}
		readAuditOK := cfg.ReadAuditTool && auditLogger != nil && auditLogger.enabled && cfg.AuditLogPath != ""
		if readAuditOK {
			endpoints["GET  /api/audit-log"] = "Tail audit log (optional ?lines=) [extended + MYSQL_MCP_READ_AUDIT_TOOL]"
		}
		if cfg.SlowQueryTool {
			endpoints["GET  /api/slow-log"] = "Slow query log rows or settings [extended + MYSQL_MCP_SLOW_QUERY_TOOL]"
		}
	}
	if cfg.VectorMode {
		endpoints["POST /api/vector/search"] = "Vector search (body: {...}) [vector]"
		endpoints["GET  /api/vector/info"] = "Vector info (requires ?database=) [vector]"
	}
	if tokenCard {
		endpoints["GET  /status"] = "Token Tracking Card live dashboard [token-card]"
	}
	response := map[string]interface{}{
		"service":   "mysql-mcp-server REST API",
		"version":   Version,
		"endpoints": endpoints,
		"modes": map[string]bool{
			"extended":   extendedMode,
			"vector":     os.Getenv("MYSQL_MCP_VECTOR") == "1",
			"token_card": tokenCard,
		},
	}
	api.WriteSuccess(w, response)
}

// ===== Token Metrics HTTP Handlers =====

// httpMetricsTokens handles GET /api/metrics/tokens
// Returns cumulative token usage since server startup.
// Always available in HTTP mode; returns zeros when token tracking is disabled.
func httpMetricsTokens(w http.ResponseWriter, r *http.Request) {
	api.WriteSuccess(w, globalTokenMetrics.Snapshot())
}

// httpStatusPage handles GET /status
// Serves a live-updating HTML dashboard (the Token Tracking Card).
func httpStatusPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(tokenCardHTML))
}

// tokenCardHTML is the embedded HTML for the live Token Tracking Card dashboard.
// It auto-refreshes every 3 seconds via a JavaScript fetch of /api/metrics/tokens.
// #nosec G101 -- static HTML template, no credentials
const tokenCardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>MySQL MCP Server – Token Tracking</title>
<style>
  :root {
    --bg: #0f172a; --card: #1e293b; --border: #334155;
    --text: #f1f5f9; --muted: #94a3b8; --accent: #38bdf8;
    --green: #4ade80; --yellow: #facc15; --red: #f87171;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: var(--bg); color: var(--text); font-family: 'Segoe UI', system-ui, sans-serif; padding: 2rem; }
  h1 { font-size: 1.5rem; font-weight: 700; color: var(--accent); margin-bottom: 0.25rem; }
  .subtitle { color: var(--muted); font-size: 0.875rem; margin-bottom: 2rem; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 1rem; margin-bottom: 2rem; }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: 0.75rem; padding: 1.25rem; }
  .card .label { font-size: 0.75rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.5rem; }
  .card .value { font-size: 1.75rem; font-weight: 700; color: var(--text); }
  .card .sub { font-size: 0.75rem; color: var(--muted); margin-top: 0.25rem; }
  .badge { display: inline-block; padding: 0.2em 0.6em; border-radius: 9999px; font-size: 0.75rem; font-weight: 600; }
  .badge-on  { background: rgba(74,222,128,.15); color: var(--green); border: 1px solid rgba(74,222,128,.3); }
  .badge-off { background: rgba(248,113,113,.15); color: var(--red);   border: 1px solid rgba(248,113,113,.3); }
  .section-title { font-size: 1rem; font-weight: 600; color: var(--accent); margin-bottom: 0.75rem; }
  table { width: 100%; border-collapse: collapse; background: var(--card); border: 1px solid var(--border); border-radius: 0.75rem; overflow: hidden; }
  th { background: #0f172a; color: var(--muted); font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; padding: 0.75rem 1rem; text-align: left; }
  td { padding: 0.7rem 1rem; border-top: 1px solid var(--border); font-size: 0.875rem; }
  tr:hover td { background: rgba(56,189,248,.05); }
  .ts { color: var(--muted); font-size: 0.75rem; }
  .footer { margin-top: 1.5rem; color: var(--muted); font-size: 0.75rem; display: flex; align-items: center; gap: 0.5rem; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--green); animation: pulse 1.5s ease-in-out infinite; }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.4} }
  .warn { color: var(--yellow); font-size: 0.875rem; padding: 1rem; background: rgba(250,204,21,.08); border: 1px solid rgba(250,204,21,.2); border-radius: 0.5rem; margin-bottom: 1.5rem; }
</style>
</head>
<body>
<h1>⚡ MySQL MCP Server – Token Tracking Card</h1>
<p class="subtitle">Live cumulative token usage since server startup &mdash; refreshes every 3 s</p>

<div id="warn-box" class="warn" style="display:none">
  ⚠️ Token tracking is disabled. Enable it with <code>MYSQL_MCP_TOKEN_TRACKING=1</code>
  or <code>logging.token_tracking: true</code> in your config file to see live data.
</div>

<div class="grid" id="metrics-grid">
  <div class="card"><div class="label">Input Tokens</div><div class="value" id="v-in">–</div><div class="sub">cumulative</div></div>
  <div class="card"><div class="label">Output Tokens</div><div class="value" id="v-out">–</div><div class="sub">cumulative</div></div>
  <div class="card"><div class="label">Total Tokens</div><div class="value" id="v-tot">–</div><div class="sub">cumulative</div></div>
  <div class="card"><div class="label">Est. Cost (USD)</div><div class="value" id="v-cost">–</div><div class="sub">GPT-4o pricing</div></div>
  <div class="card"><div class="label">Queries</div><div class="value" id="v-qc">–</div><div class="sub">tool calls tracked</div></div>
  <div class="card"><div class="label">IO Efficiency</div><div class="value" id="v-eff">–</div><div class="sub">output / input ratio</div></div>
  <div class="card"><div class="label">Uptime</div><div class="value" id="v-up">–</div><div class="sub">since server start</div></div>
  <div class="card"><div class="label">Token Tracking</div><div class="value"><span id="v-status" class="badge">–</span></div><div class="sub">feature status</div></div>
</div>

<p class="section-title">Recent Queries (last 5)</p>
<table id="recent-table">
  <thead><tr><th>Tool</th><th>Input</th><th>Output</th><th>Total</th><th>Cost (USD)</th><th>Time</th></tr></thead>
  <tbody id="recent-body"><tr><td colspan="6" style="color:var(--muted);text-align:center">Loading…</td></tr></tbody>
</table>

<div class="footer"><div class="dot"></div><span id="last-update">Connecting…</span></div>

<script>
function fmt(n){return n===undefined?'–':n.toLocaleString();}
function fmtCost(n){return n===undefined?'–':'$'+n.toFixed(6);}
function fmtDur(s){if(s<60)return s+'s';if(s<3600)return Math.floor(s/60)+'m '+s%60+'s';return Math.floor(s/3600)+'h '+Math.floor((s%3600)/60)+'m';}
function fmtTime(iso){try{return new Date(iso).toLocaleTimeString();}catch{return iso;}}

async function refresh(){
  try{
    const r=await fetch('/api/metrics/tokens');
    if(!r.ok)throw new Error('HTTP '+r.status);
    const d=(await r.json()).data;
    document.getElementById('v-in').textContent=fmt(d.total_input_tokens);
    document.getElementById('v-out').textContent=fmt(d.total_output_tokens);
    document.getElementById('v-tot').textContent=fmt(d.total_tokens);
    document.getElementById('v-cost').textContent=fmtCost(d.total_cost_usd);
    document.getElementById('v-qc').textContent=fmt(d.query_count);
    document.getElementById('v-eff').textContent=d.io_efficiency===undefined?'–':d.io_efficiency.toFixed(2)+'x';
    document.getElementById('v-up').textContent=fmtDur(d.uptime_seconds);
    const sb=document.getElementById('v-status');
    if(d.token_tracking_on){sb.className='badge badge-on';sb.textContent='Enabled';}
    else{sb.className='badge badge-off';sb.textContent='Disabled';}
    document.getElementById('warn-box').style.display=d.token_tracking_on?'none':'block';
    const tbody=document.getElementById('recent-body');
    const rq=d.recent_queries||[];
    if(rq.length===0){tbody.innerHTML='<tr><td colspan="6" style="color:var(--muted);text-align:center">No queries recorded yet</td></tr>';}
    else{tbody.innerHTML=rq.slice().reverse().map(q=>'<tr><td>'+q.tool+'</td><td>'+fmt(q.input_tokens)+'</td><td>'+fmt(q.output_tokens)+'</td><td>'+fmt(q.total_tokens)+'</td><td>'+fmtCost(q.cost_usd)+'</td><td class="ts">'+fmtTime(q.timestamp)+'</td></tr>').join('');}
    document.getElementById('last-update').textContent='Last updated: '+new Date().toLocaleTimeString();
  }catch(e){document.getElementById('last-update').textContent='Error: '+e.message;}
}
refresh();
setInterval(refresh,3000);
</script>
</body>
</html>`

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
func startHTTPServer(port int, vectorMode bool, tokenCardEnabled bool) {
	mux := http.NewServeMux()

	// Create rate limiter if enabled
	var rateLimiter *api.RateLimiter
	if cfg.RateLimitEnabled {
		rateLimiter = api.NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)
		logInfo("rate limiting enabled", map[string]interface{}{
			"rps":   cfg.RateLimitRPS,
			"burst": cfg.RateLimitBurst,
		})
	}

	// Create logging middleware
	withLog := api.WithLogging(httpLogger)
	withRateLimit := api.WithRateLimit(rateLimiter)

	// Health and index
	mux.HandleFunc("/health", api.WithCORS(httpHealth))
	mux.HandleFunc("/api", api.WithCORS(httpAPIIndex))
	mux.HandleFunc("/api/", api.WithCORS(httpAPIIndex))

	// Token metrics endpoint (always available; returns zeros when token tracking is off)
	mux.HandleFunc("/api/metrics/tokens", api.WithCORS(httpMetricsTokens))

	// Token Card status page (only registered when enabled)
	if tokenCardEnabled {
		mux.HandleFunc("/status", httpStatusPage)
		logInfo("token card UI enabled", map[string]interface{}{
			"url": fmt.Sprintf("http://localhost:%d/status", port),
		})
	}

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
	mux.HandleFunc("/api/partitions", api.Chain(httpListPartitions, api.WithCORS, extendedFeature, api.RequireQueryParam("database"), api.RequireQueryParam("table")))
	mux.HandleFunc("/api/size/database", api.Chain(httpDatabaseSize, api.WithCORS, extendedFeature))
	mux.HandleFunc("/api/size/tables", api.Chain(httpTableSize, api.WithCORS, extendedFeature, api.RequireQueryParam("database")))
	mux.HandleFunc("/api/foreign-keys", api.Chain(httpForeignKeys, api.WithCORS, extendedFeature, api.RequireQueryParam("database")))
	mux.HandleFunc("/api/status", api.Chain(httpListStatus, api.WithCORS, extendedFeature))
	mux.HandleFunc("/api/variables", api.Chain(httpListVariables, api.WithCORS, extendedFeature))

	processAdminFeature := func(next http.HandlerFunc) http.HandlerFunc {
		return api.RequireFeature(cfg.ProcessAdmin, "process admin tools (set MYSQL_MCP_PROCESS_ADMIN=1)", next)
	}
	readAuditFeature := func(next http.HandlerFunc) http.HandlerFunc {
		ok := cfg.ReadAuditTool && auditLogger != nil && auditLogger.enabled && cfg.AuditLogPath != ""
		return api.RequireFeature(ok, "read_audit_log (MYSQL_MCP_READ_AUDIT_TOOL=1 and MYSQL_MCP_AUDIT_LOG)", next)
	}
	slowQueryFeature := func(next http.HandlerFunc) http.HandlerFunc {
		return api.RequireFeature(cfg.SlowQueryTool, "slow_query_log (set MYSQL_MCP_SLOW_QUERY_TOOL=1)", next)
	}
	mux.HandleFunc("/api/processlist", api.Chain(httpProcessList, api.WithCORS, extendedFeature, processAdminFeature))
	mux.HandleFunc("/api/kill", api.Chain(httpKillQuery, api.WithCORS, extendedFeature, processAdminFeature, api.RequirePOST))
	mux.HandleFunc("/api/audit-log", api.Chain(httpReadAuditLog, api.WithCORS, extendedFeature, readAuditFeature))
	mux.HandleFunc("/api/slow-log", api.Chain(httpSlowQueryLog, api.WithCORS, extendedFeature, slowQueryFeature))

	// Vector endpoints
	vectorFeature := func(next http.HandlerFunc) http.HandlerFunc {
		return api.RequireFeature(vectorMode, "vector mode (set MYSQL_MCP_VECTOR=1)", next)
	}
	mux.HandleFunc("/api/vector/search", api.Chain(httpVectorSearch, api.WithCORS, vectorFeature, api.RequirePOST))
	mux.HandleFunc("/api/vector/info", api.Chain(httpVectorInfo, api.WithCORS, vectorFeature, api.RequireQueryParam("database")))

	addr := fmt.Sprintf(":%d", port)

	// Build handler chain: rate limit -> logging -> mux
	var handler http.HandlerFunc = mux.ServeHTTP
	handler = withLog(handler)
	handler = withRateLimit(handler)

	// Create server with timeouts
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: cfg.HTTPRequestTimeout + 5*time.Second, // Slightly longer than request timeout
		IdleTimeout:  120 * time.Second,
	}

	logInfo("HTTP REST API server starting", map[string]interface{}{
		"port":         port,
		"address":      "http://localhost" + addr,
		"extendedMode": extendedMode,
		"vectorMode":   vectorMode,
		"version":      Version,
	})
	logInfo("REST API endpoints", map[string]interface{}{
		"api":           "http://localhost:" + strconv.Itoa(port) + "/api",
		"health":        "http://localhost:" + strconv.Itoa(port) + "/health",
		"token_metrics": "http://localhost:" + strconv.Itoa(port) + "/api/metrics/tokens",
	})
	if tokenCardEnabled {
		logInfo("token card dashboard", map[string]interface{}{
			"url": "http://localhost:" + strconv.Itoa(port) + "/status",
		})
	}

	if err := serveUntilSignal(server, "", ""); err != nil {
		log.Fatal(err)
	}

	// Stop rate limiter cleanup goroutine
	if rateLimiter != nil {
		rateLimiter.Stop()
	}
}

// startTokenMetricsHTTPServer listens on cfg.HTTPPort for /health, /api/metrics/tokens, and optionally /status
// while MCP runs on stdio in the same process (e.g. Claude Desktop). Set MYSQL_MCP_METRICS_HTTP=1.
// Does not serve the full REST API; use MYSQL_MCP_HTTP=1 for that (exclusive).
func startTokenMetricsHTTPServer(port int, tokenCardEnabled bool) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", api.WithCORS(httpHealth))
	mux.HandleFunc("/api/metrics/tokens", api.WithCORS(httpMetricsTokens))
	if tokenCardEnabled {
		mux.HandleFunc("/status", httpStatusPage)
	}

	index := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" && r.URL.Path != "/api/" {
			http.NotFound(w, r)
			return
		}
		// #nosec G101 -- route descriptions, no credentials
		endpoints := map[string]string{
			"GET  /health":             "Health check",
			"GET  /api":                "This index (metrics-only; MCP uses stdio)",
			"GET  /api/metrics/tokens": "Token usage (same process as MCP)",
		}
		if tokenCardEnabled {
			endpoints["GET  /status"] = "Token Tracking Card dashboard"
		}
		api.WriteSuccess(w, map[string]interface{}{
			"service":     "mysql-mcp-server",
			"mode":        "stdio_mcp_with_metrics_http",
			"version":     Version,
			"description": "HTTP exposes token metrics; MCP protocol uses stdin/stdout.",
			"endpoints":   endpoints,
		})
	}
	mux.HandleFunc("/api", api.WithCORS(index))
	mux.HandleFunc("/api/", api.WithCORS(index))

	addr := ":" + strconv.Itoa(port)
	handler := api.WithLogging(httpLogger)(mux.ServeHTTP)

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	logInfo("token metrics HTTP sidecar (stdio MCP)", map[string]interface{}{
		"address":    "http://127.0.0.1" + addr,
		"http_port":  port,
		"token_card": tokenCardEnabled,
	})
	if tokenCardEnabled {
		logInfo("token dashboard URL (same process as MCP)", map[string]interface{}{
			"url": fmt.Sprintf("http://127.0.0.1:%d/status", port),
		})
	}

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logError("token metrics HTTP sidecar failed", map[string]interface{}{"error": err.Error(), "port": port})
	}
}
