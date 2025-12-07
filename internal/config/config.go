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

	// Feature flags
	ExtendedMode bool
	VectorMode   bool
	HTTPMode     bool
	JSONLogging  bool

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

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		MaxRows:            getEnvInt("MYSQL_MAX_ROWS", DefaultMaxRows),
		QueryTimeout:       time.Duration(getEnvInt("MYSQL_QUERY_TIMEOUT_SECONDS", DefaultQueryTimeoutSecs)) * time.Second,
		MaxOpenConns:       getEnvInt("MYSQL_MAX_OPEN_CONNS", DefaultMaxOpenConns),
		MaxIdleConns:       getEnvInt("MYSQL_MAX_IDLE_CONNS", DefaultMaxIdleConns),
		ConnMaxLifetime:    time.Duration(getEnvInt("MYSQL_CONN_MAX_LIFETIME_MINUTES", DefaultConnMaxLifetimeMins)) * time.Minute,
		ExtendedMode:       getEnvBool("MYSQL_MCP_EXTENDED"),
		VectorMode:         getEnvBool("MYSQL_MCP_VECTOR"),
		HTTPMode:           getEnvBool("MYSQL_MCP_HTTP"),
		JSONLogging:        getEnvBool("MYSQL_MCP_JSON_LOGS"),
		HTTPPort:           getEnvInt("MYSQL_HTTP_PORT", DefaultHTTPPort),
		HTTPRequestTimeout: time.Duration(getEnvInt("MYSQL_HTTP_REQUEST_TIMEOUT_SECONDS", DefaultHTTPRequestTimeoutS)) * time.Second,
		RateLimitEnabled:   getEnvBool("MYSQL_HTTP_RATE_LIMIT"),
		RateLimitRPS:       float64(getEnvInt("MYSQL_HTTP_RATE_LIMIT_RPS", DefaultRateLimitRPS)),
		RateLimitBurst:     getEnvInt("MYSQL_HTTP_RATE_LIMIT_BURST", DefaultRateLimitBurst),
		AuditLogPath:       strings.TrimSpace(os.Getenv("MYSQL_MCP_AUDIT_LOG")),
	}

	// Load connections
	conns, err := loadConnections()
	if err != nil {
		return nil, err
	}
	if len(conns) == 0 {
		return nil, fmt.Errorf("no MySQL connections configured. Set MYSQL_DSN or MYSQL_CONNECTIONS")
	}
	cfg.Connections = conns

	return cfg, nil
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
