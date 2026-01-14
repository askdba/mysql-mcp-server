// cmd/mysql-mcp-server/tools_test.go
package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockDBResult holds the results of setupMockDB for tests that need to access the mock DB.
type mockDBResult struct {
	mock    sqlmock.Sqlmock
	mockDB  *sql.DB
	cleanup func()
}

// setupMockDB sets up a mock database and configures global state for testing.
// Uses connManager with a mock DB instead of the deprecated global db variable.
func setupMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	result := setupMockDBFull(t)
	return result.mock, result.cleanup
}

// setupMockDBFull returns the full mock result including the mock DB for tests
// that need to add it to additional connection managers.
func setupMockDBFull(t *testing.T) mockDBResult {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}

	// Save original state
	oldConnManager := connManager
	oldMaxRows := maxRows
	oldQueryTimeout := queryTimeout
	oldPingTimeout := pingTimeout

	// Set up mock connection manager with mock DB
	cm := NewConnectionManager()
	cm.connections["mock"] = mockDB
	cm.configs["mock"] = config.ConnectionConfig{Name: "mock", DSN: "mock://test"}
	cm.activeConn = "mock"
	connManager = cm

	maxRows = 1000
	queryTimeout = 30 * time.Second
	pingTimeout = time.Duration(config.DefaultPingTimeoutSecs) * time.Second

	cleanup := func() {
		connManager = oldConnManager
		maxRows = oldMaxRows
		queryTimeout = oldQueryTimeout
		pingTimeout = oldPingTimeout
		mockDB.Close()
	}

	return mockDBResult{mock: mock, mockDB: mockDB, cleanup: cleanup}
}

