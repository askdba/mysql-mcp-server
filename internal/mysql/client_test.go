package mysql

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newTestClient(t *testing.T) (*Client, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	cfg := Config{
		MaxRows:       5,
		QueryTimeoutS: 5,
	}

	client, err := NewWithDB(db, cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	return client, mock
}

func TestListDatabases(t *testing.T) {
	client, mock := newTestClient(t)

	rows := sqlmock.NewRows([]string{"Database"}).
		AddRow("mysql").
		AddRow("testdb")

	mock.ExpectQuery("SHOW DATABASES").WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	dbs, err := client.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases returned error: %v", err)
	}

	if len(dbs) != 2 || dbs[0] != "mysql" || dbs[1] != "testdb" {
		t.Fatalf("unexpected databases: %+v", dbs)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListTables(t *testing.T) {
	client, mock := newTestClient(t)

	dbName := "testdb"

	mock.ExpectExec("USE `testdb`").
		WillReturnResult(sqlmock.NewResult(0, 0))

	rows := sqlmock.NewRows([]string{"Tables_in_testdb"}).
		AddRow("users").
		AddRow("orders")

	mock.ExpectQuery("SHOW TABLES").WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tables, err := client.ListTables(ctx, dbName)
	if err != nil {
		t.Fatalf("ListTables returned error: %v", err)
	}

	if len(tables) != 2 || tables[0] != "users" || tables[1] != "orders" {
		t.Fatalf("unexpected tables: %+v", tables)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDescribeTable(t *testing.T) {
	client, mock := newTestClient(t)

	dbName := "testdb"
	tableName := "users"

	mock.ExpectExec("USE `testdb`").
		WillReturnResult(sqlmock.NewResult(0, 0))

	rows := sqlmock.NewRows([]string{"Field", "Type", "Null", "Key", "Default", "Extra"}).
		AddRow("id", "int", "NO", "PRI", nil, "auto_increment").
		AddRow("name", "varchar(255)", "YES", "", nil, "")

	mock.ExpectQuery("DESCRIBE `users`").WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	cols, err := client.DescribeTable(ctx, dbName, tableName)
	if err != nil {
		t.Fatalf("DescribeTable returned error: %v", err)
	}

	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(cols))
	}

	if cols[0]["Field"] != "id" || cols[1]["Field"] != "name" {
		t.Fatalf("unexpected column metadata: %+v", cols)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRunQueryRespectsMaxRows(t *testing.T) {
	client, mock := newTestClient(t)

	// This SQL string should match your RunQuery query exactly in the test
	sqlText := "SELECT id FROM users"

	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		AddRow(2).
		AddRow(3)

	mock.ExpectQuery(sqlText).WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Ask for more than allowed; client.MaxRows is 5, but we also check internal cap
	result, err := client.RunQuery(ctx, sqlText, 2)
	if err != nil {
		t.Fatalf("RunQuery returned error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 rows due to maxRows limit, got %d", len(result))
	}

	if result[0]["id"] != int64(1) || result[1]["id"] != int64(2) {
		t.Fatalf("unexpected row data: %+v", result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestClientClose(t *testing.T) {
	client, mock := newTestClient(t)

	mock.ExpectClose()

	err := client.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestNewWithDBNilDB(t *testing.T) {
	cfg := Config{
		MaxRows:       5,
		QueryTimeoutS: 5,
	}

	_, err := NewWithDB(nil, cfg)
	if err == nil {
		t.Fatal("expected error for nil db, got nil")
	}
	if err.Error() != "db is nil" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestRunQueryEmptySQL(t *testing.T) {
	client, _ := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.RunQuery(ctx, "", 10)
	if err == nil {
		t.Fatal("expected error for empty SQL, got nil")
	}
	if err.Error() != "sql is required" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestListTablesEmptyDatabase(t *testing.T) {
	client, _ := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.ListTables(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty database, got nil")
	}
	if err.Error() != "database is required" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestDescribeTableEmptyParams(t *testing.T) {
	client, _ := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Test empty database
	_, err := client.DescribeTable(ctx, "", "table")
	if err == nil {
		t.Fatal("expected error for empty database, got nil")
	}

	// Test empty table
	_, err = client.DescribeTable(ctx, "db", "")
	if err == nil {
		t.Fatal("expected error for empty table, got nil")
	}
}

func TestRunQueryMaxRowsDefault(t *testing.T) {
	client, mock := newTestClient(t)

	sqlText := "SELECT id FROM users"
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		AddRow(2).
		AddRow(3).
		AddRow(4).
		AddRow(5).
		AddRow(6) // More than maxRows (5)

	mock.ExpectQuery(sqlText).WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Pass 0 to use default maxRows
	result, err := client.RunQuery(ctx, sqlText, 0)
	if err != nil {
		t.Fatalf("RunQuery returned error: %v", err)
	}

	// Should be limited to 5 (the configured maxRows)
	if len(result) != 5 {
		t.Fatalf("expected 5 rows (maxRows default), got %d", len(result))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRunQueryMaxRowsExceedsConfig(t *testing.T) {
	client, mock := newTestClient(t)

	sqlText := "SELECT id FROM users"
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		AddRow(2).
		AddRow(3).
		AddRow(4).
		AddRow(5).
		AddRow(6)

	mock.ExpectQuery(sqlText).WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Request more than maxRows (5) - should be capped
	result, err := client.RunQuery(ctx, sqlText, 100)
	if err != nil {
		t.Fatalf("RunQuery returned error: %v", err)
	}

	// Should be limited to 5 (the configured maxRows)
	if len(result) != 5 {
		t.Fatalf("expected 5 rows (maxRows cap), got %d", len(result))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListDatabasesQueryError(t *testing.T) {
	client, mock := newTestClient(t)

	mock.ExpectQuery("SHOW DATABASES").WillReturnError(sqlmock.ErrCancelled)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.ListDatabases(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListTablesQueryError(t *testing.T) {
	client, mock := newTestClient(t)

	mock.ExpectExec("USE `testdb`").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SHOW TABLES").WillReturnError(sqlmock.ErrCancelled)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.ListTables(ctx, "testdb")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListTablesUseError(t *testing.T) {
	client, mock := newTestClient(t)

	mock.ExpectExec("USE `testdb`").
		WillReturnError(sqlmock.ErrCancelled)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.ListTables(ctx, "testdb")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDescribeTableUseError(t *testing.T) {
	client, mock := newTestClient(t)

	mock.ExpectExec("USE `testdb`").
		WillReturnError(sqlmock.ErrCancelled)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.DescribeTable(ctx, "testdb", "users")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDescribeTableQueryError(t *testing.T) {
	client, mock := newTestClient(t)

	mock.ExpectExec("USE `testdb`").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("DESCRIBE `users`").WillReturnError(sqlmock.ErrCancelled)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.DescribeTable(ctx, "testdb", "users")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRunQueryQueryError(t *testing.T) {
	client, mock := newTestClient(t)

	mock.ExpectQuery("SELECT id FROM users").WillReturnError(sqlmock.ErrCancelled)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.RunQuery(ctx, "SELECT id FROM users", 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListTablesInvalidDatabaseName(t *testing.T) {
	client, _ := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Invalid database name with backticks
	_, err := client.ListTables(ctx, "test`db")
	if err == nil {
		t.Fatal("expected error for invalid database name, got nil")
	}
}

func TestDescribeTableInvalidNames(t *testing.T) {
	client, _ := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Invalid database name
	_, err := client.DescribeTable(ctx, "test`db", "users")
	if err == nil {
		t.Fatal("expected error for invalid database name, got nil")
	}

	// Invalid table name
	_, err = client.DescribeTable(ctx, "testdb", "use`rs")
	if err == nil {
		t.Fatal("expected error for invalid table name, got nil")
	}
}
