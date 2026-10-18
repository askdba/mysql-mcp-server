// cmd/mysql-mcp-server/connection_test.go
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/go-sql-driver/mysql"
)

func TestApplyDefaultIOTimeouts(t *testing.T) {
	base := "user:pass@tcp(127.0.0.1:3306)/db"
	out, err := applyDefaultIOTimeouts(base, 30*time.Second)
	if err != nil {
		t.Fatalf("applyDefaultIOTimeouts: %v", err)
	}
	if !strings.Contains(out, "readTimeout=32s") || !strings.Contains(out, "writeTimeout=32s") {
		t.Fatalf("expected read/write timeout params in %q", out)
	}
	parsed, err := mysql.ParseDSN(out)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ReadTimeout != 32*time.Second || parsed.WriteTimeout != 32*time.Second {
		t.Fatalf("got read=%v write=%v", parsed.ReadTimeout, parsed.WriteTimeout)
	}
}

func TestApplyStrictReadOnlyDSN(t *testing.T) {
	base := "user:pass@tcp(127.0.0.1:3306)/db"
	out, err := applyStrictReadOnlyDSN(base, true)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mysql.ParseDSN(out)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Params["transaction_read_only"] != "ON" {
		t.Fatalf("expected transaction_read_only=ON in params, got %q", parsed.Params["transaction_read_only"])
	}
	out2, err := applyStrictReadOnlyDSN(base, false)
	if err != nil || out2 != base {
		t.Fatalf("strict off should return unchanged dsn: %v %q", err, out2)
	}

	withOff := "user:pass@tcp(127.0.0.1:3306)/db?transaction_read_only=OFF"
	out3, err := applyStrictReadOnlyDSN(withOff, true)
	if err != nil {
		t.Fatal(err)
	}
	p3, err := mysql.ParseDSN(out3)
	if err != nil {
		t.Fatal(err)
	}
	if p3.Params["transaction_read_only"] != "ON" {
		t.Fatalf("strict read-only must override DSN OFF, got %q", p3.Params["transaction_read_only"])
	}
}

func TestApplyDefaultIOTimeoutsPreservesExplicit(t *testing.T) {
	base := "user:pass@tcp(127.0.0.1:3306)/db?readTimeout=7s&writeTimeout=8s"
	out, err := applyDefaultIOTimeouts(base, 30*time.Second)
	if err != nil {
		t.Fatalf("applyDefaultIOTimeouts: %v", err)
	}
	parsed, err := mysql.ParseDSN(out)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ReadTimeout != 7*time.Second || parsed.WriteTimeout != 8*time.Second {
		t.Fatalf("expected explicit timeouts preserved, got read=%v write=%v", parsed.ReadTimeout, parsed.WriteTimeout)
	}
}

func TestConnectionManagerRemoveConnection(t *testing.T) {
	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer mockDB.Close()

	cm := NewConnectionManager()
	cm.mu.Lock()
	cm.connections["dropme"] = mockDB
	cm.configs["dropme"] = config.ConnectionConfig{Name: "dropme", DSN: "user:pass@tcp(127.0.0.1:3306)/db"}
	cm.activeConn = "dropme"
	cm.mu.Unlock()

	if err := cm.RemoveConnection("dropme"); err != nil {
		t.Fatalf("RemoveConnection: %v", err)
	}
	db, name := cm.GetActive()
	if db != nil || name != "" {
		t.Fatalf("expected no active connection after remove, got db=%v name=%q", db != nil, name)
	}
	if len(cm.List()) != 0 {
		t.Fatal("expected List empty after remove")
	}
}

func TestAddConnectionIfAbsentAlreadyExists(t *testing.T) {
	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer mockDB.Close()

	cm := NewConnectionManager()
	cm.mu.Lock()
	cm.connections["dup"] = mockDB
	cm.configs["dup"] = config.ConnectionConfig{Name: "dup", DSN: "user:pass@tcp(127.0.0.1:3306)/db"}
	cm.mu.Unlock()

	cfg := &config.Config{}
	err = cm.AddConnectionIfAbsentWithPoolConfig(context.Background(), config.ConnectionConfig{
		Name: "dup",
		DSN:  "other:pass@tcp(127.0.0.1:3306)/db",
	}, cfg)
	if err == nil || !errors.Is(err, ErrConnectionAlreadyExists) {
		t.Fatalf("want ErrConnectionAlreadyExists, got %v", err)
	}
}

func TestAddConnectionIfAbsentPendingInFlightBlocksDuplicate(t *testing.T) {
	cm := NewConnectionManager()
	cm.mu.Lock()
	if cm.pendingAdds == nil {
		cm.pendingAdds = make(map[string]struct{})
	}
	cm.pendingAdds["inflight"] = struct{}{}
	cm.mu.Unlock()

	cfg := &config.Config{}
	err := cm.AddConnectionIfAbsentWithPoolConfig(context.Background(), config.ConnectionConfig{
		Name: "inflight",
		DSN:  "user:pass@tcp(127.0.0.1:3306)/db",
	}, cfg)
	if err == nil || !errors.Is(err, ErrConnectionAlreadyExists) {
		t.Fatalf("want ErrConnectionAlreadyExists when name is pending, got %v", err)
	}
}

