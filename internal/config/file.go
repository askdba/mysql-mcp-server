// internal/config/file.go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileConfig represents the structure of a configuration file.
// This mirrors the Config struct but with file-friendly field names.
type FileConfig struct {
	// Database connections
	Connections map[string]FileConnectionConfig `yaml:"connections" json:"connections"`

	// Query settings
	Query FileQueryConfig `yaml:"query" json:"query"`

	// Connection pool settings
	Pool FilePoolConfig `yaml:"pool" json:"pool"`

	// Feature flags
	Features FileFeatureConfig `yaml:"features" json:"features"`

	// Logging settings
	Logging FileLoggingConfig `yaml:"logging" json:"logging"`

	// HTTP/REST API settings
	HTTP FileHTTPConfig `yaml:"http" json:"http"`
}

// FileConnectionConfig represents a connection in the config file.
type FileConnectionConfig struct {
	DSN         string `yaml:"dsn" json:"dsn"`
	Description string `yaml:"description" json:"description"`
	ReadOnly    bool   `yaml:"read_only" json:"read_only"`
	SSL         string `yaml:"ssl" json:"ssl"` // "true", "false", "skip-verify", or empty
}

// FileQueryConfig represents query settings in the config file.
type FileQueryConfig struct {
	MaxRows        int `yaml:"max_rows" json:"max_rows"`
	TimeoutSeconds int `yaml:"timeout_seconds" json:"timeout_seconds"`
}

// FilePoolConfig represents connection pool settings in the config file.
type FilePoolConfig struct {
	MaxOpenConns           int `yaml:"max_open_conns" json:"max_open_conns"`
	MaxIdleConns           int `yaml:"max_idle_conns" json:"max_idle_conns"`
	ConnMaxLifetimeMinutes int `yaml:"conn_max_lifetime_minutes" json:"conn_max_lifetime_minutes"`
	ConnMaxIdleTimeMinutes int `yaml:"conn_max_idle_time_minutes" json:"conn_max_idle_time_minutes"`
	PingTimeoutSeconds     int `yaml:"ping_timeout_seconds" json:"ping_timeout_seconds"`
}

// FileFeatureConfig represents feature flags in the config file.
type FileFeatureConfig struct {
	ExtendedTools bool `yaml:"extended_tools" json:"extended_tools"`
	VectorTools   bool `yaml:"vector_tools" json:"vector_tools"`
}

// FileLoggingConfig represents logging settings in the config file.
type FileLoggingConfig struct {
	JSONFormat    bool   `yaml:"json_format" json:"json_format"`
	AuditLogPath  string `yaml:"audit_log_path" json:"audit_log_path"`
	TokenTracking bool   `yaml:"token_tracking" json:"token_tracking"`
	TokenModel    string `yaml:"token_model" json:"token_model"`
}

// FileHTTPConfig represents HTTP settings in the config file.
type FileHTTPConfig struct {
	Enabled               bool                `yaml:"enabled" json:"enabled"`
	Port                  int                 `yaml:"port" json:"port"`
	RequestTimeoutSeconds int                 `yaml:"request_timeout_seconds" json:"request_timeout_seconds"`
	RateLimit             FileRateLimitConfig `yaml:"rate_limit" json:"rate_limit"`
}

// FileRateLimitConfig represents rate limiting settings in the config file.
type FileRateLimitConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	RPS     int  `yaml:"rps" json:"rps"`
	Burst   int  `yaml:"burst" json:"burst"`
}

// ConfigFilePath holds the path to the config file (set by command line flag).
var ConfigFilePath string

