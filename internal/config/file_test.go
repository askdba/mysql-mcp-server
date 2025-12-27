// internal/config/file_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigFileYAML(t *testing.T) {
	content := `
connections:
  default:
    dsn: "user:pass@tcp(localhost:3306)/db"
    description: "Test DB"

query:
  max_rows: 500
  timeout_seconds: 60

pool:
  max_open_conns: 20
  max_idle_conns: 10
  conn_max_lifetime_minutes: 60
  conn_max_idle_time_minutes: 15
  ping_timeout_seconds: 10

features:
  extended_tools: true
  vector_tools: true

logging:
  json_format: true
  audit_log_path: "/var/log/audit.log"

http:
  enabled: true
  port: 8080
  request_timeout_seconds: 120
  rate_limit:
    enabled: true
    rps: 50
    burst: 100
`

	tmpFile := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	cfg, err := LoadConfigFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}

	// Verify connections
	if len(cfg.Connections) != 1 {
		t.Errorf("expected 1 connection, got %d", len(cfg.Connections))
	}
	if conn, ok := cfg.Connections["default"]; !ok {
		t.Error("expected 'default' connection")
	} else {
		if conn.DSN != "user:pass@tcp(localhost:3306)/db" {
			t.Errorf("unexpected DSN: %s", conn.DSN)
		}
		if conn.Description != "Test DB" {
			t.Errorf("unexpected description: %s", conn.Description)
		}
	}

	// Verify query settings
	if cfg.Query.MaxRows != 500 {
		t.Errorf("expected max_rows 500, got %d", cfg.Query.MaxRows)
	}
	if cfg.Query.TimeoutSeconds != 60 {
		t.Errorf("expected timeout_seconds 60, got %d", cfg.Query.TimeoutSeconds)
	}

	// Verify pool settings
	if cfg.Pool.MaxOpenConns != 20 {
		t.Errorf("expected max_open_conns 20, got %d", cfg.Pool.MaxOpenConns)
	}
	if cfg.Pool.MaxIdleConns != 10 {
		t.Errorf("expected max_idle_conns 10, got %d", cfg.Pool.MaxIdleConns)
	}

	// Verify features
	if !cfg.Features.ExtendedTools {
		t.Error("expected extended_tools true")
	}
	if !cfg.Features.VectorTools {
		t.Error("expected vector_tools true")
	}

	// Verify logging
	if !cfg.Logging.JSONFormat {
		t.Error("expected json_format true")
	}
	if cfg.Logging.AuditLogPath != "/var/log/audit.log" {
		t.Errorf("unexpected audit_log_path: %s", cfg.Logging.AuditLogPath)
	}

	// Verify HTTP settings
	if !cfg.HTTP.Enabled {
		t.Error("expected http.enabled true")
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("expected http.port 8080, got %d", cfg.HTTP.Port)
	}
	if !cfg.HTTP.RateLimit.Enabled {
		t.Error("expected rate_limit.enabled true")
	}
	if cfg.HTTP.RateLimit.RPS != 50 {
		t.Errorf("expected rate_limit.rps 50, got %d", cfg.HTTP.RateLimit.RPS)
	}
}

func TestLoadConfigFileJSON(t *testing.T) {
	content := `{
		"connections": {
			"primary": {
				"dsn": "root:secret@tcp(db:3306)/app",
				"description": "Primary DB"
			}
		},
		"query": {
			"max_rows": 100,
			"timeout_seconds": 15
		},
		"features": {
			"extended_tools": true
		}
	}`

	tmpFile := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	cfg, err := LoadConfigFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadConfigFile failed: %v", err)
	}

	if len(cfg.Connections) != 1 {
		t.Errorf("expected 1 connection, got %d", len(cfg.Connections))
	}
	if cfg.Query.MaxRows != 100 {
		t.Errorf("expected max_rows 100, got %d", cfg.Query.MaxRows)
	}
	if !cfg.Features.ExtendedTools {
		t.Error("expected extended_tools true")
	}
}

