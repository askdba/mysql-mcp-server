// internal/util/sql_parser_test.go
package util

import (
	"sort"
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

		// Semicolons inside string literals should be allowed (not multi-statement)
		"SELECT * FROM users WHERE name = 'test;value'",
		"SELECT * FROM users WHERE data = 'a;b;c;d'",
		"SELECT * FROM users WHERE sql_example = 'SELECT * FROM t; DROP TABLE t'",
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

func TestValidateSQLWithParser_AllowSystemSchemas(t *testing.T) {
	// Opt-in (MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS) unlocks the read-only diagnostic
	// schemas but must NOT unlock `mysql`.
	defer SetAllowSystemSchemas(false) // restore default for other tests
	SetAllowSystemSchemas(true)

	allowed := []struct {
		query  string
		schema string
	}{
		{"SELECT TABLE_NAME, CARDINALITY FROM information_schema.STATISTICS", "information_schema"},
		{"SELECT * FROM performance_schema.table_io_waits_summary_by_index_usage", "performance_schema"},
		{"SELECT * FROM sys.schema_redundant_indexes", "sys"},
		{"SELECT * FROM Information_Schema.STATISTICS", "information_schema (case)"},
	}
	for _, tc := range allowed {
		t.Run("allowed/"+tc.schema, func(t *testing.T) {
			if err := ValidateSQLCombined(tc.query); err != nil {
				t.Errorf("expected %s to be allowed when flag is on, got: %v\nQuery: %s", tc.schema, err, tc.query)
			}
		})
	}

	// `mysql` stays blocked even with the flag on.
	for _, q := range []string{"SELECT * FROM mysql.user", "SELECT * FROM MYSQL.user"} {
		t.Run("still-blocked/mysql", func(t *testing.T) {
			if err := ValidateSQLCombined(q); err == nil {
				t.Errorf("expected mysql schema to remain blocked even with flag on\nQuery: %s", q)
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

func TestReferencedSchemaQualifiers(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"SELECT * FROM users", nil},
		{"SELECT * FROM other.t", []string{"other"}},
		{"SELECT * FROM a.t JOIN b.u ON 1=1", []string{"a", "b"}},
		{"SELECT * FROM users u WHERE u.id IN (SELECT x FROM other.t)", []string{"other"}},
		{"SELECT 1 ORDER BY (SELECT a FROM other.t)", []string{"other"}},
		{"SHOW TABLES FROM mydb", []string{"mydb"}},
		{"USE myapp", []string{"myapp"}},
		{"EXPLAIN SELECT 1 FROM z.t", []string{"z"}},
		{"EXPLAIN FORMAT=JSON SELECT 1 FROM fmtjs.t", []string{"fmtjs"}},
		{"EXPLAIN EXTENDED SELECT 1 FROM ext.t", []string{"ext"}},
		{"DESCRIBE otherdb.tbl", []string{"otherdb"}},
	}
	for _, tc := range tests {
		name := tc.query
		if len(name) > 50 {
			name = name[:50] + "…"
		}
		t.Run(name, func(t *testing.T) {
			got, err := ReferencedSchemaQualifiers(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for k := range got {
				names = append(names, k)
			}
			sort.Strings(names)
			exp := append([]string(nil), tc.want...)
			sort.Strings(exp)
			if len(names) != len(exp) {
				t.Fatalf("got %v, want %v", names, exp)
			}
			for i := range names {
				if names[i] != exp[i] {
					t.Fatalf("got %v, want %v", names, exp)
				}
			}
		})
	}

	t.Run("multi-statement rejected", func(t *testing.T) {
		_, err := ReferencedSchemaQualifiers("SELECT 1 FROM a.t; SELECT 1 FROM b.t")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestShowEnumeratesAllSchemasInQuery(t *testing.T) {
	if !ShowEnumeratesAllSchemasInQuery("SHOW DATABASES") {
		t.Fatal("expected true for SHOW DATABASES")
	}
	if !ShowEnumeratesAllSchemasInQuery("SHOW DATABASES LIKE 'p%' ") {
		t.Fatal("expected true for SHOW DATABASES LIKE")
	}
	if ShowEnumeratesAllSchemasInQuery("SHOW TABLES") {
		t.Fatal("expected false for SHOW TABLES")
	}
	if ShowEnumeratesAllSchemasInQuery("SELECT 1") {
		t.Fatal("expected false for SELECT")
	}
}

func TestReferencedSchemaQualifiersExplainDML(t *testing.T) {
	got, err := ReferencedSchemaQualifiers("EXPLAIN DELETE FROM otherdb.t WHERE id=1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["otherdb"]; !ok {
		t.Fatalf("expected otherdb in %v", got)
	}
}

func TestCollectFromOtherReadNonExplain(t *testing.T) {
	out := make(map[string]struct{})
	ok, err := collectFromOtherRead("SELECT 1 FROM a.b", out)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected handled=false when text is not EXPLAIN/DESCRIBE")
	}
	if len(out) != 0 {
		t.Fatalf("expected empty out, got %v", out)
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

func TestValidateSQLWithParser_DangerousFunctionsInJoinCondition(t *testing.T) {
	// These queries have dangerous functions in JOIN ON clauses
	// They should be blocked by the parser
	dangerousQueries := []struct {
		name  string
		query string
	}{
		{"release_all_locks in JOIN ON", "SELECT * FROM users u JOIN orders o ON release_all_locks() = 1"},
		{"sleep in JOIN ON", "SELECT * FROM users u JOIN orders o ON sleep(5) = 0"},
		{"get_lock in INNER JOIN ON", "SELECT * FROM users u INNER JOIN orders o ON get_lock('x', 10) = 1"},
		{"benchmark in LEFT JOIN ON", "SELECT * FROM a LEFT JOIN b ON benchmark(1000000, SHA1('test')) > 0"},
		{"load_file in RIGHT JOIN ON", "SELECT * FROM a RIGHT JOIN b ON load_file('/etc/passwd') IS NOT NULL"},
		{"release_lock in multiple JOIN", "SELECT * FROM a JOIN b ON a.id = b.id JOIN c ON release_lock('x') = 1"},
		{"is_free_lock in JOIN with complex condition", "SELECT * FROM a JOIN b ON a.id = b.id AND is_free_lock('x') = 1"},
	}

	for _, tc := range dangerousQueries {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSQLWithParser(tc.query)
			if err == nil {
				t.Errorf("expected dangerous function in JOIN ON clause to be blocked\nQuery: %s", tc.query)
			}
		})
	}
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestInjectLimit(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		limit     int
		wantSufx  string // expected suffix (case-insensitive)
		unchanged bool   // true when the original query should be returned unchanged
	}{
		{
			name:     "SELECT without LIMIT gets one added",
			sql:      "SELECT * FROM users",
			limit:    100,
			wantSufx: " LIMIT 100",
		},
		{
			name:      "SELECT that already has LIMIT is not changed",
			sql:       "SELECT * FROM users LIMIT 5",
			limit:     100,
			unchanged: true,
		},
		{
			name:     "SELECT with ORDER BY gets LIMIT appended",
			sql:      "SELECT id FROM orders ORDER BY id DESC",
			limit:    50,
			wantSufx: " LIMIT 50",
		},
		{
			name:     "SELECT with trailing semicolon strips semicolon and adds LIMIT",
			sql:      "SELECT 1;",
			limit:    10,
			wantSufx: " LIMIT 10",
		},
		{
			name:      "SHOW statement is not modified",
			sql:       "SHOW TABLES",
			limit:     100,
			unchanged: true,
		},
		{
			name:      "DESCRIBE statement is not modified",
			sql:       "DESCRIBE users",
			limit:     100,
			unchanged: true,
		},
		{
			name:      "limit=0 is a no-op",
			sql:       "SELECT * FROM t",
			limit:     0,
			unchanged: true,
		},
		{
			name:      "negative limit is a no-op",
			sql:       "SELECT * FROM t",
			limit:     -1,
			unchanged: true,
		},
		{
			name:     "UNION without LIMIT gets one added",
			sql:      "SELECT id FROM a UNION SELECT id FROM b",
			limit:    20,
			wantSufx: " LIMIT 20",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := InjectLimit(tc.sql, tc.limit)
			if tc.unchanged {
				if got != tc.sql {
					t.Errorf("expected unchanged SQL %q, got %q", tc.sql, got)
				}
				return
			}
			if !strings.HasSuffix(got, tc.wantSufx) {
				t.Errorf("expected SQL to end with %q, got %q", tc.wantSufx, got)
			}
		})
	}
}

func TestHasSelectStar(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{"SELECT *", "SELECT * FROM users", true},
		{"SELECT t.*", "SELECT t.* FROM users t", true},
		{"SELECT columns", "SELECT id, name FROM users", false},
		{"SHOW TABLES", "SHOW TABLES", false},
		{"SELECT with no star", "SELECT id FROM users WHERE id = 1", false},
		{"UNION with star on left", "SELECT * FROM a UNION SELECT id FROM b", true},
		{"COUNT star is not a bare star", "SELECT COUNT(*) FROM users", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := HasSelectStar(tc.sql)
			if got != tc.want {
				t.Errorf("HasSelectStar(%q) = %v, want %v", tc.sql, got, tc.want)
			}
		})
	}
}
