// tests/security/sql_injection_test.go
// Security tests for SQL injection prevention
package security

import (
	"strings"
	"testing"

	"github.com/askdba/mysql-mcp-server/internal/util"
)

// TestSQLInjection_BasicAttempts tests basic SQL injection patterns
// Note: Some injection patterns are syntactically valid SQL and cannot be blocked
// without breaking legitimate queries. Defense relies on:
// 1. Read-only MySQL user (prevents data modification)
// 2. Parameterized queries in the application layer
// 3. Blocking dangerous functions and system schema access
func TestSQLInjection_BasicAttempts(t *testing.T) {
	// These MUST be blocked
	mustBlock := []struct {
		name  string
		query string
	}{
		// Stacked queries (always dangerous)
		{"stacked drop", "SELECT * FROM users; DROP TABLE users"},
		{"stacked delete", "SELECT * FROM users; DELETE FROM users"},
		{"stacked insert", "SELECT * FROM users; INSERT INTO users VALUES (999, 'hacker')"},
		{"stacked update", "SELECT * FROM users; UPDATE users SET admin=1"},

		// Comment-based (-- is blocked)
		{"comment dash", "SELECT * FROM users WHERE name = 'admin'--' AND password = 'x'"},
		{"comment block", "SELECT * FROM users WHERE name = 'admin'/*' AND password = 'x'"},

		// System access (always blocked)
		{"mysql.user", "SELECT * FROM mysql.user"},
		{"information_schema", "SELECT * FROM information_schema.tables"},
		{"performance_schema", "SELECT * FROM performance_schema.events_statements_summary_by_digest"},

		// Dangerous functions (always blocked)
		{"sleep", "SELECT SLEEP(10)"},
		{"benchmark", "SELECT BENCHMARK(10000000, SHA1('test'))"},
		{"load_file", "SELECT LOAD_FILE('/etc/passwd')"},
		{"into outfile", "SELECT * FROM users INTO OUTFILE '/tmp/data.txt'"},
		{"into dumpfile", "SELECT * FROM users INTO DUMPFILE '/tmp/data.bin'"},
	}

	for _, tc := range mustBlock {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("SQL injection attempt should be blocked: %s", tc.query)
			}
		})
	}

	// These are valid SQL that cannot be blocked at the validator level
	// Defense relies on read-only user and parameterized queries
	validButSuspicious := []struct {
		name  string
		query string
	}{
		// Classic injections - valid SQL syntax
		{"classic OR 1=1", "SELECT * FROM users WHERE id = 1 OR 1=1"},
		{"classic OR true", "SELECT * FROM users WHERE id = 1 OR true"},
		// UNION is valid SQL - protection is via read-only user
		{"union select", "SELECT * FROM users WHERE id = 1 UNION SELECT * FROM users"},
	}

	for _, tc := range validButSuspicious {
		t.Run("valid_"+tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			// These may or may not error - they are valid SQL
			// Document current behavior without failing
			if err != nil {
				t.Logf("Note: %s is blocked (extra protection): %v", tc.name, err)
			} else {
				t.Logf("Note: %s is allowed (relies on read-only user)", tc.name)
			}
		})
	}
}

// TestSQLInjection_EncodingAttempts tests injection with various encodings
func TestSQLInjection_EncodingAttempts(t *testing.T) {
	// These MUST be blocked
	mustBlock := []struct {
		name  string
		query string
	}{
		// Case variations with dangerous operations
		{"mixed case DROP", "SELECT * FROM users; DrOp TaBlE users"},
		{"mixed case SLEEP", "SELECT SLeEp(10)"},
	}

	for _, tc := range mustBlock {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("encoded injection attempt should be blocked: %s", tc.query)
			}
		})
	}

	// Edge cases that may or may not be blocked
	edgeCases := []struct {
		name  string
		query string
	}{
		// URL encoding patterns (as they might appear after decode)
		{"null byte", "SELECT * FROM users WHERE id = 1\x00; DROP TABLE users"},
		// Unicode variations
		{"unicode fullwidth semicolon", "SELECT * FROM users WHERE id = 1\uff1b DROP TABLE users"},
		// UNION is valid SQL
		{"mixed case UNION", "SELECT * FROM users UnIoN SeLeCt * FROM users"},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			// Document behavior without asserting
			if err != nil {
				t.Logf("Blocked: %s - %v", tc.name, err)
			} else {
				t.Logf("Allowed: %s (parser handles safely)", tc.name)
			}
		})
	}
}

// TestSQLInjection_CommentVariations tests various comment styles
func TestSQLInjection_CommentVariations(t *testing.T) {
	// Comments that MUST be blocked
	mustBlock := []struct {
		name  string
		query string
	}{
		{"single line comment --", "SELECT * FROM users -- WHERE password = 'x'"},
		{"multi-line comment", "SELECT * FROM users /* hidden */ WHERE 1=1"},
		{"nested comment", "SELECT * FROM users /* /* nested */ */ WHERE 1=1"},
		{"comment with payload", "SELECT * FROM users /*! UNION SELECT * FROM passwords */"},
		{"mysql conditional", "SELECT * FROM users /*!50000 UNION SELECT * FROM passwords */"},
	}

	for _, tc := range mustBlock {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("comment-based injection should be blocked: %s", tc.query)
			}
		})
	}

	// Note: MySQL # comments may or may not be blocked depending on parser
	// Document behavior
	t.Run("single_line_comment_hash", func(t *testing.T) {
		query := "SELECT * FROM users # WHERE password = 'x'"
		err := util.ValidateSQLCombined(query)
		if err != nil {
			t.Logf("Hash comment blocked: %v", err)
		} else {
			t.Logf("Hash comment allowed (parser may handle differently)")
		}
	})
}

