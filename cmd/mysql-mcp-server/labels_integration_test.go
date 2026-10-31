//go:build integration

// cmd/mysql-mcp-server/labels_integration_test.go
// Statement labels and connection attributes reach a real server.
package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/config"
)

func TestLabelsOnServer(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN not set")
	}
	db, err := openLabeledDB(dsn, &config.Config{Transport: config.TransportHTTP})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A statement sees its own text in the process list, label included: as plain text
	// and as a prepared statement (database/sql prepares when there are arguments).
	lctx := withCallLabel(ctx, "probe_tool")
	for _, q := range []struct {
		name string
		args []any
	}{
		{"SELECT INFO FROM information_schema.PROCESSLIST WHERE ID = CONNECTION_ID()", nil},
		{"SELECT INFO FROM information_schema.PROCESSLIST WHERE ID = CONNECTION_ID() AND ? = 1", []any{1}},
	} {
		var info string
		if err := db.QueryRowContext(lctx, q.name, q.args...).Scan(&info); err != nil {
			t.Fatalf("%s: %v", q.name, err)
		}
		if !strings.HasPrefix(info, "/* mcp:tool=probe_tool req=") {
			t.Errorf("statement not labeled on the server: %q", info)
		}
	}
	var info string
	if err := db.QueryRowContext(ctx, "SELECT INFO FROM information_schema.PROCESSLIST WHERE ID = CONNECTION_ID()").Scan(&info); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(info, "mcp:tool=") {
		t.Errorf("statement outside a tool call was labeled: %q", info)
	}

	// Connection attributes (performance_schema; off by default on MariaDB).
	rows, err := db.QueryContext(ctx, "SELECT ATTR_NAME, ATTR_VALUE FROM performance_schema.session_account_connect_attrs WHERE PROCESSLIST_ID = CONNECTION_ID()")
	if err != nil {
		t.Skipf("connection attributes not readable here: %v", err)
	}
	defer rows.Close()
	attrs := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			attrs[k] = v
		}
	}
	if len(attrs) == 0 {
		t.Skip("performance_schema has no connection attributes here")
	}
	if attrs["program_name"] != "mysql-mcp-server" || attrs["mcp_transport"] != "http" || attrs["mcp_version"] == "" {
		t.Errorf("connection attributes = %v", attrs)
	}
}
