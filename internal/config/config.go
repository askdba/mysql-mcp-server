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

// ConnectionConfig represents a single MySQL connection configuration.
type ConnectionConfig struct {
	Name        string `json:"name"`
	DSN         string `json:"dsn"`
	Description string `json:"description,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
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
	JSONLogging  bool

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
	if v := os.Getenv("MYSQL_QUERY_TIMEOUT_SECONDS"); v != "" {
		cfg.QueryTimeout = time.Duration(getEnvInt("MYSQL_QUERY_TIMEOUT_SECONDS", int(cfg.QueryTimeout.Seconds()))) * time.Second
	}
	if v := os.Getenv("MYSQL_MAX_OPEN_CONNS"); v != "" {
		cfg.MaxOpenConns = getEnvInt("MYSQL_MAX_OPEN_CONNS", cfg.MaxOpenConns)
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
	if v := os.Getenv("MYSQL_MCP_EXTENDED"); v != "" {
		cfg.ExtendedMode = getEnvBool("MYSQL_MCP_EXTENDED")
	}
	if v := os.Getenv("MYSQL_MCP_VECTOR"); v != "" {
		cfg.VectorMode = getEnvBool("MYSQL_MCP_VECTOR")
	}
	if v := os.Getenv("MYSQL_MCP_HTTP"); v != "" {
		cfg.HTTPMode = getEnvBool("MYSQL_MCP_HTTP")
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
}

// loadConnections loads DSN configurations from environment variables.
func loadConnections() ([]ConnectionConfig, error) {
	var configs []ConnectionConfig

	// Check for JSON-based configuration first
	if jsonConfig := os.Getenv("MYSQL_CONNECTIONS"); jsonConfig != "" {
		if err := json.Unmarshal([]byte(jsonConfig), &configs); err != nil {
			return nil, fmt.Errorf("failed to parse MYSQL_CONNECTIONS: %w", err)
		}
		return configs, nil
	}

	// Fall back to numbered DSN environment variables
	// MYSQL_DSN (default), MYSQL_DSN_1, MYSQL_DSN_2, etc.
	if dsn := os.Getenv("MYSQL_DSN"); dsn != "" {
		configs = append(configs, ConnectionConfig{
			Name:        "default",
			DSN:         dsn,
			Description: "Default connection",
		})
	}

	for i := 1; i <= 10; i++ {
		dsnKey := fmt.Sprintf("MYSQL_DSN_%d", i)
		nameKey := fmt.Sprintf("MYSQL_DSN_%d_NAME", i)
		descKey := fmt.Sprintf("MYSQL_DSN_%d_DESC", i)

		dsn := os.Getenv(dsnKey)
		if dsn == "" {
			continue
		}

		name := os.Getenv(nameKey)
		if name == "" {
			name = fmt.Sprintf("connection_%d", i)
		}

		configs = append(configs, ConnectionConfig{
			Name:        name,
			DSN:         dsn,
			Description: os.Getenv(descKey),
		})
	}

	return configs, nil
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

// getEnvBool reads a boolean from an environment variable (1 = true).
func getEnvBool(key string) bool {
	return os.Getenv(key) == "1"
}

// GetEnvInt is exported for use by other packages.
func GetEnvInt(key string, def int) int {
	return getEnvInt(key, def)
}

// GetEnvBool is exported for use by other packages.
func GetEnvBool(key string) bool {
	return getEnvBool(key)
}
