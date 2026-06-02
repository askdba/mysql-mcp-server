// cmd/mysql-mcp-server/connection.go
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/config"
	mysqlclient "github.com/askdba/mysql-mcp-server/internal/mysql"
	"github.com/askdba/mysql-mcp-server/internal/sshtunnel"
	"github.com/askdba/mysql-mcp-server/internal/util"
	"github.com/go-sql-driver/mysql"
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
//
// Locking: mu is an RWMutex. Short write locks (Lock) protect mutations: registering or
// removing connections, changing activeConn, tunnelCloser bookkeeping, and pendingAdds
// (in-flight add-if-absent reservations). Read locks (RLock) are used for concurrent
// readers: GetActive, GetActiveDB, List, GetServerType.
// addConnectionWithPoolConfig holds Lock only for brief registration; Ping, SSH tunnel
// setup, and server-type detection run without mu so readers are not blocked.
// Avoid calling other exported ConnectionManager methods while holding Lock to prevent
// deadlock (those methods take their own locks).
type ConnectionManager struct {
	connections   map[string]*sql.DB
	clients       map[string]*mysqlclient.Client // retry-capable clients wrapping each DB
	configs       map[string]config.ConnectionConfig
	serverTypes   map[string]ServerType
	activeConn    string
	tunnelClosers map[string]func() // per-connection SSH tunnel close functions
	// pendingAdds marks names currently in addConnectionWithPoolConfig (!replace) between
	// reservation and registration, so concurrent adds for the same name fail before I/O.
	pendingAdds map[string]struct{}
	mu          sync.RWMutex
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections:   make(map[string]*sql.DB),
		clients:       make(map[string]*mysqlclient.Client),
		configs:       make(map[string]config.ConnectionConfig),
		serverTypes:   make(map[string]ServerType),
		tunnelClosers: make(map[string]func()),
	}
}

// applyDefaultIOTimeouts ensures the MySQL driver read/write deadlines are set when the DSN
// omits them. Without this, a query can block indefinitely at the TCP layer if context
// cancellation does not unblock the driver (metadata locks, proxy stalls, etc.).
func applyDefaultIOTimeouts(dsn string, queryTimeout time.Duration) (string, error) {
	mysqlCfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", err
	}
	deadline := queryTimeout
	if deadline <= 0 {
		deadline = time.Duration(config.DefaultQueryTimeoutSecs) * time.Second
	}
	// Small margin after the logical query timeout for the server to flush result packets.
	const margin = 2 * time.Second
	if mysqlCfg.ReadTimeout == 0 {
		mysqlCfg.ReadTimeout = deadline + margin
	}
	if mysqlCfg.WriteTimeout == 0 {
		mysqlCfg.WriteTimeout = deadline + margin
	}
	return mysqlCfg.FormatDSN(), nil
}

// applyStrictReadOnlyDSN sets session parameter transaction_read_only=ON so each new
// physical connection executes SET via the driver (see go-sql-driver/mysql handleParams).
// Any existing DSN value for this param is replaced so strict mode cannot be bypassed.
func applyStrictReadOnlyDSN(dsn string, strict bool) (string, error) {
	if !strict {
		return dsn, nil
	}
	mysqlCfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", err
	}
	if mysqlCfg.Params == nil {
		mysqlCfg.Params = make(map[string]string)
	}
	// Always force ON so a DSN cannot override strict mode with OFF (or other values).
	mysqlCfg.Params["transaction_read_only"] = "ON"
	return mysqlCfg.FormatDSN(), nil
}

// ErrConnectionAlreadyExists is returned when AddConnectionIfAbsentWithPoolConfig finds an existing name.
var ErrConnectionAlreadyExists = errors.New("connection already exists")

// AddConnectionWithPoolConfig adds a new connection with pool configuration.
// If a connection with the same name already exists, it and its SSH tunnel (if any) are closed and replaced.
func (cm *ConnectionManager) AddConnectionWithPoolConfig(connCfg config.ConnectionConfig, cfg *config.Config) error {
	return cm.addConnectionWithPoolConfig(context.Background(), connCfg, cfg, true)
}

// AddConnectionIfAbsentWithPoolConfig adds a connection only if the name is not already registered.
// It uses ctx for PingContext and server detection so callers can cancel or bound wait time.
func (cm *ConnectionManager) AddConnectionIfAbsentWithPoolConfig(ctx context.Context, connCfg config.ConnectionConfig, cfg *config.Config) error {
	return cm.addConnectionWithPoolConfig(ctx, connCfg, cfg, false)
}

