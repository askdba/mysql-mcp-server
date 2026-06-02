// internal/config/config.go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Default values for configuration.
const (
	DefaultMaxRows             = 200
	DefaultQueryTimeoutSecs    = 30
	DefaultMaxOpenConns        = 10
	DefaultMaxIdleConns        = 5
	DefaultConnMaxLifetimeMins = 30
	DefaultConnMaxIdleTimeMins = 5
	DefaultPingTimeoutSecs     = 5
	DefaultHTTPPort            = 9306
	DefaultHTTPRequestTimeoutS = 60
	DefaultRateLimitRPS        = 100 // requests per second
	DefaultRateLimitBurst      = 200 // burst size
)

// SSHConfig holds SSH bastion settings for tunneling (optional).
type SSHConfig struct {
	Host    string `json:"ssh_host,omitempty"`
	User    string `json:"ssh_user,omitempty"`
	KeyPath string `json:"ssh_key_path,omitempty"`
	Port    int    `json:"ssh_port,omitempty"` // 0 = default 22

	// StrictHostKeyChecking verifies the bastion host key (default when nil: strict).
	// Set to false only to disable verification (insecure, MITM risk).
	StrictHostKeyChecking *bool `json:"ssh_strict_host_key_checking,omitempty"`

	KnownHostsPath     string `json:"ssh_known_hosts,omitempty"`
	HostKeyFingerprint string `json:"ssh_host_key_fingerprint,omitempty"`
}

// ConnectionConfig represents a single MySQL connection configuration.
type ConnectionConfig struct {
	Name        string     `json:"name"`
	DSN         string     `json:"dsn"`
	Description string     `json:"description,omitempty"`
	ReadOnly    bool       `json:"read_only,omitempty"`
	SSL         string     `json:"ssl,omitempty"` // "true", "false", "skip-verify", or empty (use DSN as-is)
	SSH         *SSHConfig `json:"ssh,omitempty"` // optional SSH tunnel (bastion)
}

// Config holds all configuration for the MySQL MCP server.
type Config struct {
	// Connection settings
	Connections []ConnectionConfig

	// Query limits
	MaxRows      int
	QueryTimeout time.Duration

	// Connection pool settings
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	PingTimeout     time.Duration

	// Feature flags
	ExtendedMode bool
	VectorMode   bool
	HTTPMode     bool
	MetricsHTTP  bool // Serve /status + /api/metrics/tokens on HTTP while MCP uses stdio (Claude Desktop)
	JSONLogging  bool
	TokenCard    bool // Enable live monitoring UI at /status

	// Token estimation (optional, disabled by default)
	TokenTracking bool
	TokenModel    string

	// HTTP settings
	HTTPPort           int
	HTTPRequestTimeout time.Duration

	// Rate limiting (HTTP mode only)
	RateLimitEnabled bool
	RateLimitRPS     float64 // requests per second
	RateLimitBurst   int     // burst size

	// Audit logging
	AuditLogPath string

	// Transient DB error retries (MCP tools / shared pool)
	DBRetryMaxRetries  int
	DBRetryMaxInterval time.Duration

	// Masking
	MaskColumns []string

	// Security / access (optional)
	AllowedDatabases []string // Empty = all databases allowed (subject to MySQL grants)
	StrictReadOnly   bool     // SET transaction_read_only=ON on each driver connection (DSN param)
	ProcessAdmin     bool     // Enable process_list and kill_query (extended tools)
	ReadAuditTool    bool     // Enable read_audit_log when AuditLogPath is set (extended)
	SlowQueryTool    bool     // Enable slow_query_log tool (extended)
}

