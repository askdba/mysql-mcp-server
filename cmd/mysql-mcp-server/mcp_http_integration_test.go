//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askdba/mysql-mcp-server/internal/config"
)

// TestMCPHTTPIntegrationCoreTools runs core tools over the Streamable HTTP transport against
// a real MySQL/MariaDB when MYSQL_TEST_DSN or MYSQL_DSN is set (see Makefile test-integration).
func TestMCPHTTPIntegrationCoreTools(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("MYSQL_DSN")
	}
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN or MYSQL_DSN not set")
	}

	c := &config.Config{
		Transport:             config.TransportHTTP,
		MCPHTTPBind:           config.DefaultMCPHTTPBind,
		MCPHTTPPath:           config.DefaultMCPHTTPPath,
		MCPHTTPToken:          "integration-token",
		MCPHTTPSessionTimeout: time.Minute,
		MaxRows:               config.DefaultMaxRows,
		QueryTimeout:          time.Duration(config.DefaultQueryTimeoutSecs) * time.Second,
		PingTimeout:           time.Duration(config.DefaultPingTimeoutSecs) * time.Second,
		MaxOpenConns:          config.DefaultMaxOpenConns,
		MaxIdleConns:          config.DefaultMaxIdleConns,
		ConnMaxLifetime:       time.Duration(config.DefaultConnMaxLifetimeMins) * time.Minute,
		ConnMaxIdleTime:       time.Duration(config.DefaultConnMaxIdleTimeMins) * time.Minute,
	}

	cm := NewConnectionManager()
	defer cm.Close()
	if err := cm.AddConnectionWithPoolConfig(config.ConnectionConfig{Name: "default", DSN: dsn}, c); err != nil {
		t.Fatalf("add connection: %v", err)
	}

	oldCfg, oldCM, oldAudit, oldMax, oldTimeout := cfg, connManager, auditLogger, maxRows, queryTimeout
	cfg, connManager, auditLogger, maxRows, queryTimeout = c, cm, &AuditLogger{}, c.MaxRows, c.QueryTimeout
	defer func() {
		cfg, connManager, auditLogger, maxRows, queryTimeout = oldCfg, oldCM, oldAudit, oldMax, oldTimeout
	}()

	// Fixture table in the DSN's default database.
	db := cm.GetActiveDB()
	ctx := context.Background()
	var dbName sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&dbName); err != nil || !dbName.Valid {
		t.Fatalf("DSN must select a database (err=%v)", err)
	}
	table := fmt.Sprintf("mcp_http_it_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE TABLE `"+table+"` (id INT PRIMARY KEY, name VARCHAR(32))"); err != nil {
		t.Fatalf("create fixture table: %v", err)
	}
	defer db.ExecContext(context.Background(), "DROP TABLE IF EXISTS `"+table+"`")
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+table+"` VALUES (1, 'one'), (2, 'two')"); err != nil {
		t.Fatalf("insert fixture rows: %v", err)
	}

	handler, err := newMCPHTTPHandler(buildMCPServer(), c, nil, false)
	if err != nil {
		t.Fatalf("newMCPHTTPHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close) // after the client session's cleanup closes its SSE stream

	f := &mcpHTTPFixture{srv: srv, cm: cm}
	cs := f.connect(t, newBearerClient("integration-token"))

	dbs := callTool[ListDatabasesOutput](t, cs, "list_databases", map[string]any{})
	found := false
	for _, d := range dbs.Databases {
		if d.Name == dbName.String {
			found = true
		}
	}
	if !found {
		t.Errorf("list_databases did not include %q: %+v", dbName.String, dbs.Databases)
	}

	desc := callTool[DescribeTableOutput](t, cs, "describe_table", map[string]any{"database": dbName.String, "table": table})
	if len(desc.Columns) != 2 {
		t.Errorf("describe_table columns = %d, want 2: %+v", len(desc.Columns), desc.Columns)
	}

	q := callTool[QueryResult](t, cs, "run_query", map[string]any{
		"sql":      "SELECT id, name FROM `" + table + "` ORDER BY id",
		"database": dbName.String,
	})
	if len(q.Rows) != 2 {
		t.Errorf("run_query rows = %d, want 2: %+v", len(q.Rows), q)
	}

	// Writes stay blocked over HTTP exactly as over stdio.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "run_query", Arguments: map[string]any{
		"sql": "DELETE FROM `" + table + "`", "database": dbName.String,
	}})
	if err == nil && !res.IsError {
		t.Error("expected run_query to reject DELETE over HTTP")
	}
}

