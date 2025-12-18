// internal/util/sql_parser_test.go
package util

import (
	"strings"
	"testing"
)

func TestValidateSQLWithParser_AllowedQueries(t *testing.T) {
	allowedQueries := []string{
		// Basic SELECT
		"SELECT * FROM users",
		"SELECT id, name FROM users",
		"SELECT id, name FROM users WHERE id = 1",
		"SELECT * FROM users ORDER BY id",
		"SELECT * FROM users LIMIT 10",
		"SELECT * FROM users LIMIT 10 OFFSET 5",

		// JOINs
		"SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id",
		"SELECT * FROM users LEFT JOIN orders ON users.id = orders.user_id",
		"SELECT * FROM users u INNER JOIN orders o ON u.id = o.user_id WHERE o.total > 100",

		// Aggregations
		"SELECT COUNT(*) FROM users",
		"SELECT AVG(price) FROM products",
		"SELECT user_id, SUM(total) FROM orders GROUP BY user_id",
		"SELECT user_id, COUNT(*) as cnt FROM orders GROUP BY user_id HAVING cnt > 5",

		// Subqueries in WHERE
		"SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)",
		"SELECT * FROM products WHERE price > (SELECT AVG(price) FROM products)",

		// UNION
		"SELECT id, name FROM users UNION SELECT id, name FROM admins",
		"SELECT id FROM users UNION ALL SELECT id FROM orders",

		// SHOW statements
		"SHOW DATABASES",
		"SHOW TABLES",
		"SHOW CREATE TABLE users",
		"SHOW COLUMNS FROM users",

		// Complex WHERE clauses
		"SELECT * FROM users WHERE name LIKE '%john%'",
		"SELECT * FROM users WHERE created_at BETWEEN '2024-01-01' AND '2024-12-31'",
		"SELECT * FROM users WHERE status IN ('active', 'pending')",
		"SELECT * FROM users WHERE (age > 18 AND status = 'active') OR role = 'admin'",

		// Functions (safe ones)
		"SELECT NOW(), CURDATE()",
		"SELECT CONCAT(first_name, ' ', last_name) FROM users",
		"SELECT UPPER(name) FROM users",
		"SELECT DATE_FORMAT(created_at, '%Y-%m-%d') FROM users",

		// CASE statements
		"SELECT id, CASE WHEN status = 1 THEN 'active' ELSE 'inactive' END FROM users",

		// Aliases
		"SELECT u.id AS user_id, u.name AS user_name FROM users u",

		// DISTINCT
		"SELECT DISTINCT category FROM products",

		// WITH trailing semicolon (should be stripped)
		"SELECT * FROM users;",
	}

	for _, query := range allowedQueries {
		t.Run(query[:min(50, len(query))], func(t *testing.T) {
			err := ValidateSQLWithParser(query)
			if err != nil {
				t.Errorf("expected query to be allowed, got error: %v\nQuery: %s", err, query)
			}
		})
	}
}

func TestValidateSQLWithParser_BlockedStatements(t *testing.T) {
	blockedQueries := []struct {
		query  string
		reason string
	}{
		// INSERT
		{"INSERT INTO users (name) VALUES ('test')", "INSERT"},
		{"INSERT INTO users SELECT * FROM temp", "INSERT"},

		// UPDATE
		{"UPDATE users SET name = 'test'", "UPDATE"},
		{"UPDATE users SET status = 1 WHERE id = 1", "UPDATE"},

		// DELETE
		{"DELETE FROM users", "DELETE"},
		{"DELETE FROM users WHERE id = 1", "DELETE"},

		// DDL
		{"CREATE TABLE test (id INT)", "CREATE"},
		{"DROP TABLE users", "DROP"},
		{"ALTER TABLE users ADD COLUMN email VARCHAR(255)", "ALTER"},
		{"TRUNCATE TABLE users", "TRUNCATE"},

		// Transactions
		{"BEGIN", "transaction"},
		{"COMMIT", "transaction"},
		{"ROLLBACK", "transaction"},
		{"START TRANSACTION", "transaction"},

		// SET statements
		{"SET @var = 1", "SET"},
		{"SET GLOBAL max_connections = 100", "SET"},

		// Multi-statement (SQL injection attempt)
		{"SELECT * FROM users; DROP TABLE users", "multi-statement"},
		{"SELECT * FROM users; DELETE FROM users", "multi-statement"},
	}

	for _, tc := range blockedQueries {
		t.Run(tc.reason, func(t *testing.T) {
			err := ValidateSQLWithParser(tc.query)
			if err == nil {
				t.Errorf("expected query to be blocked (%s), but it was allowed\nQuery: %s", tc.reason, tc.query)
			}
		})
	}
}

