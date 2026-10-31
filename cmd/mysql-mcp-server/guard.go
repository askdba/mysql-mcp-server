// cmd/mysql-mcp-server/guard.go
package main

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/go-sql-driver/mysql"
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

var guardRefusalRe = regexp.MustCompile(`running with the (mcp(?:\.[a-z0-9_]+)?|mcp_guard) option so it cannot execute this statement`)

// guardHit records the guard refusals the server returned during one tool call. Text
// alone cannot tell a refusal from an error that merely quotes one (an echoed name, say),
// so the rewrite requires the driver to have seen MySQL error 1290 naming that setting.
type guardHit struct {
	mu       sync.Mutex
	settings map[string]bool
}

// note records err if it is a guard refusal.
func (h *guardHit) note(err error) {
	if h == nil || err == nil {
		return
	}
	var me *mysql.MySQLError
	if !errors.As(err, &me) || me.Number != 1290 {
		return
	}
	m := guardRefusalRe.FindStringSubmatch(me.Message)
	if m == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.settings == nil {
		h.settings = map[string]bool{}
	}
	h.settings[m[1]] = true
}

func (h *guardHit) saw(setting string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settings[setting]
}

// guardHitFrom returns the refusal record of the tool call in ctx, if any.
func guardHitFrom(ctx context.Context) *guardHit {
	l, _ := ctx.Value(callLabelKey{}).(callLabel)
	return l.guard
}

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
// Only refusals the server actually returned during the call (hit) are rewritten.
func explainGuardRefusal(r *mcp.CallToolResult, hit *guardHit) {
	for _, c := range r.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok || !strings.Contains(tc.Text, "1290") {
			continue
		}
		m := guardRefusalRe.FindStringSubmatch(tc.Text)
		if m == nil || !hit.saw(m[1]) {
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
	Installed bool              `json:"installed" jsonschema:"true if component_mcp_guard is installed; unknown when error is set"`
	Settings  map[string]string `json:"settings,omitempty" jsonschema:"the mcp.* system variables"`
	Error     string            `json:"error,omitempty" jsonschema:"why the guard could not be detected; installed is then unknown"`
}

// guardInfo reads the mcp.* variables (SHOW is allowed under the guard). No rows: no guard.
// A failed lookup is reported in Error, so it is not mistaken for an unguarded server.
func guardInfo(ctx context.Context) *GuardInfo {
	rows, err := getDB(ctx).QueryContext(ctx, "SHOW GLOBAL VARIABLES LIKE 'mcp.%'")
	if err != nil {
		return &GuardInfo{Error: err.Error()}
	}
	defer rows.Close()
	info := &GuardInfo{Settings: map[string]string{}}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			return &GuardInfo{Error: err.Error()}
		}
		info.Settings[name] = value
	}
	if err := rows.Err(); err != nil {
		return &GuardInfo{Error: err.Error()}
	}
	info.Installed = len(info.Settings) > 0
	if !info.Installed {
		info.Settings = nil
	}
	return info
}
