// cmd/mysql-mcp-server/http_test.go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/askdba/mysql-mcp-server/internal/api"
	"github.com/askdba/mysql-mcp-server/internal/config"
)

// setupHTTPTest sets up mock database and config for HTTP handler tests
func setupHTTPTest(t *testing.T) (sqlmock.Sqlmock, func()) {
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}

	// Save original state
	oldDB := db
	oldConnManager := connManager
	oldCfg := cfg
	oldMaxRows := maxRows
	oldQueryTimeout := queryTimeout
	oldExtendedMode := extendedMode

	// Set up global state for HTTP handlers
	db = mockDB
	connManager = nil
	maxRows = 1000
	queryTimeout = 30 * time.Second
	extendedMode = true
	cfg = &config.Config{
		HTTPRequestTimeout: 30 * time.Second,
		MaxRows:            1000,
		QueryTimeout:       30 * time.Second,
	}

	cleanup := func() {
		db = oldDB
		connManager = oldConnManager
		cfg = oldCfg
		maxRows = oldMaxRows
		queryTimeout = oldQueryTimeout
		extendedMode = oldExtendedMode
		mockDB.Close()
	}

	return mock, cleanup
}

// TestHTTPHealth tests the /health endpoint
func TestHTTPHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	httpHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result api.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success to be true")
	}

	data, ok := result.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}

	if data["status"] != "healthy" {
		t.Errorf("expected status 'healthy', got '%v'", data["status"])
	}
	if data["service"] != "mysql-mcp-server" {
		t.Errorf("expected service 'mysql-mcp-server', got '%v'", data["service"])
	}
}

// TestHTTPAPIIndex tests the /api endpoint
func TestHTTPAPIIndex(t *testing.T) {
	_, cleanup := setupHTTPTest(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	w := httptest.NewRecorder()

	httpAPIIndex(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result api.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Error("expected success to be true")
	}

	data, ok := result.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}

	if data["service"] != "mysql-mcp-server REST API" {
		t.Errorf("expected service 'mysql-mcp-server REST API', got '%v'", data["service"])
	}

	endpoints, ok := data["endpoints"].(map[string]interface{})
	if !ok {
		t.Fatal("expected endpoints to be a map")
	}

	// Check some expected endpoints exist
	if _, ok := endpoints["GET  /api/databases"]; !ok {
		t.Error("expected 'GET  /api/databases' endpoint")
	}
	if _, ok := endpoints["POST /api/query"]; !ok {
		t.Error("expected 'POST /api/query' endpoint")
	}
}

