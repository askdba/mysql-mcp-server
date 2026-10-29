// cmd/mysql-mcp-server/toolpack_test.go
package main

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func TestPackCall(t *testing.T) {
	for n, want := range map[int]string{
		0: "CALL mysql_mcp.list_databases()",
		1: "CALL mysql_mcp.list_databases(?)",
		3: "CALL mysql_mcp.list_databases(?, ?, ?)",
	} {
		if got := packCall("list_databases", n); got != want {
			t.Errorf("packCall(%d) = %q, want %q", n, got, want)
		}
	}
}

// With the pack detected on the connection, catalog tools CALL mysql_mcp.*; without it
// they run their embedded SQL. Omitted optional arguments become NULL.
func TestCatalogToolsUseToolPackWhenDetected(t *testing.T) {
	r := setupMockDBFull(t)
	defer r.cleanup()
	mock := r.mock

	mock.ExpectQuery(`SELECT TABLE_NAME, ENGINE, TABLE_ROWS, TABLE_COMMENT`).WithArgs("shop").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME", "ENGINE", "TABLE_ROWS", "TABLE_COMMENT"}).AddRow("a", "InnoDB", 1, ""))
	if _, out, err := toolListTables(t.Context(), nil, ListTablesInput{Database: "shop"}); err != nil || len(out.Tables) != 1 {
		t.Fatalf("embedded list_tables: %+v %v", out, err)
	}

	connManager.mu.Lock()
	connManager.toolPacks["mock"] = "1.0.0"
	connManager.mu.Unlock()

	mock.ExpectQuery(`CALL mysql_mcp\.list_tables\(\?\)`).WithArgs("shop").
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_NAME", "ENGINE", "TABLE_ROWS", "TABLE_COMMENT"}).AddRow("a", "InnoDB", 1, ""))
	if _, out, err := toolListTables(t.Context(), nil, ListTablesInput{Database: "shop"}); err != nil || len(out.Tables) != 1 {
		t.Fatalf("pack list_tables: %+v %v", out, err)
	}

	sizeCols := []string{"TABLE_SCHEMA", "size_mb", "data_mb", "index_mb", "tables"}
	mock.ExpectQuery(`CALL mysql_mcp\.database_size\(\?\)`).WithArgs(nilArg{}).
		WillReturnRows(sqlmock.NewRows(sizeCols).AddRow("shop", 1.5, 1.0, 0.5, 3))
	if _, out, err := toolDatabaseSize(t.Context(), nil, DatabaseSizeInput{}); err != nil || len(out.Databases) != 1 {
		t.Fatalf("pack database_size (all): %+v %v", out, err)
	}

	mock.ExpectQuery(`CALL mysql_mcp\.search_tables\(\?, \?, \?\)`).WithArgs("%film%", nilArg{}, maxRows).
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_SCHEMA", "TABLE_NAME"}).AddRow("shop", "film"))
	mock.ExpectQuery(`CALL mysql_mcp\.search_columns\(\?, \?, \?\)`).WithArgs("%film%", nilArg{}, maxRows-1).
		WillReturnRows(sqlmock.NewRows([]string{"TABLE_SCHEMA", "TABLE_NAME", "COLUMN_NAME"}))
	if _, out, err := toolSearchSchema(t.Context(), nil, SearchSchemaInput{Pattern: "%film%"}); err != nil || len(out.Matches) != 1 {
		t.Fatalf("pack search_schema: %+v %v", out, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// nilArg matches a NULL query argument.
type nilArg struct{}

func (nilArg) Match(v driver.Value) bool { return v == nil }

// SHOW CREATE TABLE on a view returns four columns; the statement is the second.
func TestShowCreateTableView(t *testing.T) {
	mock, cleanup := setupMockDB(t)
	defer cleanup()
	mock.ExpectQuery("SHOW CREATE TABLE `shop`.`v`").WillReturnRows(
		sqlmock.NewRows([]string{"View", "Create View", "character_set_client", "collation_connection"}).
			AddRow("v", "CREATE VIEW `v` AS select 1", "utf8mb4", "utf8mb4_0900_ai_ci"))
	_, out, err := toolShowCreateTable(t.Context(), nil, ShowCreateTableInput{Database: "shop", Table: "v"})
	if err != nil || out.CreateStatement != "CREATE VIEW `v` AS select 1" {
		t.Fatalf("view: %+v %v", out, err)
	}
}

// A missing procedure (1305, an interrupted upgrade) or a revoked EXECUTE (1370) falls back
// to the embedded SQL and stops using the pack on that connection.
func TestCatalogRowsFallsBackWhenPackGone(t *testing.T) {
	for _, code := range []uint16{1305, 1370} {
		r := setupMockDBFull(t)
		mock := r.mock
		connManager.mu.Lock()
		connManager.toolPacks["mock"] = "1.0.0"
		connManager.mu.Unlock()

		mock.ExpectQuery(`CALL mysql_mcp\.list_databases\(\)`).
			WillReturnError(&mysql.MySQLError{Number: code, Message: "pack unavailable"})
		mock.ExpectQuery(`SELECT SCHEMA_NAME FROM information_schema\.SCHEMATA`).
			WillReturnRows(sqlmock.NewRows([]string{"SCHEMA_NAME"}).AddRow("shop"))
		if _, out, err := toolListDatabases(t.Context(), nil, ListDatabasesInput{}); err != nil || len(out.Databases) != 1 || out.Databases[0].Name != "shop" {
			t.Errorf("error %d: fallback = %+v %v", code, out, err)
		}
		if v := connManager.ToolPackOf("mock"); v != "" {
			t.Errorf("error %d: pack still in use: %q", code, v)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("error %d: %v", code, err)
		}
		r.cleanup()
	}
}

// Any other error from a pack call is returned, and the pack stays in use.
func TestCatalogRowsKeepsPackOnOtherErrors(t *testing.T) {
	r := setupMockDBFull(t)
	defer r.cleanup()
	connManager.mu.Lock()
	connManager.toolPacks["mock"] = "1.0.0"
	connManager.mu.Unlock()
	r.mock.ExpectQuery(`CALL mysql_mcp\.list_tables\(\?\)`).WillReturnError(&mysql.MySQLError{Number: 1205, Message: "Lock wait timeout"})
	_, _, err := toolListTables(t.Context(), nil, ListTablesInput{Database: "shop"})
	var me *mysql.MySQLError
	if !errors.As(err, &me) || me.Number != 1205 {
		t.Errorf("err = %v, want MySQL error 1205", err)
	}
	if v := connManager.ToolPackOf("mock"); v != "1.0.0" {
		t.Errorf("pack marker = %q after a lock wait timeout, want 1.0.0", v)
	}
	if err := r.mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