func TestValidateSQLWithParser_DangerousFunctions(t *testing.T) {
	dangerousQueries := []struct {
		query    string
		function string
	}{
		// Time-based attacks
		{"SELECT SLEEP(5)", "sleep"},
		{"SELECT * FROM users WHERE SLEEP(5)", "sleep"},
		{"SELECT BENCHMARK(1000000, SHA1('test'))", "benchmark"},

		// Locking
		{"SELECT GET_LOCK('lock', 10)", "get_lock"},
		{"SELECT RELEASE_LOCK('lock')", "release_lock"},
		{"SELECT IS_FREE_LOCK('lock')", "is_free_lock"},
		{"SELECT IS_USED_LOCK('lock')", "is_used_lock"},

		// File operations
		{"SELECT LOAD_FILE('/etc/passwd')", "load_file"},

		// Nested in WHERE
		{"SELECT * FROM users WHERE id = 1 AND SLEEP(5) = 0", "sleep"},

		// In subquery
		{"SELECT * FROM users WHERE id IN (SELECT SLEEP(5))", "sleep"},

		// Case variations
		{"SELECT sleep(5)", "sleep"},
		{"SELECT SLEEP(5)", "sleep"},
		{"SELECT Sleep(5)", "sleep"},
	}

	for _, tc := range dangerousQueries {
		t.Run(tc.function, func(t *testing.T) {
			err := ValidateSQLWithParser(tc.query)
			if err == nil {
				t.Errorf("expected dangerous function %s to be blocked\nQuery: %s", tc.function, tc.query)
			}
			if err != nil && !strings.Contains(strings.ToLower(err.Error()), "dangerous") &&
				!strings.Contains(strings.ToLower(err.Error()), tc.function) {
				t.Errorf("expected error to mention dangerous function, got: %v", err)
			}
		})
	}
}

func TestValidateSQLWithParser_SystemSchemas(t *testing.T) {
	dangerousSchemaQueries := []struct {
		query  string
		schema string
	}{
		{"SELECT * FROM mysql.user", "mysql"},
		{"SELECT * FROM information_schema.tables", "information_schema"},
		{"SELECT * FROM performance_schema.events_statements_summary_by_digest", "performance_schema"},
		{"SELECT * FROM sys.session", "sys"},

		// Case variations
		{"SELECT * FROM MYSQL.user", "mysql"},
		{"SELECT * FROM Information_Schema.tables", "information_schema"},
	}

	for _, tc := range dangerousSchemaQueries {
		t.Run(tc.schema, func(t *testing.T) {
			err := ValidateSQLWithParser(tc.query)
			if err == nil {
				t.Errorf("expected access to %s schema to be blocked\nQuery: %s", tc.schema, tc.query)
			}
		})
	}
}

func TestValidateSQLWithParser_SQLInjectionAttempts(t *testing.T) {
	// Test injection attempts that should be caught by the parser
	parserCaught := []struct {
		name  string
		query string
	}{
		// These are caught by the parser (system schema access)
		{"union injection to mysql.user", "SELECT * FROM users WHERE id = 1 UNION SELECT * FROM mysql.user"},
		{"stacked queries", "SELECT * FROM users; DROP TABLE users;"},
	}

	for _, tc := range parserCaught {
		t.Run(tc.name+"_parser", func(t *testing.T) {
			err := ValidateSQLWithParser(tc.query)
			if err == nil {
				t.Errorf("expected parser to block injection\nQuery: %s", tc.query)
			}
		})
	}

	// Test injection attempts that should be caught by combined validator (regex + parser)
	combinedCaught := []struct {
		name  string
		query string
	}{
		// SQL comments (caught by regex)
		{"comment injection", "SELECT * FROM users WHERE id = 1 -- AND password = 'x'"},

		// INTO OUTFILE/DUMPFILE (caught by regex)
		{"outfile", "SELECT * FROM users INTO OUTFILE '/tmp/test.txt'"},
		{"dumpfile", "SELECT * FROM users INTO DUMPFILE '/tmp/test.txt'"},
	}

	for _, tc := range combinedCaught {
		t.Run(tc.name+"_combined", func(t *testing.T) {
			err := ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("expected combined validator to block injection\nQuery: %s", tc.query)
			}
		})
	}
}

