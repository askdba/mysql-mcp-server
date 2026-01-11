// internal/util/sql_validator.go
package util

import (
	"fmt"
	"regexp"
	"strings"
)

// stripSQLLiterals replaces the contents of SQL string/identifier literals with spaces.
// This allows simple pattern checks (like semicolons/comments) without false-positives
// on characters that appear inside quoted strings.
//
// Supported literals:
// - single quotes: '...'
// - double quotes: "..." (may be string or identifier depending on SQL mode)
// - backticks: `...` (identifier quoting)
func stripSQLLiterals(s string) string {
	if s == "" {
		return s
	}

	b := []byte(s)
	const (
		modeNone = iota
		modeSingle
		modeDouble
		modeBacktick
	)
	mode := modeNone

	for i := 0; i < len(b); i++ {
		switch mode {
		case modeNone:
			switch b[i] {
			case '\'':
				mode = modeSingle
			case '"':
				mode = modeDouble
			case '`':
				mode = modeBacktick
			}
		case modeSingle:
			// Preserve the quote itself, blank everything else.
			if b[i] != '\'' {
				b[i] = ' '
			}
			// Note: Backslash escapes (\' or \\) are already handled by blanking above.
			// Handle end / doubled quote escape ('')
			if i < len(b) && s[i] == '\'' {
				// If doubled quote, keep string mode and skip the next quote.
				if i+1 < len(b) && s[i+1] == '\'' {
					i++
					b[i] = ' '
					continue
				}
				mode = modeNone
			}
		case modeDouble:
			if b[i] != '"' {
				b[i] = ' '
			}
			// Handle end / doubled quote escape ("")
			if i < len(b) && s[i] == '"' {
				if i+1 < len(b) && s[i+1] == '"' {
					i++
					b[i] = ' '
					continue
				}
				mode = modeNone
			}
		case modeBacktick:
			if b[i] != '`' {
				b[i] = ' '
			}
			// Handle end / escaped backtick via double backtick (``)
			if i < len(b) && s[i] == '`' {
				if i+1 < len(b) && s[i+1] == '`' {
					i++
					b[i] = ' '
					continue
				}
				mode = modeNone
			}
		}

		// Handle backslash escaping inside single/double quoted strings
		if mode == modeSingle || mode == modeDouble {
			// If we see a backslash, skip the next char (it's escaped)
			if i < len(b) && s[i] == '\\' && i+1 < len(b) {
				i++
				b[i] = ' '
			}
		}
	}

	return string(b)
}

// SQLValidationError contains details about why a query was rejected.
type SQLValidationError struct {
	Reason  string
	Pattern string
}

func (e *SQLValidationError) Error() string {
	if e.Pattern != "" {
		return fmt.Sprintf("%s: %s", e.Reason, e.Pattern)
	}
	return e.Reason
}

