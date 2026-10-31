// internal/config/config.go
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
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
	// ToolPack selects how catalog tools query the server: ToolPackAuto ("" means auto)
	// uses the mysql_mcp SQL tool pack when a connection has it, ToolPackOff never does.
	ToolPack string
	// StatementLabels prefixes each tool call's statements with
	// /* mcp:tool=<name> req=<id> */ (default true; MYSQL_MCP_STATEMENT_LABELS=0 turns it off).
	StatementLabels bool
	HTTPMode        bool
	MetricsHTTP     bool // Serve /status + /api/metrics/tokens on HTTP while MCP uses stdio (Claude Desktop)
	JSONLogging     bool
	TokenCard       bool // Enable live monitoring UI at /status

	// Token estimation (optional, disabled by default)
	TokenTracking bool
	TokenModel    string

	// HTTP settings
	HTTPPort           int
	HTTPRequestTimeout time.Duration

	// MCP transport: "stdio" (default) or "http" (MCP Streamable HTTP; see MCPHTTP*).
	Transport string

	// MCP Streamable HTTP settings (Transport == "http"). Port is HTTPPort.
	MCPHTTPBind           string        // listen address; default 127.0.0.1
	MCPHTTPPath           string        // endpoint path; default /mcp
	MCPHTTPToken          string        // bearer token (from MYSQL_MCP_HTTP_TOKEN or token file)
	MCPHTTPTokenFile      string        // file holding the bearer token
	MCPHTTPTLSCert        string        // TLS certificate path
	MCPHTTPTLSKey         string        // TLS key path
	MCPHTTPStateless      bool          // stateless MCP sessions (no Mcp-Session-Id)
	MCPHTTPSessionTimeout time.Duration // idle session timeout; unset/0 defaults to 30m (cannot be disabled)
	MCPHTTPAllowedOrigins []string      // browser origins allowed via CORS and cross-origin protection
	MCPHTTPAllowedHosts   []string      // extra Host names accepted on loopback (reverse proxy in front)
	MCPHTTPAllowInsecure  bool          // permit non-loopback bind without TLS (e.g. behind a TLS proxy)

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
	AllowedDatabases   []string // Empty = all databases allowed (subject to MySQL grants)
	StrictReadOnly     bool     // SET transaction_read_only=ON on each driver connection (DSN param)
	AllowSystemSchemas bool     // Permit read-only access to information_schema/performance_schema/sys (mysql stays blocked)
	ProcessAdmin       bool     // Enable process_list and kill_query (extended tools)
	ReadAuditTool      bool     // Enable read_audit_log when AuditLogPath is set (extended)
	SlowQueryTool      bool     // Enable slow_query_log tool (extended)
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
			StatementLabels:    true,
			DBRetryMaxRetries:  3,
			DBRetryMaxInterval: 10 * time.Second,
		}
	}

	// Apply environment variable overrides (env vars take precedence), then fill MCP HTTP
	// defaults and normalize (bind brackets, origin case). Defaults run after the overrides
	// so values from the environment are normalized like values from the config file.
	applyEnvOverrides(cfg)
	applyMCPHTTPDefaults(cfg)

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

	if err := validateConnectionNames(cfg.Connections); err != nil {
		return nil, err
	}
	if err := resolveMCPHTTPToken(cfg); err != nil {
		return nil, err
	}
	if err := validateToolPack(cfg.ToolPack); err != nil {
		return nil, err
	}
	if err := ValidateTransport(cfg); err != nil {
		return nil, err
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
	if v := os.Getenv("MYSQL_MCP_TRANSPORT"); v != "" {
		cfg.Transport = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_BIND"); v != "" {
		cfg.MCPHTTPBind = strings.TrimSpace(v)
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_PATH"); v != "" {
		cfg.MCPHTTPPath = strings.TrimSpace(v)
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_TOKEN"); v != "" {
		cfg.MCPHTTPToken = strings.TrimSpace(v)
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_TOKEN_FILE"); v != "" {
		cfg.MCPHTTPTokenFile = strings.TrimSpace(v)
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_TLS_CERT"); v != "" {
		cfg.MCPHTTPTLSCert = strings.TrimSpace(v)
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_TLS_KEY"); v != "" {
		cfg.MCPHTTPTLSKey = strings.TrimSpace(v)
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_STATELESS"); v != "" {
		cfg.MCPHTTPStateless = getEnvBool("MYSQL_MCP_HTTP_STATELESS")
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_SESSION_TIMEOUT_MINUTES"); v != "" {
		cfg.MCPHTTPSessionTimeout = time.Duration(getEnvInt("MYSQL_MCP_HTTP_SESSION_TIMEOUT_MINUTES", int(cfg.MCPHTTPSessionTimeout.Minutes()))) * time.Minute
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_ALLOWED_ORIGINS"); v != "" {
		cfg.MCPHTTPAllowedOrigins = parseCSVList(v)
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_ALLOWED_HOSTS"); v != "" {
		cfg.MCPHTTPAllowedHosts = parseCSVList(v)
	}
	if v := os.Getenv("MYSQL_MCP_HTTP_ALLOW_INSECURE"); v != "" {
		cfg.MCPHTTPAllowInsecure = getEnvBool("MYSQL_MCP_HTTP_ALLOW_INSECURE")
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
	if v := os.Getenv("MYSQL_MCP_STATEMENT_LABELS"); v != "" {
		cfg.StatementLabels = getEnvBool("MYSQL_MCP_STATEMENT_LABELS")
	}
	if v := strings.TrimSpace(os.Getenv("MYSQL_MCP_TOOL_PACK")); v != "" {
		cfg.ToolPack = strings.ToLower(v)
	}
	if v := os.Getenv("MYSQL_MCP_STRICT_READ_ONLY"); v != "" {
		cfg.StrictReadOnly = getEnvBool("MYSQL_MCP_STRICT_READ_ONLY")
	}
	if v := os.Getenv("MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS"); v != "" {
		cfg.AllowSystemSchemas = getEnvBool("MYSQL_MCP_ALLOW_SYSTEM_SCHEMAS")
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

// ReservedConnectionPrefix starts the internal names of connections that MCP HTTP
// sessions register with add_connection. Configured connections cannot use it.
const ReservedConnectionPrefix = "mcp-session/"

// validateConnectionNames rejects configured connection names that use the reserved prefix.
func validateConnectionNames(conns []ConnectionConfig) error {
	for _, c := range conns {
		if strings.HasPrefix(c.Name, ReservedConnectionPrefix) {
			return fmt.Errorf("connection name %q is invalid: names starting with %q are reserved for MCP session connections", c.Name, ReservedConnectionPrefix)
		}
	}
	return nil
}

// Transport values for Config.Transport.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

// Defaults for the MCP Streamable HTTP transport.
const (
	DefaultMCPHTTPBind                  = "127.0.0.1"
	DefaultMCPHTTPPath                  = "/mcp"
	DefaultMCPHTTPSessionTimeoutMinutes = 30
)

// applyMCPHTTPDefaults fills unset MCP HTTP transport fields with defaults.
func applyMCPHTTPDefaults(cfg *Config) {
	if cfg.Transport == "" {
		cfg.Transport = TransportStdio
	}
	// Accept IPv6 in brackets ("[::1]"); net.JoinHostPort adds them back when listening.
	cfg.MCPHTTPBind = strings.TrimSpace(cfg.MCPHTTPBind)
	if strings.HasPrefix(cfg.MCPHTTPBind, "[") && strings.HasSuffix(cfg.MCPHTTPBind, "]") {
		cfg.MCPHTTPBind = cfg.MCPHTTPBind[1 : len(cfg.MCPHTTPBind)-1]
	}
	if cfg.MCPHTTPBind == "" {
		cfg.MCPHTTPBind = DefaultMCPHTTPBind
	}
	if cfg.MCPHTTPPath == "" {
		cfg.MCPHTTPPath = DefaultMCPHTTPPath
	}
	if cfg.MCPHTTPSessionTimeout == 0 {
		cfg.MCPHTTPSessionTimeout = time.Duration(DefaultMCPHTTPSessionTimeoutMinutes) * time.Minute
	}
	// Browsers send Origin with a lowercase scheme and host, no trailing slash, and no
	// default port (https://host, not https://host:443), so store origins in that form.
	// Only valid origins are rewritten; an invalid one is kept as written so that
	// ValidateTransport reports it instead of normalization hiding the problem.
	for i, o := range cfg.MCPHTTPAllowedOrigins {
		o = strings.TrimSpace(o)
		if validateOrigin(o) == nil {
			o = canonicalOrigin(o)
		}
		cfg.MCPHTTPAllowedOrigins[i] = o
	}
}

// resolveMCPHTTPToken reads the bearer token from MCPHTTPTokenFile when no token is set directly.
func resolveMCPHTTPToken(cfg *Config) error {
	if cfg.Transport != TransportHTTP || cfg.MCPHTTPToken != "" || cfg.MCPHTTPTokenFile == "" {
		return nil
	}
	b, err := os.ReadFile(cfg.MCPHTTPTokenFile)
	if err != nil {
		return fmt.Errorf("failed to read MCP HTTP token file: %w", err)
	}
	cfg.MCPHTTPToken = strings.TrimSpace(string(b))
	if cfg.MCPHTTPToken == "" {
		return fmt.Errorf("MCP HTTP token file %s is empty", cfg.MCPHTTPTokenFile)
	}
	return nil
}

// IsLoopbackBind reports whether addr (a host or IP, no port) only accepts local connections.
func IsLoopbackBind(addr string) bool {
	host := strings.Trim(strings.TrimSpace(addr), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Tool pack modes (MYSQL_MCP_TOOL_PACK, features.tool_pack).
const (
	ToolPackAuto = "auto"
	ToolPackOff  = "off"
)

// validateToolPack accepts "", "auto" and "off".
func validateToolPack(v string) error {
	switch v {
	case "", ToolPackAuto, ToolPackOff:
		return nil
	}
	return fmt.Errorf("invalid MYSQL_MCP_TOOL_PACK %q (want %q or %q)", v, ToolPackAuto, ToolPackOff)
}

// ValidateTransport checks the MCP transport settings for unsafe or conflicting combinations.
func ValidateTransport(cfg *Config) error {
	switch cfg.Transport {
	case TransportStdio:
		return nil
	case TransportHTTP:
	default:
		return fmt.Errorf("invalid MYSQL_MCP_TRANSPORT %q (want %q or %q)", cfg.Transport, TransportStdio, TransportHTTP)
	}
	if cfg.HTTPMode {
		return fmt.Errorf("MYSQL_MCP_TRANSPORT=http cannot be combined with the REST API (MYSQL_MCP_HTTP=1); run them as separate processes")
	}
	if err := validateMCPHTTPPath(cfg.MCPHTTPPath); err != nil {
		return err
	}
	for _, o := range cfg.MCPHTTPAllowedOrigins {
		if err := validateOrigin(o); err != nil {
			return err
		}
	}
	if _, _, err := net.SplitHostPort(cfg.MCPHTTPBind); err == nil || strings.ContainsAny(cfg.MCPHTTPBind, "[]/ ") {
		return fmt.Errorf("MYSQL_MCP_HTTP_BIND must be a host name or IP address without a port (got %q); set the port with MYSQL_HTTP_PORT", cfg.MCPHTTPBind)
	}
	if (cfg.MCPHTTPTLSCert == "") != (cfg.MCPHTTPTLSKey == "") {
		return fmt.Errorf("MYSQL_MCP_HTTP_TLS_CERT and MYSQL_MCP_HTTP_TLS_KEY must be set together")
	}
	if len(cfg.MCPHTTPAllowedHosts) > 0 && cfg.MCPHTTPToken == "" {
		// Allowed hosts exist for a reverse proxy that publishes the loopback listener, so the
		// server is reachable from outside even though it binds to loopback.
		return fmt.Errorf("MYSQL_MCP_HTTP_ALLOWED_HOSTS means a reverse proxy exposes the server: a bearer token is required (MYSQL_MCP_HTTP_TOKEN or MYSQL_MCP_HTTP_TOKEN_FILE)")
	}
	if !IsLoopbackBind(cfg.MCPHTTPBind) {
		if cfg.MCPHTTPToken == "" {
			return fmt.Errorf("MCP HTTP bind %q is not loopback: a bearer token is required (MYSQL_MCP_HTTP_TOKEN or MYSQL_MCP_HTTP_TOKEN_FILE)", cfg.MCPHTTPBind)
		}
		if cfg.MCPHTTPTLSCert == "" && !cfg.MCPHTTPAllowInsecure {
			return fmt.Errorf("MCP HTTP bind %q is not loopback: TLS is required (MYSQL_MCP_HTTP_TLS_CERT/KEY), or set MYSQL_MCP_HTTP_ALLOW_INSECURE=1 when a TLS proxy terminates in front", cfg.MCPHTTPBind)
		}
	}
	return nil
}

// canonicalOrigin returns the form browsers send in the Origin header: lowercase scheme
// and host, IPv6 in brackets, no default port, no trailing slash. o must be valid.
func canonicalOrigin(o string) string {
	u, err := url.Parse(o)
	if err != nil {
		return o
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		// Browsers serialize IP hosts in canonical form ([::1], not [0:0:0:0:0:0:0:1]).
		host = ip.String()
		if ip.To4() == nil {
			host = "[" + host + "]"
		}
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host
}

// validateOrigin requires a browser origin: http or https, a host, an optional port, and no
// path, query or fragment (for example https://app.example.com or http://localhost:3000).
func validateOrigin(o string) error {
	bad := fmt.Errorf("MYSQL_MCP_HTTP_ALLOWED_ORIGINS entry %q is not an origin: use scheme://host[:port], for example https://app.example.com", o)
	// "?" and "#" are rejected outright: an empty query or fragment parses to empty fields.
	if strings.ContainsAny(o, "?#") {
		return bad
	}
	u, err := url.Parse(o)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		(u.Path != "" && u.Path != "/") || u.User != nil {
		return bad
	}
	if strings.HasSuffix(u.Host, ":") { // explicit empty port, e.g. https://app.example:
		return bad
	}
	// An IPv4-mapped IPv6 host ([::ffff:127.0.0.1]) has no unambiguous canonical form here:
	// Go prints it as IPv4, browsers as [::ffff:7f00:1]. Require the plain IPv4 address.
	if ip := net.ParseIP(u.Hostname()); ip != nil && strings.Contains(u.Hostname(), ":") && ip.To4() != nil {
		return bad
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return bad
		}
	}
	return nil
}

// reservedMCPHTTPPaths are served next to the MCP endpoint on the same listener.
var reservedMCPHTTPPaths = []string{"/health", "/status", "/api/metrics/tokens"}

// validateMCPHTTPPath requires a clean, literal path that does not collide with the
// other routes. It is used as an exact http.ServeMux pattern, so wildcards ("{...}"),
// subtree patterns (trailing "/") and duplicates would change routing or panic.
func validateMCPHTTPPath(p string) error {
	if !strings.HasPrefix(p, "/") || p == "/" {
		return fmt.Errorf("MYSQL_MCP_HTTP_PATH must start with '/' and not be the root path (got %q)", p)
	}
	if path.Clean(p) != p || strings.ContainsAny(p, "{}?#% \t") {
		return fmt.Errorf("MYSQL_MCP_HTTP_PATH must be a clean literal path without a trailing '/', wildcards or query characters (got %q)", p)
	}
	for _, r := range reservedMCPHTTPPaths {
		if p == r {
			return fmt.Errorf("MYSQL_MCP_HTTP_PATH %q is reserved for another endpoint", p)
		}
	}
	return nil
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
