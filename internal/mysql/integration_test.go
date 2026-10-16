//go:build integration

package mysql

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	tc "github.com/testcontainers/testcontainers-go"
	tc_mysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
)

// defaultTestImage is used unless MYSQL_TC_IMAGE overrides it
// (e.g. MYSQL_TC_IMAGE=mariadb:12.3 or mysql:9.7).
const defaultTestImage = "mysql:8.4"

// startMySQLContainer starts a disposable MySQL container for tests
// and returns a DSN suitable for github.com/go-sql-driver/mysql.
func startMySQLContainer(t *testing.T) (string, *tc_mysql.MySQLContainer) {
	t.Helper()

	// Skip instead of panicking when no Docker daemon is reachable.
	tc.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()

	image := os.Getenv("MYSQL_TC_IMAGE")
	if image == "" {
		image = defaultTestImage
	}

	mysqlContainer, err := tc_mysql.Run(
		ctx,
		image,
		tc_mysql.WithDatabase("testdb"),
		tc_mysql.WithUsername("testuser"),
		tc_mysql.WithPassword("testpass"),
		// The module's default waits for "MySQL Community Server", which MariaDB
		// never logs. Both servers log "port: 3306  " only once the final
		// (non-bootstrap) server is accepting connections; also keep the
		// module's mapped-port check.
		tc.WithWaitStrategyAndDeadline(3*time.Minute,
			wait.ForLog("port: 3306  "),
			wait.ForListeningPort("3306/tcp"),
		),
	)
	if err != nil {
		t.Fatalf("failed to start mysql container: %v", err)
	}

	// Ensure the container is cleaned up after the test.
	t.Cleanup(func() {
		if err := tc.TerminateContainer(mysqlContainer); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	})

	// ConnectionString returns a DSN like:
	//   testuser:testpass@tcp(127.0.0.1:XXXX)/testdb?parseTime=true
	dsn, err := mysqlContainer.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	return dsn, mysqlContainer
}

func TestIntegration_MySQLClient_BasicFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	dsn, _ := startMySQLContainer(t)

	// Open a standard DB connection to pass into our Client
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Wait for MySQL to accept connections (simple retry loop)
	ctxPing, cancelPing := context.WithTimeout(ctx, 30*time.Second)
	defer cancelPing()

	for {
		if err := db.PingContext(ctxPing); err == nil {
			break
		}
		select {
		case <-time.After(1 * time.Second):
			// retry
		case <-ctxPing.Done():
			t.Fatalf("failed to ping db within timeout: %v", ctxPing.Err())
		}
	}

	// Wrap in our Client using existing constructor
	client, err := NewWithDB(db, Config{
		MaxRows:       10,
		QueryTimeoutS: 10,
	})
	if err != nil {
		t.Fatalf("failed to create mysql client: %v", err)
	}

	// Create schema objects using the raw db handle
	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id   INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(255) NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO users (name) VALUES ('Alkin'), ('Rene')`)
	if err != nil {
		t.Fatalf("failed to insert data: %v", err)
	}

	// RunQuery
	rows, err := client.RunQuery(ctx, "SELECT id, name FROM users ORDER BY id", 10)
	if err != nil {
		t.Fatalf("RunQuery error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows from users table, got %d", len(rows))
	}

	firstName, ok := rows[0]["name"]
	if !ok {
		t.Fatalf("expected 'name' column in first row")
	}

	var nameStr string
	switch v := firstName.(type) {
	case string:
		nameStr = v
	case []byte:
		nameStr = string(v)
	default:
		t.Fatalf("unexpected type for name: %T (%v)", firstName, firstName)
	}

	if nameStr != "Alkin" {
		t.Fatalf("expected first row name to be 'Alkin', got %q", nameStr)
	}
}
