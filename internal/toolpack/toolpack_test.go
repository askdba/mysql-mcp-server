// internal/toolpack/toolpack_test.go
package toolpack

import (
	"strings"
	"testing"
)

func TestStatementsSplitsDelimiterBlocks(t *testing.T) {
	got := Statements(`-- comment; with a semicolon
CREATE DATABASE x;

DELIMITER $$
-- a routine
CREATE PROCEDURE x.p()
BEGIN
    SELECT 1;
    SELECT 2;
END$$
DELIMITER ;
DROP DATABASE x;
`)
	want := []string{
		"CREATE DATABASE x",
		"CREATE PROCEDURE x.p()\nBEGIN\n    SELECT 1;\n    SELECT 2;\nEND",
		"DROP DATABASE x",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d statements %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestInstallScriptStatements(t *testing.T) {
	stmts := Statements(InstallScript())
	var creates, drops int
	for _, s := range stmts {
		switch {
		case strings.HasPrefix(s, "CREATE PROCEDURE mysql_mcp."):
			creates++
			if !strings.HasSuffix(s, "END") {
				t.Errorf("procedure does not end with END: %.80q", s)
			}
			if !strings.Contains(s, "SQL SECURITY INVOKER") {
				t.Errorf("procedure is not SQL SECURITY INVOKER: %.80q", s)
			}
		case strings.HasPrefix(s, "DROP PROCEDURE IF EXISTS mysql_mcp."):
			drops++
		case strings.HasPrefix(s, "CREATE FUNCTION"):
			t.Errorf("stored function in the pack (needs SUPER with binary logging): %.80q", s)
		}
		if strings.Contains(s, "DELIMITER") {
			t.Errorf("DELIMITER leaked into a statement: %.80q", s)
		}
	}
	if creates == 0 || creates != drops {
		t.Errorf("%d CREATE PROCEDURE vs %d DROP PROCEDURE IF EXISTS: every routine must be dropped first so the script re-runs", creates, drops)
	}
	if !strings.Contains(InstallScript(), "SELECT '"+Version+"' AS version") {
		t.Errorf("install script does not report version %s", Version)
	}
	if got := Statements(UninstallScript()); len(got) != 1 || got[0] != "DROP DATABASE IF EXISTS mysql_mcp" {
		t.Errorf("uninstall statements = %q", got)
	}
}

func TestSupported(t *testing.T) {
	for v, want := range map[string]bool{"1.0.0": true, "1.7.3": true, "2.0.0": false, "0.9.0": false, "": false, "x": false} {
		if got := Supported(v); got != want {
			t.Errorf("Supported(%q) = %v, want %v", v, got, want)
		}
	}
}
