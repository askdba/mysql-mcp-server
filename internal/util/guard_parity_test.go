package util

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// guardParitySource is the askdba/myvector file testdata/guard-parity.tsv is copied from.
// Do not edit the copy; change it in myvector and copy it over.
const guardParitySource = "askdba/myvector@cd60ddb (sql/mcp/guard-parity.tsv)"

// TestGuardParity checks the go column of the table shared with component_mcp_guard
// (RFC 0001 §5.2 item 7): for each statement, the verdict of the run_query validator.
// myvector checks the guard column against a guarded server.
func TestGuardParity(t *testing.T) {
	f, err := os.Open("testdata/guard-parity.tsv")
	if err != nil {
		t.Fatalf("open parity table (%s): %v", guardParitySource, err)
	}
	defer f.Close()
	rows := 0
	seen := map[string]bool{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 {
			t.Fatalf("bad row %q: want 4 tab-separated columns", line)
		}
		want, stmt := cols[0], cols[2]
		got := "allow"
		if ValidateSQLCombined(stmt) != nil {
			got = "deny"
		}
		if got != want {
			t.Errorf("%q: validator says %s, parity table says %s (%s)", stmt, got, want, cols[3])
		}
		seen[stmt] = true
		rows++
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	if rows < 30 {
		t.Fatalf("parity table has %d rows, want at least 30", rows)
	}
	for _, stmt := range guardParityRequired {
		if !seen[stmt] {
			t.Errorf("parity table lacks %q", stmt)
		}
	}
}

// guardParityRequired are rows the table must keep: the cases this test was added for.
var guardParityRequired = []string{
	"SELECT * FROM d1.t FOR UPDATE",
	"SELECT * FROM d1.t FOR SHARE",
	"SELECT * FROM d1.t LOCK IN SHARE MODE",
	"SELECT * FROM d1.t /*!50000 FOR UPDATE */",
	"SELECT 1; DELETE FROM d1.t",
	"INSERT INTO d1.t VALUES (1)",
}
