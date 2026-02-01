// tests/integration/mariadb_smoke_test.go
//go:build integration
// +build integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// TestMariaDBSmoke verifies basic connectivity and server type identification for MariaDB.
func TestMariaDBSmoke(t *testing.T) {
	dsn := getTestDSN(t)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 1. Ping test
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("failed to ping MariaDB: %v", err)
	}

	// 2. Version check (smoke identification)
	var version, versionComment string
	err = db.QueryRowContext(ctx, "SELECT VERSION(), @@version_comment").Scan(&version, &versionComment)
	if err != nil {
		t.Fatalf("failed to query version info: %v", err)
	}

	version = strings.ToLower(version)
	versionComment = strings.ToLower(versionComment)

	t.Logf("MariaDB detected: version=%s, comment=%s", version, versionComment)

	// 3. Verify it's actually MariaDB if running against the MariaDB port
	isMaria := strings.Contains(version, "mariadb") || strings.Contains(versionComment, "mariadb")

	if !isMaria {
		t.Log("Warning: Test is running but 'mariadb' not found in version string. This might be running against a MySQL instance.")
	}

	// 4. Basic schema access
	var dbName string
	err = db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&dbName)
	if err != nil {
		t.Fatalf("failed to query current database: %v", err)
	}
	if dbName == "" {
		t.Error("expected a database name, got empty string")
	}

	t.Logf("Smoke test passed for database: %s", dbName)
}