func TestNewConnectionManager(t *testing.T) {
	cm := NewConnectionManager()
	if cm == nil {
		t.Fatal("NewConnectionManager returned nil")
	}
	if cm.connections == nil {
		t.Error("connections map should be initialized")
	}
	if cm.configs == nil {
		t.Error("configs map should be initialized")
	}
	if cm.activeConn != "" {
		t.Error("activeConn should be empty initially")
	}
}

func TestConnectionManagerGetActiveEmpty(t *testing.T) {
	cm := NewConnectionManager()
	db, name := cm.GetActive()
	if db != nil {
		t.Error("GetActive should return nil when no connections")
	}
	if name != "" {
		t.Error("GetActive name should be empty when no connections")
	}
}

func TestConnectionManagerSetActiveNotFound(t *testing.T) {
	cm := NewConnectionManager()
	err := cm.SetActive("nonexistent")
	if err == nil {
		t.Error("SetActive should error for nonexistent connection")
	}
	if err.Error() != "connection 'nonexistent' not found" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestConnectionManagerListEmpty(t *testing.T) {
	cm := NewConnectionManager()
	list := cm.List()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d items", len(list))
	}
}

func TestConnectionManagerClose(t *testing.T) {
	cm := NewConnectionManager()
	// Should not panic when closing empty manager
	cm.Close()
}

func TestConnectionManagerWithMockDB(t *testing.T) {
	// Create mock database
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer mockDB.Close()

	cm := NewConnectionManager()

	// Manually add the mock connection (bypassing AddConnectionWithPoolConfig)
	cm.connections["test"] = mockDB
	cm.configs["test"] = config.ConnectionConfig{
		Name:        "test",
		DSN:         "user:password@tcp(localhost:3306)/testdb",
		Description: "Test connection",
	}
	cm.activeConn = "test"

	// Test GetActive
	db, name := cm.GetActive()
	if db != mockDB {
		t.Error("GetActive should return the mock db")
	}
	if name != "test" {
		t.Errorf("expected name 'test', got '%s'", name)
	}

	// Test GetActiveDB
	activeDB := cm.GetActiveDB()
	if activeDB != mockDB {
		t.Error("GetActiveDB should return the mock db")
	}

	// Test List with masking
	list := cm.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(list))
	}
	if list[0].Name != "test" {
		t.Errorf("expected name 'test', got '%s'", list[0].Name)
	}
	// DSN should be masked
	if list[0].DSN == "user:password@tcp(localhost:3306)/testdb" {
		t.Error("DSN should be masked in List output")
	}

	// Test SetActive
	err = cm.SetActive("test")
	if err != nil {
		t.Errorf("SetActive should succeed: %v", err)
	}

	// Test Close
	cm.Close()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestConnectionManagerMultipleConnections(t *testing.T) {
	// Create two mock databases
	mockDB1, mock1, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock1: %v", err)
	}
	defer mockDB1.Close()

	mockDB2, mock2, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock2: %v", err)
	}
	defer mockDB2.Close()

	cm := NewConnectionManager()

	// Add connections manually
	cm.connections["conn1"] = mockDB1
	cm.configs["conn1"] = config.ConnectionConfig{
		Name:        "conn1",
		DSN:         "user1:pass1@tcp(host1:3306)/db1",
		Description: "Connection 1",
	}
	cm.activeConn = "conn1"

	cm.connections["conn2"] = mockDB2
	cm.configs["conn2"] = config.ConnectionConfig{
		Name:        "conn2",
		DSN:         "user2:pass2@tcp(host2:3306)/db2",
		Description: "Connection 2",
	}

	// Verify first is active
	_, name := cm.GetActive()
	if name != "conn1" {
		t.Errorf("expected active 'conn1', got '%s'", name)
	}

	// Switch to second
	err = cm.SetActive("conn2")
	if err != nil {
		t.Fatalf("SetActive failed: %v", err)
	}

	db, name := cm.GetActive()
	if name != "conn2" {
		t.Errorf("expected active 'conn2', got '%s'", name)
	}
	if db != mockDB2 {
		t.Error("active db should be mockDB2")
	}

	// List should return both
	list := cm.List()
	if len(list) != 2 {
		t.Errorf("expected 2 connections, got %d", len(list))
	}

	cm.Close()
	if err := mock1.ExpectationsWereMet(); err != nil {
		t.Errorf("mock1 unfulfilled expectations: %v", err)
	}
	if err := mock2.ExpectationsWereMet(); err != nil {
		t.Errorf("mock2 unfulfilled expectations: %v", err)
	}
}

