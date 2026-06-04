package config

import (
	"os"
	"testing"
	"time"
)

func clearEnv() {
	envVars := []string{
		"MYSQL_DSN",
		"MYSQL_CONNECTIONS",
		"MYSQL_MAX_ROWS",
		"MYSQL_QUERY_TIMEOUT_SECONDS",
		"MYSQL_QUERY_TIMEOUT",
		"MYSQL_MAX_OPEN_CONNS",
		"MYSQL_POOL_SIZE",
		"MYSQL_MAX_IDLE_CONNS",
		"MYSQL_CONN_MAX_LIFETIME_MINUTES",
		"MYSQL_CONN_MAX_IDLE_TIME_MINUTES",
		"MYSQL_PING_TIMEOUT_SECONDS",
		"MYSQL_MCP_EXTENDED",
		"MYSQL_MCP_VECTOR",
		"MYSQL_MCP_HTTP",
		"MYSQL_MCP_METRICS_HTTP",
		"MYSQL_MCP_JSON_LOGS",
		"MYSQL_MCP_TOKEN_TRACKING",
		"MYSQL_MCP_TOKEN_MODEL",
		"MYSQL_MCP_TOKEN_CARD",
		"MYSQL_HTTP_PORT",
		"MYSQL_MCP_AUDIT_LOG",
		"MYSQL_MCP_ALLOWED_DATABASES",
		"MYSQL_MCP_STRICT_READ_ONLY",
		"MYSQL_MCP_PROCESS_ADMIN",
		"MYSQL_MCP_READ_AUDIT_TOOL",
		"MYSQL_MCP_SLOW_QUERY_TOOL",
		"MYSQL_SSL",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}
	// Clear numbered DSNs
	for i := 1; i <= 10; i++ {
		os.Unsetenv("MYSQL_DSN_" + string(rune('0'+i)))
		os.Unsetenv("MYSQL_DSN_" + string(rune('0'+i)) + "_NAME")
		os.Unsetenv("MYSQL_DSN_" + string(rune('0'+i)) + "_DESC")
		os.Unsetenv("MYSQL_DSN_" + string(rune('0'+i)) + "_SSL")
	}
}

func TestLoadWithDefaults(t *testing.T) {
	clearEnv()

	// Required DSN
	if err := os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Connections) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(cfg.Connections))
	}

	if cfg.Connections[0].DSN == "" {
		t.Fatalf("expected DSN to be set")
	}

	if cfg.MaxRows != DefaultMaxRows {
		t.Fatalf("expected default MaxRows=%d, got %d", DefaultMaxRows, cfg.MaxRows)
	}

	if cfg.QueryTimeout != time.Duration(DefaultQueryTimeoutSecs)*time.Second {
		t.Fatalf("expected default QueryTimeout=%ds, got %v", DefaultQueryTimeoutSecs, cfg.QueryTimeout)
	}

	if cfg.MaxOpenConns != DefaultMaxOpenConns {
		t.Fatalf("expected default MaxOpenConns=%d, got %d", DefaultMaxOpenConns, cfg.MaxOpenConns)
	}

	if cfg.MaxIdleConns != DefaultMaxIdleConns {
		t.Fatalf("expected default MaxIdleConns=%d, got %d", DefaultMaxIdleConns, cfg.MaxIdleConns)
	}

	if cfg.ConnMaxIdleTime != time.Duration(DefaultConnMaxIdleTimeMins)*time.Minute {
		t.Fatalf("expected default ConnMaxIdleTime=%dm, got %v", DefaultConnMaxIdleTimeMins, cfg.ConnMaxIdleTime)
	}

	if cfg.PingTimeout != time.Duration(DefaultPingTimeoutSecs)*time.Second {
		t.Fatalf("expected default PingTimeout=%ds, got %v", DefaultPingTimeoutSecs, cfg.PingTimeout)
	}

	if cfg.HTTPPort != DefaultHTTPPort {
		t.Fatalf("expected default HTTPPort=%d, got %d", DefaultHTTPPort, cfg.HTTPPort)
	}

	// Feature flags should default to false
	if cfg.ExtendedMode {
		t.Fatal("expected ExtendedMode to be false by default")
	}
	if cfg.VectorMode {
		t.Fatal("expected VectorMode to be false by default")
	}
	if cfg.HTTPMode {
		t.Fatal("expected HTTPMode to be false by default")
	}
	if cfg.JSONLogging {
		t.Fatal("expected JSONLogging to be false by default")
	}

	// Token tracking should default to false, with a non-empty default model.
	if cfg.TokenTracking {
		t.Fatal("expected TokenTracking to be false by default")
	}
	if cfg.TokenModel == "" {
		t.Fatal("expected TokenModel default to be non-empty")
	}
}

