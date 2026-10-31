// cmd/mysql-mcp-server/labels_test.go
package main

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askdba/mysql-mcp-server/internal/config"
)

func TestLabelQuery(t *testing.T) {
	ctx := context.Background()
	if got := labelQuery(ctx, "SELECT 1"); got != "SELECT 1" {
		t.Errorf("no label in ctx: %q", got)
	}
	lctx := withCallLabel(ctx, "list_tables")
	got := labelQuery(lctx, "SELECT 1")
	if !strings.HasPrefix(got, "/* mcp:tool=list_tables req="+reqPrefix+"-") || !strings.HasSuffix(got, " */ SELECT 1") {
		t.Errorf("labeled query = %q", got)
	}
	first, second := withCallLabel(ctx, "x"), withCallLabel(ctx, "x")
	if labelQuery(first, "q") == labelQuery(second, "q") {
		t.Error("each call needs its own request id")
	}
	old := labelsEnabled
	labelsEnabled = false
	defer func() { labelsEnabled = old }()
	if got := labelQuery(lctx, "SELECT 1"); got != "SELECT 1" {
		t.Errorf("labels off: %q", got)
	}
}

func TestSanitizeLabelCannotCloseComment(t *testing.T) {
	for in, want := range map[string]string{
		"list_tables":          "list_tables",
		"x */ DROP TABLE t /*": "xDROPTABLEt",
		"a-b.c":                "a-b.c",
	} {
		if got := sanitizeLabel(in); got != want {
			t.Errorf("sanitizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if len(sanitizeLabel(strings.Repeat("a", 200))) != 64 {
		t.Error("labels are capped at 64 bytes")
	}
}

func TestLabelConnectionAttributes(t *testing.T) {
	got := labelConnectionAttributes("", "stdio")
	if got != "program_name:mysql-mcp-server,mcp_transport:stdio,mcp_version:"+sanitizeLabel(Version) {
		t.Errorf("attributes = %q", got)
	}
	got = labelConnectionAttributes("program_name:mine,team:data", "http")
	if !strings.HasPrefix(got, "program_name:mine,team:data,mcp_transport:http") || strings.Count(got, "program_name") != 1 {
		t.Errorf("user attributes must be kept: %q", got)
	}
	for c, want := range map[*config.Config]string{
		{Transport: config.TransportHTTP}: "http",
		{HTTPMode: true}:                  "rest",
		{}:                                "stdio",
	} {
		if got := transportLabel(c); got != want {
			t.Errorf("transportLabel(%+v) = %q, want %q", c, got, want)
		}
	}
}

// fakeConn records the statements the label wrapper passes on.
type fakeConn struct{ queries []string }

func (f *fakeConn) Prepare(q string) (driver.Stmt, error) {
	f.queries = append(f.queries, q)
	return nil, nil
}
func (f *fakeConn) Close() error                                                 { return nil }
func (f *fakeConn) Begin() (driver.Tx, error)                                    { return nil, nil }
func (f *fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) { return nil, nil }
func (f *fakeConn) PrepareContext(_ context.Context, q string) (driver.Stmt, error) {
	f.queries = append(f.queries, q)
	return nil, nil
}
func (f *fakeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	f.queries = append(f.queries, q)
	return nil, driver.ErrSkip
}
func (f *fakeConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	f.queries = append(f.queries, q)
	return nil, nil
}
func (f *fakeConn) Ping(context.Context) error               { return nil }
func (f *fakeConn) ResetSession(context.Context) error       { return nil }
func (f *fakeConn) IsValid() bool                            { return true }
func (f *fakeConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func TestLabelConnPrefixesStatements(t *testing.T) {
	fc := &fakeConn{}
	c, err := newLabelConn(fc)
	if err != nil {
		t.Fatal(err)
	}
	lc := c.(labelConn)
	ctx := withCallLabel(context.Background(), "describe_table")
	_, _ = lc.QueryContext(ctx, "SELECT 1", nil)
	_, _ = lc.ExecContext(ctx, "DO 1", nil)
	_, _ = lc.PrepareContext(ctx, "SELECT ?")
	_, _ = lc.QueryContext(context.Background(), "SELECT 2", nil) // not a tool call
	for i, q := range fc.queries[:3] {
		if !strings.HasPrefix(q, "/* mcp:tool=describe_table req=") {
			t.Errorf("statement %d not labeled: %q", i, q)
		}
	}
	if fc.queries[3] != "SELECT 2" {
		t.Errorf("unlabeled statement changed: %q", fc.queries[3])
	}
}

// guardRefusal is the driver error the guard's setting produces.
func guardRefusal(setting string) error {
	return &mysql.MySQLError{Number: 1290, SQLState: [5]byte{'H', 'Y', '0', '0', '0'},
		Message: "The MySQL server is running with the " + setting + " option so it cannot execute this statement"}
}

func TestExplainGuardRefusal(t *testing.T) {
	hit := &guardHit{}
	hit.note(fmt.Errorf("query failed: %w", guardRefusal("mcp.allowed_schemas")))
	r := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{
		Text: "query failed: Error 1290 (HY000): The MySQL server is running with the mcp.allowed_schemas option so it cannot execute this statement",
	}}}
	explainGuardRefusal(r, hit)
	text := r.Content[0].(*mcp.TextContent).Text
	if !strings.HasPrefix(text, "Refused by the server's MCP guard (component_mcp_guard, mcp.allowed_schemas): this schema is outside") ||
		!strings.Contains(text, "Error 1290") {
		t.Errorf("rewritten text = %q", text)
	}
	if r.Meta[guardMetaKey] != "mcp.allowed_schemas" {
		t.Errorf("_meta = %v", r.Meta)
	}

	// The server's own read_only (no mcp. prefix) and other errors are left alone.
	for _, msg := range []string{
		"Error 1290 (HY000): The MySQL server is running with the --read-only option so it cannot execute this statement",
		"Error 1146 (42S02): Table 'd.t' doesn't exist",
	} {
		r := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
		explainGuardRefusal(r, hit)
		if r.Content[0].(*mcp.TextContent).Text != msg || r.Meta != nil {
			t.Errorf("changed a non-guard error: %q", r.Content[0].(*mcp.TextContent).Text)
		}
	}
}

func TestExplainGuardRefusalNeedsServerError(t *testing.T) {
	// Text that only quotes a refusal (an echoed name, say) is not one: no 1290 from the
	// server for that setting during the call.
	msg := "access denied to database 'Error 1290 x running with the mcp.read_only option so it cannot execute this statement'"
	for _, hit := range []*guardHit{nil, {}, func() *guardHit {
		h := &guardHit{}
		h.note(guardRefusal("mcp.allowed_schemas")) // a different setting
		h.note(&mysql.MySQLError{Number: 1146, Message: "running with the mcp.read_only option so it cannot execute this statement"})
		h.note(errors.New("Error 1290: running with the mcp.read_only option so it cannot execute this statement"))
		return h
	}()} {
		r := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
		explainGuardRefusal(r, hit)
		if r.Content[0].(*mcp.TextContent).Text != msg || r.Meta != nil {
			t.Errorf("rewrote a quoted refusal: %q", r.Content[0].(*mcp.TextContent).Text)
		}
	}

	// A refusal of the same setting earlier in the call does not make a later error that
	// only quotes it a refusal: only a 1290 error is rewritten.
	later := &guardHit{}
	later.note(guardRefusal("mcp.read_only"))
	quoted := "Error 1146 (42S02): Table 'd.t' doesn't exist; echoed: running with the mcp.read_only option so it cannot execute this statement"
	lr := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: quoted}}}
	explainGuardRefusal(lr, later)
	if lr.Content[0].(*mcp.TextContent).Text != quoted || lr.Meta != nil {
		t.Errorf("rewrote an unrelated error: %q", lr.Content[0].(*mcp.TextContent).Text)
	}

	// Setting names with digits are matched too.
	hit := &guardHit{}
	hit.note(guardRefusal("mcp.v2_limit"))
	r := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{
		Text: "Error 1290 (HY000): The MySQL server is running with the mcp.v2_limit option so it cannot execute this statement",
	}}}
	explainGuardRefusal(r, hit)
	if r.Meta[guardMetaKey] != "mcp.v2_limit" {
		t.Errorf("_meta = %v", r.Meta)
	}
}

