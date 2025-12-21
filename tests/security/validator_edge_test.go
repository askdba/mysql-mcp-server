// tests/security/validator_edge_test.go
// Edge case tests for SQL validator
package security

import (
	"strings"
	"testing"

	"github.com/askdba/mysql-mcp-server/internal/util"
)

// TestValidator_PreparedStatementSyntax tests prepared statement blocking
func TestValidator_PreparedStatementSyntax(t *testing.T) {
	blockedQueries := []struct {
		name  string
		query string
	}{
		{"PREPARE", "PREPARE stmt FROM 'SELECT * FROM users WHERE id = ?'"},
		{"EXECUTE", "EXECUTE stmt USING @user_id"},
		{"DEALLOCATE PREPARE", "DEALLOCATE PREPARE stmt"},
		{"SET variable", "SET @user_id = 1"},
		{"SET with SELECT", "SET @count = (SELECT COUNT(*) FROM users)"},
	}

	for _, tc := range blockedQueries {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("prepared statement syntax should be blocked: %s", tc.query)
			}
		})
	}
}

// TestValidator_QuotedIdentifiers tests handling of quoted identifiers
func TestValidator_QuotedIdentifiers(t *testing.T) {
	testCases := []struct {
		name      string
		query     string
		wantError bool
	}{
		// Valid uses of quoted identifiers
		{"backtick table", "SELECT * FROM `users`", false},
		{"backtick column", "SELECT `id`, `name` FROM users", false},
		{"backtick with space", "SELECT * FROM `user table`", false},
		{"backtick reserved word", "SELECT `select`, `from` FROM `table`", false},

		// Invalid - attempts to escape quotes
		{"backtick escape attempt", "SELECT * FROM `users`; DROP TABLE `users`", true},
		{"mixed quotes", "SELECT * FROM \"users\"; DROP TABLE users", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if (err != nil) != tc.wantError {
				t.Errorf("query %q: expected error=%v, got error=%v", tc.query, tc.wantError, err)
			}
		})
	}
}

// TestValidator_UnicodeCharacters tests Unicode handling
func TestValidator_UnicodeCharacters(t *testing.T) {
	testCases := []struct {
		name      string
		query     string
		wantError bool
	}{
		// Valid Unicode in string literals
		{"japanese string", "SELECT * FROM users WHERE name = '山田太郎'", false},
		{"chinese string", "SELECT * FROM users WHERE name = '张三'", false},
		{"emoji string", "SELECT * FROM users WHERE bio = '👍🎉'", false},
		{"arabic string", "SELECT * FROM users WHERE name = 'محمد'", false},

		// Valid Unicode column values
		{"select unicode column", "SELECT unicode_text FROM special_data", false},

		// Edge cases with Unicode that look like SQL
		{"unicode semicolon lookalike", "SELECT * FROM users WHERE name = 'test；test'", false}, // fullwidth semicolon in string is OK
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if (err != nil) != tc.wantError {
				t.Errorf("query %q: expected error=%v, got error=%v", tc.query, tc.wantError, err)
			}
		})
	}
}

// TestValidator_ExtremelyLongQueries tests handling of very long queries
func TestValidator_ExtremelyLongQueries(t *testing.T) {
	testCases := []struct {
		name   string
		length int
	}{
		{"1KB query", 1024},
		{"10KB query", 10 * 1024},
		{"100KB query", 100 * 1024},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Build a query with many OR conditions
			var builder strings.Builder
			builder.WriteString("SELECT * FROM users WHERE ")
			for i := 0; i < tc.length/20; i++ {
				if i > 0 {
					builder.WriteString(" OR ")
				}
				builder.WriteString("id = ")
				builder.WriteString(strings.Repeat("1", 10))
			}
			query := builder.String()

			// Should either succeed or fail gracefully (no panic)
			_ = util.ValidateSQLCombined(query)
		})
	}
}

// TestValidator_MalformedSQL tests handling of malformed SQL
func TestValidator_MalformedSQL(t *testing.T) {
	malformedQueries := []struct {
		name  string
		query string
	}{
		{"incomplete SELECT", "SELECT"},
		{"incomplete FROM", "SELECT * FROM"},
		{"incomplete WHERE", "SELECT * FROM users WHERE"},
		{"missing table", "SELECT * FROM"},
		{"double FROM", "SELECT * FROM FROM users"},
		{"unmatched paren", "SELECT * FROM users WHERE (id = 1"},
		{"extra paren", "SELECT * FROM users WHERE id = 1))"},
		{"random tokens", "SELECT foo bar baz qux"},
		{"just keywords", "SELECT FROM WHERE ORDER BY"},
		{"numbers only", "123 456 789"},
		{"special chars only", "!@#$%^&*()"},
	}

	for _, tc := range malformedQueries {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("malformed SQL should be rejected: %s", tc.query)
			}
		})
	}
}

