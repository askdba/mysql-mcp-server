// internal/util/sql_parser.go
package util

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/xwb1989/sqlparser"
)

// explainOptionPrefix matches EXPLAIN modifiers before the actual SELECT (FORMAT=…, EXTENDED, PARTITIONS).
var explainOptionPrefix = regexp.MustCompile(`(?is)^(?:FORMAT\s*=\s*\w+\s+|EXTENDED\s+|PARTITIONS\s+)`)

func stripLeadingExplainModifiers(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < 8; i++ {
		loc := explainOptionPrefix.FindStringIndex(s)
		if loc == nil {
			break
		}
		s = strings.TrimSpace(s[loc[1]:])
	}
	return s
}

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
	"get_lock":          true,
	"release_lock":      true,
	"is_free_lock":      true,
	"is_used_lock":      true,
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

// systemReadSchemas are the read-only diagnostic schemas that may be unlocked
// via MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS (e.g. index cardinality, index usage,
// redundant-index detection). `mysql` is deliberately excluded — it holds
// credentials and grants and stays blocked regardless of the flag.
var systemReadSchemas = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"sys":                true,
}

// allowSystemSchemas, when true, permits read access to systemReadSchemas.
// Off by default; set once at startup via SetAllowSystemSchemas.
var allowSystemSchemas bool

// SetAllowSystemSchemas toggles read access to the read-only system schemas
// (information_schema, performance_schema, sys). The `mysql` schema remains
// blocked regardless. Intended to be called once during startup from config.
func SetAllowSystemSchemas(v bool) {
	allowSystemSchemas = v
}

// GetAllowSystemSchemas returns whether read access to system schemas is enabled.
func GetAllowSystemSchemas() bool {
	return allowSystemSchemas
}

// ValidateSQLWithParser performs SQL validation using a proper SQL parser.
// This is more robust than regex-based validation as it understands SQL syntax.
func ValidateSQLWithParser(sqlText string) error {
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return &ParserValidationError{Reason: "empty query"}
	}

	// Use AST-aware splitting to detect multi-statement queries.
	// This properly handles semicolons inside string literals (e.g., WHERE name = 'test;value')
	// unlike a naive strings.Contains(";") check.
	statements, err := sqlparser.SplitStatementToPieces(sqlText)
	if err != nil {
		return &ParserValidationError{
			Reason:    "failed to parse SQL statement",
			Statement: err.Error(),
		}
	}

	// Check for multi-statement queries (not allowed)
	if len(statements) > 1 {
		return &ParserValidationError{
			Reason: "multi-statement queries are not allowed",
		}
	}

	// Get the single statement (already trimmed by SplitStatementToPieces)
	if len(statements) == 0 {
		return &ParserValidationError{Reason: "empty query"}
	}
	sqlText = statements[0]

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
			if qualifier != "" && DangerousSchemas[qualifier] &&
				!(allowSystemSchemas && systemReadSchemas[qualifier]) {
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
		// Check JOIN condition (ON clause) for dangerous functions
		if t.Condition.On != nil {
			if err := checkExprForDangerousFunctions(t.Condition.On); err != nil {
				return err
			}
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

func addSchemaQualifier(out map[string]struct{}, qual sqlparser.TableIdent) {
	s := strings.TrimSpace(qual.String())
	if s == "" {
		return
	}
	out[strings.ToLower(s)] = struct{}{}
}

func collectTableExprSchemas(tableExpr sqlparser.TableExpr, out map[string]struct{}) {
	switch t := tableExpr.(type) {
	case *sqlparser.AliasedTableExpr:
		if tableName, ok := t.Expr.(sqlparser.TableName); ok {
			addSchemaQualifier(out, tableName.Qualifier)
		}
		if subquery, ok := t.Expr.(*sqlparser.Subquery); ok {
			collectSelectStatementSchemas(subquery.Select, out)
		}
	case *sqlparser.JoinTableExpr:
		collectTableExprSchemas(t.LeftExpr, out)
		collectTableExprSchemas(t.RightExpr, out)
	case *sqlparser.ParenTableExpr:
		for _, expr := range t.Exprs {
			collectTableExprSchemas(expr, out)
		}
	}
}

func collectExprSubquerySchemas(expr sqlparser.SQLNode, out map[string]struct{}) {
	if expr == nil {
		return
	}
	_ = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		if subquery, ok := node.(*sqlparser.Subquery); ok {
			collectSelectStatementSchemas(subquery.Select, out)
		}
		return true, nil
	}, expr)
}

