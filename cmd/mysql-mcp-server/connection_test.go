// cmd/mysql-mcp-server/connection_test.go
package main

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/askdba/mysql-mcp-server/internal/config"
)

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
	oldDB := db
	defer func() {
		connManager = oldConnManager
		db = oldDB
	}()

	// Set up connection manager
	cm := NewConnectionManager()
	cm.connections["test"] = mockDB
	cm.activeConn = "test"
	connManager = cm

	// getDB should return the manager's active connection
	result := getDB()
	if result != mockDB {
		t.Error("getDB should return connection manager's active db")
	}
}

func TestGetDBWithoutConnManager(t *testing.T) {
	// Create mock database
	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer mockDB.Close()

	// Save and restore global state
	oldConnManager := connManager
	oldDB := db
	defer func() {
		connManager = oldConnManager
		db = oldDB
	}()

	// Set global db, no connection manager
	connManager = nil
	db = mockDB

	// getDB should return global db
	result := getDB()
	if result != mockDB {
		t.Error("getDB should return global db when connManager is nil")
	}
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