// Load reads configuration from config file (if present) and environment variables.
// Priority: Environment variables > Config file > Defaults
func Load() (*Config, error) {
	var cfg *Config

	// Try to load config file first
	if configPath := FindConfigFile(); configPath != "" {
		fileCfg, err := LoadConfigFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config file %s: %w", configPath, err)
		}
		cfg = fileCfg.ToConfig()
	} else {
		// No config file, start with defaults
		cfg = &Config{
			MaxRows:            DefaultMaxRows,
			QueryTimeout:       time.Duration(DefaultQueryTimeoutSecs) * time.Second,
			MaxOpenConns:       DefaultMaxOpenConns,
			MaxIdleConns:       DefaultMaxIdleConns,
			ConnMaxLifetime:    time.Duration(DefaultConnMaxLifetimeMins) * time.Minute,
			ConnMaxIdleTime:    time.Duration(DefaultConnMaxIdleTimeMins) * time.Minute,
			PingTimeout:        time.Duration(DefaultPingTimeoutSecs) * time.Second,
			HTTPPort:           DefaultHTTPPort,
			HTTPRequestTimeout: time.Duration(DefaultHTTPRequestTimeoutS) * time.Second,
			RateLimitRPS:       float64(DefaultRateLimitRPS),
			RateLimitBurst:     DefaultRateLimitBurst,
			TokenModel:         "cl100k_base",
			DBRetryMaxRetries:  3,
			DBRetryMaxInterval: 10 * time.Second,
		}
	}

	// Apply environment variable overrides (env vars take precedence)
	applyEnvOverrides(cfg)

	// Load connections from environment (if any defined, they override file config)
	envConns, err := loadConnections()
	if err != nil {
		return nil, err
	}
	if len(envConns) > 0 {
		cfg.Connections = envConns
	}

	// Ensure we have at least one connection
	if len(cfg.Connections) == 0 {
		return nil, fmt.Errorf("no MySQL connections configured. Set MYSQL_DSN, MYSQL_CONNECTIONS, or use a config file")
	}

	return cfg, nil
}

