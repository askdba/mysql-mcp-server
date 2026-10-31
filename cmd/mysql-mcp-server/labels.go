// cmd/mysql-mcp-server/labels.go
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/go-sql-driver/mysql"

	"github.com/askdba/mysql-mcp-server/internal/config"
)

// ===== MCP traffic labels (RFC 0001 §3a, §5.2) =====
//
// Every statement a tool call sends starts with a comment that names the tool and the call:
//
//	/* mcp:tool=list_tables req=3f9a2c11-17 */ SELECT ...
//
// so the server side (component_mcp_guard's performance_schema.mcp_tool_calls, the slow
// log, performance_schema digests) can attribute statements to MCP tool calls. Every
// connection also carries the connection attributes program_name=mysql-mcp-server,
// mcp_transport and mcp_version. The labels are informational: the guard decides who is an
// MCP session from privileges (MCP_USER), never from labels.

// callLabel identifies one tool call.
type callLabel struct {
	tool string
	req  string
}

type callLabelKey struct{}

// labelsEnabled is MYSQL_MCP_STATEMENT_LABELS (default on).
var labelsEnabled = true

// reqPrefix makes request ids unique across processes; reqSeq within one.
var (
	reqPrefix = newReqPrefix()
	reqSeq    atomic.Uint64
)

func newReqPrefix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "0"
	}
	return hex.EncodeToString(b)
}

// withCallLabel returns ctx labeled with tool and a new request id.
func withCallLabel(ctx context.Context, tool string) context.Context {
	return context.WithValue(ctx, callLabelKey{}, callLabel{
		tool: sanitizeLabel(tool),
		req:  fmt.Sprintf("%s-%d", reqPrefix, reqSeq.Add(1)),
	})
}

// sanitizeLabel keeps label values to [A-Za-z0-9_.-], so they cannot close the comment.
func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || r == '-' || r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}

// labelQuery prefixes query with the call label in ctx, if any.
func labelQuery(ctx context.Context, query string) string {
	if !labelsEnabled {
		return query
	}
	l, ok := ctx.Value(callLabelKey{}).(callLabel)
	if !ok || l.tool == "" {
		return query
	}
	return "/* mcp:tool=" + l.tool + " req=" + l.req + " */ " + query
}

// labelConnectionAttributes adds program_name, mcp_transport and mcp_version to a
// connectionAttributes DSN value ("k:v,k:v"), keeping attributes already set.
func labelConnectionAttributes(existing, transport string) string {
	have := map[string]bool{}
	for _, kv := range strings.Split(existing, ",") {
		if k, _, ok := strings.Cut(kv, ":"); ok {
			have[strings.TrimSpace(k)] = true
		}
	}
	attrs := existing
	add := func(k, v string) {
		if have[k] || v == "" {
			return
		}
		if attrs != "" {
			attrs += ","
		}
		attrs += k + ":" + sanitizeLabel(v)
	}
	add("program_name", "mysql-mcp-server")
	add("mcp_transport", transport)
	add("mcp_version", Version)
	return attrs
}

// transportLabel is the mcp_transport connection attribute for this process.
func transportLabel(cfg *config.Config) string {
	switch {
	case cfg == nil:
		return ""
	case cfg.HTTPMode:
		return "rest"
	case cfg.Transport == config.TransportHTTP:
		return "http"
	default:
		return "stdio"
	}
}

// openLabeledDB opens dsn with the MCP connection attributes and the statement-label
// wrapper.
func openLabeledDB(dsn string, cfg *config.Config) (*sql.DB, error) {
	mc, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	mc.ConnectionAttributes = labelConnectionAttributes(mc.ConnectionAttributes, transportLabel(cfg))
	connector, err := mysql.NewConnector(mc)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(labelConnector{connector}), nil
}

// ----- driver wrapper -------------------------------------------------------------------

// labelConnector wraps the MySQL driver's connector so every connection labels the
// statements of tool calls (see labelQuery).
type labelConnector struct {
	driver.Connector
}

func (c labelConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return newLabelConn(conn)
}

// fullConn is what the go-sql-driver/mysql connection implements; the wrapper delegates
// every method so database/sql sees the same capabilities.
type fullConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.QueryerContext
	driver.ExecerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
	driver.NamedValueChecker
}

type labelConn struct {
	fullConn
}

func newLabelConn(c driver.Conn) (driver.Conn, error) {
	fc, ok := c.(fullConn)
	if !ok {
		// Not the driver this wrapper was written for: run it unwrapped.
		return c, nil
	}
	return labelConn{fc}, nil
}

func (c labelConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.fullConn.PrepareContext(ctx, labelQuery(ctx, query))
}

// QueryContext and ExecContext may return driver.ErrSkip; database/sql then prepares the
// original query through PrepareContext, which labels it there (not twice).
func (c labelConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.fullConn.QueryContext(ctx, labelQuery(ctx, query), args)
}

func (c labelConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.fullConn.ExecContext(ctx, labelQuery(ctx, query), args)
}