// tearDownNamedConnection removes an existing registration under cm.mu (must hold lock).
func (cm *ConnectionManager) tearDownNamedConnection(name string) {
	if existing, ok := cm.connections[name]; ok {
		existing.Close()
		delete(cm.connections, name)
		delete(cm.configs, name)
		delete(cm.serverTypes, name)
		if closeTunnel := cm.tunnelClosers[name]; closeTunnel != nil {
			closeTunnel()
			delete(cm.tunnelClosers, name)
		}
		if cm.activeConn == name {
			cm.activeConn = ""
		}
	}
}

// addConnectionWithPoolConfig implements add/replace. When replace is false and the name exists,
// returns ErrConnectionAlreadyExists without modifying state.
//
// When replace is true, the existing registration (pool, tunnel, config) stays in place until
// the new DSN has been fully opened, pinged, and server-typed; tearDownNamedConnection runs
// only in the final lock section after validation, so a failed replace does not remove a
// working pool.
//
// Ping, server-type detection, and SSH tunnel setup run without holding cm.mu so concurrent
// readers (getDB, List, GetActive, SetActive) are not blocked for multiple ping timeouts.
//
// For add-if-absent (!replace), a brief Lock reserves the name in pendingAdds so a concurrent
// second caller fails with ErrConnectionAlreadyExists before Ping/tunnel work (avoids TOCTOU
// duplicate I/O). The replace path skips reservation and relies on the final Lock after validation.
func (cm *ConnectionManager) addConnectionWithPoolConfig(ctx context.Context, connCfg config.ConnectionConfig, cfg *config.Config, replace bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if !replace {
		cm.mu.Lock()
		if _, exists := cm.connections[connCfg.Name]; exists {
			cm.mu.Unlock()
			return fmt.Errorf("%w: %s", ErrConnectionAlreadyExists, connCfg.Name)
		}
		if cm.pendingAdds == nil {
			cm.pendingAdds = make(map[string]struct{})
		}
		if _, busy := cm.pendingAdds[connCfg.Name]; busy {
			cm.mu.Unlock()
			return fmt.Errorf("%w: %s", ErrConnectionAlreadyExists, connCfg.Name)
		}
		cm.pendingAdds[connCfg.Name] = struct{}{}
		cm.mu.Unlock()

		defer func() {
			cm.mu.Lock()
			if cm.pendingAdds != nil {
				delete(cm.pendingAdds, connCfg.Name)
			}
			cm.mu.Unlock()
		}()
	}

	// For replace path: tear down the existing entry under the lock so concurrent
	// readers (getDB, List, SetActive) don't observe a half-removed state.
	if replace {
		cm.mu.Lock()
		cm.tearDownNamedConnection(connCfg.Name)
		cm.mu.Unlock()
	}

	dsn := config.ApplySSLToDSN(connCfg.DSN, connCfg.SSL)
	var err error
	dsn, err = applyDefaultIOTimeouts(dsn, cfg.QueryTimeout)
	if err != nil {
		return fmt.Errorf("failed to parse DSN for %s: %w", connCfg.Name, err)
	}
	dsn, err = applyStrictReadOnlyDSN(dsn, cfg.StrictReadOnly)
	if err != nil {
		return fmt.Errorf("failed to parse DSN for %s: %w", connCfg.Name, err)
	}

	var tunnelCloser func()
	cleanupLocal := func(conn *sql.DB) {
		if conn != nil {
			conn.Close()
		}
		if tunnelCloser != nil {
			tunnelCloser()
		}
	}

	// If SSH tunnel is configured, start tunnel and rewrite DSN to use local listener
	if connCfg.SSH != nil && connCfg.SSH.Host != "" && connCfg.SSH.User != "" && connCfg.SSH.KeyPath != "" {
		mysqlCfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return fmt.Errorf("failed to parse DSN for SSH tunnel %s: %w", connCfg.Name, err)
		}
		remoteAddr := mysqlCfg.Addr
		if remoteAddr == "" {
			remoteAddr = "127.0.0.1:3306"
		}
		strict := config.EffectiveStrictSSHHostKeyChecking(connCfg.SSH)
		tunnelCfg := sshtunnel.Config{
			Host:                  connCfg.SSH.Host,
			User:                  connCfg.SSH.User,
			KeyPath:               connCfg.SSH.KeyPath,
			Port:                  connCfg.SSH.Port,
			InsecureIgnoreHostKey: !strict,
			KnownHostsPath:        connCfg.SSH.KnownHostsPath,
			HostKeyFingerprint:    connCfg.SSH.HostKeyFingerprint,
		}
		localAddr, closeTunnel, err := sshtunnel.Tunnel(tunnelCfg, remoteAddr)
		if err != nil {
			return fmt.Errorf("failed to start SSH tunnel for %s: %w", connCfg.Name, err)
		}
		tunnelCloser = closeTunnel
		mysqlCfg.Addr = localAddr
		dsn = mysqlCfg.FormatDSN()
	}

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		cleanupLocal(nil)
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

	// Test connection with configurable timeout (honors caller ctx for cancellation)
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	err = conn.PingContext(pingCtx)
	cancel()
	if err != nil {
		cleanupLocal(conn)
		return fmt.Errorf("failed to ping connection %s: %w", connCfg.Name, err)
	}

	// Detect server type with a dedicated context to avoid sharing timeout with PingContext
	detectCtx, cancelDetect := context.WithTimeout(ctx, pingTimeout)
	serverType := cm.detectServerType(detectCtx, conn)
	cancelDetect()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.connections[connCfg.Name]; exists {
		if !replace {
			cleanupLocal(conn)
			return fmt.Errorf("%w: %s", ErrConnectionAlreadyExists, connCfg.Name)
		}
		// Concurrent replace: winner installs first; remove their registration then publish ours.
		cm.tearDownNamedConnection(connCfg.Name)
	}

	cm.connections[connCfg.Name] = conn

	// Wrap the DB in a retry-capable client for tools that need transient-error retry.
	client, err := mysqlclient.NewWithDB(conn, mysqlclient.Config{
		MaxRows:       cfg.MaxRows,
		QueryTimeoutS: int(cfg.QueryTimeout.Seconds()),
	})
	if err != nil {
		logWarn("failed to create retry-capable client for connection", map[string]interface{}{
			"connection": connCfg.Name,
			"error":      err.Error(),
		})
	} else {
		cm.clients[connCfg.Name] = client
	}

	cm.configs[connCfg.Name] = connCfg
	cm.serverTypes[connCfg.Name] = serverType
	if tunnelCloser != nil {
		cm.tunnelClosers[connCfg.Name] = tunnelCloser
	}

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