// applyEnvOverrides applies environment variable overrides to the config.
// Only overrides values if the environment variable is explicitly set.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("MYSQL_MAX_ROWS"); v != "" {
		cfg.MaxRows = getEnvInt("MYSQL_MAX_ROWS", cfg.MaxRows)
	}
	// MYSQL_QUERY_TIMEOUT_SECONDS takes precedence over MYSQL_QUERY_TIMEOUT (ms).
	if v := os.Getenv("MYSQL_QUERY_TIMEOUT_SECONDS"); v != "" {
		cfg.QueryTimeout = time.Duration(getEnvInt("MYSQL_QUERY_TIMEOUT_SECONDS", int(cfg.QueryTimeout.Seconds()))) * time.Second
	} else if v := os.Getenv("MYSQL_QUERY_TIMEOUT"); v != "" {
		// MYSQL_QUERY_TIMEOUT accepts a value in milliseconds (e.g. 30000 for 30 s).
		cfg.QueryTimeout = time.Duration(getEnvInt("MYSQL_QUERY_TIMEOUT", int(cfg.QueryTimeout.Milliseconds()))) * time.Millisecond
	}
	// MYSQL_MAX_OPEN_CONNS takes precedence over MYSQL_POOL_SIZE.
	if v := os.Getenv("MYSQL_MAX_OPEN_CONNS"); v != "" {
		cfg.MaxOpenConns = getEnvInt("MYSQL_MAX_OPEN_CONNS", cfg.MaxOpenConns)
	} else if v := os.Getenv("MYSQL_POOL_SIZE"); v != "" {
		// MYSQL_POOL_SIZE is an alias for MYSQL_MAX_OPEN_CONNS.
		cfg.MaxOpenConns = getEnvInt("MYSQL_POOL_SIZE", cfg.MaxOpenConns)
	}
	if v := os.Getenv("MYSQL_MAX_IDLE_CONNS"); v != "" {
		cfg.MaxIdleConns = getEnvInt("MYSQL_MAX_IDLE_CONNS", cfg.MaxIdleConns)
	}
	if v := os.Getenv("MYSQL_CONN_MAX_LIFETIME_MINUTES"); v != "" {
		cfg.ConnMaxLifetime = time.Duration(getEnvInt("MYSQL_CONN_MAX_LIFETIME_MINUTES", int(cfg.ConnMaxLifetime.Minutes()))) * time.Minute
	}
	if v := os.Getenv("MYSQL_CONN_MAX_IDLE_TIME_MINUTES"); v != "" {
		cfg.ConnMaxIdleTime = time.Duration(getEnvInt("MYSQL_CONN_MAX_IDLE_TIME_MINUTES", int(cfg.ConnMaxIdleTime.Minutes()))) * time.Minute
	}
	if v := os.Getenv("MYSQL_PING_TIMEOUT_SECONDS"); v != "" {
		cfg.PingTimeout = time.Duration(getEnvInt("MYSQL_PING_TIMEOUT_SECONDS", int(cfg.PingTimeout.Seconds()))) * time.Second
	}
	if v := strings.TrimSpace(os.Getenv("MYSQL_MCP_DB_RETRY_MAX")); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n >= 0 && n <= 20 {
			cfg.DBRetryMaxRetries = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("MYSQL_MCP_DB_RETRY_MAX_INTERVAL_MS")); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			cfg.DBRetryMaxInterval = time.Duration(n) * time.Millisecond
		}
	}
	if v := os.Getenv("MYSQL_MCP_EXTENDED"); v != "" {
		cfg.ExtendedMode = getEnvBool("MYSQL_MCP_EXTENDED")
	}
	if v := os.Getenv("MYSQL_MCP_VECTOR"); v != "" {
		cfg.VectorMode = getEnvBool("MYSQL_MCP_VECTOR")
	}
	if v := os.Getenv("MYSQL_MCP_HTTP"); v != "" {
		cfg.HTTPMode = getEnvBool("MYSQL_MCP_HTTP")
	}
	if v := os.Getenv("MYSQL_MCP_METRICS_HTTP"); v != "" {
		cfg.MetricsHTTP = getEnvBool("MYSQL_MCP_METRICS_HTTP")
	}
	if v := os.Getenv("MYSQL_MCP_JSON_LOGS"); v != "" {
		cfg.JSONLogging = getEnvBool("MYSQL_MCP_JSON_LOGS")
	}
	if v := os.Getenv("MYSQL_MCP_TOKEN_TRACKING"); v != "" {
		cfg.TokenTracking = getEnvBool("MYSQL_MCP_TOKEN_TRACKING")
	}
	if v := os.Getenv("MYSQL_MCP_TOKEN_MODEL"); v != "" {
		cfg.TokenModel = strings.TrimSpace(v)
	}
	if v := os.Getenv("MYSQL_MCP_TOKEN_CARD"); v != "" {
		cfg.TokenCard = getEnvBool("MYSQL_MCP_TOKEN_CARD")
	}
	// When HTTP is enabled via MYSQL_MCP_HTTP, serve /status by default (e.g. brew, launchd). Set MYSQL_MCP_TOKEN_CARD=0 to disable.
	if cfg.HTTPMode && os.Getenv("MYSQL_MCP_TOKEN_CARD") == "" && strings.TrimSpace(os.Getenv("MYSQL_MCP_HTTP")) != "" {
		cfg.TokenCard = true
	}
	// Metrics-only HTTP alongside stdio MCP: /status by default when MYSQL_MCP_METRICS_HTTP=1.
	if cfg.MetricsHTTP && !cfg.HTTPMode && os.Getenv("MYSQL_MCP_TOKEN_CARD") == "" {
		cfg.TokenCard = true
	}
	if v := os.Getenv("MYSQL_HTTP_PORT"); v != "" {
		cfg.HTTPPort = getEnvInt("MYSQL_HTTP_PORT", cfg.HTTPPort)
	}
	if v := os.Getenv("MYSQL_HTTP_REQUEST_TIMEOUT_SECONDS"); v != "" {
		cfg.HTTPRequestTimeout = time.Duration(getEnvInt("MYSQL_HTTP_REQUEST_TIMEOUT_SECONDS", int(cfg.HTTPRequestTimeout.Seconds()))) * time.Second
	}
	if v := os.Getenv("MYSQL_HTTP_RATE_LIMIT"); v != "" {
		cfg.RateLimitEnabled = getEnvBool("MYSQL_HTTP_RATE_LIMIT")
	}
	if v := os.Getenv("MYSQL_HTTP_RATE_LIMIT_RPS"); v != "" {
		cfg.RateLimitRPS = float64(getEnvInt("MYSQL_HTTP_RATE_LIMIT_RPS", int(cfg.RateLimitRPS)))
	}
	if v := os.Getenv("MYSQL_HTTP_RATE_LIMIT_BURST"); v != "" {
		cfg.RateLimitBurst = getEnvInt("MYSQL_HTTP_RATE_LIMIT_BURST", cfg.RateLimitBurst)
	}
	if v := os.Getenv("MYSQL_MCP_AUDIT_LOG"); v != "" {
		cfg.AuditLogPath = strings.TrimSpace(v)
	}
	if v := os.Getenv("MYSQL_MCP_MASK_COLUMNS"); v != "" {
		cfg.MaskColumns = parseCSVList(v)
	}
	if cfg.HTTPMode {
		cfg.MetricsHTTP = false // full REST API replaces metrics-only sidecar
	}
	if v := os.Getenv("MYSQL_MCP_ALLOWED_DATABASES"); v != "" {
		cfg.AllowedDatabases = parseCSVList(v)
	}
	if v := os.Getenv("MYSQL_MCP_STRICT_READ_ONLY"); v != "" {
		cfg.StrictReadOnly = getEnvBool("MYSQL_MCP_STRICT_READ_ONLY")
	}
	if v := os.Getenv("MYSQL_MCP_PROCESS_ADMIN"); v != "" {
		cfg.ProcessAdmin = getEnvBool("MYSQL_MCP_PROCESS_ADMIN")
	}
	if v := os.Getenv("MYSQL_MCP_READ_AUDIT_TOOL"); v != "" {
		cfg.ReadAuditTool = getEnvBool("MYSQL_MCP_READ_AUDIT_TOOL")
	}
	if v := os.Getenv("MYSQL_MCP_SLOW_QUERY_TOOL"); v != "" {
		cfg.SlowQueryTool = getEnvBool("MYSQL_MCP_SLOW_QUERY_TOOL")
	}
}