// TestMCPHTTPIntegrationAddConnectionIsSessionScoped registers a connection over HTTP and
// checks that it is private to the session and closed when the session ends.
func TestMCPHTTPIntegrationAddConnectionIsSessionScoped(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("MYSQL_DSN")
	}
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN or MYSQL_DSN not set")
	}
	t.Setenv("MYSQL_MCP_ENABLE_ADD_CONNECTION", "1")

	c := &config.Config{
		Transport:             config.TransportHTTP,
		MCPHTTPBind:           config.DefaultMCPHTTPBind,
		MCPHTTPPath:           config.DefaultMCPHTTPPath,
		MCPHTTPSessionTimeout: time.Minute,
		ExtendedMode:          true,
		MaxRows:               config.DefaultMaxRows,
		QueryTimeout:          time.Duration(config.DefaultQueryTimeoutSecs) * time.Second,
		PingTimeout:           time.Duration(config.DefaultPingTimeoutSecs) * time.Second,
		MaxOpenConns:          config.DefaultMaxOpenConns,
		MaxIdleConns:          config.DefaultMaxIdleConns,
		ConnMaxLifetime:       time.Duration(config.DefaultConnMaxLifetimeMins) * time.Minute,
		ConnMaxIdleTime:       time.Duration(config.DefaultConnMaxIdleTimeMins) * time.Minute,
	}
	cm := NewConnectionManager()
	defer cm.Close()
	if err := cm.AddConnectionWithPoolConfig(config.ConnectionConfig{Name: "default", DSN: dsn}, c); err != nil {
		t.Fatalf("add connection: %v", err)
	}
	oldCfg, oldCM, oldAudit, oldMax, oldTimeout, oldExt := cfg, connManager, auditLogger, maxRows, queryTimeout, extendedMode
	cfg, connManager, auditLogger, maxRows, queryTimeout, extendedMode = c, cm, &AuditLogger{}, c.MaxRows, c.QueryTimeout, true
	defer func() {
		cfg, connManager, auditLogger, maxRows, queryTimeout, extendedMode = oldCfg, oldCM, oldAudit, oldMax, oldTimeout, oldExt
	}()

	handler, err := newMCPHTTPHandler(buildMCPServer(), c, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	f := &mcpHTTPFixture{srv: srv, cm: cm}

	// add_connection rejects root DSNs; CI and the Makefile use a non-root user.
	s1 := f.connect(t, nil)
	added := callTool[AddConnectionOutput](t, s1, "add_connection", map[string]any{"name": "extra", "dsn": dsn})
	if !added.Success || added.Active != "extra" {
		t.Fatalf("add_connection: %+v", added)
	}
	if got := callTool[ListConnectionsOutput](t, s1, "list_connections", map[string]any{}); got.Active != "extra" || len(got.Connections) != 2 {
		t.Errorf("session 1 list: %+v", got)
	}

	s2 := f.connect(t, nil)
	if got := callTool[ListConnectionsOutput](t, s2, "list_connections", map[string]any{}); len(got.Connections) != 1 || got.Active != "default" {
		t.Errorf("session 2 must not see session 1's connection: %+v", got)
	}
	if again := callTool[AddConnectionOutput](t, s2, "add_connection", map[string]any{"name": "extra", "dsn": dsn}); !again.Success {
		t.Errorf("session 2 should be able to reuse the name: %+v", again)
	}

	countSessionConns := func() int {
		n := 0
		for _, cc := range cm.List() {
			if isSessionConnKey(cc.Name) {
				n++
			}
		}
		return n
	}
	if n := countSessionConns(); n != 2 {
		t.Fatalf("session connections registered = %d, want 2", n)
	}
	_ = s1.Close()
	deadline := time.Now().Add(5 * time.Second)
	for countSessionConns() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("session 1's connection was not closed when the session ended (have %d)", countSessionConns())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