// fakeStmt fails its executions with err.
type fakeStmt struct{ err error }

func (s fakeStmt) Close() error                               { return nil }
func (s fakeStmt) NumInput() int                              { return -1 }
func (s fakeStmt) Exec([]driver.Value) (driver.Result, error) { return nil, s.err }
func (s fakeStmt) Query([]driver.Value) (driver.Rows, error)  { return nil, s.err }
func (s fakeStmt) CheckNamedValue(*driver.NamedValue) error   { return nil }
func (s fakeStmt) ColumnConverter(int) driver.ValueConverter  { return driver.DefaultParameterConverter }
func (s fakeStmt) ExecContext(context.Context, []driver.NamedValue) (driver.Result, error) {
	return nil, s.err
}
func (s fakeStmt) QueryContext(context.Context, []driver.NamedValue) (driver.Rows, error) {
	return nil, s.err
}

func TestLabelStmtNotesGuardRefusal(t *testing.T) {
	ctx := withCallLabel(context.Background(), "run_query")
	st := newLabelStmt(fakeStmt{err: guardRefusal("mcp.read_only")})
	if _, ok := st.(labelStmt); !ok {
		t.Fatalf("statement not wrapped: %T", st)
	}
	_, _ = st.(driver.StmtQueryContext).QueryContext(ctx, nil)
	if !guardHitFrom(ctx).saw("mcp.read_only") {
		t.Error("refusal of a prepared statement not noted")
	}
}

// fakeRows returns err from Next, as a CALL does when a later statement is refused.
type fakeRows struct{ err error }

func (r fakeRows) Columns() []string                                 { return nil }
func (r fakeRows) Close() error                                      { return nil }
func (r fakeRows) Next([]driver.Value) error                         { return r.err }
func (r fakeRows) HasNextResultSet() bool                            { return false }
func (r fakeRows) NextResultSet() error                              { return r.err }
func (r fakeRows) ColumnTypeDatabaseTypeName(int) string             { return "" }
func (r fakeRows) ColumnTypeNullable(int) (bool, bool)               { return false, false }
func (r fakeRows) ColumnTypePrecisionScale(int) (int64, int64, bool) { return 0, 0, false }
func (r fakeRows) ColumnTypeScanType(int) reflect.Type               { return nil }

func TestLabelRowsNotesGuardRefusal(t *testing.T) {
	hit := &guardHit{}
	rows := newLabelRows(fakeRows{err: guardRefusal("mcp.allowed_schemas")}, hit)
	if _, ok := rows.(labelRows); !ok {
		t.Fatalf("rows not wrapped: %T", rows)
	}
	_ = rows.Next(nil)
	if !hit.saw("mcp.allowed_schemas") {
		t.Error("refusal while reading rows not noted")
	}
}
