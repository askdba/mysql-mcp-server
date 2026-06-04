// cmd/mysql-mcp-server/tools_extended_test.go
package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/askdba/mysql-mcp-server/internal/util"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setupExtendedMockDB sets up a mock database for extended tool tests.
// Uses connManager with a mock DB instead of the deprecated global db variable.
func setupExtendedMockDB(t *testing.T) (sqlmock.Sqlmock, func()) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}

	// Save original state
	oldConnManager := connManager
	oldMaxRows := maxRows
	oldQueryTimeout := queryTimeout

	// Set up mock connection manager with mock DB
	cm := NewConnectionManager()
	cm.connections["mock"] = mockDB
	cm.configs["mock"] = config.ConnectionConfig{Name: "mock", DSN: "mock://test"}
	cm.activeConn = "mock"
	connManager = cm

	maxRows = 1000
	queryTimeout = 30 * time.Second

	cleanup := func() {
		connManager = oldConnManager
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
		t.Fatal("expected error for missing database")
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
		t.Fatal("expected error for missing table")
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

	mock.ExpectQuery("EXPLAIN FORMAT=JSON SELECT \\* FROM users").WillReturnRows(sqlmock.NewRows([]string{"EXPLAIN"}).AddRow(
		`{"query_block":{"cost_info":{"query_cost":"10.0"},"table":{"table_name":"users","access_type":"ALL","rows":100,"filtered":100}}}`,
	))

	ctx := context.Background()
	_, output, err := toolExplainQuery(ctx, &mcp.CallToolRequest{}, ExplainQueryInput{
		SQL: "SELECT * FROM users",
	})

	if err != nil {
		t.Fatalf("toolExplainQuery failed: %v", err)
	}

	plan, ok := output.Plan.(UnifiedExplainPlan)
	if !ok {
		t.Fatal("expected output.Plan to be UnifiedExplainPlan")
	}
	if plan.QueryCost != 10.0 {
		t.Errorf("expected QueryCost 10.0, got %v", plan.QueryCost)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(plan.Operations))
	}
	if plan.Operations[0].TableName != "users" || plan.Operations[0].AccessType != "ALL" {
		t.Errorf("unexpected op0: %+v", plan.Operations[0])
	}
	if plan.Operations[0].RowsExamined != 100 || plan.Operations[0].Filtered != 100 {
		t.Errorf("expected rows/filtered mapped, got RowsExamined=%d Filtered=%v", plan.Operations[0].RowsExamined, plan.Operations[0].Filtered)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMapRawExplainToUnifiedMariaDBQueryBlockCost(t *testing.T) {
	raw := `{"query_block":{"select_id":1,"cost":42.5,"table":{"table_name":"t1","access_type":"ALL","rows":1}}}`
	plan, err := mapRawExplainToUnified(raw)
	if err != nil {
		t.Fatalf("mapRawExplainToUnified: %v", err)
	}
	if plan.QueryCost != 42.5 {
		t.Errorf("expected QueryCost from query_block.cost, got %v", plan.QueryCost)
	}
}

func TestMapRawExplainToUnifiedFilteredNumericString(t *testing.T) {
	raw := `{"query_block":{"table":{"table_name":"t1","access_type":"ALL","filtered":"100"}}}`
	plan, err := mapRawExplainToUnified(raw)
	if err != nil {
		t.Fatalf("mapRawExplainToUnified: %v", err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Filtered != 100 {
		t.Fatalf("expected Filtered 100 from string, got %+v", plan.Operations)
	}
}

func TestMapRawExplainToUnifiedFilteredInvalidType(t *testing.T) {
	raw := `{"query_block":{"table":{"table_name":"t1","access_type":"ALL","filtered":true}}}`
	_, err := mapRawExplainToUnified(raw)
	if err == nil {
		t.Fatal("expected error for invalid filtered type")
	}
}

func TestToolExplainQueryFilteredInvalidReturnsError(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	mock.ExpectQuery("EXPLAIN FORMAT=JSON SELECT 1").WillReturnRows(
		sqlmock.NewRows([]string{"EXPLAIN"}).AddRow(`{"query_block":{"table":{"table_name":"t1","filtered":"not-a-number"}}}`),
	)

	ctx := context.Background()
	_, _, err := toolExplainQuery(ctx, &mcp.CallToolRequest{}, ExplainQueryInput{SQL: "SELECT 1"})
	if err == nil {
		t.Fatal("expected error from invalid filtered numeric string")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMapRawExplainToUnifiedBlockNLJoinMergesAttachedCondition(t *testing.T) {
	raw := `{
  "query_block": {
    "select_id": 1,
    "nested_loop": [
      {"table": {"table_name": "a", "access_type": "ALL", "rows": 1}},
      {
        "block-nl-join": {
          "table": {"table_name": "b", "access_type": "ALL", "rows": 2},
          "attached_condition": "b.x = a.y"
        }
      }
    ]
  }
}`
	plan, err := mapRawExplainToUnified(raw)
	if err != nil {
		t.Fatalf("mapRawExplainToUnified: %v", err)
	}
	if len(plan.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(plan.Operations))
	}
	if plan.Operations[0].TableName != "a" {
		t.Errorf("op0 table: %q", plan.Operations[0].TableName)
	}
	if plan.Operations[1].TableName != "b" {
		t.Errorf("op1 table: %q", plan.Operations[1].TableName)
	}
	if plan.Operations[1].AttachedCondition != "b.x = a.y" {
		t.Errorf("expected merged attached_condition on b, got %q", plan.Operations[1].AttachedCondition)
	}
}

func TestMapRawExplainToUnifiedBlockNLJoinKeepsTableAttachedCondition(t *testing.T) {
	raw := `{
  "query_block": {
    "nested_loop": [
      {
        "block-nl-join": {
          "table": {
            "table_name": "b",
            "access_type": "ALL",
            "attached_condition": "(b.id > 0)"
          },
          "attached_condition": "wrapper only"
        }
      }
    ]
  }
}`
	plan, err := mapRawExplainToUnified(raw)
	if err != nil {
		t.Fatalf("mapRawExplainToUnified: %v", err)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(plan.Operations))
	}
	if plan.Operations[0].AttachedCondition != "(b.id > 0)" {
		t.Errorf("table attached_condition must win over wrapper: got %q", plan.Operations[0].AttachedCondition)
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
		t.Fatal("expected error for empty SQL")
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
		t.Fatal("expected error for non-SELECT query")
	}
	if err.Error() != "only SELECT statements can be explained" {
		t.Errorf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestToolExplainQuerySystemSchemaDefaultDeny verifies EXPLAIN blocks system
// schemas by default (no allowlist, flag off) and never reaches the DB.
func TestToolExplainQuerySystemSchemaDefaultDeny(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	for _, q := range []string{
		"SELECT * FROM information_schema.tables",
		"SELECT * FROM performance_schema.events_statements_summary_by_digest",
		"SELECT * FROM sys.session",
		"SELECT * FROM mysql.user",
	} {
		_, _, err := toolExplainQuery(context.Background(), &mcp.CallToolRequest{}, ExplainQueryInput{SQL: q})
		// Assert the rejection comes from the validator (before any DB access),
		// not from an incidental "unexpected DB call" sqlmock error — otherwise
		// the test would pass even if EXPLAIN reached the DB.
		if err == nil {
			t.Errorf("expected EXPLAIN of %q to be blocked by default", q)
		} else if !strings.Contains(err.Error(), "query validation failed") {
			t.Errorf("expected validation rejection for %q (proving no DB call), got: %v", q, err)
		}
	}
	// Belt-and-suspenders: no mock.ExpectQuery was registered, so any DB call
	// would also surface here.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestToolExplainQuerySystemSchemaOptInUnlocks verifies that with
// MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS on, EXPLAIN of a read-only system schema passes
// validation and reaches the DB, while mysql stays blocked.
func TestToolExplainQuerySystemSchemaOptInUnlocks(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	util.SetAllowSystemSchemas(true)
	defer util.SetAllowSystemSchemas(false)

	// information_schema now reaches the DB (validation passes).
	mock.ExpectQuery(`EXPLAIN FORMAT=JSON SELECT \* FROM information_schema.STATISTICS`).
		WillReturnRows(sqlmock.NewRows([]string{"EXPLAIN"}).AddRow(
			`{"query_block":{"cost_info":{"query_cost":"1.0"},"table":{"table_name":"STATISTICS","access_type":"ALL","rows":1,"filtered":100}}}`,
		))
	if _, _, err := toolExplainQuery(context.Background(), &mcp.CallToolRequest{},
		ExplainQueryInput{SQL: "SELECT * FROM information_schema.STATISTICS"}); err != nil {
		t.Fatalf("expected EXPLAIN of information_schema to pass with flag on, got: %v", err)
	}

	// mysql must remain blocked even with the flag on — and the rejection must
	// come from the validator (no DB call), not an unexpected-query sqlmock error.
	if _, _, err := toolExplainQuery(context.Background(), &mcp.CallToolRequest{},
		ExplainQueryInput{SQL: "SELECT * FROM mysql.user"}); err == nil {
		t.Error("expected EXPLAIN of mysql.user to remain blocked even with flag on")
	} else if !strings.Contains(err.Error(), "query validation failed") {
		t.Errorf("expected validation rejection for mysql.user (proving no DB call), got: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestToolExplainQuerySystemSchemaAllowlistStillApplies codifies the documented
// policy: MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS and MYSQL_MCP_ALLOWED_DATABASES are
// orthogonal and both enforced. With an allowlist set that does NOT include
// information_schema, the flag does not exempt it — the allowlist independently
// rejects the reference.
func TestToolExplainQuerySystemSchemaAllowlistStillApplies(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	initAccessControl([]string{"appdb"}) // allowlist excludes information_schema
	defer initAccessControl(nil)
	util.SetAllowSystemSchemas(true)
	defer util.SetAllowSystemSchemas(false)

	_, _, err := toolExplainQuery(context.Background(), &mcp.CallToolRequest{},
		ExplainQueryInput{SQL: "SELECT * FROM information_schema.STATISTICS", Database: "appdb"})
	if err == nil {
		t.Fatal("expected information_schema to be rejected by the allowlist even with the flag on")
	}
	if !strings.Contains(err.Error(), "MYSQL_MCP_ALLOWED_DATABASES") {
		t.Errorf("expected an allowlist rejection (not a validator/DB error), got: %v", err)
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
		t.Fatal("expected error for missing database")
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

	rows := sqlmock.NewRows([]string{"VARIABLE_NAME", "VARIABLE_VALUE"}).
		AddRow("Uptime", "12345").
		AddRow("Threads_connected", "5")

	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status ORDER BY VARIABLE_NAME").
		WillReturnRows(rows)

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

	rows := sqlmock.NewRows([]string{"VARIABLE_NAME", "VARIABLE_VALUE"}).
		AddRow("Threads_connected", "5").
		AddRow("Threads_running", "2")

	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME LIKE .* ORDER BY VARIABLE_NAME").
		WithArgs("Threads%").
		WillReturnRows(rows)

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

func TestToolListStatusFallback(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	// Primary performance_schema query fails
	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status ORDER BY VARIABLE_NAME").
		WillReturnError(fmt.Errorf("Table 'performance_schema.global_status' doesn't exist"))

	// Fallback SHOW GLOBAL STATUS succeeds
	rows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("Uptime", "12345").
		AddRow("Threads_connected", "5")
	mock.ExpectQuery("SHOW GLOBAL STATUS").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListStatus(ctx, &mcp.CallToolRequest{}, ListStatusInput{})

	if err != nil {
		t.Fatalf("toolListStatus fallback failed: %v", err)
	}

	if len(output.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(output.Variables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListStatusFallbackWithPattern(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	// Primary performance_schema query fails
	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME LIKE .* ORDER BY VARIABLE_NAME").
		WillReturnError(fmt.Errorf("Table 'performance_schema.global_status' doesn't exist"))

	// Fallback SHOW GLOBAL STATUS LIKE succeeds
	rows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("Threads_connected", "5").
		AddRow("Threads_running", "2")
	mock.ExpectQuery("SHOW GLOBAL STATUS LIKE").WithArgs("Threads%").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListStatus(ctx, &mcp.CallToolRequest{}, ListStatusInput{
		Pattern: "Threads%",
	})

	if err != nil {
		t.Fatalf("toolListStatus fallback with pattern failed: %v", err)
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
		AddRow("innodb_buffer_pool_instances", "1").
		AddRow("innodb_buffer_pool_size", "134217728")

	mock.ExpectQuery("SHOW GLOBAL VARIABLES LIKE").WithArgs("innodb_buffer%").WillReturnRows(rows)

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

func TestToolListVariablesFallback(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	mock.ExpectQuery("SHOW GLOBAL VARIABLES").
		WillReturnError(fmt.Errorf("access denied"))

	rows := sqlmock.NewRows([]string{"VARIABLE_NAME", "VARIABLE_VALUE"}).
		AddRow("max_connections", "151").
		AddRow("innodb_buffer_pool_size", "134217728")
	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_variables ORDER BY VARIABLE_NAME").
		WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListVariables(ctx, &mcp.CallToolRequest{}, ListVariablesInput{})

	if err != nil {
		t.Fatalf("toolListVariables fallback failed: %v", err)
	}

	if len(output.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(output.Variables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolListVariablesFallbackWithPattern(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	mock.ExpectQuery("SHOW GLOBAL VARIABLES LIKE").WithArgs("innodb_buffer%").
		WillReturnError(fmt.Errorf("access denied"))

	rows := sqlmock.NewRows([]string{"VARIABLE_NAME", "VARIABLE_VALUE"}).
		AddRow("innodb_buffer_pool_instances", "1").
		AddRow("innodb_buffer_pool_size", "134217728")
	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_variables WHERE VARIABLE_NAME LIKE .* ORDER BY VARIABLE_NAME").
		WithArgs("innodb_buffer%").
		WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolListVariables(ctx, &mcp.CallToolRequest{}, ListVariablesInput{
		Pattern: "innodb_buffer%",
	})

	if err != nil {
		t.Fatalf("toolListVariables fallback with pattern failed: %v", err)
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
	// Save and restore global state
	oldConnManager := connManager
	defer func() { connManager = oldConnManager }()

	// Helper to set up a connection manager with a specific server type
	setupServerType := func(serverType ServerType) {
		cm := NewConnectionManager()
		cm.serverTypes["test"] = serverType
		cm.activeConn = "test"
		connManager = cm
	}

	t.Run("MySQL server type", func(t *testing.T) {
		setupServerType(ServerTypeMySQL)

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
	})

	t.Run("MariaDB server type - always false", func(t *testing.T) {
		setupServerType(ServerTypeMariaDB)

		// Even with version >= 9, MariaDB should return false
		versions := []string{"10.11.2", "11.4.2", "9.0.0", "8.0.30"}
		for _, version := range versions {
			t.Run(version, func(t *testing.T) {
				if isVectorSupported(version) {
					t.Errorf("isVectorSupported(%s) should be false for MariaDB", version)
				}
			})
		}
	})

	t.Run("Unknown server type - always false", func(t *testing.T) {
		setupServerType(ServerTypeUnknown)

		// Even with version >= 9, Unknown should return false to be safe
		versions := []string{"10.0.0", "11.0.0", "9.0.0", "8.0.30"}
		for _, version := range versions {
			t.Run(version, func(t *testing.T) {
				if isVectorSupported(version) {
					t.Errorf("isVectorSupported(%s) should be false for Unknown server type", version)
				}
			})
		}
	})

	t.Run("nil connManager - returns false", func(t *testing.T) {
		connManager = nil

		// When connManager is nil, getServerType returns Unknown, so should be false
		if isVectorSupported("9.0.0") {
			t.Error("isVectorSupported should return false when connManager is nil")
		}
	})
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
				t.Fatal("expected error")
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

// ===== analyzeExplainPlan Tests =====

func TestAnalyzeExplainPlanFullTableScanNoIndexes(t *testing.T) {
	plan := []map[string]interface{}{
		{
			"table":         "orders",
			"type":          "ALL",
			"possible_keys": nil,
			"key":           nil,
			"Extra":         "",
		},
	}
	warnings := analyzeExplainPlan(plan)
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning for full table scan")
	}
	found := false
	for _, w := range warnings {
		if containsCI(w, "full table scan") && containsCI(w, "orders") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected full-table-scan warning for table 'orders', got: %v", warnings)
	}
}

func TestAnalyzeExplainPlanFullTableScanWithCandidateIndexes(t *testing.T) {
	plan := []map[string]interface{}{
		{
			"table":         "users",
			"type":          "ALL",
			"possible_keys": "idx_email,idx_name",
			"key":           nil,
			"Extra":         "",
		},
	}
	warnings := analyzeExplainPlan(plan)
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning")
	}
	found := false
	for _, w := range warnings {
		if containsCI(w, "full table scan") && containsCI(w, "idx_email") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected full-table-scan warning mentioning candidate indexes, got: %v", warnings)
	}
}

func TestAnalyzeExplainPlanIndexAvailableButUnused(t *testing.T) {
	plan := []map[string]interface{}{
		{
			"table":         "products",
			"type":          "ref",
			"possible_keys": "idx_category",
			"key":           nil,
			"Extra":         "",
		},
	}
	warnings := analyzeExplainPlan(plan)
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning for unused index")
	}
	found := false
	for _, w := range warnings {
		if containsCI(w, "idx_category") && containsCI(w, "none were chosen") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected unused-index warning, got: %v", warnings)
	}
}

func TestAnalyzeExplainPlanNoUnusedIndexWarningWhenAccessTypeMissing(t *testing.T) {
	plan := []map[string]interface{}{
		{
			"table":         "products",
			"type":          nil,
			"possible_keys": "idx_category",
			"key":           nil,
			"Extra":         "",
		},
	}
	warnings := analyzeExplainPlan(plan)
	for _, w := range warnings {
		if containsCI(w, "none were chosen") {
			t.Fatalf("did not expect unused-index warning when access type is unknown, got: %v", warnings)
		}
	}
}

func TestAnalyzeExplainPlanFilesort(t *testing.T) {
	plan := []map[string]interface{}{
		{
			"table":         "orders",
			"type":          "index",
			"possible_keys": nil,
			"key":           "PRIMARY",
			"Extra":         "Using filesort",
		},
	}
	warnings := analyzeExplainPlan(plan)
	if len(warnings) == 0 {
		t.Fatal("expected filesort warning")
	}
	found := false
	for _, w := range warnings {
		if containsCI(w, "filesort") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected filesort warning, got: %v", warnings)
	}
}

func TestAnalyzeExplainPlanTemporaryTable(t *testing.T) {
	plan := []map[string]interface{}{
		{
			"table":         "sales",
			"type":          "ALL",
			"possible_keys": nil,
			"key":           nil,
			"Extra":         "Using temporary; Using filesort",
		},
	}
	warnings := analyzeExplainPlan(plan)
	foundTmp := false
	foundSort := false
	for _, w := range warnings {
		if containsCI(w, "temporary table") {
			foundTmp = true
		}
		if containsCI(w, "filesort") {
			foundSort = true
		}
	}
	if !foundTmp {
		t.Errorf("expected temporary-table warning, got: %v", warnings)
	}
	if !foundSort {
		t.Errorf("expected filesort warning, got: %v", warnings)
	}
}

func TestAnalyzeExplainPlanGoodPlan(t *testing.T) {
	// A plan using a specific key with no problematic extras should produce no warnings.
	plan := []map[string]interface{}{
		{
			"table":         "users",
			"type":          "ref",
			"possible_keys": "idx_email",
			"key":           "idx_email",
			"Extra":         "Using index",
		},
	}
	warnings := analyzeExplainPlan(plan)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for efficient plan, got: %v", warnings)
	}
}

func TestToolExplainQueryWarningsPopulated(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	// Simulate a full-table scan plan row
	rows := sqlmock.NewRows([]string{"id", "select_type", "table", "type", "possible_keys", "key", "key_len", "ref", "rows", "Extra"}).
		AddRow(1, "SIMPLE", "orders", "ALL", nil, nil, nil, nil, 5000, "")

	mock.ExpectQuery("EXPLAIN SELECT \\* FROM orders").WillReturnRows(rows)

	ctx := context.Background()
	_, output, err := toolExplainQuery(ctx, &mcp.CallToolRequest{}, ExplainQueryInput{
		SQL: "SELECT * FROM orders", Format: "traditional",
	})

	if err != nil {
		t.Fatalf("toolExplainQuery failed: %v", err)
	}
	plan, ok := output.Plan.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected output.Plan to be []map[string]interface{}, got %T", output.Plan)
	}
	if len(plan) == 0 {
		t.Fatalf("expected non-empty plan")
	}
	if len(output.Warnings) == 0 {
		t.Error("expected warnings for full table scan plan")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolSearchSchema Tests =====

func TestToolSearchSchemaSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	// Mock table search
	tableRows := sqlmock.NewRows([]string{"TABLE_SCHEMA", "TABLE_NAME"}).
		AddRow("testdb", "users").
		AddRow("testdb", "user_profiles")

	mock.ExpectQuery("SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.TABLES WHERE TABLE_NAME LIKE").
		WithArgs("%user%", 1000).
		WillReturnRows(tableRows)

	// Mock column search (maxRows - 2 = 998)
	colRows := sqlmock.NewRows([]string{"TABLE_SCHEMA", "TABLE_NAME", "COLUMN_NAME"}).
		AddRow("testdb", "orders", "user_id")

	mock.ExpectQuery("SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME FROM information_schema.COLUMNS WHERE COLUMN_NAME LIKE").
		WithArgs("%user%", 998).
		WillReturnRows(colRows)

	ctx := context.Background()
	_, output, err := toolSearchSchema(ctx, &mcp.CallToolRequest{}, SearchSchemaInput{
		Pattern: "%user%",
	})

	if err != nil {
		t.Fatalf("toolSearchSchema failed: %v", err)
	}

	if len(output.Matches) != 3 {
		t.Errorf("expected 3 matches, got %d", len(output.Matches))
	}

	if output.Matches[0].Type != "TABLE" || output.Matches[0].Table != "users" {
		t.Errorf("unexpected first match: %+v", output.Matches[0])
	}

	if output.Matches[2].Type != "COLUMN" || output.Matches[2].Column != "user_id" {
		t.Errorf("unexpected third match: %+v", output.Matches[2])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolSearchSchemaEmptyPattern(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolSearchSchema(ctx, &mcp.CallToolRequest{}, SearchSchemaInput{
		Pattern: "",
	})

	if err == nil {
		t.Fatal("expected error for empty pattern")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// ===== toolSchemaDiff Tests =====

func TestToolSchemaDiffSuccess(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	// Source tables: users, orders
	sourceRows := sqlmock.NewRows([]string{"TABLE_NAME"}).
		AddRow("users").
		AddRow("orders")
	mock.ExpectQuery("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = \\?").
		WithArgs("db1").
		WillReturnRows(sourceRows)

	// Target tables: users, products
	targetRows := sqlmock.NewRows([]string{"TABLE_NAME"}).
		AddRow("users").
		AddRow("products")
	mock.ExpectQuery("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = \\?").
		WithArgs("db2").
		WillReturnRows(targetRows)

	// Mock column comparison for "users" table
	userColsSource := sqlmock.NewRows([]string{"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE", "COLUMN_DEFAULT"}).
		AddRow("id", "int", "NO", nil)
	mock.ExpectQuery("SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = \\? AND TABLE_NAME = \\?").
		WithArgs("db1", "users").
		WillReturnRows(userColsSource)

	userColsTarget := sqlmock.NewRows([]string{"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE", "COLUMN_DEFAULT"}).
		AddRow("id", "int", "NO", nil)
	mock.ExpectQuery("SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = \\? AND TABLE_NAME = \\?").
		WithArgs("db2", "users").
		WillReturnRows(userColsTarget)

	ctx := context.Background()
	_, output, err := toolSchemaDiff(ctx, &mcp.CallToolRequest{}, SchemaDiffInput{
		SourceDatabase: "db1",
		TargetDatabase: "db2",
	})

	if err != nil {
		t.Fatalf("toolSchemaDiff failed: %v", err)
	}

	foundMissing := false
	foundExtra := false
	for _, diff := range output.Diffs {
		if diff.Table == "orders" && diff.Status == "MISSING" {
			foundMissing = true
		}
		if diff.Table == "products" && diff.Status == "EXTRA" {
			foundExtra = true
		}
	}

	if !foundMissing {
		t.Errorf("expected MISSING status for table 'orders' in Diffs, got: %+v", output.Diffs)
	}

	if !foundExtra {
		t.Errorf("expected EXTRA status for table 'products' in Diffs, got: %+v", output.Diffs)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestToolSchemaDiffMissingInputs(t *testing.T) {
	mock, cleanup := setupExtendedMockDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _, err := toolSchemaDiff(ctx, &mcp.CallToolRequest{}, SchemaDiffInput{
		SourceDatabase: "",
		TargetDatabase: "db2",
	})

	if err == nil {
		t.Fatal("expected error for missing source database")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// containsCI is a case-insensitive substring check helper for test assertions.
func containsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