// TestValidator_MultiStatementVariations tests various multi-statement attempts
func TestValidator_MultiStatementVariations(t *testing.T) {
	multiStatementQueries := []struct {
		name  string
		query string
	}{
		{"semicolon space", "SELECT * FROM users ; DROP TABLE users"},
		{"semicolon no space", "SELECT * FROM users;DROP TABLE users"},
		{"semicolon newline", "SELECT * FROM users;\nDROP TABLE users"},
		{"semicolon tab", "SELECT * FROM users;\tDROP TABLE users"},
		{"multiple semicolons", "SELECT 1; SELECT 2; SELECT 3"},
		{"trailing semicolon drop", "SELECT * FROM users; DROP TABLE users;"},

		// Semicolon in string literals should be allowed
		// but followed by actual SQL should not
	}

	for _, tc := range multiStatementQueries {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("multi-statement query should be blocked: %s", tc.query)
			}
		})
	}
}

// TestValidator_AdminCommands tests blocking of administrative commands
func TestValidator_AdminCommands(t *testing.T) {
	adminCommands := []struct {
		name  string
		query string
	}{
		// User management
		{"CREATE USER", "CREATE USER 'hacker'@'%' IDENTIFIED BY 'password'"},
		{"DROP USER", "DROP USER 'testuser'@'%'"},
		{"ALTER USER", "ALTER USER 'root'@'%' IDENTIFIED BY 'newpass'"},
		{"GRANT", "GRANT ALL PRIVILEGES ON *.* TO 'hacker'@'%'"},
		{"REVOKE", "REVOKE ALL PRIVILEGES ON *.* FROM 'testuser'@'%'"},

		// Server administration
		{"FLUSH PRIVILEGES", "FLUSH PRIVILEGES"},
		{"FLUSH TABLES", "FLUSH TABLES"},
		{"FLUSH LOGS", "FLUSH LOGS"},
		{"RESET", "RESET MASTER"},
		{"SHUTDOWN", "SHUTDOWN"},
		{"KILL", "KILL 12345"},
		{"KILL QUERY", "KILL QUERY 12345"},

		// Replication
		{"CHANGE MASTER", "CHANGE MASTER TO MASTER_HOST='evil.com'"},
		{"START SLAVE", "START SLAVE"},
		{"STOP SLAVE", "STOP SLAVE"},

		// Database operations
		{"CREATE DATABASE", "CREATE DATABASE hacker_db"},
		{"DROP DATABASE", "DROP DATABASE testdb"},

		// Table operations (DDL)
		{"CREATE TABLE", "CREATE TABLE hacker_table (id INT)"},
		{"DROP TABLE", "DROP TABLE users"},
		{"ALTER TABLE", "ALTER TABLE users ADD COLUMN hacked BOOLEAN"},
		{"RENAME TABLE", "RENAME TABLE users TO old_users"},
		{"TRUNCATE", "TRUNCATE TABLE users"},

		// Index operations
		{"CREATE INDEX", "CREATE INDEX idx_hack ON users(name)"},
		{"DROP INDEX", "DROP INDEX idx_name ON users"},

		// View operations
		{"CREATE VIEW", "CREATE VIEW hacker_view AS SELECT * FROM users"},
		{"DROP VIEW", "DROP VIEW user_orders"},

		// Stored procedures
		{"CREATE PROCEDURE", "CREATE PROCEDURE hack() BEGIN SELECT 1; END"},
		{"DROP PROCEDURE", "DROP PROCEDURE get_user_by_id"},
		{"CALL", "CALL get_user_by_id(1)"},

		// Triggers
		{"CREATE TRIGGER", "CREATE TRIGGER hack BEFORE INSERT ON users FOR EACH ROW SET NEW.id = 999"},
		{"DROP TRIGGER", "DROP TRIGGER IF EXISTS some_trigger"},

		// Events
		{"CREATE EVENT", "CREATE EVENT hack ON SCHEDULE EVERY 1 SECOND DO DELETE FROM users"},
		{"DROP EVENT", "DROP EVENT IF EXISTS some_event"},
	}

	for _, tc := range adminCommands {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("admin command should be blocked: %s", tc.query)
			}
		})
	}
}

// TestValidator_TransactionCommands tests blocking of transaction commands
func TestValidator_TransactionCommands(t *testing.T) {
	transactionCommands := []struct {
		name  string
		query string
	}{
		{"BEGIN", "BEGIN"},
		{"BEGIN WORK", "BEGIN WORK"},
		{"START TRANSACTION", "START TRANSACTION"},
		{"START TRANSACTION READ ONLY", "START TRANSACTION READ ONLY"},
		{"COMMIT", "COMMIT"},
		{"COMMIT WORK", "COMMIT WORK"},
		{"ROLLBACK", "ROLLBACK"},
		{"ROLLBACK WORK", "ROLLBACK WORK"},
		{"SAVEPOINT", "SAVEPOINT sp1"},
		{"ROLLBACK TO", "ROLLBACK TO SAVEPOINT sp1"},
		{"RELEASE SAVEPOINT", "RELEASE SAVEPOINT sp1"},
		{"SET AUTOCOMMIT", "SET AUTOCOMMIT = 0"},
		{"LOCK TABLES", "LOCK TABLES users READ"},
		{"UNLOCK TABLES", "UNLOCK TABLES"},
	}

	for _, tc := range transactionCommands {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("transaction command should be blocked: %s", tc.query)
			}
		})
	}
}