// parseCSVList splits comma-separated values, trims space, drops empties.
func parseCSVList(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// EffectiveStrictSSHHostKeyChecking returns whether SSH host keys must be verified.
// Nil StrictHostKeyChecking means strict (verify).
func EffectiveStrictSSHHostKeyChecking(s *SSHConfig) bool {
	if s == nil {
		return true
	}
	if s.StrictHostKeyChecking == nil {
		return true
	}
	return *s.StrictHostKeyChecking
}

// AllowedDatabaseSet builds a case-insensitive lookup set for schema names.
func AllowedDatabaseSet(list []string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s != "" {
			m[strings.ToLower(s)] = struct{}{}
		}
	}
	return m
}

// loadConnections loads DSN configurations from environment variables.
func loadConnections() ([]ConnectionConfig, error) {
	var configs []ConnectionConfig

	// Global SSL setting from MYSQL_SSL (applies to all connections without explicit SSL)
	globalSSL := os.Getenv("MYSQL_SSL")

	// Global SSH (applies to env-based connections when set)
	globalSSH := loadGlobalSSHFromEnv()

	// Check for JSON-based configuration first
	if jsonConfig := os.Getenv("MYSQL_CONNECTIONS"); jsonConfig != "" {
		if err := json.Unmarshal([]byte(jsonConfig), &configs); err != nil {
			return nil, fmt.Errorf("failed to parse MYSQL_CONNECTIONS: %w", err)
		}
		// Apply global SSL and SSH to connections that don't have their own
		for i := range configs {
			if configs[i].SSL == "" && globalSSL != "" {
				configs[i].SSL = globalSSL
			}
			if configs[i].SSH == nil && globalSSH != nil {
				configs[i].SSH = globalSSH
			}
		}
		return configs, nil
	}

	// Fall back to numbered DSN environment variables
	// MYSQL_DSN (default), MYSQL_DSN_1, MYSQL_DSN_2, etc.

	if dsn := os.Getenv("MYSQL_DSN"); dsn != "" {
		c := ConnectionConfig{
			Name:        "default",
			DSN:         dsn,
			Description: "Default connection",
			SSL:         globalSSL,
		}
		if globalSSH != nil {
			c.SSH = globalSSH
		}
		configs = append(configs, c)
	}

	for i := 1; i <= 10; i++ {
		dsnKey := fmt.Sprintf("MYSQL_DSN_%d", i)
		nameKey := fmt.Sprintf("MYSQL_DSN_%d_NAME", i)
		descKey := fmt.Sprintf("MYSQL_DSN_%d_DESC", i)
		sslKey := fmt.Sprintf("MYSQL_DSN_%d_SSL", i)

		dsn := os.Getenv(dsnKey)
		if dsn == "" {
			continue
		}

		name := os.Getenv(nameKey)
		if name == "" {
			name = fmt.Sprintf("connection_%d", i)
		}

		// Per-connection SSL overrides global
		ssl := os.Getenv(sslKey)
		if ssl == "" {
			ssl = globalSSL
		}

		c := ConnectionConfig{
			Name:        name,
			DSN:         dsn,
			Description: os.Getenv(descKey),
			SSL:         ssl,
		}
		if globalSSH != nil {
			c.SSH = globalSSH
		}
		configs = append(configs, c)
	}

	return configs, nil
}

