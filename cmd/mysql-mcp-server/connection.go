// cmd/mysql-mcp-server/connection.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/config"
	"github.com/askdba/mysql-mcp-server/internal/util"
)

// ===== Multi-DSN Connection Manager =====

// ConnectionManager manages multiple MySQL connections.
type ConnectionManager struct {
	connections map[string]*sql.DB
	configs     map[string]config.ConnectionConfig
	activeConn  string
	mu          sync.RWMutex
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*sql.DB),
		configs:     make(map[string]config.ConnectionConfig),
	}
}

// AddConnectionWithPoolConfig adds a new connection with pool configuration.
func (cm *ConnectionManager) AddConnectionWithPoolConfig(connCfg config.ConnectionConfig, cfg *config.Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conn, err := sql.Open("mysql", connCfg.DSN)
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
