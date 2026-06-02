package mysql

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

type testNetTempErr struct{}

func (testNetTempErr) Error() string   { return "temporary network error" }
func (testNetTempErr) Timeout() bool   { return true }
func (testNetTempErr) Temporary() bool { return true }

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

func TestRunQueryWithRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	cfg := Config{
		MaxRows:       5,
		QueryTimeoutS: 5,
		Retry: RetryConfig{
			MaxRetries:  2,
			MaxInterval: 2 * time.Second,
		},
	}

	client, err := NewWithDB(db, cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sqlText := "SELECT id FROM users"

	// First call fails with a transient network-style error (covers net.Error path).
	mock.ExpectQuery(sqlText).WillReturnError(testNetTempErr{})


	// Second call succeeds
	rows := sqlmock.NewRows([]string{"id"}).AddRow(1)
	mock.ExpectQuery(sqlText).WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result, err := client.RunQuery(ctx, sqlText, 10)
	if err != nil {
		t.Fatalf("RunQuery returned error after retry: %v", err)
	}

	if len(result) != 1 || result[0]["id"] != int64(1) {
		t.Fatalf("unexpected result: %+v", result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRunQueryPermanentError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	cfg := Config{
		MaxRows:       5,
		QueryTimeoutS: 5,
		Retry: RetryConfig{
			MaxRetries:  2,
			MaxInterval: 2 * time.Second,
		},
	}

	client, err := NewWithDB(db, cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sqlText := "SELECT id FROM users"

	// Permanent error (e.g. syntax error) should not retry
	mock.ExpectQuery(sqlText).WillReturnError(&mysql.MySQLError{Number: 1064, Message: "Syntax error"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err = client.RunQuery(ctx, sqlText, 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1064 {
		t.Fatalf("expected MySQL 1064 error, got %v", err)
	}

	// Should only have 1 expectation met because it shouldn't retry
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

func TestRunQueryRejectsDML(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	for _, stmt := range []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET x=1",
		"DELETE FROM t",
		"DROP TABLE t",
		"CREATE TABLE t (id INT)",
	} {
		_, err := client.RunQuery(ctx, stmt, 10)
		if err == nil {
			t.Errorf("expected rejection for %q, got nil", stmt)
			continue
		}
		if !strings.Contains(err.Error(), "read-only") {
			t.Errorf("expected read-only error for %q, got: %v", stmt, err)
		}
	}
}

func TestRunQueryAllowsReadStatements(t *testing.T) {
	client, mock := newTestClient(t)
	ctx := context.Background()

	for _, stmt := range []string{"SELECT 1", "SHOW TABLES", "DESCRIBE t", "DESC t", "EXPLAIN SELECT 1", "WITH cte AS (SELECT 1) SELECT * FROM cte"} {
		mock.ExpectQuery(regexp.QuoteMeta(stmt)).
			WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
		_, err := client.RunQuery(ctx, stmt, 10)
		if err != nil {
			t.Errorf("expected %q to be allowed, got: %v", stmt, err)
		}
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

// Regression: negative cfg.MaxRows must not make make(..., 0, maxRows) panic (Codex P2).
func TestRunQueryClampsNegativeClientMaxRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	client, err := NewWithDB(db, Config{MaxRows: -1, QueryTimeoutS: 5})
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}

	rows := sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2)
	mock.ExpectQuery("SELECT 1").WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	out, err := client.RunQuery(ctx, "SELECT 1", 10)
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected 0 rows with effective maxRows 0 after negative clamp, got %d", len(out))
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
