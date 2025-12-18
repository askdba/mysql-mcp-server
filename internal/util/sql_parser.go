// internal/util/sql_parser.go
package util

import (
	"fmt"
	"strings"

	"github.com/xwb1989/sqlparser"
)

// ParserValidationError contains details about why a query was rejected by the parser.
type ParserValidationError struct {
	Reason    string
	Statement string
}

func (e *ParserValidationError) Error() string {
	if e.Statement != "" {
		return fmt.Sprintf("%s: %s", e.Reason, e.Statement)
	}
	return e.Reason
}

// DangerousFunctions lists MySQL functions that should be blocked even in SELECT statements.
var DangerousFunctions = map[string]bool{
	// Time-based attacks
	"sleep":     true,
	"benchmark": true,

	// Locking functions
	"get_lock":      true,
	"release_lock":  true,
	"is_free_lock":  true,
	"is_used_lock":  true,
	"release_all_locks": true,

	// File operations
	"load_file": true,

	// System information that could aid attacks
	"sys_eval": true,
	"sys_exec": true,
}

// DangerousSchemas lists schemas that should not be accessible.
var DangerousSchemas = map[string]bool{
	"mysql":              true,
	"information_schema": true,
	"performance_schema": true,
	"sys":                true,
}

// ValidateSQLWithParser performs SQL validation using a proper SQL parser.
// This is more robust than regex-based validation as it understands SQL syntax.
func ValidateSQLWithParser(sqlText string) error {
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return &ParserValidationError{Reason: "empty query"}
	}

	// Remove trailing semicolon for parsing (parser handles it, but cleaner)
	sqlText = strings.TrimSuffix(strings.TrimSpace(sqlText), ";")

	// Check for multi-statement queries (not allowed)
	if strings.Contains(sqlText, ";") {
		return &ParserValidationError{
			Reason: "multi-statement queries are not allowed",
		}
	}

	// Parse the SQL statement
	stmt, err := sqlparser.Parse(sqlText)
	if err != nil {
		// If parsing fails, reject the query for safety
		return &ParserValidationError{
			Reason:    "failed to parse SQL statement",
			Statement: err.Error(),
		}
	}

	// Validate the parsed statement
	return validateStatement(stmt)
}

// validateStatement checks if a parsed SQL statement is allowed.
func validateStatement(stmt sqlparser.Statement) error {
	switch s := stmt.(type) {
	case *sqlparser.Select:
		return validateSelect(s)

	case *sqlparser.ParenSelect:
		return validateSelectStatement(s.Select)

	case *sqlparser.Show:
		// SHOW statements are generally safe for read-only access
		return nil

	case *sqlparser.OtherRead:
		// DESCRIBE, EXPLAIN are safe
		return nil

	case *sqlparser.Union:
		// Validate each SELECT in the UNION
		return validateUnion(s)

	case *sqlparser.Use:
		// USE database is safe (switches context)
		return nil

	// Block all write operations
	case *sqlparser.Insert:
		return &ParserValidationError{Reason: "INSERT statements are not allowed"}

	case *sqlparser.Update:
		return &ParserValidationError{Reason: "UPDATE statements are not allowed"}

	case *sqlparser.Delete:
		return &ParserValidationError{Reason: "DELETE statements are not allowed"}

	case *sqlparser.DDL:
		return &ParserValidationError{
			Reason:    "DDL statements are not allowed",
			Statement: s.Action,
		}

	case *sqlparser.DBDDL:
		return &ParserValidationError{
			Reason:    "database DDL statements are not allowed",
			Statement: s.Action,
		}

	case *sqlparser.Set:
		return &ParserValidationError{Reason: "SET statements are not allowed"}

	case *sqlparser.OtherAdmin:
		return &ParserValidationError{Reason: "administrative statements are not allowed"}

	default:
		// Reject any unknown statement type for safety
		return &ParserValidationError{
			Reason:    "statement type not allowed",
			Statement: fmt.Sprintf("%T", stmt),
		}
	}
}

// validateSelect validates a SELECT statement for dangerous patterns.
func validateSelect(sel *sqlparser.Select) error {
	// Check for dangerous functions in SELECT expressions
	for _, expr := range sel.SelectExprs {
		if err := checkExprForDangerousFunctions(expr); err != nil {
			return err
		}
	}

	// Check FROM clause for dangerous schemas
	if sel.From != nil {
		for _, tableExpr := range sel.From {
			if err := checkTableExpr(tableExpr); err != nil {
				return err
			}
		}
	}

	// Check WHERE clause for dangerous functions
	if sel.Where != nil {
		if err := checkExprForDangerousFunctions(sel.Where.Expr); err != nil {
			return err
		}
	}

	// Note: INTO OUTFILE/DUMPFILE is caught by the regex validator in ValidateSQLCombined

	// Check subqueries in FROM clause
	for _, tableExpr := range sel.From {
		if err := checkSubqueries(tableExpr); err != nil {
			return err
		}
	}

	return nil
}

