// internal/toolpack/toolpack.go
//
// Package toolpack vendors the mysql_mcp SQL tool pack (RFC 0001 §5.1) and installs it.
//
// The pack is maintained in askdba/myvector under sql/mcp/ (Apache-2.0, see sql/LICENSE);
// the files in sql/ are copies of the version in SourceRef. Each procedure returns exactly
// the result set of the query the server runs without the pack, so the server can use
// either one.
package toolpack

import (
	"bufio"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

// Version is the pack version vendored here; CALL mysql_mcp.version() returns it once installed.
const Version = "1.0.0"

// SupportedMajor is the pack major version whose result sets the server expects.
const SupportedMajor = 1

// SourceRef identifies the askdba/myvector commit the files in sql/ were copied from.
const SourceRef = "askdba/myvector@173cf96b9f518ce8dd1a9f05f7f0d64f07aff038 (sql/mcp/)"

//go:embed sql/mysql_mcp.sql
var installSQL string

//go:embed sql/uninstall.sql
var uninstallSQL string

// InstallScript returns the install/upgrade script, as for the mysql client.
func InstallScript() string { return installSQL }

// UninstallScript returns the script that removes the pack.
func UninstallScript() string { return uninstallSQL }

// Statements splits a script the way the mysql client does for these files: statements
// end with the current delimiter (";" until a "DELIMITER x" line changes it), and comment
// lines between statements are dropped.
func Statements(script string) []string {
	var stmts []string
	var cur strings.Builder
	delim := ";"
	sc := bufio.NewScanner(strings.NewReader(script))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if cur.Len() == 0 {
			if trimmed == "" || strings.HasPrefix(trimmed, "--") {
				continue
			}
			if f := strings.Fields(trimmed); len(f) == 2 && strings.EqualFold(f[0], "DELIMITER") {
				delim = f[1]
				continue
			}
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
		if strings.HasSuffix(trimmed, delim) {
			stmt := strings.TrimSpace(cur.String())
			stmts = append(stmts, strings.TrimSpace(strings.TrimSuffix(stmt, delim)))
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

// Install runs the install/upgrade script on one connection of db. The account needs
// CREATE, DROP, CREATE ROUTINE and ALTER ROUTINE on mysql_mcp.* (no SUPER: the pack has
// no stored functions).
func Install(ctx context.Context, db *sql.DB) error {
	return run(ctx, db, installSQL)
}

// Uninstall drops the mysql_mcp schema.
func Uninstall(ctx context.Context, db *sql.DB) error {
	return run(ctx, db, uninstallSQL)
}

func run(ctx context.Context, db *sql.DB, script string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, stmt := range Statements(script) {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", firstLine(stmt), err)
		}
	}
	return nil
}

// Detect returns the installed pack version, or "" when the pack is missing, not
// executable by this account, or of an unsupported major version.
func Detect(ctx context.Context, db *sql.DB) string {
	var v string
	if err := db.QueryRowContext(ctx, "CALL mysql_mcp.version()").Scan(&v); err != nil {
		return ""
	}
	if !Supported(v) {
		return ""
	}
	return v
}

// Supported reports whether a pack version string has the supported major version.
func Supported(v string) bool {
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	return err == nil && n == SupportedMajor
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