func TestLoadOverridesFromEnv(t *testing.T) {
	clearEnv()

	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/otherdb")
	os.Setenv("MYSQL_MAX_ROWS", "500")
	os.Setenv("MYSQL_QUERY_TIMEOUT_SECONDS", "60")
	os.Setenv("MYSQL_MAX_OPEN_CONNS", "20")
	os.Setenv("MYSQL_MAX_IDLE_CONNS", "10")
	os.Setenv("MYSQL_CONN_MAX_IDLE_TIME_MINUTES", "10")
	os.Setenv("MYSQL_PING_TIMEOUT_SECONDS", "15")
	os.Setenv("MYSQL_MCP_EXTENDED", "1")
	os.Setenv("MYSQL_MCP_VECTOR", "1")
	os.Setenv("MYSQL_MCP_HTTP", "1")
	os.Setenv("MYSQL_MCP_JSON_LOGS", "1")
	os.Setenv("MYSQL_MCP_TOKEN_TRACKING", "1")
	os.Setenv("MYSQL_MCP_TOKEN_MODEL", "cl100k_base")
	os.Setenv("MYSQL_HTTP_PORT", "8080")
	os.Setenv("MYSQL_MCP_AUDIT_LOG", "/var/log/audit.log")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Connections[0].DSN != "user:pass@tcp(localhost:3306)/otherdb" {
		t.Fatalf("unexpected DSN: %s", cfg.Connections[0].DSN)
	}
	if cfg.MaxRows != 500 {
		t.Fatalf("expected MaxRows=500, got %d", cfg.MaxRows)
	}
	if cfg.QueryTimeout != 60*time.Second {
		t.Fatalf("expected QueryTimeout=60s, got %v", cfg.QueryTimeout)
	}
	if cfg.MaxOpenConns != 20 {
		t.Fatalf("expected MaxOpenConns=20, got %d", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 10 {
		t.Fatalf("expected MaxIdleConns=10, got %d", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxIdleTime != 10*time.Minute {
		t.Fatalf("expected ConnMaxIdleTime=10m, got %v", cfg.ConnMaxIdleTime)
	}
	if cfg.PingTimeout != 15*time.Second {
		t.Fatalf("expected PingTimeout=15s, got %v", cfg.PingTimeout)
	}
	if !cfg.ExtendedMode {
		t.Fatal("expected ExtendedMode to be true")
	}
	if !cfg.VectorMode {
		t.Fatal("expected VectorMode to be true")
	}
	if !cfg.HTTPMode {
		t.Fatal("expected HTTPMode to be true")
	}
	if !cfg.JSONLogging {
		t.Fatal("expected JSONLogging to be true")
	}
	if !cfg.TokenTracking {
		t.Fatal("expected TokenTracking to be true")
	}
	if cfg.TokenModel != "cl100k_base" {
		t.Fatalf("expected TokenModel=cl100k_base, got %q", cfg.TokenModel)
	}
	if cfg.HTTPPort != 8080 {
		t.Fatalf("expected HTTPPort=8080, got %d", cfg.HTTPPort)
	}
	if cfg.AuditLogPath != "/var/log/audit.log" {
		t.Fatalf("expected AuditLogPath=/var/log/audit.log, got %s", cfg.AuditLogPath)
	}
	if !cfg.TokenCard {
		t.Fatal("expected TokenCard true by default when HTTP mode is on and MYSQL_MCP_TOKEN_CARD is unset")
	}
}

func TestMetricsHTTPSidecarTokenCardDefault(t *testing.T) {
	clearEnv()
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
	os.Setenv("MYSQL_MCP_METRICS_HTTP", "1")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPMode {
		t.Fatal("expected HTTPMode false when only METRICS_HTTP")
	}
	if !cfg.MetricsHTTP {
		t.Fatal("expected MetricsHTTP true")
	}
	if !cfg.TokenCard {
		t.Fatal("expected TokenCard true by default for metrics sidecar when MYSQL_MCP_TOKEN_CARD unset")
	}
}

func TestHTTPModeClearsMetricsHTTP(t *testing.T) {
	clearEnv()
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
	os.Setenv("MYSQL_MCP_HTTP", "1")
	os.Setenv("MYSQL_MCP_METRICS_HTTP", "1")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.HTTPMode {
		t.Fatal("expected HTTPMode true")
	}
	if cfg.MetricsHTTP {
		t.Fatal("expected MetricsHTTP cleared when full HTTP mode is on")
	}
}

func TestHTTPModeTokenCardExplicitOff(t *testing.T) {
	clearEnv()
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
	os.Setenv("MYSQL_MCP_HTTP", "1")
	os.Setenv("MYSQL_MCP_TOKEN_CARD", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TokenCard {
		t.Fatal("expected TokenCard false when MYSQL_MCP_TOKEN_CARD=0")
	}
}

func TestLoadMissingDSN(t *testing.T) {
	clearEnv()

	_, err := Load()
	if err == nil {
		t.Fatalf("expected error when no DSN is configured")
	}
}

func TestLoadJSONConnections(t *testing.T) {
	clearEnv()

	jsonConns := `[
		{"name": "prod", "dsn": "user:pass@tcp(prod:3306)/db", "description": "Production"},
		{"name": "staging", "dsn": "user:pass@tcp(staging:3306)/db", "description": "Staging"}
	]`
	os.Setenv("MYSQL_CONNECTIONS", jsonConns)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(cfg.Connections))
	}

	if cfg.Connections[0].Name != "prod" {
		t.Fatalf("expected first connection name 'prod', got '%s'", cfg.Connections[0].Name)
	}
	if cfg.Connections[1].Name != "staging" {
		t.Fatalf("expected second connection name 'staging', got '%s'", cfg.Connections[1].Name)
	}
}

func TestLoadNumberedDSNs(t *testing.T) {
	clearEnv()

	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/default")
	os.Setenv("MYSQL_DSN_1", "user:pass@tcp(server1:3306)/db1")
	os.Setenv("MYSQL_DSN_1_NAME", "server1")
	os.Setenv("MYSQL_DSN_1_DESC", "First server")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(cfg.Connections))
	}

	if cfg.Connections[0].Name != "default" {
		t.Fatalf("expected first connection name 'default', got '%s'", cfg.Connections[0].Name)
	}
	if cfg.Connections[1].Name != "server1" {
		t.Fatalf("expected second connection name 'server1', got '%s'", cfg.Connections[1].Name)
	}
	if cfg.Connections[1].Description != "First server" {
		t.Fatalf("expected description 'First server', got '%s'", cfg.Connections[1].Description)
	}
}

func TestGetEnvInt(t *testing.T) {
	clearEnv()

	// Test default value
	if v := GetEnvInt("NONEXISTENT", 42); v != 42 {
		t.Fatalf("expected default 42, got %d", v)
	}

	// Test valid value
	os.Setenv("TEST_INT", "100")
	if v := GetEnvInt("TEST_INT", 42); v != 100 {
		t.Fatalf("expected 100, got %d", v)
	}

	// Test invalid value returns default
	os.Setenv("TEST_INT", "invalid")
	if v := GetEnvInt("TEST_INT", 42); v != 42 {
		t.Fatalf("expected default 42 for invalid, got %d", v)
	}

	// Test negative value returns default
	os.Setenv("TEST_INT", "-5")
	if v := GetEnvInt("TEST_INT", 42); v != 42 {
		t.Fatalf("expected default 42 for negative, got %d", v)
	}

	os.Unsetenv("TEST_INT")
}

func TestGetEnvBool(t *testing.T) {
	clearEnv()

	// Test unset returns false
	if GetEnvBool("NONEXISTENT") {
		t.Fatal("expected false for unset var")
	}

	// Test truthy values
	for _, v := range []string{"1", "true", "TRUE", " yes ", "on", "y"} {
		os.Setenv("TEST_BOOL", v)
		if !GetEnvBool("TEST_BOOL") {
			t.Fatalf("expected true for %q", v)
		}
	}

	os.Setenv("TEST_BOOL", "0")
	if GetEnvBool("TEST_BOOL") {
		t.Fatal("expected false for '0'")
	}
	os.Setenv("TEST_BOOL", "false")
	if GetEnvBool("TEST_BOOL") {
		t.Fatal("expected false for 'false'")
	}
	os.Setenv("TEST_BOOL", "maybe")
	if GetEnvBool("TEST_BOOL") {
		t.Fatal("expected false for unknown value")
	}

	os.Unsetenv("TEST_BOOL")
}

func TestLoadJSONConnectionsWithGlobalSSL(t *testing.T) {
	clearEnv()

	// Set global SSL and JSON connections without SSL settings
	os.Setenv("MYSQL_SSL", "true")
	jsonConns := `[
		{"name": "prod", "dsn": "user:pass@tcp(prod:3306)/db", "description": "Production"},
		{"name": "staging", "dsn": "user:pass@tcp(staging:3306)/db", "description": "Staging"}
	]`
	os.Setenv("MYSQL_CONNECTIONS", jsonConns)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(cfg.Connections))
	}

	// Both connections should inherit global SSL
	for _, conn := range cfg.Connections {
		if conn.SSL != "true" {
			t.Errorf("expected SSL 'true' for connection %s, got '%s'", conn.Name, conn.SSL)
		}
	}
}

