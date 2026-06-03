package main

import (
	"reflect"
	"testing"

	"github.com/askdba/mysql-mcp-server/internal/util"
)

func TestRequireReferencedSchemasBlocksShowDatabases(t *testing.T) {
	t.Cleanup(func() { initAccessControl(nil) })
	initAccessControl([]string{"app"})
	if err := requireReferencedSchemasInQuery("SHOW DATABASES"); err == nil {
		t.Fatal("expected SHOW DATABASES to be rejected with allowlist")
	}
	if err := requireReferencedSchemasInQuery("SHOW DATABASES LIKE 'x%'"); err == nil {
		t.Fatal("expected SHOW DATABASES LIKE to be rejected with allowlist")
	}
	if err := requireReferencedSchemasInQuery("SELECT 1 FROM app.t"); err != nil {
		t.Fatalf("allowed schema should pass: %v", err)
	}
}

func TestRequireSafeDatabase(t *testing.T) {
	t.Cleanup(func() { util.SetAllowSystemSchemas(false) })

	// mysql is always blocked regardless of the flag
	for _, name := range []string{"mysql", "MySQL", "MYSQL"} {
		if err := requireSafeDatabase(name); err == nil {
			t.Errorf("expected %q to be blocked", name)
		}
	}

	// system schemas blocked by default
	for _, name := range []string{"information_schema", "performance_schema", "sys"} {
		if err := requireSafeDatabase(name); err == nil {
			t.Errorf("expected %q to be blocked when flag is off", name)
		}
	}

	// system schemas allowed when flag is on; mysql stays blocked
	util.SetAllowSystemSchemas(true)
	for _, name := range []string{"information_schema", "performance_schema", "sys"} {
		if err := requireSafeDatabase(name); err != nil {
			t.Errorf("expected %q to be allowed when flag is on: %v", name, err)
		}
	}
	if err := requireSafeDatabase("mysql"); err == nil {
		t.Error("mysql must remain blocked even when AllowSystemSchemas is on")
	}

	// ordinary databases are always allowed
	util.SetAllowSystemSchemas(false)
	if err := requireSafeDatabase("myapp"); err != nil {
		t.Errorf("ordinary database should be allowed: %v", err)
	}
	if err := requireSafeDatabase(""); err != nil {
		t.Errorf("empty database should be allowed: %v", err)
	}
}

func TestAllowedDatabasesLower(t *testing.T) {
	t.Cleanup(func() { initAccessControl(nil) })

	initAccessControl([]string{"zebra", "App", "  middle  "})
	got := allowedDatabasesLower()
	want := []string{"app", "middle", "zebra"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allowedDatabasesLower() = %v, want %v", got, want)
	}

	initAccessControl(nil)
	if got := allowedDatabasesLower(); got != nil {
		t.Fatalf("with nil allowlist expected nil slice, got %#v", got)
	}
}
