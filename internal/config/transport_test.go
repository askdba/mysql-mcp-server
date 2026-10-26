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

func TestValidateTransportBindAndPath(t *testing.T) {
	tests := []struct {
		bind, path, wantErr string
	}{
		{"[::1]", "/mcp", ""},
		{"::1", "/v1/mcp", ""},
		{"0.0.0.0:9306", "/mcp", "without a port"},
		{"127.0.0.1:9306", "/mcp", "without a port"},
		{"127.0.0.1", "/health", "reserved"},
		{"127.0.0.1", "/api/metrics/tokens", "reserved"},
		{"127.0.0.1", "/mcp/", "clean literal path"},
		{"127.0.0.1", "/{x}", "clean literal path"},
		{"127.0.0.1", "/a/../mcp", "clean literal path"},
	}
	for _, tc := range tests {
		c := &Config{Transport: TransportHTTP, MCPHTTPBind: tc.bind, MCPHTTPPath: tc.path}
		applyMCPHTTPDefaults(c)
		err := ValidateTransport(c)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("bind=%q path=%q: unexpected error %v", tc.bind, tc.path, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("bind=%q path=%q: error %v, want containing %q", tc.bind, tc.path, err, tc.wantErr)
		}
	}
	c := &Config{MCPHTTPBind: "[::1]"}
	applyMCPHTTPDefaults(c)
	if c.MCPHTTPBind != "::1" {
		t.Errorf("bracketed bind normalized to %q, want ::1", c.MCPHTTPBind)
	}
}

func TestValidateConfigFileTransport(t *testing.T) {
	clearEnv()
	defer clearEnv()
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "c.yaml")
		if err := os.WriteFile(p, []byte("connections:\n  default:\n    dsn: \"u:p@tcp(h:3306)/d\"\n"+body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("t"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ValidateConfigFile(write("transport: htp\n")); err == nil || !strings.Contains(err.Error(), "invalid MYSQL_MCP_TRANSPORT") {
		t.Errorf("typo transport: %v", err)
	}
	if err := ValidateConfigFile(write("transport: http\nhttp:\n  mcp:\n    bind: 0.0.0.0\n")); err == nil || !strings.Contains(err.Error(), "bearer token is required") {
		t.Errorf("remote bind without token: %v", err)
	}
	if err := ValidateConfigFile(write("transport: http\nhttp:\n  mcp:\n    tls_cert: c.pem\n")); err == nil || !strings.Contains(err.Error(), "set together") {
		t.Errorf("cert without key: %v", err)
	}
	ok := "transport: http\nhttp:\n  mcp:\n    bind: 0.0.0.0\n    token_file: " + tokenFile + "\n    allow_insecure: true\n    allowed_hosts: [mcp.example.com]\n"
	if err := ValidateConfigFile(write(ok)); err != nil {
		t.Errorf("valid remote config rejected: %v", err)
	}
	// A token supplied by the environment at runtime satisfies the check.
	remote := "transport: http\nhttp:\n  mcp:\n    bind: 0.0.0.0\n    allow_insecure: true\n"
	os.Setenv("MYSQL_MCP_HTTP_TOKEN_FILE", tokenFile)
	if err := ValidateConfigFile(write(remote)); err != nil {
		t.Errorf("env token file not honored: %v", err)
	}
	os.Unsetenv("MYSQL_MCP_HTTP_TOKEN_FILE")
	os.Setenv("MYSQL_MCP_HTTP_TOKEN", "env-token")
	if err := ValidateConfigFile(write(remote)); err != nil {
		t.Errorf("env token not honored: %v", err)
	}
}

func TestValidateTransportAllowedHostsRequireToken(t *testing.T) {
	c := &Config{Transport: TransportHTTP, MCPHTTPAllowedHosts: []string{"mcp.example.com"}}
	applyMCPHTTPDefaults(c)
	if err := ValidateTransport(c); err == nil || !strings.Contains(err.Error(), "reverse proxy exposes the server") {
		t.Fatalf("allowed hosts without token: %v", err)
	}
	c.MCPHTTPToken = "t"
	if err := ValidateTransport(c); err != nil {
		t.Fatalf("allowed hosts with token: %v", err)
	}
}

func TestTokenFileIgnoredForStdio(t *testing.T) {
	clearEnv()
	defer clearEnv()
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
	os.Setenv("MYSQL_MCP_HTTP_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := Load(); err != nil {
		t.Fatalf("stdio must not read the MCP HTTP token file: %v", err)
	}
	os.Setenv("MYSQL_MCP_TRANSPORT", "http")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "token file") {
		t.Fatalf("http transport must read the token file: %v", err)
	}
}

func TestAllowedOriginsNormalized(t *testing.T) {
	c := &Config{MCPHTTPAllowedOrigins: []string{" HTTPS://App.Example/ ", "https://app.example:443", "http://local.test:80", "https://app.example:8443", "http://[::1]:80",
		"https://[0:0:0:0:0:0:0:1]", "https://[2001:DB8:0:0:0:0:0:1]:8443"}}
	applyMCPHTTPDefaults(c)
	want := []string{"https://app.example", "https://app.example", "http://local.test", "https://app.example:8443", "http://[::1]",
		"https://[::1]", "https://[2001:db8::1]:8443"}
	for i, w := range want {
		if c.MCPHTTPAllowedOrigins[i] != w {
			t.Errorf("origin %d normalized to %q, want %q", i, c.MCPHTTPAllowedOrigins[i], w)
		}
	}
}

func TestReservedConnectionPrefixRejected(t *testing.T) {
	clearEnv()
	defer clearEnv()
	os.Setenv("MYSQL_CONNECTIONS", `[{"name": "mcp-session/prod", "dsn": "user:pass@tcp(prod:3306)/db"}]`)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "reserved for MCP session connections") {
		t.Fatalf("env connection with reserved prefix: %v", err)
	}

	clearEnv()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte("connections:\n  mcp-session/prod:\n    dsn: \"u:p@tcp(h:3306)/d\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfigFile(p); err == nil || !strings.Contains(err.Error(), "reserved for MCP session connections") {
		t.Fatalf("--validate-config with reserved prefix: %v", err)
	}
}