func TestValidateSQLWithParser_HexEncodingAllowed(t *testing.T) {
	// Hex encoding is valid SQL and not inherently dangerous
	// The actual protection comes from using parameterized queries
	// and the read-only MySQL user
	query := "SELECT * FROM users WHERE name = 0x61646d696e"
	err := ValidateSQLWithParser(query)
	if err != nil {
		t.Errorf("hex encoding should be allowed (it's valid SQL): %v", err)
	}
}

func TestValidateSQLWithParser_EdgeCases(t *testing.T) {
	testCases := []struct {
		name      string
		query     string
		expectErr bool
	}{
		{"empty query", "", true},
		{"whitespace only", "   ", true},
		{"just semicolon", ";", true},
		{"invalid SQL", "NOT VALID SQL AT ALL", true},
		{"incomplete SELECT", "SELECT FROM", true},

		// Valid edge cases
		{"select 1", "SELECT 1", false},
		{"select with newlines", "SELECT\n*\nFROM\nusers", false},
		{"select with tabs", "SELECT\t*\tFROM\tusers", false},

		// Note: Standalone parenthesized SELECT like "(SELECT * FROM users)" 
		// is not valid top-level SQL syntax - it's only valid as a subquery
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSQLWithParser(tc.query)
			if tc.expectErr && err == nil {
				t.Errorf("expected error for query: %s", tc.query)
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error: %v\nQuery: %s", err, tc.query)
			}
		})
	}
}

func TestValidateSQLCombined(t *testing.T) {
	// Test that combined validation catches things both validators would catch
	testCases := []struct {
		name      string
		query     string
		expectErr bool
	}{
		// Should be allowed
		{"basic select", "SELECT * FROM users", false},
		{"show tables", "SHOW TABLES", false},

		// Should be blocked by parser
		{"insert", "INSERT INTO users VALUES (1)", true},
		{"delete", "DELETE FROM users", true},

		// Should be blocked by regex (defense in depth)
		{"sleep function", "SELECT SLEEP(5)", true},
		{"load_file", "SELECT LOAD_FILE('/etc/passwd')", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSQLCombined(tc.query)
			if tc.expectErr && err == nil {
				t.Errorf("expected error for query: %s", tc.query)
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error: %v\nQuery: %s", err, tc.query)
			}
		})
	}
}

func TestParserValidationError(t *testing.T) {
	err := &ParserValidationError{Reason: "test reason", Statement: "test statement"}
	expected := "test reason: test statement"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}

	err2 := &ParserValidationError{Reason: "only reason"}
	if err2.Error() != "only reason" {
		t.Errorf("expected 'only reason', got %q", err2.Error())
	}
}

func TestDangerousFunctionsMap(t *testing.T) {
	// Ensure dangerous functions are properly defined
	expectedDangerous := []string{
		"sleep", "benchmark", "get_lock", "release_lock",
		"is_free_lock", "is_used_lock", "load_file",
	}

	for _, fn := range expectedDangerous {
		if !DangerousFunctions[fn] {
			t.Errorf("expected %s to be in DangerousFunctions map", fn)
		}
	}
}

func TestDangerousSchemasMap(t *testing.T) {
	// Ensure dangerous schemas are properly defined
	expectedDangerous := []string{
		"mysql", "information_schema", "performance_schema", "sys",
	}

	for _, schema := range expectedDangerous {
		if !DangerousSchemas[schema] {
			t.Errorf("expected %s to be in DangerousSchemas map", schema)
		}
	}
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
