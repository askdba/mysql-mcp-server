// cmd/mysql-mcp-server/logging.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ===== Structured Logging =====

// LogEntry represents a structured log entry.
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

func logJSON(level, message string, fields map[string]interface{}) {
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Message:   message,
		Fields:    fields,
	}
	if jsonLogging {
		data, _ := json.Marshal(entry)
		fmt.Fprintln(os.Stderr, string(data))
	} else {
		if len(fields) > 0 {
			log.Printf("[%s] %s %v", level, message, fields)
		} else {
			log.Printf("[%s] %s", level, message)
		}
	}
}

func logInfo(message string, fields map[string]interface{}) {
	logJSON("INFO", message, fields)
}

func logWarn(message string, fields map[string]interface{}) {
	logJSON("WARN", message, fields)
}

func logError(message string, fields map[string]interface{}) {
	logJSON("ERROR", message, fields)
}

// ===== Audit Logging =====

// AuditEntry represents an audit log entry for query tracking.
type AuditEntry struct {
	Timestamp  string `json:"timestamp"`
	Tool       string `json:"tool"`
	Database   string `json:"database,omitempty"`
	Query      string `json:"query,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	RowCount   int    `json:"row_count,omitempty"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// AuditLogger handles writing audit logs to a file.
type AuditLogger struct {
	file    *os.File
	mu      sync.Mutex
	enabled bool
}

// NewAuditLogger creates a new audit logger.
// If path is empty, the logger is disabled.
func NewAuditLogger(path string) (*AuditLogger, error) {
	if path == "" {
		return &AuditLogger{enabled: false}, nil
	}
	// Clean the path to prevent directory traversal attacks
	cleanPath := filepath.Clean(path)
	// #nosec G304 -- path is from trusted environment variable MYSQL_MCP_AUDIT_LOG
	// #nosec G302 -- audit logs need to be readable by log aggregation tools
	f, err := os.OpenFile(cleanPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log: %w", err)
	}
	return &AuditLogger{file: f, enabled: true}, nil
}

// Log writes an audit entry to the log file.
func (a *AuditLogger) Log(entry AuditEntry) {
	if !a.enabled {
		return
	}
	entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	a.mu.Lock()
	defer a.mu.Unlock()
	data, _ := json.Marshal(entry)
	a.file.WriteString(string(data) + "\n")
}

// Close closes the audit log file.
func (a *AuditLogger) Close() {
	if a.file != nil {
		a.file.Close()
	}
}

// ===== Query Timing Helper =====

// QueryTimer tracks query execution time and provides logging helpers.
type QueryTimer struct {
	start time.Time
	tool  string
}

// NewQueryTimer creates a new query timer for the given tool.
func NewQueryTimer(tool string) *QueryTimer {
	return &QueryTimer{start: time.Now(), tool: tool}
}

// Elapsed returns the time elapsed since the timer was created.
func (t *QueryTimer) Elapsed() time.Duration {
	return time.Since(t.start)
}

// ElapsedMs returns the elapsed time in milliseconds.
func (t *QueryTimer) ElapsedMs() int64 {
	return t.Elapsed().Milliseconds()
}

// LogSuccess logs a successful query execution.
func (t *QueryTimer) LogSuccess(rowCount int, query string) {
	fields := map[string]interface{}{
		"tool":        t.tool,
		"duration_ms": t.ElapsedMs(),
		"row_count":   rowCount,
	}
	if query != "" && len(query) <= 200 {
		fields["query"] = query
	}
	logInfo("query executed", fields)
}

// LogError logs a failed query execution.
func (t *QueryTimer) LogError(err error, query string) {
	fields := map[string]interface{}{
		"tool":        t.tool,
		"duration_ms": t.ElapsedMs(),
		"error":       err.Error(),
	}
	if query != "" && len(query) <= 200 {
		fields["query"] = query
	}
	logError("query failed", fields)
}
