// cmd/mysql-mcp-server/guard.go
package main

import (
	"context"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ===== component_mcp_guard (RFC 0001 §5.2) =====
//
// The guard is a MySQL component (askdba/myvector) that enforces a policy on the sessions
// of accounts holding MCP_USER. It refuses statements with error 1290
// (ER_OPTION_PREVENTS_STATEMENT), naming the setting: "The MySQL server is running with the
// mcp.read_only option so it cannot execute this statement". This file turns that into a
// tool error that says what happened, and reports the guard in server_info.

// guardMetaKey names the guard setting that refused a tool call, in the result's _meta.
const guardMetaKey = "mysql-mcp-server/guard"

var guardRefusalRe = regexp.MustCompile(`running with the (mcp(?:\.[a-z_]+)?|mcp_guard) option so it cannot execute this statement`)

// guardHints explains each setting in the words of a tool user.
var guardHints = map[string]string{
	"mcp.read_only":             "MCP sessions may only read on this server",
	"mcp.deny_system_schemas":   "MCP sessions may not read the mysql and sys schemas on this server",
	"mcp.allowed_schemas":       "this schema is outside the ones MCP sessions may access on this server",
	"mcp.enabled":               "MCP access is switched off on this server",
	"mcp.max_execution_time_ms": "MCP sessions may not raise the statement time limit on this server",
	"mcp_guard":                 "the server could not track this MCP session",
}

// explainGuardRefusal rewrites a tool error caused by a guard refusal so it says which
// setting refused it and why, keeping the server's message, and names the setting in _meta.
func explainGuardRefusal(r *mcp.CallToolResult) {
	for _, c := range r.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok || !strings.Contains(tc.Text, "1290") {
			continue
		}
		m := guardRefusalRe.FindStringSubmatch(tc.Text)
		if m == nil {
			continue
		}
		setting := m[1]
		hint := guardHints[setting]
		if hint == "" {
			hint = "the statement is not allowed for MCP sessions on this server"
		}
		tc.Text = "Refused by the server's MCP guard (component_mcp_guard, " + setting + "): " + hint +
			". Ask the database administrator if this is needed. Server error: " + tc.Text
		if r.Meta == nil {
			r.Meta = mcp.Meta{}
		}
		r.Meta[guardMetaKey] = setting
		return
	}
}

// GuardInfo reports component_mcp_guard on the server, for server_info.
type GuardInfo struct {
	Installed bool              `json:"installed" jsonschema:"true if component_mcp_guard is installed"`
	Settings  map[string]string `json:"settings,omitempty" jsonschema:"the mcp.* system variables"`
}

// guardInfo reads the mcp.* variables (SHOW is allowed under the guard). No rows: no guard.
func guardInfo(ctx context.Context) *GuardInfo {
	rows, err := getDB(ctx).QueryContext(ctx, "SHOW GLOBAL VARIABLES LIKE 'mcp.%'")
	if err != nil {
		return nil
	}
	defer rows.Close()
	info := &GuardInfo{Settings: map[string]string{}}
	for rows.Next() {
		var name, value string
		if rows.Scan(&name, &value) == nil {
			info.Settings[name] = value
		}
	}
	if rows.Err() != nil {
		return nil
	}
	info.Installed = len(info.Settings) > 0
	if !info.Installed {
		info.Settings = nil
	}
	return info
}