// FindConfigFile searches for a config file in standard locations.
// Returns the path to the first config file found, or empty string if none found.
func FindConfigFile() string {
	// 1. Command line flag (--config)
	if ConfigFilePath != "" {
		return ConfigFilePath
	}

	// 2. Environment variable
	if envPath := os.Getenv("MYSQL_MCP_CONFIG"); envPath != "" {
		return envPath
	}

	// 3. Current directory
	candidates := []string{
		"mysql-mcp-server.yaml",
		"mysql-mcp-server.yml",
		"mysql-mcp-server.json",
	}
	for _, name := range candidates {
		if _, err := os.Stat(name); err == nil {
			return name
		}
	}

	// 4. User config directory
	if homeDir, err := os.UserHomeDir(); err == nil {
		userConfigPaths := []string{
			filepath.Join(homeDir, ".config", "mysql-mcp-server", "config.yaml"),
			filepath.Join(homeDir, ".config", "mysql-mcp-server", "config.yml"),
			filepath.Join(homeDir, ".config", "mysql-mcp-server", "config.json"),
		}
		for _, path := range userConfigPaths {
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}

	// 5. System config directory
	systemConfigPaths := []string{
		"/etc/mysql-mcp-server/config.yaml",
		"/etc/mysql-mcp-server/config.yml",
		"/etc/mysql-mcp-server/config.json",
	}
	for _, path := range systemConfigPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// LoadConfigFile loads configuration from a file (YAML or JSON).
func LoadConfigFile(path string) (*FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg FileConfig

	// Determine format by extension
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	default:
		// Try YAML first, then JSON
		// Use separate variables to prevent state contamination if YAML
		// partially populates the struct before failing
		var yamlCfg FileConfig
		if err := yaml.Unmarshal(data, &yamlCfg); err != nil {
			var jsonCfg FileConfig
			if err := json.Unmarshal(data, &jsonCfg); err != nil {
				return nil, fmt.Errorf("failed to parse config file (tried YAML and JSON): %w", err)
			}
			cfg = jsonCfg
		} else {
			cfg = yamlCfg
		}
	}

	return &cfg, nil
}

// ValidateConfigFile validates a config file without loading it into the server.
func ValidateConfigFile(path string) error {
	cfg, err := LoadConfigFile(path)
	if err != nil {
		return err
	}

	// Validate connections
	if len(cfg.Connections) == 0 {
		return fmt.Errorf("no connections defined in config file")
	}

	for name, conn := range cfg.Connections {
		if conn.DSN == "" {
			return fmt.Errorf("connection '%s' has empty DSN", name)
		}
	}

	return nil
}

// ToConfig converts a FileConfig to the runtime Config struct.
// Values from FileConfig are used as base, can be overridden by env vars.
func (fc *FileConfig) ToConfig() *Config {
	cfg := &Config{
		// Set defaults first (must include all fields to avoid zero-value issues)
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

	// Apply file config values (if set)
	if fc.Query.MaxRows > 0 {
		cfg.MaxRows = fc.Query.MaxRows
	}
	if fc.Query.TimeoutSeconds > 0 {
		cfg.QueryTimeout = secondsToDuration(fc.Query.TimeoutSeconds)
	}

	if fc.Pool.MaxOpenConns > 0 {
		cfg.MaxOpenConns = fc.Pool.MaxOpenConns
	}
	if fc.Pool.MaxIdleConns > 0 {
		cfg.MaxIdleConns = fc.Pool.MaxIdleConns
	}
	if fc.Pool.ConnMaxLifetimeMinutes > 0 {
		cfg.ConnMaxLifetime = minutesToDuration(fc.Pool.ConnMaxLifetimeMinutes)
	}
	if fc.Pool.ConnMaxIdleTimeMinutes > 0 {
		cfg.ConnMaxIdleTime = minutesToDuration(fc.Pool.ConnMaxIdleTimeMinutes)
	}
	if fc.Pool.PingTimeoutSeconds > 0 {
		cfg.PingTimeout = secondsToDuration(fc.Pool.PingTimeoutSeconds)
	}

	cfg.ExtendedMode = fc.Features.ExtendedTools
	cfg.VectorMode = fc.Features.VectorTools

	cfg.JSONLogging = fc.Logging.JSONFormat
	cfg.AuditLogPath = fc.Logging.AuditLogPath
	cfg.TokenTracking = fc.Logging.TokenTracking
	if strings.TrimSpace(fc.Logging.TokenModel) != "" {
		cfg.TokenModel = strings.TrimSpace(fc.Logging.TokenModel)
	}

	cfg.HTTPMode = fc.HTTP.Enabled
	if fc.HTTP.Port > 0 {
		cfg.HTTPPort = fc.HTTP.Port
	}
	if fc.HTTP.RequestTimeoutSeconds > 0 {
		cfg.HTTPRequestTimeout = secondsToDuration(fc.HTTP.RequestTimeoutSeconds)
	}

	cfg.RateLimitEnabled = fc.HTTP.RateLimit.Enabled
	if fc.HTTP.RateLimit.RPS > 0 {
		cfg.RateLimitRPS = float64(fc.HTTP.RateLimit.RPS)
	}
	if fc.HTTP.RateLimit.Burst > 0 {
		cfg.RateLimitBurst = fc.HTTP.RateLimit.Burst
	}

	// Convert connections - sort keys for deterministic ordering
	// "default" connection is placed first if it exists, then alphabetically
	names := make([]string, 0, len(fc.Connections))
	for name := range fc.Connections {
		names = append(names, name)
	}
	sort.Strings(names)

	// Move "default" to front if it exists
	for i, name := range names {
		if name == "default" && i > 0 {
			names = append([]string{"default"}, append(names[:i], names[i+1:]...)...)
			break
		}
	}

	for _, name := range names {
		conn := fc.Connections[name]
		cfg.Connections = append(cfg.Connections, ConnectionConfig{
			Name:        name,
			DSN:         conn.DSN,
			Description: conn.Description,
			ReadOnly:    conn.ReadOnly,
			SSL:         conn.SSL,
		})
	}

	return cfg
}

// PrintConfig outputs the current configuration as YAML.
func PrintConfig(cfg *Config) string {
	fc := &FileConfig{
		Connections: make(map[string]FileConnectionConfig),
		Query: FileQueryConfig{
			MaxRows:        cfg.MaxRows,
			TimeoutSeconds: int(cfg.QueryTimeout.Seconds()),
		},
		Pool: FilePoolConfig{
			MaxOpenConns:           cfg.MaxOpenConns,
			MaxIdleConns:           cfg.MaxIdleConns,
			ConnMaxLifetimeMinutes: int(cfg.ConnMaxLifetime.Minutes()),
			ConnMaxIdleTimeMinutes: int(cfg.ConnMaxIdleTime.Minutes()),
			PingTimeoutSeconds:     int(cfg.PingTimeout.Seconds()),
		},
		Features: FileFeatureConfig{
			ExtendedTools: cfg.ExtendedMode,
			VectorTools:   cfg.VectorMode,
		},
		Logging: FileLoggingConfig{
			JSONFormat:    cfg.JSONLogging,
			AuditLogPath:  cfg.AuditLogPath,
			TokenTracking: cfg.TokenTracking,
			TokenModel:    cfg.TokenModel,
		},
		HTTP: FileHTTPConfig{
			Enabled:               cfg.HTTPMode,
			Port:                  cfg.HTTPPort,
			RequestTimeoutSeconds: int(cfg.HTTPRequestTimeout.Seconds()),
			RateLimit: FileRateLimitConfig{
				Enabled: cfg.RateLimitEnabled,
				RPS:     int(cfg.RateLimitRPS),
				Burst:   cfg.RateLimitBurst,
			},
		},
	}

	for _, conn := range cfg.Connections {
		fc.Connections[conn.Name] = FileConnectionConfig{
			DSN:         maskDSN(conn.DSN),
			Description: conn.Description,
			ReadOnly:    conn.ReadOnly,
			SSL:         conn.SSL,
		}
	}

	data, _ := yaml.Marshal(fc)
	return string(data)
}

// maskDSN masks the password in a DSN for safe printing.
func maskDSN(dsn string) string {
	// Simple masking: replace password with ***
	// DSN format: user:password@tcp(host:port)/db
	// Use LastIndex for @ to handle passwords containing @ characters
	// e.g., user:p@ssword@tcp(host:3306)/db should mask to user:***@tcp(host:3306)/db
	if idx := strings.Index(dsn, ":"); idx > 0 {
		if atIdx := strings.LastIndex(dsn, "@"); atIdx > idx {
			return dsn[:idx+1] + "***" + dsn[atIdx:]
		}
	}
	return dsn
}

// ApplySSLToDSN appends TLS configuration to a DSN based on the SSL setting.
// SSL values:
//   - "true" or "1": Enable TLS with certificate verification (tls=true)
//   - "skip-verify": Enable TLS without certificate verification (tls=skip-verify)
//   - "preferred": Maps to skip-verify (Go MySQL driver doesn't support tls=preferred)
//   - "false", "0", or "": No change to DSN (use DSN as-is)
//
// Note: The go-sql-driver/mysql only supports tls=true, tls=false, tls=skip-verify,
// or a custom TLS config name. The "preferred" option from MySQL client is not supported,
// so we map it to "skip-verify" as the closest equivalent behavior.
//
// If the DSN already contains a tls= parameter, it is not modified.
func ApplySSLToDSN(dsn, ssl string) string {
	ssl = strings.TrimSpace(strings.ToLower(ssl))

	// If SSL is empty, disabled, or DSN already has tls parameter, return as-is
	if ssl == "" || ssl == "false" || ssl == "0" {
		return dsn
	}

	// Check for existing tls= parameter only in the query string (after ?)
	// to avoid false positives from passwords containing "tls="
	if idx := strings.Index(dsn, "?"); idx != -1 {
		queryString := dsn[idx:]
		if strings.Contains(queryString, "tls=") {
			return dsn
		}
	}

	// Determine the tls parameter value
	// Note: go-sql-driver/mysql only supports: true, false, skip-verify, or custom config name
	var tlsValue string
	switch ssl {
	case "true", "1":
		tlsValue = "true"
	case "skip-verify":
		tlsValue = "skip-verify"
	case "preferred":
		// "preferred" is not supported by go-sql-driver/mysql
		// Map to "skip-verify" as the closest equivalent (TLS without cert verification)
		tlsValue = "skip-verify"
	default:
		// Unknown value, treat as true for safety
		tlsValue = "true"
	}

	// Append tls parameter to DSN
	// DSN format: user:pass@tcp(host:port)/db?param=value
	if strings.Contains(dsn, "?") {
		return dsn + "&tls=" + tlsValue
	}
	return dsn + "?tls=" + tlsValue
}

func secondsToDuration(s int) time.Duration {
	return time.Duration(s) * time.Second
}

func minutesToDuration(m int) time.Duration {
	return time.Duration(m) * time.Minute
}