// TestHTTPListDatabases tests the /api/databases endpoint
func TestHTTPListDatabases(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Database"}).
		AddRow("information_schema").
		AddRow("mysql").
		AddRow("testdb")
	mock.ExpectQuery("SHOW DATABASES").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/databases", nil)
	w := httptest.NewRecorder()

	httpListDatabases(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var result api.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPListTables tests the /api/tables endpoint
func TestHTTPListTables(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Tables_in_testdb"}).
		AddRow("users").
		AddRow("orders")
	mock.ExpectQuery("SHOW TABLES FROM `testdb`").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/tables?database=testdb", nil)
	w := httptest.NewRecorder()

	httpListTables(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPDescribeTable tests the /api/describe endpoint
func TestHTTPDescribeTable(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Field", "Type", "Collation", "Null", "Key", "Default", "Extra", "Privileges", "Comment"}).
		AddRow("id", "int", "", "NO", "PRI", "", "auto_increment", "select,insert", "").
		AddRow("name", "varchar(255)", "utf8mb4_general_ci", "NO", "", "", "", "select,insert", "")
	mock.ExpectQuery("SHOW FULL COLUMNS FROM `testdb`.`users`").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/describe?database=testdb&table=users", nil)
	w := httptest.NewRecorder()

	httpDescribeTable(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPRunQuerySuccess tests the /api/query endpoint with valid input
func TestHTTPRunQuerySuccess(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "Alice").
		AddRow(2, "Bob")
	mock.ExpectQuery("SELECT id, name FROM users").WillReturnRows(rows)

	body := `{"sql": "SELECT id, name FROM users"}`
	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	httpRunQuery(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPRunQueryInvalidJSON tests the /api/query endpoint with invalid JSON
func TestHTTPRunQueryInvalidJSON(t *testing.T) {
	_, cleanup := setupHTTPTest(t)
	defer cleanup()

	body := `{invalid json}`
	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	httpRunQuery(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

// TestHTTPRunQueryEmptySQL tests the /api/query endpoint with empty SQL
func TestHTTPRunQueryEmptySQL(t *testing.T) {
	_, cleanup := setupHTTPTest(t)
	defer cleanup()

	body := `{"sql": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	httpRunQuery(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

// TestHTTPPing tests the /api/ping endpoint
func TestHTTPPing(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	mock.ExpectPing()

	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	w := httptest.NewRecorder()

	httpPing(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPServerInfo tests the /api/server-info endpoint
func TestHTTPServerInfo(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	// Server info requires multiple queries - match the actual function behavior
	// 1. Get version
	mock.ExpectQuery("SELECT VERSION\\(\\)").WillReturnRows(
		sqlmock.NewRows([]string{"VERSION()"}).AddRow("8.0.30"),
	)

	// 2. Get server variables from performance_schema (or fallback)
	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE(.|\n)*FROM performance_schema.global_variables").WillReturnRows(
		sqlmock.NewRows([]string{"VARIABLE_NAME", "VARIABLE_VALUE"}).
			AddRow("version_comment", "MySQL Community Server").
			AddRow("character_set_server", "utf8mb4").
			AddRow("collation_server", "utf8mb4_general_ci").
			AddRow("max_connections", "151"),
	)

	// 3. Get status from performance_schema (or fallback)
	mock.ExpectQuery("SELECT VARIABLE_NAME, VARIABLE_VALUE(.|\n)*FROM performance_schema.global_status").WillReturnRows(
		sqlmock.NewRows([]string{"VARIABLE_NAME", "VARIABLE_VALUE"}).
			AddRow("Uptime", "12345").
			AddRow("Threads_connected", "5"),
	)

	// 4. Get current user and database
	mock.ExpectQuery("SELECT CURRENT_USER\\(\\), IFNULL\\(DATABASE\\(\\), ''\\)").WillReturnRows(
		sqlmock.NewRows([]string{"CURRENT_USER()", "DATABASE()"}).AddRow("root@localhost", "testdb"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/server-info", nil)
	w := httptest.NewRecorder()

	httpServerInfo(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPListConnections tests the /api/connections endpoint
func TestHTTPListConnections(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	// Set up connection manager
	cm := NewConnectionManager()
	cm.connections["test1"] = db
	cm.configs["test1"] = config.ConnectionConfig{Name: "test1", DSN: "user:pass@tcp(localhost)/db1"}
	cm.activeConn = "test1"
	connManager = cm

	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	w := httptest.NewRecorder()

	httpListConnections(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	_ = mock
}

// TestHTTPUseConnectionSuccess tests the /api/connections/use endpoint
func TestHTTPUseConnectionSuccess(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	// Set up connection manager with multiple connections
	cm := NewConnectionManager()
	cm.connections["conn1"] = db
	cm.configs["conn1"] = config.ConnectionConfig{Name: "conn1", DSN: "user:pass@tcp(localhost)/db1"}
	cm.connections["conn2"] = db
	cm.configs["conn2"] = config.ConnectionConfig{Name: "conn2", DSN: "user:pass@tcp(localhost)/db2"}
	cm.activeConn = "conn1"
	connManager = cm

	// Expect DATABASE() query after switch
	mock.ExpectQuery("SELECT DATABASE\\(\\)").WillReturnRows(
		sqlmock.NewRows([]string{"DATABASE()"}).AddRow("db2"),
	)

	body := `{"name": "conn2"}`
	req := httptest.NewRequest(http.MethodPost, "/api/connections/use", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	httpUseConnection(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPUseConnectionInvalidJSON tests the /api/connections/use endpoint with invalid JSON
func TestHTTPUseConnectionInvalidJSON(t *testing.T) {
	_, cleanup := setupHTTPTest(t)
	defer cleanup()

	body := `{invalid}`
	req := httptest.NewRequest(http.MethodPost, "/api/connections/use", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	httpUseConnection(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

// TestHTTPUseConnectionEmptyName tests the /api/connections/use endpoint with empty name
func TestHTTPUseConnectionEmptyName(t *testing.T) {
	_, cleanup := setupHTTPTest(t)
	defer cleanup()

	body := `{"name": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/connections/use", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	httpUseConnection(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

// TestHTTPListIndexes tests the /api/indexes endpoint (extended)
func TestHTTPListIndexes(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"Table", "Non_unique", "Key_name", "Seq_in_index", "Column_name",
		"Collation", "Cardinality", "Sub_part", "Packed", "Null", "Index_type",
		"Comment", "Index_comment",
	}).AddRow("users", 0, "PRIMARY", 1, "id", "A", 100, nil, nil, "", "BTREE", "", "")

	mock.ExpectQuery("SHOW INDEX FROM `testdb`.`users`").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/indexes?database=testdb&table=users", nil)
	w := httptest.NewRecorder()

	httpListIndexes(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPShowCreateTable tests the /api/create-table endpoint (extended)
func TestHTTPShowCreateTable(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Table", "Create Table"}).
		AddRow("users", "CREATE TABLE `users` (\n  `id` int NOT NULL AUTO_INCREMENT,\n  PRIMARY KEY (`id`)\n)")
	mock.ExpectQuery("SHOW CREATE TABLE `testdb`.`users`").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/create-table?database=testdb&table=users", nil)
	w := httptest.NewRecorder()

	httpShowCreateTable(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPExplainQuerySuccess tests the /api/explain endpoint (extended)
func TestHTTPExplainQuerySuccess(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "select_type", "table", "type", "possible_keys", "key", "key_len", "ref", "rows", "Extra"}).
		AddRow(1, "SIMPLE", "users", "ALL", nil, nil, nil, nil, 100, "")
	mock.ExpectQuery("EXPLAIN SELECT \\* FROM users").WillReturnRows(rows)

	body := `{"sql": "SELECT * FROM users"}`
	req := httptest.NewRequest(http.MethodPost, "/api/explain", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	httpExplainQuery(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPExplainQueryInvalidJSON tests the /api/explain endpoint with invalid JSON
func TestHTTPExplainQueryInvalidJSON(t *testing.T) {
	_, cleanup := setupHTTPTest(t)
	defer cleanup()

	body := `{invalid}`
	req := httptest.NewRequest(http.MethodPost, "/api/explain", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	httpExplainQuery(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

// TestHTTPExplainQueryEmptySQL tests the /api/explain endpoint with empty SQL
func TestHTTPExplainQueryEmptySQL(t *testing.T) {
	_, cleanup := setupHTTPTest(t)
	defer cleanup()

	body := `{"sql": ""}`
	req := httptest.NewRequest(http.MethodPost, "/api/explain", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	httpExplainQuery(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

// TestHTTPListViews tests the /api/views endpoint (extended)
func TestHTTPListViews(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TABLE_NAME", "DEFINER", "SECURITY_TYPE", "IS_UPDATABLE"}).
		AddRow("user_view", "root@localhost", "DEFINER", "YES")
	mock.ExpectQuery("SELECT TABLE_NAME, DEFINER, SECURITY_TYPE, IS_UPDATABLE").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/views?database=testdb", nil)
	w := httptest.NewRecorder()

	httpListViews(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPListTriggers tests the /api/triggers endpoint (extended)
func TestHTTPListTriggers(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TRIGGER_NAME", "EVENT_MANIPULATION", "EVENT_OBJECT_TABLE", "ACTION_TIMING", "ACTION_STATEMENT"}).
		AddRow("before_insert_users", "INSERT", "users", "BEFORE", "SET NEW.created_at = NOW()")
	mock.ExpectQuery("SELECT TRIGGER_NAME, EVENT_MANIPULATION, EVENT_OBJECT_TABLE, ACTION_TIMING").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/triggers?database=testdb", nil)
	w := httptest.NewRecorder()

	httpListTriggers(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPDatabaseSize tests the /api/size/database endpoint (extended)
func TestHTTPDatabaseSize(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TABLE_SCHEMA", "size_mb", "data_mb", "index_mb", "tables"}).
		AddRow("testdb", 10.5, 8.0, 2.5, 5)
	mock.ExpectQuery("SELECT(.|\n)*TABLE_SCHEMA(.|\n)*FROM information_schema.TABLES").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/size/database?database=testdb", nil)
	w := httptest.NewRecorder()

	httpDatabaseSize(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPTableSize tests the /api/size/tables endpoint (extended)
func TestHTTPTableSize(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"TABLE_NAME", "TABLE_ROWS", "data_mb", "index_mb", "total_mb", "ENGINE"}).
		AddRow("users", 1000, 5.0, 1.5, 6.5, "InnoDB")
	mock.ExpectQuery("SELECT(.|\n)*TABLE_NAME(.|\n)*FROM information_schema.TABLES").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/size/tables?database=testdb", nil)
	w := httptest.NewRecorder()

	httpTableSize(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPListStatus tests the /api/status endpoint (extended)
func TestHTTPListStatus(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("Uptime", "12345").
		AddRow("Threads_connected", "5")
	mock.ExpectQuery("SHOW GLOBAL STATUS").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	httpListStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPListVariables tests the /api/variables endpoint (extended)
func TestHTTPListVariables(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"Variable_name", "Value"}).
		AddRow("max_connections", "151").
		AddRow("innodb_buffer_pool_size", "134217728")
	mock.ExpectQuery("SHOW GLOBAL VARIABLES").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/variables", nil)
	w := httptest.NewRecorder()

	httpListVariables(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPForeignKeys tests the /api/foreign-keys endpoint (extended)
func TestHTTPForeignKeys(t *testing.T) {
	mock, cleanup := setupHTTPTest(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{
		"CONSTRAINT_NAME", "TABLE_NAME", "COLUMN_NAME",
		"REFERENCED_TABLE_NAME", "REFERENCED_COLUMN_NAME", "on_update", "on_delete",
	}).AddRow("fk_orders_user", "orders", "user_id", "users", "id", "CASCADE", "RESTRICT")
	mock.ExpectQuery("SELECT(.|\n)*CONSTRAINT_NAME(.|\n)*FROM information_schema.KEY_COLUMN_USAGE").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/api/foreign-keys?database=testdb", nil)
	w := httptest.NewRecorder()

	httpForeignKeys(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// TestHTTPLogger tests the httpLogger function
func TestHTTPLogger(t *testing.T) {
	// Test that httpLogger doesn't panic
	httpLogger("GET", "/api/test", 200, 100*time.Millisecond)
	httpLogger("POST", "/api/query", 500, 50*time.Millisecond)
}