func TestEnvMCPHTTPValuesAreNormalized(t *testing.T) {
	clearEnv()
	defer clearEnv()
	os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
	os.Setenv("MYSQL_MCP_TRANSPORT", "http")
	os.Setenv("MYSQL_MCP_HTTP_BIND", "[::1]")
	os.Setenv("MYSQL_MCP_HTTP_ALLOWED_ORIGINS", "https://App.Example/, http://localhost:3000")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCPHTTPBind != "::1" {
		t.Errorf("bind from env = %q, want ::1", cfg.MCPHTTPBind)
	}
	if got := strings.Join(cfg.MCPHTTPAllowedOrigins, ","); got != "https://app.example,http://localhost:3000" {
		t.Errorf("origins from env = %q", got)
	}
}

func TestInvalidOriginsRejectedAtLoad(t *testing.T) {
	for _, o := range []string{"app.example.com", "https://app.example.com/ui", "ftp://app.example.com", "https://app.example.com?x=1",
		"https://app.example.com?", "https://app.example.com#", "https://app.example.com:99999", "https://app.example.com:0", "https://:443", "https://app.example.com:", "https://app.example.com//", "https://app.example.com:443:443", "https://[::ffff:127.0.0.1]", "http://[::ffff:7f00:1]:3000"} {
		clearEnv()
		os.Setenv("MYSQL_DSN", "user:pass@tcp(localhost:3306)/testdb")
		os.Setenv("MYSQL_MCP_TRANSPORT", "http")
		os.Setenv("MYSQL_MCP_HTTP_ALLOWED_ORIGINS", o)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "is not an origin") {
			t.Errorf("origin %q: err = %v, want 'is not an origin'", o, err)
		}
	}
	clearEnv()
}

func TestValidateConfigFileMatchesStartup(t *testing.T) {
	clearEnv()
	defer clearEnv()
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("t"), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(body string) string {
		p := filepath.Join(dir, "c.yaml")
		if err := os.WriteFile(p, []byte("connections:\n  default:\n    dsn: \"u:p@tcp(h:3306)/d\"\n"+body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// ALLOW_INSECURE from the environment is honored, as at startup.
	os.Setenv("MYSQL_MCP_HTTP_ALLOW_INSECURE", "1")
	if err := ValidateConfigFile(write("transport: http\nhttp:\n  mcp:\n    bind: 0.0.0.0\n    token_file: " + tokenFile + "\n")); err != nil {
		t.Errorf("file + env ALLOW_INSECURE should validate like startup: %v", err)
	}
	os.Unsetenv("MYSQL_MCP_HTTP_ALLOW_INSECURE")

	// A bad origin in the file is reported by --validate-config, not only at startup.
	if err := ValidateConfigFile(write("transport: http\nhttp:\n  mcp:\n    allowed_origins: [\"app.example.com\"]\n")); err == nil || !strings.Contains(err.Error(), "is not an origin") {
		t.Errorf("bad origin in file: %v", err)
	}

	// Transport from the environment applies too.
	os.Setenv("MYSQL_MCP_TRANSPORT", "http")
	os.Setenv("MYSQL_MCP_HTTP_BIND", "0.0.0.0")
	if err := ValidateConfigFile(write("")); err == nil || !strings.Contains(err.Error(), "bearer token is required") {
		t.Errorf("env transport + remote bind without token: %v", err)
	}
}

func TestValidateOriginAcceptsOrigins(t *testing.T) {
	for _, o := range []string{"https://app.example.com", "http://localhost:3000", "https://app.example.com:443", "https://app.example.com/", "http://[::1]:8080"} {
		if err := validateOrigin(o); err != nil {
			t.Errorf("%q rejected: %v", o, err)
		}
	}
}