func TestLoadJSONConnectionsWithPerConnectionSSL(t *testing.T) {
	clearEnv()

	// Set global SSL and JSON connections with mixed SSL settings
	os.Setenv("MYSQL_SSL", "true")
	jsonConns := `[
		{"name": "prod", "dsn": "user:pass@tcp(prod:3306)/db", "description": "Production", "ssl": "skip-verify"},
		{"name": "staging", "dsn": "user:pass@tcp(staging:3306)/db", "description": "Staging"}
	]`
	os.Setenv("MYSQL_CONNECTIONS", jsonConns)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(cfg.Connections))
	}

	// prod should keep its own SSL setting
	if cfg.Connections[0].SSL != "skip-verify" {
		t.Errorf("expected SSL 'skip-verify' for prod, got '%s'", cfg.Connections[0].SSL)
	}

	// staging should inherit global SSL
	if cfg.Connections[1].SSL != "true" {
		t.Errorf("expected SSL 'true' for staging, got '%s'", cfg.Connections[1].SSL)
	}
}

func TestLoadNumberedDSNsWithGlobalSSL(t *testing.T) {
	clearEnv()

	os.Setenv("MYSQL_SSL", "skip-verify")
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/default")
	os.Setenv("MYSQL_DSN_1", "user:pass@tcp(server1:3306)/db1")
	os.Setenv("MYSQL_DSN_1_NAME", "server1")
	os.Setenv("MYSQL_DSN_1_SSL", "true") // Override global SSL

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Connections) != 2 {
		t.Fatalf("expected 2 connections, got %d", len(cfg.Connections))
	}

	// Default connection should use global SSL
	if cfg.Connections[0].SSL != "skip-verify" {
		t.Errorf("expected SSL 'skip-verify' for default, got '%s'", cfg.Connections[0].SSL)
	}

	// server1 should use its own SSL setting
	if cfg.Connections[1].SSL != "true" {
		t.Errorf("expected SSL 'true' for server1, got '%s'", cfg.Connections[1].SSL)
	}
}