// TestValidator_FileOperations tests blocking of file operations
func TestValidator_FileOperations(t *testing.T) {
	fileOperations := []struct {
		name  string
		query string
	}{
		{"LOAD DATA INFILE", "LOAD DATA INFILE '/tmp/data.csv' INTO TABLE users"},
		{"LOAD DATA LOCAL", "LOAD DATA LOCAL INFILE '/tmp/data.csv' INTO TABLE users"},
		{"SELECT INTO OUTFILE", "SELECT * FROM users INTO OUTFILE '/tmp/users.csv'"},
		{"SELECT INTO DUMPFILE", "SELECT * FROM users INTO DUMPFILE '/tmp/users.bin'"},
		{"LOAD_FILE function", "SELECT LOAD_FILE('/etc/passwd')"},
		{"LOAD_FILE in WHERE", "SELECT * FROM users WHERE data = LOAD_FILE('/etc/shadow')"},
	}

	for _, tc := range fileOperations {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("file operation should be blocked: %s", tc.query)
			}
		})
	}
}

// TestValidator_SystemVariables tests blocking of system variable access
func TestValidator_SystemVariables(t *testing.T) {
	// Setting system variables MUST be blocked
	mustBlock := []struct {
		name  string
		query string
	}{
		{"SET GLOBAL", "SET GLOBAL max_connections = 1000"},
		{"SET SESSION", "SET SESSION wait_timeout = 100"},
		{"SET @@global", "SET @@global.max_connections = 1000"},
		{"SET @@session", "SET @@session.wait_timeout = 100"},
	}

	for _, tc := range mustBlock {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("SET command should be blocked: %s", tc.query)
			}
		})
	}

	// Reading system variables in SELECT/WHERE - may or may not be blocked
	// These are technically valid SQL, protection relies on read-only user
	edgeCases := []struct {
		name  string
		query string
	}{
		{"@@version in WHERE", "SELECT * FROM users WHERE id = @@version"},
		{"@@datadir in WHERE", "SELECT * FROM users WHERE path = @@datadir"},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			// Document behavior without asserting
			if err != nil {
				t.Logf("Blocked: %s - %v", tc.name, err)
			} else {
				t.Logf("Allowed: %s (SELECT with system var, read-only user protects)", tc.name)
			}
		})
	}
}

// TestValidator_InformationDisclosure tests blocking of information disclosure
func TestValidator_InformationDisclosure(t *testing.T) {
	infoDisclosure := []struct {
		name  string
		query string
	}{
		// System tables
		{"mysql.user", "SELECT * FROM mysql.user"},
		{"mysql.db", "SELECT * FROM mysql.db"},
		{"mysql.tables_priv", "SELECT * FROM mysql.tables_priv"},
		{"mysql.columns_priv", "SELECT * FROM mysql.columns_priv"},
		{"mysql.proc", "SELECT * FROM mysql.proc"},
		{"mysql.event", "SELECT * FROM mysql.event"},

		// Information schema sensitive tables
		{"processlist", "SELECT * FROM information_schema.processlist"},
		{"user_privileges", "SELECT * FROM information_schema.user_privileges"},
		{"schema_privileges", "SELECT * FROM information_schema.schema_privileges"},
		{"table_privileges", "SELECT * FROM information_schema.table_privileges"},

		// Performance schema
		{"threads", "SELECT * FROM performance_schema.threads"},
		{"events_statements", "SELECT * FROM performance_schema.events_statements_current"},
		{"setup_instruments", "SELECT * FROM performance_schema.setup_instruments"},

		// sys schema
		{"sys.session", "SELECT * FROM sys.session"},
		{"sys.processlist", "SELECT * FROM sys.processlist"},
		{"sys.user_summary", "SELECT * FROM sys.user_summary"},
	}

	for _, tc := range infoDisclosure {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if err == nil {
				t.Errorf("information disclosure query should be blocked: %s", tc.query)
			}
		})
	}
}

// TestValidator_EdgeCaseStrings tests edge cases in string handling
func TestValidator_EdgeCaseStrings(t *testing.T) {
	testCases := []struct {
		name      string
		query     string
		wantError bool
	}{
		// Empty string
		{"empty string literal", "SELECT * FROM users WHERE name = ''", false},

		// Escaped quotes in strings
		{"escaped single quote", "SELECT * FROM users WHERE name = 'O\\'Brien'", false},
		{"doubled single quote", "SELECT * FROM users WHERE name = 'O''Brien'", false},

		// Strings that look like SQL
		{"SQL in string", "SELECT * FROM users WHERE note = 'SELECT * FROM users'", false},
		{"DROP in string", "SELECT * FROM users WHERE note = 'DROP TABLE'", false},

		// But actual SQL injection should still be caught
		{"injection with string", "SELECT * FROM users WHERE name = ''; DROP TABLE users; --", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := util.ValidateSQLCombined(tc.query)
			if (err != nil) != tc.wantError {
				t.Errorf("query %q: expected error=%v, got error=%v (%v)", tc.query, tc.wantError, err != nil, err)
			}
		})
	}
}