func TestFileConfigToConfig(t *testing.T) {
	fc := &FileConfig{
		Connections: map[string]FileConnectionConfig{
			"default": {
				DSN:         "user:pass@tcp(localhost:3306)/db",
				Description: "Test",
				ReadOnly:    true,
			},
		},
		Query: FileQueryConfig{
			MaxRows:        300,
			TimeoutSeconds: 45,
		},
		Pool: FilePoolConfig{
			MaxOpenConns:           15,
			MaxIdleConns:           8,
			ConnMaxLifetimeMinutes: 45,
			ConnMaxIdleTimeMinutes: 10,
			PingTimeoutSeconds:     7,
		},
		Features: FileFeatureConfig{
			ExtendedTools: true,
			VectorTools:   false,
		},
		Logging: FileLoggingConfig{
			JSONFormat:   true,
			AuditLogPath: "/tmp/audit.log",
		},
		HTTP: FileHTTPConfig{
			Enabled:               true,
			Port:                  9000,
			RequestTimeoutSeconds: 90,
			RateLimit: FileRateLimitConfig{
				Enabled: true,
				RPS:     75,
				Burst:   150,
			},
		},
	}

	cfg := fc.ToConfig()

	// Verify connections
	if len(cfg.Connections) != 1 {
		t.Errorf("expected 1 connection, got %d", len(cfg.Connections))
	}
	if cfg.Connections[0].Name != "default" {
		t.Errorf("unexpected connection name: %s", cfg.Connections[0].Name)
	}
	if !cfg.Connections[0].ReadOnly {
		t.Error("expected connection to be read_only")
	}

	// Verify query settings
	if cfg.MaxRows != 300 {
		t.Errorf("expected MaxRows 300, got %d", cfg.MaxRows)
	}
	if cfg.QueryTimeout != 45*time.Second {
		t.Errorf("expected QueryTimeout 45s, got %v", cfg.QueryTimeout)
	}

	// Verify pool settings
	if cfg.MaxOpenConns != 15 {
		t.Errorf("expected MaxOpenConns 15, got %d", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 8 {
		t.Errorf("expected MaxIdleConns 8, got %d", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != 45*time.Minute {
		t.Errorf("expected ConnMaxLifetime 45m, got %v", cfg.ConnMaxLifetime)
	}
	if cfg.PingTimeout != 7*time.Second {
		t.Errorf("expected PingTimeout 7s, got %v", cfg.PingTimeout)
	}

	// Verify features
	if !cfg.ExtendedMode {
		t.Error("expected ExtendedMode true")
	}
	if cfg.VectorMode {
		t.Error("expected VectorMode false")
	}

	// Verify logging
	if !cfg.JSONLogging {
		t.Error("expected JSONLogging true")
	}
	if cfg.AuditLogPath != "/tmp/audit.log" {
		t.Errorf("unexpected AuditLogPath: %s", cfg.AuditLogPath)
	}

	// Verify HTTP
	if !cfg.HTTPMode {
		t.Error("expected HTTPMode true")
	}
	if cfg.HTTPPort != 9000 {
		t.Errorf("expected HTTPPort 9000, got %d", cfg.HTTPPort)
	}
	if !cfg.RateLimitEnabled {
		t.Error("expected RateLimitEnabled true")
	}
	if cfg.RateLimitRPS != 75 {
		t.Errorf("expected RateLimitRPS 75, got %f", cfg.RateLimitRPS)
	}
}

// TestMinimalConfigDefaults verifies that a minimal config file (connections only)
// properly receives default values for all duration fields to avoid zero-value issues
// where context.WithTimeout() would create immediately-expired contexts.
func TestMinimalConfigDefaults(t *testing.T) {
	// Minimal config with only connections - no query, pool, http settings
	fc := &FileConfig{
		Connections: map[string]FileConnectionConfig{
			"default": {
				DSN:         "user:pass@tcp(localhost:3306)/db",
				Description: "Minimal config",
			},
		},
		// All other fields are zero values
	}

	cfg := fc.ToConfig()

	// Verify all duration fields have non-zero defaults
	// These are critical - zero values would cause immediate timeouts
	if cfg.QueryTimeout == 0 {
		t.Errorf("QueryTimeout should have default value, got 0")
	}
	if cfg.QueryTimeout != time.Duration(DefaultQueryTimeoutSecs)*time.Second {
		t.Errorf("expected QueryTimeout %ds, got %v", DefaultQueryTimeoutSecs, cfg.QueryTimeout)
	}

	if cfg.ConnMaxLifetime == 0 {
		t.Errorf("ConnMaxLifetime should have default value, got 0")
	}
	if cfg.ConnMaxLifetime != time.Duration(DefaultConnMaxLifetimeMins)*time.Minute {
		t.Errorf("expected ConnMaxLifetime %dm, got %v", DefaultConnMaxLifetimeMins, cfg.ConnMaxLifetime)
	}

	if cfg.ConnMaxIdleTime == 0 {
		t.Errorf("ConnMaxIdleTime should have default value, got 0")
	}
	if cfg.ConnMaxIdleTime != time.Duration(DefaultConnMaxIdleTimeMins)*time.Minute {
		t.Errorf("expected ConnMaxIdleTime %dm, got %v", DefaultConnMaxIdleTimeMins, cfg.ConnMaxIdleTime)
	}

	if cfg.PingTimeout == 0 {
		t.Errorf("PingTimeout should have default value, got 0")
	}
	if cfg.PingTimeout != time.Duration(DefaultPingTimeoutSecs)*time.Second {
		t.Errorf("expected PingTimeout %ds, got %v", DefaultPingTimeoutSecs, cfg.PingTimeout)
	}

	if cfg.HTTPRequestTimeout == 0 {
		t.Errorf("HTTPRequestTimeout should have default value, got 0")
	}
	if cfg.HTTPRequestTimeout != time.Duration(DefaultHTTPRequestTimeoutS)*time.Second {
		t.Errorf("expected HTTPRequestTimeout %ds, got %v", DefaultHTTPRequestTimeoutS, cfg.HTTPRequestTimeout)
	}

	// Also verify integer defaults
	if cfg.MaxRows != DefaultMaxRows {
		t.Errorf("expected MaxRows %d, got %d", DefaultMaxRows, cfg.MaxRows)
	}
	if cfg.MaxOpenConns != DefaultMaxOpenConns {
		t.Errorf("expected MaxOpenConns %d, got %d", DefaultMaxOpenConns, cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != DefaultMaxIdleConns {
		t.Errorf("expected MaxIdleConns %d, got %d", DefaultMaxIdleConns, cfg.MaxIdleConns)
	}
	if cfg.HTTPPort != DefaultHTTPPort {
		t.Errorf("expected HTTPPort %d, got %d", DefaultHTTPPort, cfg.HTTPPort)
	}
	if cfg.RateLimitRPS != float64(DefaultRateLimitRPS) {
		t.Errorf("expected RateLimitRPS %d, got %f", DefaultRateLimitRPS, cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst != DefaultRateLimitBurst {
		t.Errorf("expected RateLimitBurst %d, got %d", DefaultRateLimitBurst, cfg.RateLimitBurst)
	}
}

func TestValidateConfigFile(t *testing.T) {
	// Valid config
	validContent := `
connections:
  default:
    dsn: "user:pass@tcp(localhost:3306)/db"
`
	validFile := filepath.Join(t.TempDir(), "valid.yaml")
	if err := os.WriteFile(validFile, []byte(validContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	if err := ValidateConfigFile(validFile); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}

	// Invalid config - no connections
	invalidContent := `
query:
  max_rows: 100
`
	invalidFile := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(invalidFile, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	if err := ValidateConfigFile(invalidFile); err == nil {
		t.Error("expected error for config without connections")
	}

	// Invalid config - empty DSN
	emptyDSNContent := `
connections:
  default:
    dsn: ""
    description: "Empty DSN"
`
	emptyDSNFile := filepath.Join(t.TempDir(), "empty_dsn.yaml")
	if err := os.WriteFile(emptyDSNFile, []byte(emptyDSNContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	if err := ValidateConfigFile(emptyDSNFile); err == nil {
		t.Error("expected error for config with empty DSN")
	}
}

func TestFindConfigFile(t *testing.T) {
	// Reset global state
	originalPath := ConfigFilePath
	defer func() { ConfigFilePath = originalPath }()

	// Test with explicit path
	ConfigFilePath = "/some/explicit/path.yaml"
	if path := FindConfigFile(); path != "/some/explicit/path.yaml" {
		t.Errorf("expected explicit path, got: %s", path)
	}

	// Reset and test environment variable
	ConfigFilePath = ""
	t.Setenv("MYSQL_MCP_CONFIG", "/env/config.yaml")
	if path := FindConfigFile(); path != "/env/config.yaml" {
		t.Errorf("expected env var path, got: %s", path)
	}

	// Reset env var for remaining tests
	t.Setenv("MYSQL_MCP_CONFIG", "")

	// Test current directory file
	ConfigFilePath = ""
	tmpDir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(oldWd)

	// Create a config file in current directory
	if err := os.WriteFile("mysql-mcp-server.yaml", []byte("connections: {}"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	if path := FindConfigFile(); path != "mysql-mcp-server.yaml" {
		t.Errorf("expected current dir file, got: %s", path)
	}
}

func TestMaskDSN(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"user:password@tcp(localhost:3306)/db", "user:***@tcp(localhost:3306)/db"},
		{"root:secret123@tcp(127.0.0.1:3306)/mysql", "root:***@tcp(127.0.0.1:3306)/mysql"},
		{"user@tcp(localhost:3306)/db", "user@tcp(localhost:3306)/db"}, // No password
		{"localhost:3306/db", "localhost:3306/db"},                     // No user
		// Passwords containing @ characters (must use LastIndex to find separator)
		{"user:p@ssword@tcp(localhost:3306)/db", "user:***@tcp(localhost:3306)/db"},
		{"user:p@ss@word@tcp(host:3306)/db", "user:***@tcp(host:3306)/db"},
		{"root:@dm1n@123@tcp(127.0.0.1:3306)/mysql", "root:***@tcp(127.0.0.1:3306)/mysql"},
	}

	for _, tt := range tests {
		result := maskDSN(tt.input)
		if result != tt.expected {
			t.Errorf("maskDSN(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestPrintConfig(t *testing.T) {
	cfg := &Config{
		Connections: []ConnectionConfig{
			{Name: "default", DSN: "user:secret@tcp(localhost:3306)/db", Description: "Test"},
		},
		MaxRows:            200,
		QueryTimeout:       30 * time.Second,
		MaxOpenConns:       10,
		MaxIdleConns:       5,
		ConnMaxLifetime:    30 * time.Minute,
		ConnMaxIdleTime:    5 * time.Minute,
		PingTimeout:        5 * time.Second,
		ExtendedMode:       true,
		VectorMode:         false,
		JSONLogging:        false,
		HTTPMode:           false,
		HTTPPort:           9306,
		HTTPRequestTimeout: 60 * time.Second,
		RateLimitEnabled:   false,
		RateLimitRPS:       100,
		RateLimitBurst:     200,
	}

	output := PrintConfig(cfg)

	// Check that password is masked
	if !contains(output, "user:***@tcp(localhost:3306)/db") {
		t.Error("expected DSN password to be masked")
	}

	// Check that key fields are present
	if !contains(output, "max_rows: 200") {
		t.Error("expected max_rows in output")
	}
	if !contains(output, "extended_tools: true") {
		t.Error("expected extended_tools in output")
	}
}

// TestConnectionOrderingDeterministic verifies that connections from a config file
// are always returned in a deterministic order, with "default" connection first
// if it exists, then alphabetically by name. This is critical because the
// ConnectionManager uses the first connection as the active default.
func TestConnectionOrderingDeterministic(t *testing.T) {
	// Config with multiple connections including "default"
	fc := &FileConfig{
		Connections: map[string]FileConnectionConfig{
			"zebra":      {DSN: "zebra:pass@tcp(localhost:3306)/zebra"},
			"alpha":      {DSN: "alpha:pass@tcp(localhost:3306)/alpha"},
			"default":    {DSN: "default:pass@tcp(localhost:3306)/default"},
			"production": {DSN: "prod:pass@tcp(localhost:3306)/prod"},
		},
	}

	// Run multiple times to verify determinism (map iteration is random)
	for i := 0; i < 10; i++ {
		cfg := fc.ToConfig()

		if len(cfg.Connections) != 4 {
			t.Fatalf("iteration %d: expected 4 connections, got %d", i, len(cfg.Connections))
		}

		// "default" should always be first
		if cfg.Connections[0].Name != "default" {
			t.Errorf("iteration %d: expected first connection to be 'default', got '%s'", i, cfg.Connections[0].Name)
		}

		// Remaining should be alphabetically sorted
		expectedOrder := []string{"default", "alpha", "production", "zebra"}
		for j, expected := range expectedOrder {
			if cfg.Connections[j].Name != expected {
				t.Errorf("iteration %d: expected connection[%d] to be '%s', got '%s'", i, j, expected, cfg.Connections[j].Name)
			}
		}
	}
}

// TestConnectionOrderingWithoutDefault verifies alphabetical ordering when
// there is no "default" connection defined.
func TestConnectionOrderingWithoutDefault(t *testing.T) {
	fc := &FileConfig{
		Connections: map[string]FileConnectionConfig{
			"zebra":      {DSN: "zebra:pass@tcp(localhost:3306)/zebra"},
			"alpha":      {DSN: "alpha:pass@tcp(localhost:3306)/alpha"},
			"production": {DSN: "prod:pass@tcp(localhost:3306)/prod"},
		},
	}

	// Run multiple times to verify determinism
	for i := 0; i < 10; i++ {
		cfg := fc.ToConfig()

		expectedOrder := []string{"alpha", "production", "zebra"}
		for j, expected := range expectedOrder {
			if cfg.Connections[j].Name != expected {
				t.Errorf("iteration %d: expected connection[%d] to be '%s', got '%s'", i, j, expected, cfg.Connections[j].Name)
			}
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