func TestMySQLPoolSizeAlias(t *testing.T) {
	clearEnv()

	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/db")
	os.Setenv("MYSQL_POOL_SIZE", "15")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.MaxOpenConns != 15 {
		t.Fatalf("expected MaxOpenConns=15 from MYSQL_POOL_SIZE, got %d", cfg.MaxOpenConns)
	}
}

func TestMySQLMaxOpenConnsTakesPrecedenceOverPoolSize(t *testing.T) {
	clearEnv()

	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/db")
	os.Setenv("MYSQL_POOL_SIZE", "15")
	os.Setenv("MYSQL_MAX_OPEN_CONNS", "25")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// MYSQL_MAX_OPEN_CONNS must take precedence over MYSQL_POOL_SIZE
	if cfg.MaxOpenConns != 25 {
		t.Fatalf("expected MaxOpenConns=25 (MYSQL_MAX_OPEN_CONNS wins), got %d", cfg.MaxOpenConns)
	}
}

func TestMySQLQueryTimeoutMsAlias(t *testing.T) {
	clearEnv()

	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/db")
	os.Setenv("MYSQL_QUERY_TIMEOUT", "45000") // 45 000 ms = 45 s

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.QueryTimeout != 45*time.Second {
		t.Fatalf("expected QueryTimeout=45s from MYSQL_QUERY_TIMEOUT=45000, got %v", cfg.QueryTimeout)
	}
}