func collectSelectSchemas(sel *sqlparser.Select, out map[string]struct{}) {
	for _, tableExpr := range sel.From {
		collectTableExprSchemas(tableExpr, out)
	}
	for _, expr := range sel.SelectExprs {
		collectExprSubquerySchemas(expr, out)
	}
	if sel.Where != nil {
		collectExprSubquerySchemas(sel.Where.Expr, out)
	}
	if sel.Having != nil {
		collectExprSubquerySchemas(sel.Having.Expr, out)
	}
	for _, g := range sel.GroupBy {
		collectExprSubquerySchemas(g, out)
	}
	for _, ob := range sel.OrderBy {
		collectExprSubquerySchemas(ob.Expr, out)
	}
}

func collectSelectStatementSchemas(stmt sqlparser.SelectStatement, out map[string]struct{}) {
	switch s := stmt.(type) {
	case *sqlparser.Select:
		collectSelectSchemas(s, out)
	case *sqlparser.Union:
		collectSelectStatementSchemas(s.Left, out)
		collectSelectStatementSchemas(s.Right, out)
	case *sqlparser.ParenSelect:
		collectSelectStatementSchemas(s.Select, out)
	}
}

func stripOuterBackticks(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`") && len(s) >= 2 {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// schemaFromDescribeTableRef extracts the schema from DESCRIBE/DESC <ref>
// when <ref> is schema-qualified (db.table).
func schemaFromDescribeTableRef(rest string) string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	var token string
	if idx := strings.IndexFunc(rest, unicode.IsSpace); idx >= 0 {
		token = rest[:idx]
	} else {
		token = rest
	}
	token = stripOuterBackticks(token)
	if token == "" {
		return ""
	}
	if idx := strings.Index(token, "."); idx > 0 {
		return stripOuterBackticks(token[:idx])
	}
	return ""
}

// collectFromOtherRead extracts schema references from statements the Vitess
// parser exposes only as *sqlparser.OtherRead (EXPLAIN, DESCRIBE, …).
// handled is false when the text is not a recognized EXPLAIN/DESCRIBE pattern.
func collectFromOtherRead(sqlText string, out map[string]struct{}) (handled bool, err error) {
	u := strings.ToUpper(strings.TrimSpace(sqlText))
	trimmed := strings.TrimSpace(sqlText)

	for _, p := range []string{"EXPLAIN ANALYZE ", "EXPLAIN "} {
		if len(u) >= len(p) && u[:len(p)] == strings.ToUpper(p) {
			inner := strings.TrimSpace(trimmed[len(p):])
			if inner == "" {
				return true, nil
			}
			inner = stripLeadingExplainModifiers(inner)
			if inner == "" {
				return true, nil
			}
			st, perr := sqlparser.Parse(inner)
			if perr != nil {
				return true, &ParserValidationError{
					Reason:    "failed to parse SQL after EXPLAIN",
					Statement: perr.Error(),
				}
			}
			collectStmtReferencedSchemas(st, out)
			return true, nil
		}
	}
	for _, p := range []string{"DESCRIBE ", "DESC "} {
		if len(u) >= len(p) && u[:len(p)] == strings.ToUpper(p) {
			rest := strings.TrimSpace(trimmed[len(p):])
			if qual := schemaFromDescribeTableRef(rest); qual != "" {
				out[strings.ToLower(qual)] = struct{}{}
			}
			return true, nil
		}
	}
	return false, nil
}

func collectStmtReferencedSchemas(stmt sqlparser.Statement, out map[string]struct{}) {
	switch s := stmt.(type) {
	case *sqlparser.Select:
		collectSelectSchemas(s, out)
	case *sqlparser.ParenSelect:
		collectSelectStatementSchemas(s.Select, out)
	case *sqlparser.Union:
		collectSelectStatementSchemas(s.Left, out)
		collectSelectStatementSchemas(s.Right, out)
	case *sqlparser.Show:
		if s.HasOnTable() {
			addSchemaQualifier(out, s.OnTable.Qualifier)
		}
		if s.ShowTablesOpt != nil {
			if db := strings.TrimSpace(s.ShowTablesOpt.DbName); db != "" {
				out[strings.ToLower(db)] = struct{}{}
			}
		}
	case *sqlparser.Use:
		addSchemaQualifier(out, s.DBName)
	case *sqlparser.Insert:
		addSchemaQualifier(out, s.Table.Qualifier)
	case *sqlparser.Update:
		for _, te := range s.TableExprs {
			collectTableExprSchemas(te, out)
		}
		if s.Where != nil {
			collectExprSubquerySchemas(s.Where.Expr, out)
		}
	case *sqlparser.Delete:
		for _, te := range s.TableExprs {
			collectTableExprSchemas(te, out)
		}
		if s.Where != nil {
			collectExprSubquerySchemas(s.Where.Expr, out)
		}
	}
}

// ShowEnumeratesAllSchemas reports whether stmt lists every schema on the server
// (e.g. SHOW DATABASES). Used to enforce allowlist policy in callers.
func ShowEnumeratesAllSchemas(stmt sqlparser.Statement) bool {
	s, ok := stmt.(*sqlparser.Show)
	if !ok {
		return false
	}
	t := strings.TrimSpace(strings.ToLower(s.Type))
	return t == "databases" || strings.HasPrefix(t, "databases ")
}

// ShowEnumeratesAllSchemasInQuery parses a single SQL statement and returns true
// for SHOW DATABASES (including LIKE variants the parser types as "databases").
func ShowEnumeratesAllSchemasInQuery(sqlText string) bool {
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return false
	}
	statements, err := sqlparser.SplitStatementToPieces(sqlText)
	if err != nil || len(statements) != 1 {
		return false
	}
	stmt, err := sqlparser.Parse(strings.TrimSpace(statements[0]))
	if err != nil {
		return false
	}
	return ShowEnumeratesAllSchemas(stmt)
}

// ReferencedSchemaQualifiers returns the set of distinct, non-empty database
// names explicitly referenced in the statement (table qualifiers, USE targets,
// SHOW … FROM db, DESCRIBE db.t, and the inner statement of EXPLAIN, including
// EXPLAIN FORMAT=… / EXTENDED / PARTITIONS prefixes). Names are lowercased map
// keys. Unqualified table references are not listed; the session default
// database applies to those.
func ReferencedSchemaQualifiers(sqlText string) (map[string]struct{}, error) {
	sqlText = strings.TrimSpace(sqlText)
	if sqlText == "" {
		return nil, &ParserValidationError{Reason: "empty query"}
	}

	statements, err := sqlparser.SplitStatementToPieces(sqlText)
	if err != nil {
		return nil, &ParserValidationError{
			Reason:    "failed to parse SQL statement",
			Statement: err.Error(),
		}
	}
	if len(statements) > 1 {
		return nil, &ParserValidationError{Reason: "multi-statement queries are not allowed"}
	}
	if len(statements) == 0 {
		return nil, &ParserValidationError{Reason: "empty query"}
	}
	sqlText = statements[0]

	stmt, err := sqlparser.Parse(sqlText)
	if err != nil {
		return nil, &ParserValidationError{
			Reason:    "failed to parse SQL statement",
			Statement: err.Error(),
		}
	}

	out := make(map[string]struct{})
	switch stmt.(type) {
	case *sqlparser.OtherRead:
		ok, err := collectFromOtherRead(sqlText, out)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, &ParserValidationError{
				Reason:    "cannot extract schema references from this statement type",
				Statement: "use SELECT or standard EXPLAIN SELECT / DESCRIBE",
			}
		}
	default:
		collectStmtReferencedSchemas(stmt, out)
	}
	return out, nil
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

// InjectLimit rewrites a SELECT statement to add a LIMIT clause when none is
// present and the row cap would otherwise be enforced only on the client side.
// Non-SELECT statements (SHOW, DESCRIBE, EXPLAIN, …) are returned unchanged.
// If the SQL cannot be parsed it is also returned unchanged so that the
// existing validation layer can produce a meaningful error later.
// The original SQL text is preserved (only a LIMIT suffix is appended).
func InjectLimit(sqlText string, limit int) string {
	if limit <= 0 {
		return sqlText
	}

	trimmed := strings.TrimSpace(sqlText)
	stmt, err := sqlparser.Parse(trimmed)
	if err != nil {
		return sqlText
	}

	var hasLimit bool
	switch s := stmt.(type) {
	case *sqlparser.Select:
		hasLimit = s.Limit != nil
	case *sqlparser.Union:
		hasLimit = s.Limit != nil
	default:
		// Not a SELECT/UNION - no LIMIT injection needed.
		return sqlText
	}

	if hasLimit {
		return sqlText
	}

	// Strip trailing semicolons, then append the LIMIT clause.
	// This preserves the original SQL formatting (e.g., capitalization).
	base := strings.TrimRight(strings.TrimSpace(trimmed), ";")
	return fmt.Sprintf("%s LIMIT %d", base, limit)
}

// InjectLimitWithOffset appends LIMIT fetchLimit OFFSET offset when the top-level
// statement is SELECT or UNION and has no LIMIT. Used for keyset-free pagination:
// callers fetch fetchLimit rows (often limit+1) to detect a next page.
// Non-SELECT statements and unparsable SQL return an error.
func InjectLimitWithOffset(sqlText string, fetchLimit, offset int) (string, error) {
	if fetchLimit <= 0 {
		return sqlText, nil
	}
	if offset < 0 {
		return "", fmt.Errorf("offset must be non-negative")
	}

	trimmed := strings.TrimSpace(sqlText)
	stmt, err := sqlparser.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("cannot paginate unparsable SQL: %w", err)
	}

	var hasLimit bool
	switch s := stmt.(type) {
	case *sqlparser.Select:
		hasLimit = s.Limit != nil
	case *sqlparser.Union:
		hasLimit = s.Limit != nil
	default:
		return "", fmt.Errorf("pagination requires a SELECT or UNION query")
	}

	if hasLimit {
		return "", fmt.Errorf("query already contains LIMIT; remove it to use offset pagination")
	}

	base := strings.TrimRight(strings.TrimSpace(trimmed), ";")
	return fmt.Sprintf("%s LIMIT %d OFFSET %d", base, fetchLimit, offset), nil
}

// HasSelectStar reports whether the SQL statement selects all columns with a
// bare "*" wildcard (e.g. SELECT * or SELECT t.*).  Non-SELECT statements and
// statements that cannot be parsed always return false.
func HasSelectStar(sqlText string) bool {
	stmt, err := sqlparser.Parse(strings.TrimSpace(sqlText))
	if err != nil {
		return false
	}
	return selectHasStar(stmt)
}

// selectHasStar recursively checks parsed statements for star expressions.
func selectHasStar(stmt sqlparser.Statement) bool {
	switch s := stmt.(type) {
	case *sqlparser.Select:
		for _, expr := range s.SelectExprs {
			if _, ok := expr.(*sqlparser.StarExpr); ok {
				return true
			}
		}
	case *sqlparser.Union:
		return selectHasStar(s.Left) || selectHasStar(s.Right)
	case *sqlparser.ParenSelect:
		return selectHasStar(s.Select)
	}
	return false
}