// Blocked SQL patterns - these are dangerous even in SELECT statements.
var blockedPatterns = []*regexp.Regexp{
	// File operations
	regexp.MustCompile(`(?i)\bLOAD_FILE\s*\(`),
	regexp.MustCompile(`(?i)\bINTO\s+OUTFILE\b`),
	regexp.MustCompile(`(?i)\bINTO\s+DUMPFILE\b`),
	regexp.MustCompile(`(?i)\bLOAD\s+DATA\b`),

	// DDL statements that might slip through
	regexp.MustCompile(`(?i)^\s*CREATE\b`),
	regexp.MustCompile(`(?i)^\s*ALTER\b`),
	regexp.MustCompile(`(?i)^\s*DROP\b`),
	regexp.MustCompile(`(?i)^\s*TRUNCATE\b`),
	regexp.MustCompile(`(?i)^\s*RENAME\b`),

	// DML statements
	regexp.MustCompile(`(?i)^\s*INSERT\b`),
	regexp.MustCompile(`(?i)^\s*UPDATE\b`),
	regexp.MustCompile(`(?i)^\s*DELETE\b`),
	regexp.MustCompile(`(?i)^\s*REPLACE\b`),

	// Administrative commands
	regexp.MustCompile(`(?i)^\s*GRANT\b`),
	regexp.MustCompile(`(?i)^\s*REVOKE\b`),
	regexp.MustCompile(`(?i)^\s*SET\s+(GLOBAL|SESSION|@@)`),
	regexp.MustCompile(`(?i)^\s*FLUSH\b`),
	regexp.MustCompile(`(?i)^\s*RESET\b`),
	regexp.MustCompile(`(?i)^\s*KILL\b`),
	regexp.MustCompile(`(?i)^\s*SHUTDOWN\b`),

	// Locking
	regexp.MustCompile(`(?i)^\s*LOCK\s+TABLES\b`),
	regexp.MustCompile(`(?i)^\s*UNLOCK\s+TABLES\b`),

	// Transactions (should not be user-controlled)
	regexp.MustCompile(`(?i)^\s*START\s+TRANSACTION\b`),
	regexp.MustCompile(`(?i)^\s*BEGIN\b`),
	regexp.MustCompile(`(?i)^\s*COMMIT\b`),
	regexp.MustCompile(`(?i)^\s*ROLLBACK\b`),
	regexp.MustCompile(`(?i)^\s*SAVEPOINT\b`),

	// Prepared statements (could be abused)
	regexp.MustCompile(`(?i)^\s*PREPARE\b`),
	regexp.MustCompile(`(?i)^\s*EXECUTE\b`),
	regexp.MustCompile(`(?i)^\s*DEALLOCATE\b`),

	// Stored procedure calls
	regexp.MustCompile(`(?i)^\s*CALL\b`),

	// User-defined functions that might be dangerous
	regexp.MustCompile(`(?i)\bSLEEP\s*\(`),
	regexp.MustCompile(`(?i)\bBENCHMARK\s*\(`),
	regexp.MustCompile(`(?i)\bGET_LOCK\s*\(`),
	regexp.MustCompile(`(?i)\bRELEASE_LOCK\s*\(`),
	regexp.MustCompile(`(?i)\bIS_FREE_LOCK\s*\(`),
	regexp.MustCompile(`(?i)\bIS_USED_LOCK\s*\(`),

	// SQL comments (could be used to truncate/hide malicious SQL)
	regexp.MustCompile(`--`),
	regexp.MustCompile(`/\*`),
}

// Allowed query prefixes (read-only operations).
var allowedPrefixes = []string{
	"SELECT",
	"SHOW",
	"DESCRIBE",
	"DESC",
	"EXPLAIN",
}

// ValidateSQL performs comprehensive SQL safety validation.
func ValidateSQL(sqlText string) error {
	s := strings.TrimSpace(sqlText)
	if s == "" {
		return &SQLValidationError{Reason: "empty query"}
	}

	scan := stripSQLLiterals(s)

	// Check for multi-statement attacks (semicolon-separated queries)
	// Allow semicolon only at the very end (single statement)
	cleaned := strings.TrimRight(scan, "; \t\n\r")
	if strings.Contains(cleaned, ";") {
		return &SQLValidationError{
			Reason:  "multi-statement queries are not allowed",
			Pattern: ";",
		}
	}

	// Check against blocked patterns
	for _, pattern := range blockedPatterns {
		target := s
		// For patterns that aren't anchored at the start, scan the version with
		// string/identifier literals removed to avoid false positives.
		if !strings.Contains(pattern.String(), "^") {
			target = scan
		}
		if pattern.MatchString(target) {
			return &SQLValidationError{
				Reason:  "query contains blocked pattern",
				Pattern: pattern.String(),
			}
		}
	}

	// Verify query starts with an allowed prefix
	upper := strings.ToUpper(s)
	allowed := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(upper, prefix) {
			allowed = true
			break
		}
	}

	if !allowed {
		return &SQLValidationError{
			Reason: "only SELECT, SHOW, DESCRIBE, and EXPLAIN queries are allowed",
		}
	}

	return nil
}

// IsReadOnlySQL is a convenience wrapper for ValidateSQL.
func IsReadOnlySQL(sqlText string) bool {
	return ValidateSQL(sqlText) == nil
}