func TestMySQLQueryTimeoutSecondsTakesPrecedenceOverMs(t *testing.T) {
	clearEnv()

	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/db")
	os.Setenv("MYSQL_QUERY_TIMEOUT", "45000")      // 45 s in ms
	os.Setenv("MYSQL_QUERY_TIMEOUT_SECONDS", "60") // 60 s in seconds

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// MYSQL_QUERY_TIMEOUT_SECONDS must take precedence
	if cfg.QueryTimeout != 60*time.Second {
		t.Fatalf("expected QueryTimeout=60s (MYSQL_QUERY_TIMEOUT_SECONDS wins), got %v", cfg.QueryTimeout)
	}
}

func TestSecurityEnvOverrides(t *testing.T) {
	clearEnv()
	_ = os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/db")
	_ = os.Setenv("MYSQL_MCP_ALLOWED_DATABASES", " a , b, c ")
	_ = os.Setenv("MYSQL_MCP_STRICT_READ_ONLY", "1")
	_ = os.Setenv("MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS", "1")
	_ = os.Setenv("MYSQL_MCP_PROCESS_ADMIN", "1")
	_ = os.Setenv("MYSQL_MCP_READ_AUDIT_TOOL", "true")
	_ = os.Setenv("MYSQL_MCP_SLOW_QUERY_TOOL", "y")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedDatabases) != 3 || cfg.AllowedDatabases[0] != "a" {
		t.Fatalf("allowed db list: %#v", cfg.AllowedDatabases)
	}
	if !cfg.StrictReadOnly || !cfg.ProcessAdmin || !cfg.ReadAuditTool || !cfg.SlowQueryTool {
		t.Fatalf("flags: strict=%v admin=%v audit=%v slow=%v", cfg.StrictReadOnly, cfg.ProcessAdmin, cfg.ReadAuditTool, cfg.SlowQueryTool)
	}
	if !cfg.AllowSystemSchemas {
		t.Fatalf("expected AllowSystemSchemas=true from MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS=1")
	}
	set := AllowedDatabaseSet(cfg.AllowedDatabases)
	if len(set) != 3 {
		t.Fatalf("set len %d", len(set))
	}
}