// validateUnion validates a UNION statement.
func validateUnion(union *sqlparser.Union) error {
	// Validate left side
	if err := validateSelectStatement(union.Left); err != nil {
		return err
	}

	// Validate right side
	if err := validateSelectStatement(union.Right); err != nil {
		return err
	}

	return nil
}

// validateSelectStatement validates a SelectStatement (which can be Select or Union).
func validateSelectStatement(stmt sqlparser.SelectStatement) error {
	switch s := stmt.(type) {
	case *sqlparser.Select:
		return validateSelect(s)
	case *sqlparser.Union:
		return validateUnion(s)
	case *sqlparser.ParenSelect:
		return validateSelectStatement(s.Select)
	default:
		return &ParserValidationError{
			Reason:    "unsupported select statement type",
			Statement: fmt.Sprintf("%T", stmt),
		}
	}
}

// checkExprForDangerousFunctions recursively checks expressions for dangerous function calls.
func checkExprForDangerousFunctions(expr sqlparser.SQLNode) error {
	if expr == nil {
		return nil
	}

	var checkErr error

	// Walk the expression tree
	_ = sqlparser.Walk(func(node sqlparser.SQLNode) (kontinue bool, err error) {
		switch n := node.(type) {
		case *sqlparser.FuncExpr:
			funcName := strings.ToLower(n.Name.String())
			if DangerousFunctions[funcName] {
				checkErr = &ParserValidationError{
					Reason:    "dangerous function not allowed",
					Statement: funcName,
				}
				return false, nil
			}

		case *sqlparser.Subquery:
			// Validate subqueries recursively
			if err := validateSelectStatement(n.Select); err != nil {
				checkErr = err
				return false, nil
			}
		}
		return true, nil
	}, expr)

	return checkErr
}

// checkTableExpr checks table expressions for dangerous schema access.
func checkTableExpr(tableExpr sqlparser.TableExpr) error {
	switch t := tableExpr.(type) {
	case *sqlparser.AliasedTableExpr:
		if tableName, ok := t.Expr.(sqlparser.TableName); ok {
			// Check if accessing a dangerous schema
			qualifier := strings.ToLower(tableName.Qualifier.String())
			if qualifier != "" && DangerousSchemas[qualifier] {
				return &ParserValidationError{
					Reason:    "access to system schema is not allowed",
					Statement: qualifier,
				}
			}
		}

		// Check for subqueries in FROM clause
		if subquery, ok := t.Expr.(*sqlparser.Subquery); ok {
			return validateSelectStatement(subquery.Select)
		}

	case *sqlparser.JoinTableExpr:
		if err := checkTableExpr(t.LeftExpr); err != nil {
			return err
		}
		if err := checkTableExpr(t.RightExpr); err != nil {
			return err
		}

	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			if err := checkTableExpr(expr); err != nil {
				return err
			}
		}
	}

	return nil
}

// checkSubqueries checks for subqueries that might contain dangerous operations.
func checkSubqueries(tableExpr sqlparser.TableExpr) error {
	switch t := tableExpr.(type) {
	case *sqlparser.AliasedTableExpr:
		if subquery, ok := t.Expr.(*sqlparser.Subquery); ok {
			return validateSelectStatement(subquery.Select)
		}

	case *sqlparser.JoinTableExpr:
		if err := checkSubqueries(t.LeftExpr); err != nil {
			return err
		}
		return checkSubqueries(t.RightExpr)

	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			if err := checkSubqueries(expr); err != nil {
				return err
			}
		}
	}

	return nil
}

// ValidateSQLCombined performs both parser-based and regex-based validation.
// This provides defense-in-depth: the parser catches structural issues,
// while regex catches edge cases the parser might miss.
func ValidateSQLCombined(sqlText string) error {
	// First, try parser-based validation
	if err := ValidateSQLWithParser(sqlText); err != nil {
		return err
	}

	// Then, apply regex-based validation as defense-in-depth
	if err := ValidateSQL(sqlText); err != nil {
		return err
	}

	return nil
}