// ValidateSelectColumns validates and quotes column names in a SELECT list.
// Accepts: "col1, col2, col3" or "col1 AS alias, col2"
// Returns quoted column string or error if invalid.
func ValidateSelectColumns(selectStr string) (string, error) {
	if selectStr == "" {
		return "*", nil
	}

	// Block dangerous patterns in select
	dangerousPatterns := []string{
		"(", ")", ";", "--", "/*", "*/", "@@", "SLEEP", "BENCHMARK",
		"LOAD_FILE", "INTO", "OUTFILE", "DUMPFILE", "UNION", "INFORMATION_SCHEMA",
	}
	upperSelect := strings.ToUpper(selectStr)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(upperSelect, pattern) {
			return "", fmt.Errorf("select contains forbidden pattern: %s", pattern)
		}
	}

	// Split by comma and validate each column
	parts := strings.Split(selectStr, ",")
	var quotedCols []string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Handle "column AS alias" syntax
		var colName, alias string
		if idx := strings.Index(strings.ToUpper(part), " AS "); idx != -1 {
			colName = strings.TrimSpace(part[:idx])
			alias = strings.TrimSpace(part[idx+4:])
		} else {
			colName = part
		}

		// Allow * as a special case
		if colName == "*" {
			quotedCols = append(quotedCols, "*")
			continue
		}

		// Handle table.column syntax
		if strings.Contains(colName, ".") {
			dotParts := strings.Split(colName, ".")
			if len(dotParts) != 2 {
				return "", fmt.Errorf("invalid column reference: %s", colName)
			}
			tablePart, err := QuoteIdent(strings.TrimSpace(dotParts[0]))
			if err != nil {
				return "", fmt.Errorf("invalid table in column reference: %w", err)
			}
			colPart, err := QuoteIdent(strings.TrimSpace(dotParts[1]))
			if err != nil {
				return "", fmt.Errorf("invalid column in reference: %w", err)
			}
			colName = tablePart + "." + colPart
		} else {
			quoted, err := QuoteIdent(colName)
			if err != nil {
				return "", fmt.Errorf("invalid column name: %w", err)
			}
			colName = quoted
		}

		// Quote alias if present
		if alias != "" {
			quotedAlias, err := QuoteIdent(alias)
			if err != nil {
				return "", fmt.Errorf("invalid alias: %w", err)
			}
			quotedCols = append(quotedCols, colName+" AS "+quotedAlias)
		} else {
			quotedCols = append(quotedCols, colName)
		}
	}

	if len(quotedCols) == 0 {
		return "*", nil
	}

	return strings.Join(quotedCols, ", "), nil
}

// Patterns for WHERE clause validation.
var dangerousWherePatterns = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(?i);\s*`), "semicolon (multi-statement)"},
	{regexp.MustCompile(`(?i)--`), "SQL comment"},
	{regexp.MustCompile(`(?i)/\*`), "SQL block comment"},
	{regexp.MustCompile(`(?i)\bUNION\b`), "UNION keyword"},
	{regexp.MustCompile(`(?i)\bINTO\b`), "INTO keyword"},
	{regexp.MustCompile(`(?i)\bLOAD_FILE\s*\(`), "LOAD_FILE function"},
	{regexp.MustCompile(`(?i)\bSLEEP\s*\(`), "SLEEP function"},
	{regexp.MustCompile(`(?i)\bBENCHMARK\s*\(`), "BENCHMARK function"},
	{regexp.MustCompile(`(?i)\bGET_LOCK\s*\(`), "GET_LOCK function"},
	{regexp.MustCompile(`(?i)\bRELEASE_LOCK\s*\(`), "RELEASE_LOCK function"},
	{regexp.MustCompile(`(?i)@@`), "system variable access"},
	{regexp.MustCompile(`(?i)\bINFORMATION_SCHEMA\b`), "INFORMATION_SCHEMA access"},
	{regexp.MustCompile(`(?i)\bPERFORMANCE_SCHEMA\b`), "PERFORMANCE_SCHEMA access"},
	{regexp.MustCompile(`(?i)\bMYSQL\s*\.\b`), "mysql system database access"},
	{regexp.MustCompile(`(?i)\bSYS\s*\.\b`), "sys database access"},
	{regexp.MustCompile(`(?i)\bEXEC\s*\(`), "EXEC function"},
	{regexp.MustCompile(`(?i)\bSHUTDOWN\b`), "SHUTDOWN command"},
	{regexp.MustCompile(`(?i)0x[0-9a-fA-F]{10,}`), "long hex string (possible injection)"},
}

// ValidateWhereClause checks a WHERE clause for SQL injection attempts.
// This is a defense-in-depth measure - the primary protection is the read-only
// MySQL user, but we still block obvious injection patterns.
func ValidateWhereClause(where string) error {
	if where == "" {
		return nil
	}

	scan := stripSQLLiterals(where)
	for _, dp := range dangerousWherePatterns {
		if dp.pattern.MatchString(scan) {
			return fmt.Errorf("forbidden pattern detected: %s", dp.reason)
		}
	}

	// Check for balanced parentheses (basic sanity check)
	openParens := strings.Count(scan, "(")
	closeParens := strings.Count(scan, ")")
	if openParens != closeParens {
		return fmt.Errorf("unbalanced parentheses in WHERE clause")
	}

	// Limit length to prevent abuse
	if len(where) > 1000 {
		return fmt.Errorf("WHERE clause too long (max 1000 characters)")
	}

	return nil
}