func TestToolListDatabases(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	// Set up expected query
	rows := sqlmock.NewRows([]string{"Database"}).
		AddRow("information_schema").
		AddRow("mysql").
		AddRow("testdb")
	mock.ExpectQuery("SHOW DATABASES").WillReturnRows(rows)

	// Call the tool
	ctx := context.Background()
	_, output, err := toolListDatabases(ctx, &mcp.CallToolRequest{}, ListDatabasesInput{})

	if err != nil {
		t.Fatalf("toolListDatabases failed: %v", err)
	}

	if len(output.Databases) != 3 {
		t.Errorf("expected 3 databases, got %d", len(output.Databases))
	}

	expectedDBs := []string{"information_schema", "mysql", "testdb"}
	for i, expected := range expectedDBs {
		if output.Databases[i].Name != expected {
			t.Errorf("expected database '%s', got '%s'", expected, output.Databases[i].Name)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListTablesSuccess(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Tables_in_testdb"}).
		AddRow("users").
		AddRow("orders").
		AddRow("products")
	mock.ExpectQuery("SHOW TABLES FROM `testdb`").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListTables(ctx, &mcp.CallToolRequest{}, ListTablesInput{Database: "testdb"})

	if err != nil {
		t.Fatalf("toolListTables failed: %v", err)
	}

	if len(output.Tables) != 3 {
		t.Errorf("expected 3 tables, got %d", len(output.Tables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListTablesEmptyDatabase(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolListTables(ctx, &mcp.CallToolRequest{}, ListTablesInput{Database: ""})

	if err == nil {
		t.Error("expected error for empty database")
	}
	if err.Error() != "database is required" {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolDescribeTableSuccess(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Field", "Type", "Collation", "Null", "Key", "Default", "Extra", "Privileges", "Comment"}).
		AddRow("id", "int", "", "NO", "PRI", "", "auto_increment", "select,insert,update,references", "").
		AddRow("name", "varchar(255)", "utf8mb4_general_ci", "NO", "", "", "", "select,insert,update,references", "User name").
		AddRow("email", "varchar(255)", "utf8mb4_general_ci", "YES", "UNI", "", "", "select,insert,update,references", "")

	mock.ExpectQuery("SHOW FULL COLUMNS FROM `testdb`.`users`").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolDescribeTable(ctx, &mcp.CallToolRequest{}, DescribeTableInput{
		Database: "testdb",
		Table:    "users",
	})

	if err != nil {
		t.Fatalf("toolDescribeTable failed: %v", err)
	}

	if len(output.Columns) != 3 {
		t.Errorf("expected 3 columns, got %d", len(output.Columns))
	}

	// Check first column
	if output.Columns[0].Name != "id" {
		t.Errorf("expected column name 'id', got '%s'", output.Columns[0].Name)
	}
	if output.Columns[0].Type != "int" {
		t.Errorf("expected type 'int', got '%s'", output.Columns[0].Type)
	}
	if output.Columns[0].Key != "PRI" {
		t.Errorf("expected key 'PRI', got '%s'", output.Columns[0].Key)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolDescribeTableWithNullCollation(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	// MySQL 8.4+ returns NULL for Collation on non-string columns
	rows := sqlmock.NewRows([]string{"Field", "Type", "Collation", "Null", "Key", "Default", "Extra", "Privileges", "Comment"}).
		AddRow("id", "int", nil, "NO", "PRI", nil, "auto_increment", "select,insert,update,references", nil).
		AddRow("created_at", "timestamp", nil, "YES", "", nil, "", "select,insert,update,references", nil).
		AddRow("name", "varchar(255)", "utf8mb4_general_ci", "NO", "", nil, "", "select,insert,update,references", "User name")

	mock.ExpectQuery("SHOW FULL COLUMNS FROM `testdb`.`users`").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolDescribeTable(ctx, &mcp.CallToolRequest{}, DescribeTableInput{
		Database: "testdb",
		Table:    "users",
	})

	if err != nil {
		t.Fatalf("toolDescribeTable failed with NULL collation: %v", err)
	}

	if len(output.Columns) != 3 {
		t.Errorf("expected 3 columns, got %d", len(output.Columns))
	}

	// Check that NULL collation is handled (returns empty string)
	if output.Columns[0].Collation != "" {
		t.Errorf("expected empty collation for int column, got '%s'", output.Columns[0].Collation)
	}
	if output.Columns[2].Collation != "utf8mb4_general_ci" {
		t.Errorf("expected 'utf8mb4_general_ci' collation, got '%s'", output.Columns[2].Collation)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolDescribeTableMissingDatabase(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolDescribeTable(ctx, &mcp.CallToolRequest{}, DescribeTableInput{
		Database: "",
		Table:    "users",
	})

	if err == nil {
		t.Error("expected error for missing database")
	}
	if err.Error() != "database is required" {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolDescribeTableMissingTable(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolDescribeTable(ctx, &mcp.CallToolRequest{}, DescribeTableInput{
		Database: "testdb",
		Table:    "",
	})

	if err == nil {
		t.Error("expected error for missing table")
	}
	if err.Error() != "table is required" {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQuerySelectSuccess(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "name", "email"}).
		AddRow(1, "Alice", "alice@example.com").
		AddRow(2, "Bob", "bob@example.com")

	mock.ExpectQuery("SELECT \\* FROM users").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "SELECT * FROM users",
	})

	if err != nil {
		t.Fatalf("toolRunQuery failed: %v", err)
	}

	if len(output.Columns) != 3 {
		t.Errorf("expected 3 columns, got %d", len(output.Columns))
	}

	if len(output.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(output.Rows))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQueryEmptySQL(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "",
	})

	if err == nil {
		t.Error("expected error for empty SQL")
	}
	if err.Error() != "sql is required" {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQueryBlockedQuery(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "DROP TABLE users",
	})

	if err == nil {
		t.Error("expected error for blocked query")
	}

	// Should be rejected by validator
	if err.Error() == "sql is required" {
		t.Error("should fail validation, not be empty")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQueryWithMaxRows(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		AddRow(2).
		AddRow(3).
		AddRow(4).
		AddRow(5)

	mock.ExpectQuery("SELECT id FROM numbers").WillReturnRows(rows)

	ctx := context.Background()
	maxRows := 3
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL:     "SELECT id FROM numbers",
		MaxRows: &maxRows,
	})

	if err != nil {
		t.Fatalf("toolRunQuery failed: %v", err)
	}

	// Should be limited to 3 rows
	if len(output.Rows) != 3 {
		t.Errorf("expected 3 rows (limited), got %d", len(output.Rows))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQueryWithDatabase(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	// Expect USE statement followed by SELECT
	mock.ExpectExec("USE `testdb`").WillReturnResult(sqlmock.NewResult(0, 0))
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "Alice")
	mock.ExpectQuery("SELECT \\* FROM users").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL:      "SELECT * FROM users",
		Database: "testdb",
	})

	if err != nil {
		t.Fatalf("toolRunQuery with database failed: %v", err)
	}

	if len(output.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(output.Rows))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQueryQueryError(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mock.ExpectQuery("SELECT \\* FROM nonexistent").WillReturnError(sqlmock.ErrCancelled)

	ctx := context.Background()
	_, _, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "SELECT * FROM nonexistent",
	})

	if err == nil {
		t.Fatal("expected error for query failure")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQueryInvalidDatabase(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL:      "SELECT * FROM users",
		Database: "test`db", // Invalid name with backtick
	})

	if err == nil {
		t.Fatal("expected error for invalid database name")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolPingSuccess(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mock.ExpectPing()

	ctx := context.Background()
	_, output, err := toolPing(ctx, &mcp.CallToolRequest{}, PingInput{})

	if err != nil {
		t.Fatalf("toolPing failed: %v", err)
	}

	if !output.Success {
		t.Error("expected ping to succeed")
	}
	if output.Message != "pong" {
		t.Errorf("expected message 'pong', got '%s'", output.Message)
	}
	if output.LatencyMs < 0 {
		t.Error("latency should be non-negative")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListConnectionsNoManager(t *testing.T) {
	// Save and restore global state
	oldConnManager := connManager
	defer func() { connManager = oldConnManager }()

	connManager = nil

	ctx := context.Background()
	_, _, err := toolListConnections(ctx, &mcp.CallToolRequest{}, ListConnectionsInput{})

	if err == nil {
		t.Error("expected error when connManager is nil")
	}
	if err.Error() != "connection manager not initialized" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestToolListConnectionsSuccess(t *testing.T) {
	result := setupMockDBFull(t)
	defer result.cleanup()

	// Set up connection manager with multiple connections using the mock DB
	cm := NewConnectionManager()
	cm.connections["test1"] = result.mockDB
	cm.configs["test1"] = config.ConnectionConfig{Name: "test1", DSN: "user:pass@tcp(localhost)/db1", Description: "Test 1"}
	cm.connections["test2"] = result.mockDB
	cm.configs["test2"] = config.ConnectionConfig{Name: "test2", DSN: "user:pass@tcp(localhost)/db2", Description: "Test 2"}
	cm.activeConn = "test1"
	connManager = cm

	ctx := context.Background()
	_, output, err := toolListConnections(ctx, &mcp.CallToolRequest{}, ListConnectionsInput{})

	if err != nil {
		t.Fatalf("toolListConnections failed: %v", err)
	}

	if len(output.Connections) != 2 {
		t.Errorf("expected 2 connections, got %d", len(output.Connections))
	}
	if output.Active != "test1" {
		t.Errorf("expected active 'test1', got '%s'", output.Active)
	}

	_ = result.mock
}

func TestToolUseConnectionNoManager(t *testing.T) {
	oldConnManager := connManager
	defer func() { connManager = oldConnManager }()

	connManager = nil

	ctx := context.Background()
	_, _, err := toolUseConnection(ctx, &mcp.CallToolRequest{}, UseConnectionInput{Name: "test"})

	if err == nil {
		t.Error("expected error when connManager is nil")
	}
}

func TestToolUseConnectionEmptyName(t *testing.T) {
	result := setupMockDBFull(t)
	defer result.cleanup()

	cm := NewConnectionManager()
	cm.connections["test"] = result.mockDB
	cm.configs["test"] = config.ConnectionConfig{Name: "test", DSN: "user:pass@tcp(localhost)/testdb", Description: "Test"}
	cm.activeConn = "test"
	connManager = cm

	ctx := context.Background()
	_, _, err := toolUseConnection(ctx, &mcp.CallToolRequest{}, UseConnectionInput{Name: ""})

	if err == nil {
		t.Error("expected error for empty name")
	}
	if err.Error() != "connection name is required" {
		t.Errorf("unexpected error: %v", err)
	}

	_ = result.mock
}

func TestToolUseConnectionSuccess(t *testing.T) {
	result := setupMockDBFull(t)
	defer result.cleanup()

	// Expect DATABASE() query after switch
	result.mock.ExpectQuery("SELECT DATABASE\\(\\)").WillReturnRows(
		sqlmock.NewRows([]string{"DATABASE()"}).AddRow("testdb"),
	)

	cm := NewConnectionManager()
	cm.connections["conn1"] = result.mockDB
	cm.configs["conn1"] = config.ConnectionConfig{Name: "conn1", DSN: "user:pass@tcp(localhost)/db1", Description: "Conn 1"}
	cm.connections["conn2"] = result.mockDB
	cm.configs["conn2"] = config.ConnectionConfig{Name: "conn2", DSN: "user:pass@tcp(localhost)/db2", Description: "Conn 2"}
	cm.activeConn = "conn1"
	connManager = cm

	ctx := context.Background()
	_, output, err := toolUseConnection(ctx, &mcp.CallToolRequest{}, UseConnectionInput{Name: "conn2"})

	if err != nil {
		t.Fatalf("toolUseConnection failed: %v", err)
	}

	if !output.Success {
		t.Error("expected success")
	}
	if output.Active != "conn2" {
		t.Errorf("expected active 'conn2', got '%s'", output.Active)
	}

	if err := result.mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolUseConnectionNotFound(t *testing.T) {
	result := setupMockDBFull(t)
	defer result.cleanup()

	cm := NewConnectionManager()
	cm.connections["conn1"] = result.mockDB
	cm.configs["conn1"] = config.ConnectionConfig{Name: "conn1", DSN: "user:pass@tcp(localhost)/db1", Description: "Conn 1"}
	cm.activeConn = "conn1"
	connManager = cm

	ctx := context.Background()
	_, output, err := toolUseConnection(ctx, &mcp.CallToolRequest{}, UseConnectionInput{Name: "nonexistent"})

	// Should not return error, but output.Success should be false
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Success {
		t.Error("expected success to be false")
	}

	_ = result.mock
}
