// cmd/mysql-mcp-server/tools_test.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/askdba/mysql-mcp-server/internal/dbretry"
	"github.com/askdba/mysql-mcp-server/internal/util"
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
	oldDBRetryCfg := dbRetryCfg

	// Set up mock connection manager with mock DB
	cm := NewConnectionManager()
	cm.connections["mock"] = mockDB
	cm.configs["mock"] = config.ConnectionConfig{Name: "mock", DSN: "mock://test"}
	cm.activeConn = "mock"
	connManager = cm

	maxRows = 1000
	queryTimeout = 30 * time.Second
	pingTimeout = time.Duration(config.DefaultPingTimeoutSecs) * time.Second
	dbRetryCfg = dbretry.DefaultConfig()

	cleanup := func() {
		connManager = oldConnManager
		maxRows = oldMaxRows
		queryTimeout = oldQueryTimeout
		pingTimeout = oldPingTimeout
		dbRetryCfg = oldDBRetryCfg
		mockDB.Close()
	}

	return mockDBResult{mock: mock, mockDB: mockDB, cleanup: cleanup}
}

func TestToolListDatabases(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	// Set up expected query
	rows := sqlmock.NewRows([]string{"schema_name"}).
		AddRow("information_schema").
		AddRow("mysql").
		AddRow("testdb")
	mock.ExpectQuery("SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME").WillReturnRows(rows)

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

	// New query fetches TABLE_NAME, ENGINE, TABLE_ROWS, TABLE_COMMENT
	rows := sqlmock.NewRows([]string{"TABLE_NAME", "ENGINE", "TABLE_ROWS", "TABLE_COMMENT"}).
		AddRow("users", "InnoDB", 100, "Users table").
		AddRow("orders", "InnoDB", 200, "Orders table").
		AddRow("products", "MyISAM", 50, "Products table")

	mock.ExpectQuery(`(?s)SELECT\s+TABLE_NAME\s*,\s*ENGINE\s*,\s*TABLE_ROWS\s*,\s*TABLE_COMMENT\s+FROM\s+information_schema\.TABLES\s+WHERE\s+TABLE_SCHEMA\s*=\s*\?\s+ORDER\s+BY\s+TABLE_NAME`).
		WithArgs("testdb").
		WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListTables(ctx, &mcp.CallToolRequest{}, ListTablesInput{Database: "testdb"})

	if err != nil {
		t.Fatalf("toolListTables failed: %v", err)
	}

	if len(output.Tables) != 3 {
		t.Errorf("expected 3 tables, got %d", len(output.Tables))
	}

	// Verify new fields
	if output.Tables[0].Name != "users" {
		t.Errorf("expected table 'users', got '%s'", output.Tables[0].Name)
	}
	if output.Tables[0].Engine != "InnoDB" {
		t.Errorf("expected engine 'InnoDB', got '%s'", output.Tables[0].Engine)
	}
	if output.Tables[0].Rows == nil || *output.Tables[0].Rows != 100 {
		t.Errorf("expected rows 100, got %v", output.Tables[0].Rows)
	}
	if output.Tables[0].Comment != "Users table" {
		t.Errorf("expected comment 'Users table', got '%s'", output.Tables[0].Comment)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListTablesMissingDatabase(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TABLE_NAME", "ENGINE", "TABLE_ROWS", "TABLE_COMMENT"})
	mock.ExpectQuery(`(?s)SELECT\s+TABLE_NAME\s*,\s*ENGINE\s*,\s*TABLE_ROWS\s*,\s*TABLE_COMMENT\s+FROM\s+information_schema\.TABLES\s+WHERE\s+TABLE_SCHEMA\s*=\s*\?\s+ORDER\s+BY\s+TABLE_NAME`).
		WithArgs("missingdb").
		WillReturnRows(rows)

	schemaRows := sqlmock.NewRows([]string{"1"})
	mock.ExpectQuery("SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = \\? LIMIT 1").
		WithArgs("missingdb").
		WillReturnRows(schemaRows)

	ctx := context.Background()
	_, _, err := toolListTables(ctx, &mcp.CallToolRequest{}, ListTablesInput{Database: "missingdb"})
	if err == nil {
		t.Fatal("expected error for missing database, got nil")
	}
	if err.Error() != "database not found: missingdb" {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListTablesEmptySchemaReturnsEmpty(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	mock.MatchExpectationsInOrder(false)

	rows := sqlmock.NewRows([]string{"TABLE_NAME", "ENGINE", "TABLE_ROWS", "TABLE_COMMENT"})
	mock.ExpectQuery(`(?s)SELECT\s+TABLE_NAME\s*,\s*ENGINE\s*,\s*TABLE_ROWS\s*,\s*TABLE_COMMENT\s+FROM\s+information_schema\.TABLES\s+WHERE\s+TABLE_SCHEMA\s*=\s*\?\s+ORDER\s+BY\s+TABLE_NAME`).
		WithArgs("emptydb").
		WillReturnRows(rows)

	schemaRows := sqlmock.NewRows([]string{"1"}).AddRow(1)
	mock.ExpectQuery("SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = \\? LIMIT 1").
		WithArgs("emptydb").
		WillReturnRows(schemaRows)

	ctx := context.Background()
	_, output, err := toolListTables(ctx, &mcp.CallToolRequest{}, ListTablesInput{Database: "emptydb"})
	if err != nil {
		t.Fatalf("expected empty list, got error: %v", err)
	}
	if len(output.Tables) != 0 {
		t.Fatalf("expected 0 tables, got %d", len(output.Tables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListTablesNullMetadata(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TABLE_NAME", "ENGINE", "TABLE_ROWS", "TABLE_COMMENT"}).
		AddRow("audit_log", nil, nil, nil)
	mock.ExpectQuery(`(?s)SELECT\s+TABLE_NAME\s*,\s*ENGINE\s*,\s*TABLE_ROWS\s*,\s*TABLE_COMMENT\s+FROM\s+information_schema\.TABLES\s+WHERE\s+TABLE_SCHEMA\s*=\s*\?\s+ORDER\s+BY\s+TABLE_NAME`).
		WithArgs("testdb").
		WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListTables(ctx, &mcp.CallToolRequest{}, ListTablesInput{Database: "testdb"})
	if err != nil {
		t.Fatalf("toolListTables failed: %v", err)
	}
	if len(output.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(output.Tables))
	}
	if output.Tables[0].Engine != "" {
		t.Errorf("expected empty engine, got '%s'", output.Tables[0].Engine)
	}
	if output.Tables[0].Rows != nil {
		t.Errorf("expected nil rows, got %v", output.Tables[0].Rows)
	}
	if output.Tables[0].Comment != "" {
		t.Errorf("expected empty comment, got '%s'", output.Tables[0].Comment)
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

	// New query fetches 8 columns: COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT, EXTRA, COLUMN_COMMENT, COLLATION_NAME
	rows := sqlmock.NewRows([]string{"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT", "EXTRA", "COLUMN_COMMENT", "COLLATION_NAME"}).
		AddRow("id", "int", "NO", "PRI", nil, "auto_increment", "", nil).
		AddRow("name", "varchar(255)", "YES", "UNI", nil, "", "User name", "utf8mb4_unicode_ci").
		AddRow("email", "varchar(255)", "YES", "", nil, "", "", "utf8mb4_unicode_ci")

	mock.ExpectQuery(`(?s)SELECT\s+COLUMN_NAME\s*,\s*COLUMN_TYPE\s*,\s*IS_NULLABLE\s*,\s*COLUMN_KEY\s*,\s*COLUMN_DEFAULT\s*,\s*EXTRA\s*,\s*COLUMN_COMMENT\s*,\s*COLLATION_NAME\s+FROM\s+information_schema\.COLUMNS\s+WHERE\s+TABLE_SCHEMA\s*=\s*\?\s+AND\s+TABLE_NAME\s*=\s*\?\s+ORDER\s+BY\s+ORDINAL_POSITION`).
		WithArgs("testdb", "users").
		WillReturnRows(rows)

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

func TestToolDescribeTableNonExistentTable(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT", "EXTRA", "COLUMN_COMMENT", "COLLATION_NAME"})
	mock.ExpectQuery(`(?s)SELECT\s+COLUMN_NAME\s*,\s*COLUMN_TYPE\s*,\s*IS_NULLABLE\s*,\s*COLUMN_KEY\s*,\s*COLUMN_DEFAULT\s*,\s*EXTRA\s*,\s*COLUMN_COMMENT\s*,\s*COLLATION_NAME\s+FROM\s+information_schema\.COLUMNS\s+WHERE\s+TABLE_SCHEMA\s*=\s*\?\s+AND\s+TABLE_NAME\s*=\s*\?\s+ORDER\s+BY\s+ORDINAL_POSITION`).
		WithArgs("testdb", "missing").
		WillReturnRows(rows)

	tableRows := sqlmock.NewRows([]string{"1"})
	mock.ExpectQuery("SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = \\? AND TABLE_NAME = \\? LIMIT 1").
		WithArgs("testdb", "missing").
		WillReturnRows(tableRows)

	schemaRows := sqlmock.NewRows([]string{"1"}).AddRow(1)
	mock.ExpectQuery("SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = \\? LIMIT 1").
		WithArgs("testdb").
		WillReturnRows(schemaRows)

	ctx := context.Background()
	_, _, err := toolDescribeTable(ctx, &mcp.CallToolRequest{}, DescribeTableInput{
		Database: "testdb",
		Table:    "missing",
	})
	if err == nil {
		t.Fatal("expected error for missing table, got nil")
	}
	if err.Error() != "table not found: testdb.missing" {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolDescribeTableNoColumnsFound(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT", "EXTRA", "COLUMN_COMMENT", "COLLATION_NAME"})
	mock.ExpectQuery(`(?s)SELECT\s+COLUMN_NAME\s*,\s*COLUMN_TYPE\s*,\s*IS_NULLABLE\s*,\s*COLUMN_KEY\s*,\s*COLUMN_DEFAULT\s*,\s*EXTRA\s*,\s*COLUMN_COMMENT\s*,\s*COLLATION_NAME\s+FROM\s+information_schema\.COLUMNS\s+WHERE\s+TABLE_SCHEMA\s*=\s*\?\s+AND\s+TABLE_NAME\s*=\s*\?\s+ORDER\s+BY\s+ORDINAL_POSITION`).
		WithArgs("testdb", "empty_table").
		WillReturnRows(rows)

	tableRows := sqlmock.NewRows([]string{"1"}).AddRow(1)
	mock.ExpectQuery("SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = \\? AND TABLE_NAME = \\? LIMIT 1").
		WithArgs("testdb", "empty_table").
		WillReturnRows(tableRows)

	ctx := context.Background()
	_, _, err := toolDescribeTable(ctx, &mcp.CallToolRequest{}, DescribeTableInput{
		Database: "testdb",
		Table:    "empty_table",
	})
	if err == nil {
		t.Fatal("expected error for table with no columns, got nil")
	}
	if err.Error() != "no columns found for table: testdb.empty_table" {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolDescribeTableDatabaseNotFound(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT", "EXTRA", "COLUMN_COMMENT", "COLLATION_NAME"})
	mock.ExpectQuery(`(?s)SELECT\s+COLUMN_NAME\s*,\s*COLUMN_TYPE\s*,\s*IS_NULLABLE\s*,\s*COLUMN_KEY\s*,\s*COLUMN_DEFAULT\s*,\s*EXTRA\s*,\s*COLUMN_COMMENT\s*,\s*COLLATION_NAME\s+FROM\s+information_schema\.COLUMNS\s+WHERE\s+TABLE_SCHEMA\s*=\s*\?\s+AND\s+TABLE_NAME\s*=\s*\?\s+ORDER\s+BY\s+ORDINAL_POSITION`).
		WithArgs("missingdb", "users").
		WillReturnRows(rows)

	tableRows := sqlmock.NewRows([]string{"1"})
	mock.ExpectQuery("SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = \\? AND TABLE_NAME = \\? LIMIT 1").
		WithArgs("missingdb", "users").
		WillReturnRows(tableRows)

	schemaRows := sqlmock.NewRows([]string{"1"})
	mock.ExpectQuery("SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = \\? LIMIT 1").
		WithArgs("missingdb").
		WillReturnRows(schemaRows)

	ctx := context.Background()
	_, _, err := toolDescribeTable(ctx, &mcp.CallToolRequest{}, DescribeTableInput{
		Database: "missingdb",
		Table:    "users",
	})
	if err == nil {
		t.Fatal("expected error for missing database, got nil")
	}
	if err.Error() != "database not found: missingdb" {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolDescribeTableWithNullCollation(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	// MySQL 8.4+ returns NULL for Collation on non-string columns
	rows := sqlmock.NewRows([]string{"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE", "COLUMN_KEY", "COLUMN_DEFAULT", "EXTRA", "COLUMN_COMMENT", "COLLATION_NAME"}).
		AddRow("id", "int", "NO", "PRI", nil, "auto_increment", "", nil).
		AddRow("created_at", "timestamp", "YES", "", nil, "", "", nil).
		AddRow("name", "varchar(255)", "NO", "", nil, "", "User name", "utf8mb4_unicode_ci")

	mock.ExpectQuery(`(?s)SELECT\s+COLUMN_NAME\s*,\s*COLUMN_TYPE\s*,\s*IS_NULLABLE\s*,\s*COLUMN_KEY\s*,\s*COLUMN_DEFAULT\s*,\s*EXTRA\s*,\s*COLUMN_COMMENT\s*,\s*COLLATION_NAME\s+FROM\s+information_schema\.COLUMNS\s+WHERE\s+TABLE_SCHEMA\s*=\s*\?\s+AND\s+TABLE_NAME\s*=\s*\?\s+ORDER\s+BY\s+ORDINAL_POSITION`).
		WithArgs("testdb", "users").
		WillReturnRows(rows)

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
	if output.Columns[2].Collation != "utf8mb4_unicode_ci" {
		t.Errorf("expected 'utf8mb4_unicode_ci' collation, got '%s'", output.Columns[2].Collation)
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
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "Alice")
	mock.ExpectExec("USE `testdb`").WillReturnResult(sqlmock.NewResult(0, 0))
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

// TestToolRunQuerySystemDatabaseBypass verifies that passing database: "mysql"
// (or other blocked schemas) with an unqualified SQL query is rejected before
// any DB call is made — the SQL validator cannot see "mysql." in the query text.
func TestToolRunQuerySystemDatabaseBypass(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	// mysql is always blocked
	_, _, err := toolRunQuery(context.Background(), &mcp.CallToolRequest{}, RunQueryInput{
		SQL:      "SELECT * FROM user",
		Database: "mysql",
	})
	if err == nil {
		t.Fatal("expected database: mysql to be rejected")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled mock expectations (DB was called): %v", err)
	}

	// diagnostic schemas blocked by default
	for _, db := range []string{"information_schema", "performance_schema", "sys"} {
		_, _, err := toolRunQuery(context.Background(), &mcp.CallToolRequest{}, RunQueryInput{
			SQL:      "SELECT * FROM STATISTICS",
			Database: db,
		})
		if err == nil {
			t.Errorf("expected database: %q to be rejected when flag is off", db)
		}
	}
}

// TestToolRunQuerySystemDatabaseOptIn verifies that diagnostic schemas are
// accessible via the database parameter when MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS=1,
// while mysql stays blocked.
func TestToolRunQuerySystemDatabaseOptIn(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	util.SetAllowSystemSchemas(true)
	defer util.SetAllowSystemSchemas(false)

	// information_schema allowed — query should reach the DB (USE + SELECT both expected)
	mock.ExpectExec("USE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME"}).AddRow("users"))
	_, _, err := toolRunQuery(context.Background(), &mcp.CallToolRequest{}, RunQueryInput{
		SQL:      "SELECT TABLE_NAME FROM information_schema.TABLES",
		Database: "information_schema",
	})
	if err != nil {
		t.Errorf("expected information_schema to be accessible with flag on, got: %v", err)
	}

	// mysql still blocked even with flag on — no DB call expected
	_, _, err = toolRunQuery(context.Background(), &mcp.CallToolRequest{}, RunQueryInput{
		SQL:      "SELECT * FROM user",
		Database: "mysql",
	})
	if err == nil {
		t.Error("expected mysql to remain blocked even with flag on")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled mock expectations: %v", err)
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
func TestToolServerInfoFallback(t *testing.T) {
	result := setupMockDBFull(t)
	defer result.cleanup()

	mock := result.mock
	// Set server type to MariaDB for this test
	connManager.serverTypes["mock"] = ServerTypeMariaDB

	// 1. Mock VERSION() query
	mock.ExpectQuery("SELECT VERSION\\(\\)").WillReturnRows(
		sqlmock.NewRows([]string{"VERSION()"}).AddRow("11.4.2-MariaDB"),
	)

	// 2. Mock performance_schema.global_variables FAILURE
	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_variables").
		WillReturnError(fmt.Errorf("Table 'performance_schema.global_variables' doesn't exist"))

	// 3. Mock SHOW VARIABLES FALLBACK
	varRows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("version_comment", "mariadb.org binary distribution").
		AddRow("character_set_server", "utf8mb4").
		AddRow("collation_server", "utf8mb4_unicode_ci").
		AddRow("max_connections", "151")
	mock.ExpectQuery("SHOW VARIABLES WHERE Variable_name IN").WillReturnRows(varRows)

	// 4. Mock performance_schema.global_status FAILURE
	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status").
		WillReturnError(fmt.Errorf("Table 'performance_schema.global_status' doesn't exist"))

	// 5. Mock SHOW GLOBAL STATUS FALLBACK
	statusRows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("Uptime", "3600").
		AddRow("Threads_connected", "5")
	mock.ExpectQuery("SHOW GLOBAL STATUS WHERE Variable_name IN").WillReturnRows(statusRows)

	// 6. Mock final info query
	mock.ExpectQuery("SELECT CURRENT_USER\\(\\), IFNULL\\(DATABASE\\(\\), ''\\)").WillReturnRows(
		sqlmock.NewRows([]string{"CURRENT_USER()", "DATABASE()"}).AddRow("root@localhost", "testdb"),
	)

	ctx := context.Background()
	_, output, err := toolServerInfo(ctx, &mcp.CallToolRequest{}, ServerInfoInput{})

	if err != nil {
		t.Fatalf("toolServerInfo failed unexpectedly during fallback: %v", err)
	}

	// Verify the output got populated via fallbacks
	if output.Version != "11.4.2-MariaDB" {
		t.Errorf("expected version 11.4.2-MariaDB, got %s", output.Version)
	}
	if output.ServerEngine != "mariadb" {
		t.Errorf("expected engine mariadb, got %s", output.ServerEngine)
	}
	if output.VersionComment != "mariadb.org binary distribution" {
		t.Errorf("expected comment, got %s", output.VersionComment)
	}
	if output.Uptime != 3600 {
		t.Errorf("expected uptime 3600, got %d", output.Uptime)
	}
	if output.ThreadsConnected != 5 {
		t.Errorf("expected threads 5, got %d", output.ThreadsConnected)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== Tests for performance improvement features =====

// Regression: negative MYSQL_MAX_ROWS / maxRows must not panic on slice prealloc (Codex P2).
func TestToolRunQueryNegativeMaxRowsDoesNotPanic(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	oldMaxRows := maxRows
	maxRows = -1
	defer func() { maxRows = oldMaxRows }()

	mock.ExpectQuery("SELECT id FROM t").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	ctx := context.Background()
	_, _, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "SELECT id FROM t",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestToolRunQueryTruncatedFlag(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	// Set a small maxRows so truncation is triggered
	oldMaxRows := maxRows
	maxRows = 2
	defer func() { maxRows = oldMaxRows }()

	// Return 5 rows but only read 2
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		AddRow(2).
		AddRow(3).
		AddRow(4).
		AddRow(5)

	// With LIMIT injection, the query will have LIMIT 2 appended
	mock.ExpectQuery("SELECT id FROM t LIMIT 2").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "SELECT id FROM t",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(output.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(output.Rows))
	}

	if !output.Truncated {
		t.Error("expected Truncated=true when row limit was hit")
	}
}

func TestToolRunQueryNotTruncatedWhenResultMatchesLimitExactly(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	oldMaxRows := maxRows
	maxRows = 2
	defer func() { maxRows = oldMaxRows }()

	// Exactly two rows: no third row exists, so Truncated must stay false.
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		AddRow(2)

	mock.ExpectQuery("SELECT id FROM t LIMIT 2").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "SELECT id FROM t",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(output.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(output.Rows))
	}

	if output.Truncated {
		t.Error("expected Truncated=false when result count equals the limit and no further rows exist")
	}
}

func TestToolRunQueryNotTruncated(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		AddRow(2)

	mock.ExpectQuery("SELECT id FROM t LIMIT 1000").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "SELECT id FROM t",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(output.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(output.Rows))
	}

	if output.Truncated {
		t.Error("expected Truncated=false when all rows were returned")
	}
}

func TestToolRunQuerySelectStarWarning(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "Alice")
	mock.ExpectQuery("SELECT \\* FROM users LIMIT 1000").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "SELECT * FROM users",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Warning == "" {
		t.Error("expected a warning when SELECT * is used")
	}
}

func TestToolRunQueryNoWarningForSpecificColumns(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "Alice")
	mock.ExpectQuery("SELECT id, name FROM users LIMIT 1000").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "SELECT id, name FROM users",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Warning != "" {
		t.Errorf("expected no warning for specific column selection, got: %q", output.Warning)
	}
}

func TestToolRunQueryLimitNotInjectedWhenPresent(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2)
	// Query already has LIMIT 5 - should not get another LIMIT appended
	mock.ExpectQuery("SELECT id FROM t LIMIT 5").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL: "SELECT id FROM t LIMIT 5",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(output.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(output.Rows))
	}
}

func TestToolRunQueryOffsetPaginationHasMore(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	offset := 0
	maxRowsArg := 3
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		AddRow(2).
		AddRow(3).
		AddRow(4)

	mock.ExpectQuery("SELECT id FROM t ORDER BY id LIMIT 4 OFFSET 0").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL:     "SELECT id FROM t ORDER BY id",
		MaxRows: &maxRowsArg,
		Offset:  &offset,
	})

	if err != nil {
		t.Fatalf("toolRunQuery failed: %v", err)
	}
	if len(output.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(output.Rows))
	}
	if !output.HasMore {
		t.Error("expected HasMore when a fourth row exists")
	}
	if output.NextOffset == nil || *output.NextOffset != 3 {
		t.Errorf("expected NextOffset 3, got %v", output.NextOffset)
	}
	if output.Truncated {
		t.Error("pagination should use HasMore, not Truncated")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQueryOffsetPaginationNextPage(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	offset := 3
	maxRowsArg := 3
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(4).
		AddRow(5).
		AddRow(6)

	mock.ExpectQuery("SELECT id FROM t ORDER BY id LIMIT 4 OFFSET 3").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL:     "SELECT id FROM t ORDER BY id",
		MaxRows: &maxRowsArg,
		Offset:  &offset,
	})

	if err != nil {
		t.Fatalf("toolRunQuery failed: %v", err)
	}
	if len(output.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(output.Rows))
	}
	if output.HasMore {
		t.Error("expected HasMore=false on last page")
	}
	if output.NextOffset != nil {
		t.Errorf("expected nil NextOffset, got %v", output.NextOffset)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQueryOffsetNegative(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	bad := -1
	ctx := context.Background()
	_, _, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL:    "SELECT 1",
		Offset: &bad,
	})
	if err == nil {
		t.Fatal("expected error for negative offset")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// Regression: MYSQL_MAX_ROWS=0 with offset must not return next_offset equal to offset (Codex P2).
func TestToolRunQueryOffsetPaginationRequiresPositiveLimit(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	oldMaxRows := maxRows
	maxRows = 0
	defer func() { maxRows = oldMaxRows }()

	off := 0
	ctx := context.Background()
	_, _, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL:    "SELECT id FROM t ORDER BY id",
		Offset: &off,
	})
	if err == nil {
		t.Fatal("expected error when offset pagination is used with non-positive row limit")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolRunQueryPaginationRequiresSelect(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()

	off := 0
	ctx := context.Background()
	_, _, err := toolRunQuery(ctx, &mcp.CallToolRequest{}, RunQueryInput{
		SQL:    "SHOW TABLES",
		Offset: &off,
	})
	if err == nil {
		t.Fatal("expected error when offset is used with non-SELECT")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
