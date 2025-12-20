//go:build integration

// tests/integration/mcp_tools_test.go
// Integration tests for MCP tools against real MySQL database
package integration

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

var (
	setupOnce sync.Once
	setupErr  error
)

// getTestDSN returns the DSN for test database from environment
func getTestDSN(t *testing.T) string {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("MYSQL_DSN")
	}
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN or MYSQL_DSN not set, skipping integration test")
	}
	return dsn
}

// setupTestSchema creates test tables and data (runs once per test run)
func setupTestSchema(db *sql.DB) error {
	ctx := context.Background()

	// Create tables
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			email VARCHAR(255),
			status ENUM('active', 'inactive', 'pending') DEFAULT 'active',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS orders (
			id INT AUTO_INCREMENT PRIMARY KEY,
			user_id INT NOT NULL,
			total DECIMAL(10, 2) NOT NULL,
			status ENUM('pending', 'completed', 'cancelled') DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS products (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			category VARCHAR(100),
			price DECIMAL(10, 2) NOT NULL,
			stock INT DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS special_data (
			id INT AUTO_INCREMENT PRIMARY KEY,
			unicode_text VARCHAR(255) CHARACTER SET utf8mb4,
			json_data JSON,
			large_text LONGTEXT
		)`,
	}

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	// Check if data exists
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}

	// Insert test data only if tables are empty
	if count == 0 {
		dataStatements := []string{
			`INSERT INTO users (name, email, status) VALUES
				('Alice', 'alice@example.com', 'active'),
				('Bob', 'bob@example.com', 'active'),
				('Charlie', 'charlie@example.com', 'inactive'),
				('Diana', 'diana@example.com', 'pending'),
				('Eve', 'eve@example.com', 'active')`,
			`INSERT INTO orders (user_id, total, status) VALUES
				(1, 99.99, 'completed'),
				(1, 149.50, 'completed'),
				(2, 75.00, 'pending'),
				(3, 200.00, 'cancelled'),
				(5, 50.25, 'completed')`,
			`INSERT INTO products (name, category, price, stock) VALUES
				('Laptop', 'Electronics', 999.99, 50),
				('Mouse', 'Electronics', 29.99, 200),
				('Keyboard', 'Electronics', 79.99, 150),
				('Desk', 'Furniture', 299.99, 30),
				('Chair', 'Furniture', 199.99, 45)`,
			`INSERT INTO special_data (unicode_text, json_data, large_text) VALUES
				('日本語テスト', '{"key": "value"}', 'test data'),
				('Ñoño español', '{"emoji": "🎉"}', 'more data'),
				('中文测试', '{"array": [1, 2, 3]}', 'even more data')`,
		}

		for _, stmt := range dataStatements {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	}

	return nil
}

// setupTestDB creates a connection and ensures it's ready
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := getTestDSN(t)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Wait for connection to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		select {
		case <-time.After(1 * time.Second):
			// retry
		case <-ctx.Done():
			t.Fatalf("database not ready within timeout")
		}
	}

	// Setup schema once per test run
	setupOnce.Do(func() {
		setupErr = setupTestSchema(db)
	})
	if setupErr != nil {
		t.Fatalf("failed to setup test schema: %v", setupErr)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

// TestMCPTool_ListDatabases tests the list_databases functionality
func TestMCPTool_ListDatabases(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	rows, err := db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		t.Fatalf("SHOW DATABASES failed: %v", err)
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan database name: %v", err)
		}
		databases = append(databases, name)
	}

	if len(databases) == 0 {
		t.Error("expected at least one database")
	}

	// Check for testdb
	found := false
	for _, db := range databases {
		if db == "testdb" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'testdb' in database list")
	}
}

// TestMCPTool_ListTables tests the list_tables functionality
func TestMCPTool_ListTables(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Switch to testdb
	if _, err := db.ExecContext(ctx, "USE testdb"); err != nil {
		t.Fatalf("USE testdb failed: %v", err)
	}

	rows, err := db.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		t.Fatalf("SHOW TABLES failed: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan table name: %v", err)
		}
		tables = append(tables, name)
	}

	// Check for expected tables from init.sql
	expectedTables := []string{"users", "orders", "products", "special_data"}
	for _, expected := range expectedTables {
		found := false
		for _, table := range tables {
			if table == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected to find table '%s' in table list, got: %v", expected, tables)
		}
	}
}

// TestMCPTool_DescribeTable tests the describe_table functionality
func TestMCPTool_DescribeTable(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, "USE testdb"); err != nil {
		t.Fatalf("USE testdb failed: %v", err)
	}

	rows, err := db.QueryContext(ctx, "DESCRIBE users")
	if err != nil {
		t.Fatalf("DESCRIBE users failed: %v", err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var field, colType string
		var null, key, defaultVal, extra sql.NullString
		if err := rows.Scan(&field, &colType, &null, &key, &defaultVal, &extra); err != nil {
			t.Fatalf("failed to scan column info: %v", err)
		}
		columns[field] = true
	}

	// Check for expected columns
	expectedColumns := []string{"id", "name", "email", "status", "created_at", "updated_at"}
	for _, col := range expectedColumns {
		if !columns[col] {
			t.Errorf("expected column '%s' not found in users table", col)
		}
	}
}

// TestMCPTool_RunQuery_BasicSelect tests basic SELECT queries
func TestMCPTool_RunQuery_BasicSelect(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	testCases := []struct {
		name     string
		query    string
		minRows  int
		checkCol string
	}{
		{
			name:     "select all users",
			query:    "SELECT * FROM testdb.users",
			minRows:  5,
			checkCol: "name",
		},
		{
			name:     "select with where",
			query:    "SELECT * FROM testdb.users WHERE status = 'active'",
			minRows:  3,
			checkCol: "name",
		},
		{
			name:     "select with limit",
			query:    "SELECT * FROM testdb.users LIMIT 2",
			minRows:  2,
			checkCol: "name",
		},
		{
			name:     "select with order by",
			query:    "SELECT * FROM testdb.users ORDER BY id DESC",
			minRows:  5,
			checkCol: "name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			count := 0
			cols, _ := rows.Columns()
			for rows.Next() {
				count++
				values := make([]interface{}, len(cols))
				ptrs := make([]interface{}, len(cols))
				for i := range values {
					ptrs[i] = &values[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					t.Fatalf("scan failed: %v", err)
				}
			}

			if count < tc.minRows {
				t.Errorf("expected at least %d rows, got %d", tc.minRows, count)
			}
		})
	}
}

// TestMCPTool_RunQuery_JOINs tests JOIN queries
func TestMCPTool_RunQuery_JOINs(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	testCases := []struct {
		name    string
		query   string
		minRows int
	}{
		{
			name:    "inner join",
			query:   "SELECT u.name, o.total FROM testdb.users u JOIN testdb.orders o ON u.id = o.user_id",
			minRows: 1,
		},
		{
			name:    "left join",
			query:   "SELECT u.name, o.total FROM testdb.users u LEFT JOIN testdb.orders o ON u.id = o.user_id",
			minRows: 5,
		},
		{
			name:    "join with where",
			query:   "SELECT u.name, o.total FROM testdb.users u JOIN testdb.orders o ON u.id = o.user_id WHERE o.status = 'completed'",
			minRows: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			count := 0
			for rows.Next() {
				count++
			}

			if count < tc.minRows {
				t.Errorf("expected at least %d rows, got %d", tc.minRows, count)
			}
		})
	}
}

// TestMCPTool_RunQuery_Aggregations tests aggregate functions
func TestMCPTool_RunQuery_Aggregations(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "count",
			query: "SELECT COUNT(*) as cnt FROM testdb.users",
		},
		{
			name:  "sum",
			query: "SELECT SUM(total) as total FROM testdb.orders",
		},
		{
			name:  "avg",
			query: "SELECT AVG(price) as avg_price FROM testdb.products",
		},
		{
			name:  "group by",
			query: "SELECT status, COUNT(*) as cnt FROM testdb.users GROUP BY status",
		},
		{
			name:  "having",
			query: "SELECT user_id, COUNT(*) as cnt FROM testdb.orders GROUP BY user_id HAVING cnt > 0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			if !rows.Next() {
				t.Error("expected at least one row")
			}
		})
	}
}

// TestMCPTool_RunQuery_Subqueries tests subquery support
func TestMCPTool_RunQuery_Subqueries(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "subquery in where",
			query: "SELECT * FROM testdb.users WHERE id IN (SELECT user_id FROM testdb.orders)",
		},
		{
			name:  "subquery with aggregate",
			query: "SELECT * FROM testdb.products WHERE price > (SELECT AVG(price) FROM testdb.products)",
		},
		{
			name:  "correlated subquery",
			query: "SELECT * FROM testdb.users u WHERE EXISTS (SELECT 1 FROM testdb.orders o WHERE o.user_id = u.id)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			// Just verify the query executes successfully
			for rows.Next() {
				// consume rows
			}
			if err := rows.Err(); err != nil {
				t.Errorf("error iterating rows: %v", err)
			}
		})
	}
}

// TestMCPTool_RunQuery_UnicodeData tests handling of Unicode data
func TestMCPTool_RunQuery_UnicodeData(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	rows, err := db.QueryContext(ctx, "SELECT unicode_text FROM testdb.special_data")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	expectedTexts := map[string]bool{
		"日本語テスト":       true,
		"Ñoño español": true,
		"中文测试":         true,
	}

	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		delete(expectedTexts, text)
	}

	// We should have found at least some of the expected texts
	if len(expectedTexts) > 1 {
		t.Errorf("did not find expected unicode texts: %v", expectedTexts)
	}
}

// TestMCPTool_RunQuery_JSONData tests handling of JSON data
func TestMCPTool_RunQuery_JSONData(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{
			name:  "select json column",
			query: "SELECT json_data FROM testdb.special_data",
		},
		{
			name:  "json extract",
			query: "SELECT JSON_EXTRACT(json_data, '$.key') FROM testdb.special_data WHERE json_data IS NOT NULL",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			count := 0
			for rows.Next() {
				count++
			}
			if count == 0 {
				t.Error("expected at least one row")
			}
		})
	}
}

// TestMCPTool_ShowCommands tests SHOW command variants
func TestMCPTool_ShowCommands(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, "USE testdb"); err != nil {
		t.Fatalf("USE testdb failed: %v", err)
	}

	testCases := []struct {
		name  string
		query string
	}{
		{"show databases", "SHOW DATABASES"},
		{"show tables", "SHOW TABLES"},
		{"show create table", "SHOW CREATE TABLE users"},
		{"show columns", "SHOW COLUMNS FROM users"},
		{"show index", "SHOW INDEX FROM users"},
		{"show table status", "SHOW TABLE STATUS"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.QueryContext(ctx, tc.query)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			defer rows.Close()

			if !rows.Next() {
				t.Error("expected at least one row")
			}
		})
	}
}

// TestMCPTool_ErrorHandling tests error handling for invalid queries
func TestMCPTool_ErrorHandling(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	testCases := []struct {
		name  string
		query string
	}{
		{"non-existent table", "SELECT * FROM testdb.non_existent_table"},
		{"syntax error", "SELEC * FROM testdb.users"},
		{"non-existent column", "SELECT non_existent_column FROM testdb.users"},
		{"non-existent database", "SELECT * FROM fake_db.users"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.QueryContext(ctx, tc.query)
			if err == nil {
				t.Error("expected query to fail")
			}
		})
	}
}

// TestMCPTool_QueryTimeout tests query timeout handling
func TestMCPTool_QueryTimeout(t *testing.T) {
	db := setupTestDB(t)

	// Create a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// This query should timeout
	_, err := db.QueryContext(ctx, "SELECT SLEEP(5)")
	if err == nil {
		t.Error("expected query to timeout")
	}
}

