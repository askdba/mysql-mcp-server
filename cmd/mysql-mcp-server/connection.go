// cmd/mysql-mcp-server/connection.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/askdba/mysql-mcp-server/internal/util"
)

// ServerType represents the type of the database server.
type ServerType string

const (
	ServerTypeMySQL   ServerType = "mysql"
	ServerTypeMariaDB ServerType = "mariadb"
	ServerTypeUnknown ServerType = "unknown"
)

// ===== Multi-DSN Connection Manager =====

// ConnectionManager manages multiple MySQL connections.
type ConnectionManager struct {
	connections map[string]*sql.DB
	configs     map[string]config.ConnectionConfig
	serverTypes map[string]ServerType
	activeConn  string
	mu          sync.RWMutex
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*sql.DB),
		configs:     make(map[string]config.ConnectionConfig),
		serverTypes: make(map[string]ServerType),
	}
}

// AddConnectionWithPoolConfig adds a new connection with pool configuration.
func (cm *ConnectionManager) AddConnectionWithPoolConfig(connCfg config.ConnectionConfig, cfg *config.Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Apply SSL/TLS settings to DSN if configured
	dsn := config.ApplySSLToDSN(connCfg.DSN, connCfg.SSL)

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open connection %s: %w", connCfg.Name, err)
	}

	// Apply pool settings with sensible defaults (defensive against zero values)
	maxOpen := cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = config.DefaultMaxOpenConns
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = config.DefaultMaxIdleConns
	}
	lifetime := cfg.ConnMaxLifetime
	if lifetime <= 0 {
		lifetime = time.Duration(config.DefaultConnMaxLifetimeMins) * time.Minute
	}
	idleTime := cfg.ConnMaxIdleTime
	if idleTime <= 0 {
		idleTime = time.Duration(config.DefaultConnMaxIdleTimeMins) * time.Minute
	}
	pingTimeout := cfg.PingTimeout
	if pingTimeout <= 0 {
		pingTimeout = time.Duration(config.DefaultPingTimeoutSecs) * time.Second
	}

	conn.SetMaxOpenConns(maxOpen)
	conn.SetMaxIdleConns(maxIdle)
	conn.SetConnMaxLifetime(lifetime)
	conn.SetConnMaxIdleTime(idleTime)

	// Test connection with configurable timeout
	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return fmt.Errorf("failed to ping connection %s: %w", connCfg.Name, err)
	}

	cm.connections[connCfg.Name] = conn
	cm.configs[connCfg.Name] = connCfg

	// Detect server type with a dedicated context to avoid sharing timeout with PingContext
	ctxDetect, cancelDetect := context.WithTimeout(context.Background(), pingTimeout)
	defer cancelDetect()
	cm.serverTypes[connCfg.Name] = cm.detectServerType(ctxDetect, conn)

	// Set as active if it's the first connection
	if cm.activeConn == "" {
		cm.activeConn = connCfg.Name
	}

	return nil
}

// GetActive returns the active database connection and its name.
func (cm *ConnectionManager) GetActive() (*sql.DB, string) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.connections[cm.activeConn], cm.activeConn
}

// SetActive sets the active connection by name.
func (cm *ConnectionManager) SetActive(name string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.connections[name]; !exists {
		return fmt.Errorf("connection '%s' not found", name)
	}
	cm.activeConn = name
	return nil
}

// List returns a list of all connection configurations with masked DSNs.
func (cm *ConnectionManager) List() []config.ConnectionConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var list []config.ConnectionConfig
	for _, cfg := range cm.configs {
		// Mask DSN for security
		maskedCfg := cfg
		maskedCfg.DSN = util.MaskDSN(cfg.DSN)
		list = append(list, maskedCfg)
	}
	return list
}

// GetActiveDB returns the active database connection.
func (cm *ConnectionManager) GetActiveDB() *sql.DB {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.connections[cm.activeConn]
}

// Close closes all connections managed by the manager.
func (cm *ConnectionManager) Close() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for _, conn := range cm.connections {
		conn.Close()
	}
}

// getDB returns the active database connection in a thread-safe manner.
// All database access should go through this function to ensure proper
// connection management and avoid data races when connections are switched.
func getDB() *sql.DB {
	if connManager == nil {
		panic("getDB called before connManager initialized")
	}
	return connManager.GetActiveDB()
}

// GetServerType returns the server type of the active connection.
func (cm *ConnectionManager) GetServerType() ServerType {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if st, exists := cm.serverTypes[cm.activeConn]; exists {
		return st
	}
	return ServerTypeUnknown
}

// detectServerType queries the server to determine if it's MySQL or MariaDB.
func (cm *ConnectionManager) detectServerType(ctx context.Context, db *sql.DB) ServerType {
	var version, versionComment string
	// Try VERSION() and @@version_comment first
	err := db.QueryRowContext(ctx, "SELECT VERSION(), @@version_comment").Scan(&version, &versionComment)
	if err != nil {
		// Fallback to just VERSION() if the combined query fails
		err = db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
		if err != nil {
			return ServerTypeUnknown
		}
	}

	version = strings.TrimSpace(strings.ToLower(version))
	versionComment = strings.TrimSpace(strings.ToLower(versionComment))

	// If we got nothing back, we can't reliably identify the server
	if version == "" && versionComment == "" {
		return ServerTypeUnknown
	}

	if strings.Contains(version, "mariadb") || strings.Contains(versionComment, "mariadb") {
		return ServerTypeMariaDB
	}

	return ServerTypeMySQL
}

// getServerType is a helper to get the active server type.
func getServerType() ServerType {
	if connManager == nil {
		return ServerTypeUnknown
	}
	return connManager.GetServerType()
}
