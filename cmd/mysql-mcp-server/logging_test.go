// cmd/mysql-mcp-server/logging_test.go
package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewQueryTimer(t *testing.T) {
	timer := NewQueryTimer("test_tool")
	if timer == nil {
		t.Fatal("NewQueryTimer returned nil")
	}
	if timer.tool != "test_tool" {
		t.Errorf("expected tool 'test_tool', got '%s'", timer.tool)
	}

	// Verify timing works
	time.Sleep(10 * time.Millisecond)
	elapsed := timer.Elapsed()
	if elapsed < 10*time.Millisecond {
		t.Errorf("expected elapsed >= 10ms, got %v", elapsed)
	}

	elapsedMs := timer.ElapsedMs()
	if elapsedMs < 10 {
		t.Errorf("expected elapsedMs >= 10, got %d", elapsedMs)
	}
}

func TestQueryTimerLogSuccess(t *testing.T) {
	// Capture stderr and log output
	oldStderr := os.Stderr
	oldLogOutput := log.Writer()
	r, w, _ := os.Pipe()
	os.Stderr = w
	log.SetOutput(w)

	timer := NewQueryTimer("test_query")
	timer.LogSuccess(5, "SELECT * FROM test", nil, nil)

	w.Close()
	os.Stderr = oldStderr
	log.SetOutput(oldLogOutput)

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Should contain log output - either JSON or plain log format should work
	if output == "" {
		t.Error("expected log output, got empty string")
	} else if !strings.Contains(output, "test_query") && !strings.Contains(output, "query executed") {
		t.Errorf("expected output to contain 'test_query' or 'query executed', got: %s", output)
	}
}

func TestQueryTimerLogError(t *testing.T) {
	// Capture stderr and log output
	oldStderr := os.Stderr
	oldLogOutput := log.Writer()
	r, w, _ := os.Pipe()
	os.Stderr = w
	log.SetOutput(w)

	timer := NewQueryTimer("test_query")
	timer.LogError(os.ErrNotExist, "SELECT * FROM test", nil, nil)

	w.Close()
	os.Stderr = oldStderr
	log.SetOutput(oldLogOutput)

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Should contain error info - either tool name or failure indicator
	if output == "" {
		t.Error("expected log output, got empty string")
	} else if !strings.Contains(output, "test_query") && !strings.Contains(output, "failed") {
		t.Errorf("expected output to contain 'test_query' or 'failed', got: %s", output)
	}
}

func TestNewAuditLoggerDisabled(t *testing.T) {
	logger, err := NewAuditLogger("")
	if err != nil {
		t.Fatalf("NewAuditLogger with empty path should not error: %v", err)
	}
	if logger == nil {
		t.Fatal("NewAuditLogger returned nil")
	}
	if logger.enabled {
		t.Error("logger should be disabled with empty path")
	}

	// Logging to disabled logger should not panic
	logger.Log(&AuditEntry{
		Tool:    "test",
		Query:   "SELECT 1",
		Success: true,
	})

	logger.Close()
}

func TestNewAuditLoggerEnabled(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}
	defer logger.Close()

	if !logger.enabled {
		t.Error("logger should be enabled")
	}
	if logger.file == nil {
		t.Error("logger file should not be nil")
	}

	// Log an entry
	logger.Log(&AuditEntry{
		Tool:       "test_tool",
		Database:   "testdb",
		Query:      "SELECT * FROM users",
		DurationMs: 42,
		RowCount:   10,
		Success:    true,
	})

	// Close and verify file contents
	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read audit log: %v", err)
	}

	var entry AuditEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("failed to parse audit log entry: %v", err)
	}

	if entry.Tool != "test_tool" {
		t.Errorf("expected tool 'test_tool', got '%s'", entry.Tool)
	}
	if entry.Database != "testdb" {
		t.Errorf("expected database 'testdb', got '%s'", entry.Database)
	}
	if entry.Query != "SELECT * FROM users" {
		t.Errorf("expected query 'SELECT * FROM users', got '%s'", entry.Query)
	}
	if entry.DurationMs != 42 {
		t.Errorf("expected duration 42, got %d", entry.DurationMs)
	}
	if entry.RowCount != 10 {
		t.Errorf("expected row_count 10, got %d", entry.RowCount)
	}
	if !entry.Success {
		t.Error("expected success true")
	}
	if entry.Timestamp == "" {
		t.Error("timestamp should be set")
	}
}

func TestNewAuditLoggerInvalidPath(t *testing.T) {
	// Try to create logger with invalid path
	logger, err := NewAuditLogger("/nonexistent/directory/audit.log")
	if err == nil {
		logger.Close()
		t.Error("expected error for invalid path")
	}
}

func TestAuditLoggerConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit_concurrent.log")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}
	defer logger.Close()

	// Concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 10; j++ {
				logger.Log(&AuditEntry{
					Tool:    "concurrent_test",
					Query:   "SELECT " + string(rune('0'+n)),
					Success: true,
				})
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	logger.Close()

	// Verify file has entries
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read audit log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 100 {
		t.Errorf("expected 100 log entries, got %d", len(lines))
	}
}

func TestLogEntry(t *testing.T) {
	entry := LogEntry{
		Timestamp: "2025-01-01T00:00:00Z",
		Level:     "INFO",
		Message:   "test message",
		Fields: map[string]interface{}{
			"key": "value",
		},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal LogEntry: %v", err)
	}

	var parsed LogEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal LogEntry: %v", err)
	}

	if parsed.Level != "INFO" {
		t.Errorf("expected level 'INFO', got '%s'", parsed.Level)
	}
	if parsed.Message != "test message" {
		t.Errorf("expected message 'test message', got '%s'", parsed.Message)
	}
}

func TestAuditEntry(t *testing.T) {
	entry := AuditEntry{
		Timestamp:  "2025-01-01T00:00:00Z",
		Tool:       "run_query",
		Database:   "testdb",
		Query:      "SELECT 1",
		DurationMs: 100,
		RowCount:   1,
		Success:    true,
		Error:      "",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal AuditEntry: %v", err)
	}

	var parsed AuditEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal AuditEntry: %v", err)
	}

	if parsed.Tool != "run_query" {
		t.Errorf("expected tool 'run_query', got '%s'", parsed.Tool)
	}
	if parsed.DurationMs != 100 {
		t.Errorf("expected duration_ms 100, got %d", parsed.DurationMs)
	}
}

func TestAuditEntryWithError(t *testing.T) {
	entry := AuditEntry{
		Timestamp:  "2025-01-01T00:00:00Z",
		Tool:       "run_query",
		Database:   "testdb",
		Query:      "SELECT * FROM nonexistent",
		DurationMs: 5,
		Success:    false,
		Error:      "table does not exist",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal AuditEntry: %v", err)
	}

	if !strings.Contains(string(data), "table does not exist") {
		t.Error("error message should be in JSON output")
	}
}
