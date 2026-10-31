// cmd/mysql-mcp-server/labels_test.go
package main

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"

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

func TestExplainGuardRefusal(t *testing.T) {
	r := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{
		Text: "query failed: Error 1290 (HY000): The MySQL server is running with the mcp.allowed_schemas option so it cannot execute this statement",
	}}}
	explainGuardRefusal(r)
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
		explainGuardRefusal(r)
		if r.Content[0].(*mcp.TextContent).Text != msg || r.Meta != nil {
			t.Errorf("changed a non-guard error: %q", r.Content[0].(*mcp.TextContent).Text)
		}
	}
}
