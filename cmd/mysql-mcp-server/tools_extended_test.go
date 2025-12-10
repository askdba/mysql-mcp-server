// cmd/mysql-mcp-server/tools_extended_test.go
package main

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupExtendedMockDB sets up a mock database for extended tool tests
func setupExtendedMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}

	// Save original state
	oldConnManager := connManager
	oldDB := db
	oldMaxRows := maxRows
	oldQueryTimeout := queryTimeout

	// Set up global state
	db = mockDB
	connManager = nil
	maxRows = 1000
	queryTimeout = 30 * time.Second

	cleanup := func() {
		connManager = oldConnManager
		db = oldDB
		maxRows = oldMaxRows
		queryTimeout = oldQueryTimeout
		mockDB.Close()
	}

	return mock, cleanup
}

// ===== toolListIndexes Tests =====

func TestToolListIndexesSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"Table", "Non_unique", "Key_name", "Seq_in_index", "Column_name",
		"Collation", "Cardinality", "Sub_part", "Packed", "Null", "Index_type",
		"Comment", "Index_comment",
	}).
		AddRow("users", 0, "PRIMARY", 1, "id", "A", 100, nil, nil, "", "BTREE", "", "").
		AddRow("users", 1, "idx_name", 1, "name", "A", 50, nil, nil, "YES", "BTREE", "", "")

	mock.ExpectQuery("SHOW INDEX FROM `testdb`.`users`").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListIndexes(ctx, &mcp.CallToolRequest{}, ListIndexesInput{
		Database: "testdb",
		Table:    "users",
	})

	if err != nil {
		t.Fatalf("toolListIndexes failed: %v", err)
	}

	if len(output.Indexes) != 2 {
		t.Errorf("expected 2 indexes, got %d", len(output.Indexes))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListIndexesMissingDatabase(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolListIndexes(ctx, &mcp.CallToolRequest{}, ListIndexesInput{
		Database: "",
		Table:    "users",
	})

	if err == nil {
		t.Error("expected error for missing database")
	}
	if err.Error() != "database and table are required" {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListIndexesMissingTable(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolListIndexes(ctx, &mcp.CallToolRequest{}, ListIndexesInput{
		Database: "testdb",
		Table:    "",
	})

	if err == nil {
		t.Error("expected error for missing table")
	}
	if err.Error() != "database and table are required" {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolShowCreateTable Tests =====

func TestToolShowCreateTableSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Table", "Create Table"}).
		AddRow("users", "CREATE TABLE `users` (\n  `id` int NOT NULL AUTO_INCREMENT,\n  PRIMARY KEY (`id`)\n)")

	mock.ExpectQuery("SHOW CREATE TABLE `testdb`.`users`").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolShowCreateTable(ctx, &mcp.CallToolRequest{}, ShowCreateTableInput{
		Database: "testdb",
		Table:    "users",
	})

	if err != nil {
		t.Fatalf("toolShowCreateTable failed: %v", err)
	}

	if output.CreateStatement == "" {
		t.Error("expected non-empty create statement")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolShowCreateTableMissingInputs(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	tests := []struct {
		name     string
		database string
		table    string
	}{
		{"missing database", "", "users"},
		{"missing table", "testdb", ""},
		{"missing both", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, _, err := toolShowCreateTable(ctx, &mcp.CallToolRequest{}, ShowCreateTableInput{
				Database: tt.database,
				Table:    tt.table,
			})

			if err == nil {
				t.Error("expected error for missing inputs")
			}
		})
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolExplainQuery Tests =====

func TestToolExplainQuerySuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "select_type", "table", "type", "possible_keys", "key", "key_len", "ref", "rows", "Extra"}).
		AddRow(1, "SIMPLE", "users", "ALL", nil, nil, nil, nil, 100, "")

	mock.ExpectQuery("EXPLAIN SELECT \\* FROM users").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolExplainQuery(ctx, &mcp.CallToolRequest{}, ExplainQueryInput{
		SQL: "SELECT * FROM users",
	})

	if err != nil {
		t.Fatalf("toolExplainQuery failed: %v", err)
	}

	if len(output.Plan) == 0 {
		t.Error("expected non-empty plan")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolExplainQueryEmptySQL(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolExplainQuery(ctx, &mcp.CallToolRequest{}, ExplainQueryInput{
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

func TestToolExplainQueryNonSelect(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolExplainQuery(ctx, &mcp.CallToolRequest{}, ExplainQueryInput{
		SQL: "INSERT INTO users (name) VALUES ('test')",
	})

	if err == nil {
		t.Error("expected error for non-SELECT query")
	}
	if err.Error() != "only SELECT statements can be explained" {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolListViews Tests =====

func TestToolListViewsSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TABLE_NAME", "DEFINER", "SECURITY_TYPE", "IS_UPDATABLE"}).
		AddRow("user_view", "root@localhost", "DEFINER", "YES").
		AddRow("order_view", "root@localhost", "INVOKER", "NO")

	mock.ExpectQuery("SELECT TABLE_NAME, DEFINER, SECURITY_TYPE, IS_UPDATABLE").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListViews(ctx, &mcp.CallToolRequest{}, ListViewsInput{
		Database: "testdb",
	})

	if err != nil {
		t.Fatalf("toolListViews failed: %v", err)
	}

	if len(output.Views) != 2 {
		t.Errorf("expected 2 views, got %d", len(output.Views))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListViewsMissingDatabase(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolListViews(ctx, &mcp.CallToolRequest{}, ListViewsInput{
		Database: "",
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

// ===== toolListTriggers Tests =====

func TestToolListTriggersSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TRIGGER_NAME", "EVENT_MANIPULATION", "EVENT_OBJECT_TABLE", "ACTION_TIMING", "ACTION_STATEMENT"}).
		AddRow("before_insert_users", "INSERT", "users", "BEFORE", "SET NEW.created_at = NOW()")

	mock.ExpectQuery("SELECT TRIGGER_NAME, EVENT_MANIPULATION, EVENT_OBJECT_TABLE, ACTION_TIMING").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListTriggers(ctx, &mcp.CallToolRequest{}, ListTriggersInput{
		Database: "testdb",
	})

	if err != nil {
		t.Fatalf("toolListTriggers failed: %v", err)
	}

	if len(output.Triggers) != 1 {
		t.Errorf("expected 1 trigger, got %d", len(output.Triggers))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListTriggersMissingDatabase(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolListTriggers(ctx, &mcp.CallToolRequest{}, ListTriggersInput{
		Database: "",
	})

	if err == nil {
		t.Error("expected error for missing database")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolListProcedures Tests =====

func TestToolListProceduresSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"ROUTINE_NAME", "DEFINER", "CREATED", "LAST_ALTERED", "PARAMETER_STYLE"}).
		AddRow("get_users", "root@localhost", "2024-01-01 00:00:00", "2024-01-01 00:00:00", "SQL")

	mock.ExpectQuery("SELECT ROUTINE_NAME, DEFINER, CREATED, LAST_ALTERED").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListProcedures(ctx, &mcp.CallToolRequest{}, ListProceduresInput{
		Database: "testdb",
	})

	if err != nil {
		t.Fatalf("toolListProcedures failed: %v", err)
	}

	if len(output.Procedures) != 1 {
		t.Errorf("expected 1 procedure, got %d", len(output.Procedures))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListProceduresMissingDatabase(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolListProcedures(ctx, &mcp.CallToolRequest{}, ListProceduresInput{
		Database: "",
	})

	if err == nil {
		t.Error("expected error for missing database")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolListFunctions Tests =====

func TestToolListFunctionsSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"ROUTINE_NAME", "DEFINER", "DTD_IDENTIFIER", "CREATED"}).
		AddRow("calc_total", "root@localhost", "DECIMAL(10,2)", "2024-01-01 00:00:00")

	mock.ExpectQuery("SELECT ROUTINE_NAME, DEFINER, DTD_IDENTIFIER, CREATED").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListFunctions(ctx, &mcp.CallToolRequest{}, ListFunctionsInput{
		Database: "testdb",
	})

	if err != nil {
		t.Fatalf("toolListFunctions failed: %v", err)
	}

	if len(output.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(output.Functions))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListFunctionsMissingDatabase(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolListFunctions(ctx, &mcp.CallToolRequest{}, ListFunctionsInput{
		Database: "",
	})

	if err == nil {
		t.Error("expected error for missing database")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolListPartitions Tests =====

func TestToolListPartitionsSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"PARTITION_NAME", "PARTITION_METHOD", "PARTITION_EXPRESSION", "PARTITION_DESCRIPTION", "TABLE_ROWS", "DATA_LENGTH"}).
		AddRow("p0", "RANGE", "year(created_at)", "2024", 50000, 1048576)

	mock.ExpectQuery("SELECT PARTITION_NAME, PARTITION_METHOD, PARTITION_EXPRESSION").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListPartitions(ctx, &mcp.CallToolRequest{}, ListPartitionsInput{
		Database: "testdb",
		Table:    "events",
	})

	if err != nil {
		t.Fatalf("toolListPartitions failed: %v", err)
	}

	if len(output.Partitions) != 1 {
		t.Errorf("expected 1 partition, got %d", len(output.Partitions))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListPartitionsMissingInputs(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolListPartitions(ctx, &mcp.CallToolRequest{}, ListPartitionsInput{
		Database: "",
		Table:    "",
	})

	if err == nil {
		t.Error("expected error for missing inputs")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolDatabaseSize Tests =====

func TestToolDatabaseSizeSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TABLE_SCHEMA", "size_mb", "data_mb", "index_mb", "tables"}).
		AddRow("testdb", 100.5, 80.0, 20.5, 25)

	mock.ExpectQuery("SELECT(.|\n)*TABLE_SCHEMA(.|\n)*FROM information_schema.TABLES").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolDatabaseSize(ctx, &mcp.CallToolRequest{}, DatabaseSizeInput{
		Database: "testdb",
	})

	if err != nil {
		t.Fatalf("toolDatabaseSize failed: %v", err)
	}

	if len(output.Databases) != 1 {
		t.Errorf("expected 1 database, got %d", len(output.Databases))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolDatabaseSizeAllDatabases(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TABLE_SCHEMA", "size_mb", "data_mb", "index_mb", "tables"}).
		AddRow("db1", 50.0, 40.0, 10.0, 10).
		AddRow("db2", 30.0, 25.0, 5.0, 5)

	mock.ExpectQuery("SELECT(.|\n)*TABLE_SCHEMA(.|\n)*FROM information_schema.TABLES").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolDatabaseSize(ctx, &mcp.CallToolRequest{}, DatabaseSizeInput{
		Database: "", // All databases
	})

	if err != nil {
		t.Fatalf("toolDatabaseSize failed: %v", err)
	}

	if len(output.Databases) != 2 {
		t.Errorf("expected 2 databases, got %d", len(output.Databases))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolTableSize Tests =====

func TestToolTableSizeSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TABLE_NAME", "TABLE_ROWS", "data_mb", "index_mb", "total_mb", "ENGINE"}).
		AddRow("users", 1000, 5.0, 1.5, 6.5, "InnoDB").
		AddRow("orders", 5000, 10.0, 3.0, 13.0, "InnoDB")

	mock.ExpectQuery("SELECT(.|\n)*TABLE_NAME(.|\n)*FROM information_schema.TABLES").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolTableSize(ctx, &mcp.CallToolRequest{}, TableSizeInput{
		Database: "testdb",
	})

	if err != nil {
		t.Fatalf("toolTableSize failed: %v", err)
	}

	if len(output.Tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(output.Tables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolTableSizeMissingDatabase(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolTableSize(ctx, &mcp.CallToolRequest{}, TableSizeInput{
		Database: "",
	})

	if err == nil {
		t.Error("expected error for missing database")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolForeignKeys Tests =====

func TestToolForeignKeysSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"CONSTRAINT_NAME", "TABLE_NAME", "COLUMN_NAME",
		"REFERENCED_TABLE_NAME", "REFERENCED_COLUMN_NAME", "on_update", "on_delete",
	}).AddRow("fk_orders_user", "orders", "user_id", "users", "id", "CASCADE", "RESTRICT")

	mock.ExpectQuery("SELECT(.|\n)*CONSTRAINT_NAME(.|\n)*FROM information_schema.KEY_COLUMN_USAGE").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolForeignKeys(ctx, &mcp.CallToolRequest{}, ForeignKeysInput{
		Database: "testdb",
	})

	if err != nil {
		t.Fatalf("toolForeignKeys failed: %v", err)
	}

	if len(output.ForeignKeys) != 1 {
		t.Errorf("expected 1 foreign key, got %d", len(output.ForeignKeys))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolForeignKeysMissingDatabase(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolForeignKeys(ctx, &mcp.CallToolRequest{}, ForeignKeysInput{
		Database: "",
	})

	if err == nil {
		t.Error("expected error for missing database")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolListStatus Tests =====

func TestToolListStatusSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("Uptime", "12345").
		AddRow("Threads_connected", "5")

	mock.ExpectQuery("SHOW GLOBAL STATUS").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListStatus(ctx, &mcp.CallToolRequest{}, ListStatusInput{})

	if err != nil {
		t.Fatalf("toolListStatus failed: %v", err)
	}

	if len(output.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(output.Variables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListStatusWithPattern(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("Threads_connected", "5").
		AddRow("Threads_running", "2")

	mock.ExpectQuery("SHOW GLOBAL STATUS LIKE").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListStatus(ctx, &mcp.CallToolRequest{}, ListStatusInput{
		Pattern: "Threads%",
	})

	if err != nil {
		t.Fatalf("toolListStatus failed: %v", err)
	}

	if len(output.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(output.Variables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolListVariables Tests =====

func TestToolListVariablesSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("max_connections", "151").
		AddRow("innodb_buffer_pool_size", "134217728")

	mock.ExpectQuery("SHOW GLOBAL VARIABLES").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListVariables(ctx, &mcp.CallToolRequest{}, ListVariablesInput{})

	if err != nil {
		t.Fatalf("toolListVariables failed: %v", err)
	}

	if len(output.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(output.Variables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListVariablesWithPattern(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("innodb_buffer_pool_size", "134217728").
		AddRow("innodb_buffer_pool_instances", "1")

	mock.ExpectQuery("SHOW GLOBAL VARIABLES LIKE").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListVariables(ctx, &mcp.CallToolRequest{}, ListVariablesInput{
		Pattern: "innodb_buffer%",
	})

	if err != nil {
		t.Fatalf("toolListVariables failed: %v", err)
	}

	if len(output.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(output.Variables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== Vector Helper Function Tests =====

func TestBuildVectorString(t *testing.T) {
	tests := []struct {
		name     string
		input    []float64
		expected string
	}{
		{"empty", []float64{}, "[]"},
		{"single", []float64{0.5}, "[0.500000]"},
		{"multiple", []float64{0.1, 0.2, 0.3}, "[0.100000,0.200000,0.300000]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildVectorString(tt.input)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestIsVectorSupported(t *testing.T) {
	tests := []struct {
		version  string
		expected bool
	}{
		{"8.0.30", false},
		{"8.4.0", false},
		{"9.0.0", true},
		{"9.0.1", true},
		{"10.0.0", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			result := isVectorSupported(tt.version)
			if result != tt.expected {
				t.Errorf("isVectorSupported(%s) = %v, expected %v", tt.version, result, tt.expected)
			}
		})
	}
}

// ===== toolVectorSearch Tests =====

func TestToolVectorSearchMissingInputs(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	tests := []struct {
		name   string
		input  VectorSearchInput
		errMsg string
	}{
		{
			name:   "missing database",
			input:  VectorSearchInput{Database: "", Table: "test", Column: "vec", Query: []float64{0.1}},
			errMsg: "database, table, and column are required",
		},
		{
			name:   "missing table",
			input:  VectorSearchInput{Database: "db", Table: "", Column: "vec", Query: []float64{0.1}},
			errMsg: "database, table, and column are required",
		},
		{
			name:   "missing column",
			input:  VectorSearchInput{Database: "db", Table: "test", Column: "", Query: []float64{0.1}},
			errMsg: "database, table, and column are required",
		},
		{
			name:   "empty query vector",
			input:  VectorSearchInput{Database: "db", Table: "test", Column: "vec", Query: []float64{}},
			errMsg: "query vector is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, _, err := toolVectorSearch(ctx, &mcp.CallToolRequest{}, tt.input)

			if err == nil {
				t.Error("expected error")
			}
			if err.Error() != tt.errMsg {
				t.Errorf("expected error '%s', got '%s'", tt.errMsg, err.Error())
			}
		})
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolVectorInfo Tests =====

func TestToolVectorInfoMissingDatabase(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolVectorInfo(ctx, &mcp.CallToolRequest{}, VectorInfoInput{
		Database: "",
	})

	if err == nil {
		t.Error("expected error for missing database")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
