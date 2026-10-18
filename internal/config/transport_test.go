package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTransportDefaultsToStdio(t *testing.T) {
	clearEnv()
	defer clearEnv()
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Transport != TransportStdio {
		t.Errorf("Transport = %q, want %q", cfg.Transport, TransportStdio)
	}
	if cfg.MCPHTTPBind != DefaultMCPHTTPBind || cfg.MCPHTTPPath != DefaultMCPHTTPPath {
		t.Errorf("bind/path = %q/%q, want defaults", cfg.MCPHTTPBind, cfg.MCPHTTPPath)
	}
	if cfg.MCPHTTPSessionTimeout != time.Duration(DefaultMCPHTTPSessionTimeoutMinutes)*time.Minute {
		t.Errorf("session timeout = %v", cfg.MCPHTTPSessionTimeout)
	}
}

func TestTransportHTTPFromEnv(t *testing.T) {
	clearEnv()
	defer clearEnv()
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
	os.Setenv("MYSQL_MCP_TRANSPORT", "HTTP")
	os.Setenv("MYSQL_MCP_HTTP_PATH", "/v1/mcp")
	os.Setenv("MYSQL_MCP_HTTP_TOKEN", " secret ")
	os.Setenv("MYSQL_MCP_HTTP_STATELESS", "1")
	os.Setenv("MYSQL_MCP_HTTP_SESSION_TIMEOUT_MINUTES", "5")
	os.Setenv("MYSQL_MCP_HTTP_ALLOWED_ORIGINS", "https://a.example, https://b.example")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Transport != TransportHTTP {
		t.Errorf("Transport = %q, want http", cfg.Transport)
	}
	if cfg.MCPHTTPPath != "/v1/mcp" || cfg.MCPHTTPToken != "secret" || !cfg.MCPHTTPStateless {
		t.Errorf("unexpected MCP HTTP settings: %+v", cfg)
	}
	if cfg.MCPHTTPSessionTimeout != 5*time.Minute {
		t.Errorf("session timeout = %v, want 5m", cfg.MCPHTTPSessionTimeout)
	}
	if len(cfg.MCPHTTPAllowedOrigins) != 2 || cfg.MCPHTTPAllowedOrigins[1] != "https://b.example" {
		t.Errorf("allowed origins = %v", cfg.MCPHTTPAllowedOrigins)
	}
}

func TestTransportTokenFile(t *testing.T) {
	clearEnv()
	defer clearEnv()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
	os.Setenv("MYSQL_MCP_TRANSPORT", "http")
	os.Setenv("MYSQL_MCP_HTTP_TOKEN_FILE", tokenPath)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCPHTTPToken != "from-file" {
		t.Errorf("token = %q, want from-file", cfg.MCPHTTPToken)
	}

	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("MYSQL_MCP_HTTP_TOKEN_FILE", empty)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected empty token file error, got %v", err)
	}
}

func TestIsLoopbackBind(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1": true,
		"::1":       true,
		"[::1]":     true,
		"localhost": true,
		"127.0.0.2": true,
		"0.0.0.0":   false,
		"::":        false,
		"10.0.0.5":  false,
		"":          false,
		"example":   false,
	} {
		if got := IsLoopbackBind(addr); got != want {
			t.Errorf("IsLoopbackBind(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestValidateTransport(t *testing.T) {
	base := func() *Config {
		c := &Config{Transport: TransportHTTP}
		applyMCPHTTPDefaults(c)
		return c
	}
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"stdio ok", func(c *Config) { c.Transport = TransportStdio; c.HTTPMode = true }, ""},
		{"loopback without token ok", func(c *Config) {}, ""},
		{"unknown transport", func(c *Config) { c.Transport = "grpc" }, "invalid MYSQL_MCP_TRANSPORT"},
		{"rest mode conflict", func(c *Config) { c.HTTPMode = true }, "cannot be combined"},
		{"root path", func(c *Config) { c.MCPHTTPPath = "/" }, "MYSQL_MCP_HTTP_PATH"},
		{"relative path", func(c *Config) { c.MCPHTTPPath = "mcp" }, "MYSQL_MCP_HTTP_PATH"},
		{"cert without key", func(c *Config) { c.MCPHTTPTLSCert = "c.pem" }, "set together"},
		{"remote without token", func(c *Config) { c.MCPHTTPBind = "0.0.0.0"; c.MCPHTTPTLSCert, c.MCPHTTPTLSKey = "c", "k" }, "bearer token is required"},
		{"remote without TLS", func(c *Config) { c.MCPHTTPBind = "0.0.0.0"; c.MCPHTTPToken = "t" }, "TLS is required"},
		{"remote with TLS and token", func(c *Config) {
			c.MCPHTTPBind = "0.0.0.0"
			c.MCPHTTPToken = "t"
			c.MCPHTTPTLSCert, c.MCPHTTPTLSKey = "c", "k"
		}, ""},
		{"remote insecure opt-in still needs token", func(c *Config) { c.MCPHTTPBind = "0.0.0.0"; c.MCPHTTPAllowInsecure = true }, "bearer token is required"},
		{"remote insecure opt-in with token", func(c *Config) {
			c.MCPHTTPBind = "0.0.0.0"
			c.MCPHTTPAllowInsecure = true
			c.MCPHTTPToken = "t"
		}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(c)
			err := ValidateTransport(c)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadRejectsRemoteBindWithoutToken(t *testing.T) {
	clearEnv()
	defer clearEnv()
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
	os.Setenv("MYSQL_MCP_TRANSPORT", "http")
	os.Setenv("MYSQL_MCP_HTTP_BIND", "0.0.0.0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "bearer token is required") {
		t.Fatalf("expected token error, got %v", err)
	}
}

func TestFileConfigMCPHTTP(t *testing.T) {
	clearEnv()
	defer clearEnv()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `connections:
  default:
    dsn: "user:pass@tcp(localhost:3306)/db"
transport: http
http:
  port: 9400
  mcp:
    bind: 127.0.0.1
    path: /mcp-x
    stateless: true
    session_timeout_minutes: 7
    allowed_origins: ["https://app.example"]
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	old := ConfigFilePath
	ConfigFilePath = path
	defer func() { ConfigFilePath = old }()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Transport != TransportHTTP || cfg.HTTPPort != 9400 || cfg.MCPHTTPPath != "/mcp-x" || !cfg.MCPHTTPStateless {
		t.Errorf("unexpected config: transport=%q port=%d path=%q stateless=%v", cfg.Transport, cfg.HTTPPort, cfg.MCPHTTPPath, cfg.MCPHTTPStateless)
	}
	if cfg.MCPHTTPSessionTimeout != 7*time.Minute || len(cfg.MCPHTTPAllowedOrigins) != 1 {
		t.Errorf("unexpected timeout/origins: %v %v", cfg.MCPHTTPSessionTimeout, cfg.MCPHTTPAllowedOrigins)
	}
	printed := PrintConfig(cfg)
	if !strings.Contains(printed, "transport: http") || !strings.Contains(printed, "path: /mcp-x") {
		t.Errorf("PrintConfig missing MCP HTTP settings:\n%s", printed)
	}
}