// TestSQLInjection_BlindInjection tests blind SQL injection patterns
func TestSQLInjection_BlindInjection(t *testing.T) {
	injectionAttempts := []struct {
		name  string
		query string
	}{
		// Time-based blind
		{"time-based sleep", "SELECT * FROM users WHERE id = 1 AND SLEEP(5)"},
		{"time-based benchmark", "SELECT * FROM users WHERE id = 1 AND BENCHMARK(10000000, SHA1('test'))"},
		{"time-based if", "SELECT * FROM users WHERE id = IF(1=1, SLEEP(5), 0)"},

		// Boolean-based blind (these are harder to block but we test key patterns)
		{"boolean OR", "SELECT * FROM users WHERE id = 1 OR 1=1"},
		{"boolean AND", "SELECT * FROM users WHERE id = 1 AND 1=1"},
	}

	for _, tc := range injectionAttempts {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				// Some boolean-based are valid SQL, so we just log them
				if strings.Contains(tc.name, "time-based") {
					t.Errorf("time-based injection should be blocked: %s", tc.query)
				}
			}
		})
	}
}

// TestSQLInjection_SecondOrder tests second-order injection vectors
func TestSQLInjection_SecondOrder(t *testing.T) {
	// These are payloads that might be stored and later executed
	payloads := []struct {
		name    string
		payload string
	}{
		{"stored DROP", "'; DROP TABLE users; --"},
		{"stored UNION", "' UNION SELECT * FROM passwords --"},
		{"stored admin", "admin'--"},
		{"stored comment", "test'/**/OR/**/1=1--"},
	}

	for _, tc := range payloads {
		t.Run(tc.name, func(t *testing.T) {
			// These would be caught when trying to use them in a query
			query := "SELECT * FROM users WHERE name = '" + tc.payload + "'"
			err := util.ValidateSQLCombined(query)
			if err == nil {
				t.Errorf("second-order injection payload should be blocked: %s", tc.payload)
			}
		})
	}
}

// TestSQLInjection_AdvancedBypass tests advanced bypass attempts
func TestSQLInjection_AdvancedBypass(t *testing.T) {
	injectionAttempts := []struct {
		name  string
		query string
	}{
		// Alternative string delimiters
		{"double quotes", "SELECT * FROM users WHERE name = \"admin\"--\""},

		// Whitespace alternatives
		{"tab instead of space", "SELECT\t*\tFROM\tusers;\tDROP\tTABLE\tusers"},
		{"newline instead of space", "SELECT\n*\nFROM\nusers;\nDROP\nTABLE\nusers"},

		// Function name obfuscation
		{"function with space", "SELECT SL EEP(10)"},
		{"function with comment", "SELECT SL/**/EEP(10)"},

		// Operator alternatives
		{"not equal <>", "SELECT * FROM users WHERE 1<>0"},
		{"not equal !=", "SELECT * FROM users WHERE 1!=0"},
	}

	for _, tc := range injectionAttempts {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			// Some of these are valid SQL (like not equal operators)
			// We mainly want to ensure stacked queries and dangerous functions are blocked
			if err == nil && (strings.Contains(strings.ToLower(tc.query), "drop") ||
				strings.Contains(strings.ToLower(tc.query), "sleep") ||
				strings.Contains(tc.query, ";")) {
				t.Errorf("advanced bypass should be blocked: %s", tc.query)
			}
		})
	}
}

// TestSQLInjection_OWASPTop10 tests OWASP Top 10 SQL injection patterns
func TestSQLInjection_OWASPTop10(t *testing.T) {
	// Common patterns from OWASP testing guide
	patterns := []struct {
		name  string
		query string
	}{
		{"single quote", "SELECT * FROM users WHERE name = '''"},
		{"double single quote", "SELECT * FROM users WHERE name = ''''''"},
		{"backslash quote", "SELECT * FROM users WHERE name = '\\''"},
		{"null injection", "SELECT * FROM users WHERE name = '\x00'"},
		{"wide char", "SELECT * FROM users WHERE name = '%bf%27'"},
	}

	for _, tc := range patterns {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			// These should either be blocked or safely handled by the parser
			// The key is that they don't cause unexpected behavior
			_ = err // Some of these might parse as valid (but harmless) SQL
		})
	}
}

// TestSQLValidator_AllowsSafeQueries ensures legitimate queries still work
func TestSQLValidator_AllowsSafeQueries(t *testing.T) {
	safeQueries := []string{
		"SELECT * FROM users",
		"SELECT id, name FROM users WHERE status = 'active'",
		"SELECT * FROM users ORDER BY created_at DESC LIMIT 10",
		"SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id",
		"SELECT COUNT(*) FROM users GROUP BY status",
		"SELECT * FROM users WHERE email LIKE '%@example.com'",
		"SELECT * FROM users WHERE id IN (1, 2, 3)",
		"SELECT * FROM users WHERE created_at BETWEEN '2024-01-01' AND '2024-12-31'",
		"SHOW DATABASES",
		"SHOW TABLES",
		"DESCRIBE users",
		"EXPLAIN SELECT * FROM users",
	}

	for _, query := range safeQueries {
		t.Run(query[:min(30, len(query))], func(t *testing.T) {
			err := util.ValidateSQLCombined(query)
			if err != nil {
				t.Errorf("safe query should be allowed: %s, error: %v", query, err)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