// RemoveConnection tears down a named connection: closes the pool, removes config and
// server type metadata, closes any SSH tunnel, and clears activeConn if it pointed at name.
// Used for rollback (e.g. after add_connection fails activation).
func (cm *ConnectionManager) RemoveConnection(name string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, ok := cm.connections[name]; !ok {
		return fmt.Errorf("connection '%s' not found", name)
	}
	cm.tearDownNamedConnection(name)
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

// GetActiveClient returns the retry-capable client for the active connection.
// If no pre-built client exists (e.g. in tests that bypass AddConnectionWithPoolConfig),
// one is created lazily wrapping the active *sql.DB with default retry settings.
// Panics if no active connection is available, consistent with getDB().
func (cm *ConnectionManager) GetActiveClient() *mysqlclient.Client {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if c, ok := cm.clients[cm.activeConn]; ok {
		return c
	}
	// Lazy creation for callers that bypass AddConnectionWithPoolConfig (e.g. tests).
	// Use global server values so row caps and timeouts match normal operation.
	if db := cm.connections[cm.activeConn]; db != nil {
		c, err := mysqlclient.NewWithDB(db, mysqlclient.Config{
			MaxRows:       maxRows,
			QueryTimeoutS: int(queryTimeout.Seconds()),
		})
		if err != nil {
			panic("GetActiveClient: lazy client creation failed: " + err.Error())
		}
		cm.clients[cm.activeConn] = c
		return c
	}
	panic("GetActiveClient: no active connection")
}

// Close closes all connections and SSH tunnels managed by the manager.
func (cm *ConnectionManager) Close() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for _, conn := range cm.connections {
		conn.Close()
	}
	for _, closeFn := range cm.tunnelClosers {
		closeFn()
	}
	cm.tunnelClosers = make(map[string]func())
	cm.pendingAdds = nil
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

// getClient returns the retry-capable mysql.Client for the active connection.
// Use this for query execution paths that benefit from transient-error retry.
func getClient() *mysqlclient.Client {
	if connManager == nil {
		panic("getClient called before connManager initialized")
	}
	return connManager.GetActiveClient()
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