func TestGetDBWithConnManager(t *testing.T) {
	// Create mock database
	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer mockDB.Close()

	// Save and restore global state
	oldConnManager := connManager
	defer func() {
		connManager = oldConnManager
	}()

	// Set up connection manager
	cm := NewConnectionManager()
	cm.connections["test"] = mockDB
	cm.activeConn = "test"
	connManager = cm

	// getDB should return the manager's active connection
	result := getDB(context.Background())
	if result != mockDB {
		t.Error("getDB should return connection manager's active db")
	}
}

func TestGetDBWithoutConnManagerPanics(t *testing.T) {
	// Save and restore global state
	oldConnManager := connManager
	defer func() {
		connManager = oldConnManager
	}()

	// Set connManager to nil
	connManager = nil

	// getDB should panic when connManager is nil
	defer func() {
		if r := recover(); r == nil {
			t.Error("getDB should panic when connManager is nil")
		}
	}()

	getDB(context.Background()) // This should panic
}

func TestConnectionConfigStruct(t *testing.T) {
	cfg := config.ConnectionConfig{
		Name:        "production",
		DSN:         "root:secret@tcp(prod.mysql.example.com:3306)/app",
		Description: "Production database",
	}

	if cfg.Name != "production" {
		t.Error("Name mismatch")
	}
	if cfg.DSN == "" {
		t.Error("DSN should not be empty")
	}
	if cfg.Description != "Production database" {
		t.Error("Description mismatch")
	}
}

func TestConnectionManagerConcurrency(t *testing.T) {
	// Create mock database
	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer mockDB.Close()

	cm := NewConnectionManager()
	cm.connections["test"] = mockDB
	cm.configs["test"] = config.ConnectionConfig{Name: "test", DSN: "test:test@tcp(localhost)/test"}
	cm.activeConn = "test"

	// Concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = cm.GetActiveDB()
				_ = cm.List()
				_, _ = cm.GetActive()
			}
			done <- true
		}()
	}

	// Concurrent SetActive
	go func() {
		for j := 0; j < 100; j++ {
			_ = cm.SetActive("test")
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// Wait for all
	for i := 0; i < 11; i++ {
		<-done
	}
}

func TestDetectServerType(t *testing.T) {
	cm := NewConnectionManager()

	t.Run("MariaDB detection - combined query", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectQuery("SELECT VERSION\\(\\), @@version_comment").
			WillReturnRows(sqlmock.NewRows([]string{"VERSION()", "@@version_comment"}).
				AddRow("11.4.2-MariaDB", "mariadb.org binary distribution"))

		st := cm.detectServerType(context.Background(), db)
		if st != ServerTypeMariaDB {
			t.Errorf("expected ServerTypeMariaDB, got %s", st)
		}
	})

	t.Run("MariaDB detection - fallback query", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		// First fails, second succeeds
		mock.ExpectQuery("SELECT VERSION\\(\\), @@version_comment").WillReturnError(fmt.Errorf("error"))
		mock.ExpectQuery("SELECT VERSION\\(\\)").
			WillReturnRows(sqlmock.NewRows([]string{"VERSION()"}).AddRow("10.11.2-MariaDB"))

		st := cm.detectServerType(context.Background(), db)
		if st != ServerTypeMariaDB {
			t.Errorf("expected ServerTypeMariaDB, got %s", st)
		}
	})

	t.Run("MySQL detection", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectQuery("SELECT VERSION\\(\\), @@version_comment").
			WillReturnRows(sqlmock.NewRows([]string{"VERSION()", "@@version_comment"}).
				AddRow("8.0.36", "MySQL Community Server - GPL"))

		st := cm.detectServerType(context.Background(), db)
		if st != ServerTypeMySQL {
			t.Errorf("expected ServerTypeMySQL, got %s", st)
		}
	})

	t.Run("Unknown detection - both fail", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectQuery("SELECT VERSION\\(\\), @@version_comment").WillReturnError(fmt.Errorf("error"))
		mock.ExpectQuery("SELECT VERSION\\(\\)").WillReturnError(fmt.Errorf("error"))

		st := cm.detectServerType(context.Background(), db)
		if st != ServerTypeUnknown {
			t.Errorf("expected ServerTypeUnknown, got %s", st)
		}
	})

	t.Run("Unknown detection - empty response", func(t *testing.T) {
		db, mock, _ := sqlmock.New()
		defer db.Close()
		mock.ExpectQuery("SELECT VERSION\\(\\), @@version_comment").
			WillReturnRows(sqlmock.NewRows([]string{"VERSION()", "@@version_comment"}).
				AddRow("", ""))

		st := cm.detectServerType(context.Background(), db)
		if st != ServerTypeUnknown {
			t.Errorf("expected ServerTypeUnknown, got %s", st)
		}
	})
}
