// internal/util/sql_validator_test.go
package util

import (
	"testing"
)

func TestValidateSQL(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantError bool
	}{
		// Valid queries
		{"simple select", "SELECT * FROM users", false},
		{"select with where", "SELECT id, name FROM users WHERE id = 1", false},
		{"show databases", "SHOW DATABASES", false},
		{"show tables", "SHOW TABLES", false},
		{"describe table", "DESCRIBE users", false},
		{"desc table", "DESC users", false},
		{"explain query", "EXPLAIN SELECT * FROM users", false},
		{"select lowercase", "select * from users", false},
		{"trailing semicolon", "SELECT * FROM users;", false},
		{"semicolon inside string literal", "SELECT ';' AS semi", false},
		{"comment marker inside string literal", "SELECT '/* not a comment */' AS txt", false},
		{"double hyphen inside string literal", "SELECT '-- not a comment' AS txt", false},

		// Invalid queries - DDL
		{"create table", "CREATE TABLE users (id INT)", true},
		{"alter table", "ALTER TABLE users ADD column email VARCHAR(255)", true},
		{"drop table", "DROP TABLE users", true},
		{"truncate table", "TRUNCATE TABLE users", true},
		{"rename table", "RENAME TABLE users TO old_users", true},

		// Invalid queries - DML
		{"insert", "INSERT INTO users (name) VALUES ('test')", true},
		{"update", "UPDATE users SET name = 'test'", true},
		{"delete", "DELETE FROM users WHERE id = 1", true},
		{"replace", "REPLACE INTO users (id, name) VALUES (1, 'test')", true},

		// Invalid queries - Administrative
		{"grant", "GRANT SELECT ON *.* TO 'user'@'localhost'", true},
		{"revoke", "REVOKE SELECT ON *.* FROM 'user'@'localhost'", true},
		{"flush", "FLUSH PRIVILEGES", true},
		{"kill", "KILL 1234", true},
		{"shutdown", "SHUTDOWN", true},
		{"set global", "SET GLOBAL max_connections = 100", true},
		{"set session", "SET SESSION wait_timeout = 300", true},

		// Invalid queries - Transactions
		{"begin", "BEGIN", true},
		{"commit", "COMMIT", true},
		{"rollback", "ROLLBACK", true},
		{"start transaction", "START TRANSACTION", true},

		// Invalid queries - Dangerous functions
		{"sleep function", "SELECT SLEEP(10)", true},
		{"benchmark function", "SELECT BENCHMARK(1000000, SHA1('test'))", true},
		{"get_lock function", "SELECT GET_LOCK('test', 10)", true},
		{"load_file function", "SELECT LOAD_FILE('/etc/passwd')", true},

		// Invalid queries - File operations
		{"into outfile", "SELECT * FROM users INTO OUTFILE '/tmp/test.csv'", true},
		{"into dumpfile", "SELECT * FROM users INTO DUMPFILE '/tmp/test.bin'", true},
		{"load data", "LOAD DATA INFILE '/tmp/test.csv' INTO TABLE users", true},

		// Invalid queries - Multi-statement
		{"multi-statement", "SELECT 1; DROP TABLE users", true},
		{"multi with comment", "SELECT 1; -- DROP TABLE users", true},

		// Invalid queries - Stored procedures
		{"call procedure", "CALL my_procedure()", true},
		{"prepare statement", "PREPARE stmt FROM 'SELECT ?'", true},
		{"execute statement", "EXECUTE stmt USING @var", true},

		// Edge cases
		{"empty query", "", true},
		{"whitespace only", "   ", true},
		{"random text", "hello world", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSQL(tt.sql)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateSQL(%q) error = %v, wantError %v", tt.sql, err, tt.wantError)
			}
		})
	}
}

func TestIsReadOnlySQL(t *testing.T) {
	if !IsReadOnlySQL("SELECT * FROM users") {
		t.Error("IsReadOnlySQL should return true for SELECT")
	}
	if IsReadOnlySQL("DROP TABLE users") {
		t.Error("IsReadOnlySQL should return false for DROP")
	}
}

func TestValidateSelectColumns(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantError bool
	}{
		{"empty string", "", "*", false},
		{"single column", "id", "`id`", false},
		{"multiple columns", "id, name, email", "`id`, `name`, `email`", false},
		{"with spaces", "  id  ,  name  ", "`id`, `name`", false},
		{"star", "*", "*", false},
		{"column with alias", "id AS user_id", "`id` AS `user_id`", false},
		{"table.column", "users.id", "`users`.`id`", false},
		{"table.column with alias", "users.id AS uid", "`users`.`id` AS `uid`", false},

		// Invalid patterns
		{"with parentheses", "COUNT(id)", "", true},
		{"with semicolon", "id; DROP TABLE", "", true},
		{"with comment", "id -- comment", "", true},
		{"with union", "id UNION SELECT", "", true},
		{"with sleep", "SLEEP(10)", "", true},
		{"with benchmark", "BENCHMARK(1000, 1)", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateSelectColumns(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateSelectColumns(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
				return
			}
			if !tt.wantError && got != tt.want {
				t.Errorf("ValidateSelectColumns(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateWhereClause(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{"empty string", "", false},
		{"simple condition", "id = 1", false},
		{"multiple conditions", "id = 1 AND status = 'active'", false},
		{"with parentheses", "(id = 1 OR id = 2) AND status = 'active'", false},
		{"with IN clause", "id IN (1, 2, 3)", false},
		{"semicolon in string literal", "note = ';' AND id = 1", false},
		{"comment marker in string literal", "note = '/* ok */' AND id = 1", false},
		{"double hyphen in string literal", "note = '-- ok' AND id = 1", false},

		// Invalid patterns
		{"with semicolon", "id = 1; DROP TABLE users", true},
		{"with comment --", "id = 1 -- DROP TABLE", true},
		{"with comment /*", "id = 1 /* comment */", true},
		{"with union", "id = 1 UNION SELECT", true},
		{"with sleep", "id = SLEEP(10)", true},
		{"with benchmark", "id = BENCHMARK(1000, 1)", true},
		{"with load_file", "name = LOAD_FILE('/etc/passwd')", true},
		{"with system variable", "id = @@version", true},
		{"unbalanced parens", "(id = 1", true},
		{"too long", string(make([]byte, 1001)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWhereClause(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateWhereClause(%q) error = %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

func TestSQLValidationError(t *testing.T) {
	// Test with pattern
	err1 := &SQLValidationError{Reason: "blocked", Pattern: "DROP"}
	if err1.Error() != "blocked: DROP" {
		t.Errorf("unexpected error message: %s", err1.Error())
	}

	// Test without pattern
	err2 := &SQLValidationError{Reason: "empty query"}
	if err2.Error() != "empty query" {
		t.Errorf("unexpected error message: %s", err2.Error())
	}
}