// loadGlobalSSHFromEnv loads SSH tunnel settings from MYSQL_SSH_* env vars.
// Returns nil if SSH is not configured.
func loadGlobalSSHFromEnv() *SSHConfig {
	host := strings.TrimSpace(os.Getenv("MYSQL_SSH_HOST"))
	user := strings.TrimSpace(os.Getenv("MYSQL_SSH_USER"))
	keyPath := strings.TrimSpace(os.Getenv("MYSQL_SSH_KEY_PATH"))
	if host == "" || user == "" || keyPath == "" {
		return nil
	}
	port := 0
	if v := os.Getenv("MYSQL_SSH_PORT"); v != "" {
		if p, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && p > 0 {
			port = p
		}
	}
	out := &SSHConfig{
		Host:    host,
		User:    user,
		KeyPath: keyPath,
		Port:    port,
	}
	if kh := strings.TrimSpace(os.Getenv("MYSQL_SSH_KNOWN_HOSTS")); kh != "" {
		out.KnownHostsPath = kh
	}
	if fp := strings.TrimSpace(os.Getenv("MYSQL_SSH_HOST_KEY_FINGERPRINT")); fp != "" {
		out.HostKeyFingerprint = fp
	}
	out.StrictHostKeyChecking = parseEnvStrictSSHHostKeyChecking()
	return out
}

// parseEnvStrictSSHHostKeyChecking returns nil if MYSQL_SSH_STRICT_HOST_KEY_CHECKING is unset (default strict).
func parseEnvStrictSSHHostKeyChecking() *bool {
	raw := strings.TrimSpace(os.Getenv("MYSQL_SSH_STRICT_HOST_KEY_CHECKING"))
	if raw == "" {
		return nil
	}
	v := strings.ToLower(raw)
	insecure := v == "0" || v == "false" || v == "no" || v == "off"
	strict := !insecure
	return &strict
}

// getEnvInt reads an integer from an environment variable with a default value.
func getEnvInt(key string, def int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// getEnvBool reads a boolean from an environment variable.
// True: 1, true, yes, on, y (case-insensitive, trimmed). False: 0, false, no, off, empty, or unknown.
func getEnvBool(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on", "y":
		return true
	default:
		return false
	}
}

// GetEnvInt is exported for use by other packages.
func GetEnvInt(key string, def int) int {
	return getEnvInt(key, def)
}

// GetEnvBool is exported for use by other packages.
func GetEnvBool(key string) bool {
	return getEnvBool(key)
}
