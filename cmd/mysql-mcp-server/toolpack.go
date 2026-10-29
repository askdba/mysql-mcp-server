// cmd/mysql-mcp-server/toolpack.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/askdba/mysql-mcp-server/internal/toolpack"
)

// ===== mysql_mcp SQL tool pack (RFC 0001 §5.1) =====
//
// When a connection has the pack installed (CALL mysql_mcp.version() with a supported major
// version, detected when the connection is added), catalog tools run
// CALL mysql_mcp.<procedure>(...) instead of their embedded SQL. Each procedure returns
// exactly the embedded query's result set, so everything around the query (input checks,
// access control, row limits, errors) is shared. MYSQL_MCP_TOOL_PACK=off disables it.

// detectToolPack returns the pack version on db, or "" when it is absent or disabled.
func detectToolPack(ctx context.Context, db *sql.DB, mode string) string {
	if mode == config.ToolPackOff {
		return ""
	}
	return toolpack.Detect(ctx, db)
}

// ToolPackFor returns the tool pack version of the connection ctx resolves to, or "".
func (cm *ConnectionManager) ToolPackFor(ctx context.Context) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.toolPacks[cm.activeNameLocked(ctx)]
}

// ToolPackOf returns the tool pack version detected on the named connection, or "".
func (cm *ConnectionManager) ToolPackOf(name string) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.toolPacks[name]
}

// usesToolPack reports whether catalog tools for ctx go through the tool pack.
func usesToolPack(ctx context.Context) bool {
	return connManager != nil && connManager.ToolPackFor(ctx) != ""
}

// packCall returns "CALL mysql_mcp.<proc>(?, ...)" for n arguments.
func packCall(proc string, n int) string {
	return "CALL mysql_mcp." + proc + "(" + strings.TrimSuffix(strings.Repeat("?, ", n), ", ") + ")"
}

// catalogRows runs a catalog query: the tool pack's procedure with procArgs when the
// connection has the pack, otherwise query with args. Both return the same result set.
func catalogRows(ctx context.Context, proc string, procArgs []any, query string, args ...any) (*sql.Rows, error) {
	if usesToolPack(ctx) {
		return getDB(ctx).QueryContext(ctx, packCall(proc, len(procArgs)), procArgs...)
	}
	return getDB(ctx).QueryContext(ctx, query, args...)
}

// catalogRow is catalogRows for a single row.
func catalogRow(ctx context.Context, proc string, procArgs []any, query string, args ...any) *sql.Row {
	if usesToolPack(ctx) {
		return getDB(ctx).QueryRowContext(ctx, packCall(proc, len(procArgs)), procArgs...)
	}
	return getDB(ctx).QueryRowContext(ctx, query, args...)
}

// nullIfEmpty maps an omitted optional argument to NULL for a pack procedure.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ===== install-schema =====

// handleInstallSchema implements `mysql-mcp-server install-schema`: install (or upgrade, or
// with --uninstall remove) the tool pack on one configured connection, or with --print
// write the SQL to stdout for a DBA to run.
func handleInstallSchema(p parsedArgs) int {
	if p.installPrint {
		if p.installUninstall {
			fmt.Print(toolpack.UninstallScript())
		} else {
			fmt.Print(toolpack.InstallScript())
		}
		return 0
	}

	c, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: config: %v\n", err)
		return 1
	}
	var target *config.ConnectionConfig
	for i := range c.Connections {
		if p.installConn == "" || c.Connections[i].Name == p.installConn {
			target = &c.Connections[i]
			break
		}
	}
	if target == nil {
		fmt.Fprintf(os.Stderr, "Error: no connection named %q\n", p.installConn)
		return 1
	}

	// Installing creates routines, so the connection must not be read-only, and the
	// pack must not be detected on it as part of the add.
	ic := *c
	ic.StrictReadOnly = false
	ic.ToolPack = config.ToolPackOff
	pingTimeout, queryTimeout = c.PingTimeout, c.QueryTimeout
	cm := NewConnectionManager()
	defer cm.Close()
	if err := cm.AddConnectionWithPoolConfig(*target, &ic); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	db := cm.GetActiveDB()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if p.installUninstall {
		if err := toolpack.Uninstall(ctx, db); err != nil {
			fmt.Fprintf(os.Stderr, "Error: uninstall on %s: %v\n", target.Name, err)
			return 1
		}
		fmt.Printf("Removed the mysql_mcp tool pack from connection %q.\n", target.Name)
		return 0
	}
	if err := toolpack.Install(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "Error: install on %s: %v\n", target.Name, err)
		fmt.Fprintln(os.Stderr, "The account needs CREATE, DROP, CREATE ROUTINE and ALTER ROUTINE on mysql_mcp.*; or have a DBA run `mysql-mcp-server install-schema --print | mysql ...`.")
		return 1
	}
	v := toolpack.Detect(ctx, db)
	fmt.Printf("Installed the mysql_mcp tool pack %s on connection %q.\n", v, target.Name)
	fmt.Println("Grant it to the MCP account: GRANT EXECUTE ON mysql_mcp.* TO '<user>'@'<host>';")
	return 0
}
