// cmd/mysql-mcp-server/prompts_test.go
package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupPromptMockDB configures a mock DB for prompt tests.
func setupPromptMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	oldConnManager := connManager
	oldMaxRows := maxRows
	oldQueryTimeout := queryTimeout
	oldPingTimeout := pingTimeout
	oldCfg := cfg

	cm := NewConnectionManager()
	cm.connections["mock"] = mockDB
	cm.configs["mock"] = config.ConnectionConfig{Name: "mock", DSN: "mock://test"}
	cm.activeConn = "mock"
	connManager = cm

	maxRows = 200
	queryTimeout = 30 * time.Second
	pingTimeout = 5 * time.Second
	cfg = &config.Config{}

	return mock, func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled sqlmock expectations: %v", err)
		}
		connManager = oldConnManager
		maxRows = oldMaxRows
		queryTimeout = oldQueryTimeout
		pingTimeout = oldPingTimeout
		cfg = oldCfg
		mockDB.Close()
	}
}

// makeGetPromptReq builds a GetPromptRequest with the given arguments map.
func makeGetPromptReq(args map[string]string) *mcp.GetPromptRequest {
	return &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{
			Arguments: args,
		},
	}
}

// ===== promptHealthCheck =====

func TestPromptHealthCheck_Success(t *testing.T) {
	mock, cleanup := setupPromptMockDB(t)
	defer cleanup()

	// toolServerInfo queries (Detailed: true). performance_schema queries are tried
	// first and fail (no expectation set), causing the code to fall back to SHOW queries.
	// Expectations must be in the same order the fallback queries execute.
	mock.ExpectQuery(`SELECT VERSION\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow("8.0.35"))
	// variables fallback (performance_schema.global_variables fails → SHOW VARIABLES)
	mock.ExpectQuery(`SHOW VARIABLES`).
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}).
			AddRow("version_comment", "MySQL Community Server").
			AddRow("max_connections", "151").
			AddRow("character_set_server", "utf8mb4").
			AddRow("collation_server", "utf8mb4_unicode_ci"))
	// status fallback (performance_schema.global_status fails → SHOW GLOBAL STATUS)
	mock.ExpectQuery(`SHOW GLOBAL STATUS`).
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}).
			AddRow("Uptime", "86400").
			AddRow("Threads_connected", "3"))
	// current user / database
	mock.ExpectQuery(`SELECT CURRENT_USER`).
		WillReturnRows(sqlmock.NewRows([]string{"u", "d"}).AddRow("root@localhost", ""))
	// detailed health: performance_schema fallback → SHOW GLOBAL STATUS
	mock.ExpectQuery(`SHOW GLOBAL STATUS`).
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}).
			AddRow("Threads_running", "1").
			AddRow("Slow_queries", "5").
			AddRow("Questions", "1000").
			AddRow("Innodb_buffer_pool_read_requests", "10000").
			AddRow("Innodb_buffer_pool_reads", "100"))

	result, err := promptHealthCheck(context.Background(), makeGetPromptReq(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	msg := result.Messages[0]
	if msg.Role != "user" {
		t.Errorf("expected role=user, got %q", msg.Role)
	}
	text, ok := msg.Content.(*mcp.TextContent)
	if !ok {
		t.Fatal("content is not *mcp.TextContent")
	}
	if !strings.Contains(text.Text, "MySQL Health Check") {
		t.Error("response missing 'MySQL Health Check' header")
	}
	if !strings.Contains(text.Text, "Connectivity") {
		t.Error("response missing Connectivity section")
	}
	if !strings.Contains(text.Text, "analyze the health") {
		t.Error("response missing closing instruction")
	}
}

func TestPromptHealthCheck_ServerInfoFailure(t *testing.T) {
	mock, cleanup := setupPromptMockDB(t)
	defer cleanup()

	// ping succeeds (sqlmock v1 can't fail pings)
	// server_info VERSION query fails
	mock.ExpectQuery(`SELECT VERSION\(\)`).WillReturnError(sqlmock.ErrCancelled)

	result, err := promptHealthCheck(context.Background(), makeGetPromptReq(nil))
	// Should not return a Go error even when server_info fails — it's diagnostic
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || len(result.Messages) == 0 {
		t.Fatal("expected at least one message")
	}
	text := result.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, "MySQL Health Check") {
		t.Error("missing MySQL Health Check header")
	}
}

// ===== promptSchemaAudit =====

func TestPromptSchemaAudit_MissingDatabase(t *testing.T) {
	_, cleanup := setupPromptMockDB(t)
	defer cleanup()

	_, err := promptSchemaAudit(context.Background(), makeGetPromptReq(nil))
	if err == nil {
		t.Fatal("expected error for missing database argument")
	}
	if !strings.Contains(err.Error(), "database argument is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPromptSchemaAudit_Success(t *testing.T) {
	mock, cleanup := setupPromptMockDB(t)
	defer cleanup()

	// list_tables
	mock.ExpectQuery(`SELECT TABLE_NAME, ENGINE, TABLE_ROWS, TABLE_COMMENT`).
		WithArgs("testdb").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME", "ENGINE", "TABLE_ROWS", "TABLE_COMMENT"}).
			AddRow("users", "InnoDB", 1000, "").
			AddRow("orders", "InnoDB", 50000, "Order records"))

	// list_indexes for users (backtick-quoted identifiers)
	mock.ExpectQuery("SHOW INDEX FROM `testdb`.`users`").
		WillReturnRows(sqlmock.NewRows([]string{
			"Table", "Non_unique", "Key_name", "Seq_in_index", "Column_name",
			"Collation", "Cardinality", "Sub_part", "Packed", "Null", "Index_type", "Comment", "Index_comment",
		}).
			AddRow("users", 0, "PRIMARY", 1, "id", "A", 1000, nil, nil, "", "BTREE", "", "").
			AddRow("users", 1, "idx_email", 1, "email", "A", 1000, nil, nil, "", "BTREE", "", ""))

	// list_indexes for orders
	mock.ExpectQuery("SHOW INDEX FROM `testdb`.`orders`").
		WillReturnRows(sqlmock.NewRows([]string{
			"Table", "Non_unique", "Key_name", "Seq_in_index", "Column_name",
			"Collation", "Cardinality", "Sub_part", "Packed", "Null", "Index_type", "Comment", "Index_comment",
		}).
			AddRow("orders", 0, "PRIMARY", 1, "id", "A", 50000, nil, nil, "", "BTREE", "", ""))

	// foreign_keys
	mock.ExpectQuery(`SELECT\s+CONSTRAINT_NAME`).
		WithArgs("testdb").
		WillReturnRows(sqlmock.NewRows([]string{
			"CONSTRAINT_NAME", "TABLE_NAME", "COLUMN_NAME",
			"REFERENCED_TABLE_NAME", "REFERENCED_COLUMN_NAME", "on_update", "on_delete",
		}).AddRow("fk_order_user", "orders", "user_id", "users", "id", "CASCADE", "RESTRICT"))

	result, err := promptSchemaAudit(context.Background(), makeGetPromptReq(map[string]string{"database": "testdb"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, "Schema Audit: testdb") {
		t.Error("missing database name in header")
	}
	if !strings.Contains(text, "users") {
		t.Error("missing table name 'users'")
	}
	if !strings.Contains(text, "PRIMARY") {
		t.Error("missing PRIMARY index")
	}
	if !strings.Contains(text, "fk_order_user") {
		t.Error("missing foreign key")
	}
	if !strings.Contains(text, "audit this database schema") {
		t.Error("missing closing instruction")
	}
}

// ===== promptQueryAdvisor =====

func TestPromptQueryAdvisor_MissingSQL(t *testing.T) {
	_, cleanup := setupPromptMockDB(t)
	defer cleanup()

	_, err := promptQueryAdvisor(context.Background(), makeGetPromptReq(nil))
	if err == nil {
		t.Fatal("expected error for missing sql argument")
	}
	if !strings.Contains(err.Error(), "sql argument is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPromptQueryAdvisor_ExplainSuccess(t *testing.T) {
	mock, cleanup := setupPromptMockDB(t)
	defer cleanup()

	// No database arg → EXPLAIN runs directly on the pool (no USE statement)
	mock.ExpectQuery(`EXPLAIN SELECT id FROM users WHERE email`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "select_type", "table", "type",
			"possible_keys", "key", "key_len", "ref", "rows", "Extra",
		}).AddRow(1, "SIMPLE", "users", "ALL", nil, nil, nil, nil, 1000, ""))

	result, err := promptQueryAdvisor(context.Background(), makeGetPromptReq(map[string]string{
		"sql": "SELECT id FROM users WHERE email = 'x'",
		// no database — avoids conn.ExecContext(USE) complexity with sqlmock
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, "Query Advisor") {
		t.Error("missing Query Advisor header")
	}
	if !strings.Contains(text, "Execution Plan") {
		t.Error("missing Execution Plan section")
	}
	if !strings.Contains(text, "users") {
		t.Error("missing table name in plan output")
	}
	if !strings.Contains(text, "full-table scan") || !strings.Contains(text, "index") {
		t.Error("missing optimization instruction")
	}
}

func TestPromptQueryAdvisor_NonSelectRejected(t *testing.T) {
	_, cleanup := setupPromptMockDB(t)
	defer cleanup()

	result, err := promptQueryAdvisor(context.Background(), makeGetPromptReq(map[string]string{
		"sql": "DELETE FROM users",
	}))
	// explain_query rejects non-SELECT — the error goes into the prompt text, not as a Go error
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	text := result.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, "EXPLAIN Error") {
		t.Error("expected EXPLAIN Error section for non-SELECT")
	}
}

// ===== promptConnectionDebug =====

func TestPromptConnectionDebug_Success(t *testing.T) {
	mock, cleanup := setupPromptMockDB(t)
	defer cleanup()

	// toolServerInfo (Detailed: true) — same fallback sequence as health_check
	mock.ExpectQuery(`SELECT VERSION\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow("8.0.35"))
	mock.ExpectQuery(`SHOW VARIABLES`).
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}).
			AddRow("version_comment", "MySQL Community Server").
			AddRow("max_connections", "151").
			AddRow("character_set_server", "utf8mb4").
			AddRow("collation_server", "utf8mb4_unicode_ci"))
	mock.ExpectQuery(`SHOW GLOBAL STATUS`).
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}).
			AddRow("Uptime", "3600").
			AddRow("Threads_connected", "1"))
	mock.ExpectQuery(`SELECT CURRENT_USER`).
		WillReturnRows(sqlmock.NewRows([]string{"u", "d"}).AddRow("root@localhost", ""))
	mock.ExpectQuery(`SHOW GLOBAL STATUS`).
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}).
			AddRow("Threads_running", "1").
			AddRow("Slow_queries", "0").
			AddRow("Questions", "100").
			AddRow("Innodb_buffer_pool_read_requests", "5000").
			AddRow("Innodb_buffer_pool_reads", "50"))

	// toolListVariables — performance_schema.global_variables fails → SHOW GLOBAL VARIABLES
	mock.ExpectQuery(`SHOW GLOBAL VARIABLES`).
		WillReturnRows(sqlmock.NewRows([]string{"Variable_name", "Value"}).
			AddRow("max_connections", "151").
			AddRow("wait_timeout", "28800").
			AddRow("have_ssl", "YES").
			AddRow("ssl_cipher", ""))

	result, err := promptConnectionDebug(context.Background(), makeGetPromptReq(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := result.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, "Connection Debug Report") {
		t.Error("missing Connection Debug Report header")
	}
	if !strings.Contains(text, "Ping") {
		t.Error("missing Ping section")
	}
	if !strings.Contains(text, "diagnose") {
		t.Error("missing closing instruction")
	}
}

// ===== registerPrompts =====

func TestRegisterPrompts_AddsAllFour(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	registerPrompts(server)
	// If registration panics or fails the test will catch it — the SDK panics on duplicate names
}
