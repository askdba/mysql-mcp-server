// cmd/mysql-mcp-server/connection.go
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
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
		delete(cm.clients, name) // wraps the closed pool; holds the DSN credentials
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

	// Set as active if it's the first connection. A session-owned connection (MCP HTTP
	// add_connection) never becomes the process-wide default.
	if cm.activeConn == "" && !isSessionConnKey(connCfg.Name) {
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

// getDB returns the active database connection for ctx in a thread-safe manner.
// All database access should go through this function to ensure proper
// connection management and avoid data races when connections are switched.
// Over the MCP HTTP transport, ctx carries the MCP session's own active connection.
func getDB(ctx context.Context) *sql.DB {
	if connManager == nil {
		panic("getDB called before connManager initialized")
	}
	db, name := connManager.ActiveFor(ctx)
	if db == nil {
		// Never hand tools a nil *sql.DB: a nil dereference would crash the whole server.
		// This handle fails every operation with a clear error instead.
		return missingConnectionDB(name)
	}
	return db
}

// missingConnDBs caches one failing handle per name (sql.OpenDB starts a goroutine).
var missingConnDBs sync.Map // name -> *sql.DB

// missingConnectionDB returns a *sql.DB whose every operation fails because the named
// connection no longer exists (or none is active).
func missingConnectionDB(name string) *sql.DB {
	if db, ok := missingConnDBs.Load(name); ok {
		return db.(*sql.DB)
	}
	msg := "no active MySQL connection"
	if name != "" {
		msg = fmt.Sprintf("connection '%s' no longer exists; call use_connection to pick another", name)
	}
	fresh := sql.OpenDB(missingConnector{err: errors.New(msg)})
	db, loaded := missingConnDBs.LoadOrStore(name, fresh)
	if loaded {
		_ = fresh.Close()
	}
	return db.(*sql.DB)
}

type missingConnector struct{ err error }

func (m missingConnector) Connect(context.Context) (driver.Conn, error) { return nil, m.err }
func (m missingConnector) Driver() driver.Driver                        { return missingDriver(m) }

type missingDriver missingConnector

func (m missingDriver) Open(string) (driver.Conn, error) { return nil, m.err }

// ===== Per-session active connection (MCP HTTP transport) =====

// sessionConnPrefix namespaces connections registered by add_connection over the MCP
// HTTP transport: "mcp-session/<session id>/<name>". Users cannot pick such names.
const sessionConnPrefix = "mcp-session/"

// maxSessionConnections caps how many connections one MCP HTTP session may register.
const maxSessionConnections = 5

// sessionConn holds one MCP session's connection state over the HTTP transport. Stdio has
// a single client and never attaches one, so it keeps the process-wide behavior.
//
// name is the connection manager key of the session's active connection ("" = use the
// process-wide active connection). owned maps the names this session registered with
// add_connection to their manager keys; they are visible only to this session and are
// removed when it ends.
type sessionConn struct {
	mu     sync.Mutex
	id     string
	name   string
	owned  map[string]string
	closed bool
}

func newSessionConn() *sessionConn {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return &sessionConn{id: hex.EncodeToString(b), owned: make(map[string]string)}
}

func (sc *sessionConn) get() string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.name
}

func (sc *sessionConn) set(key string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.name = key
}

// resolve maps a user-facing connection name to its manager key: a connection this
// session registered wins over a process-wide one with the same name.
func (sc *sessionConn) resolve(name string) string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if key, ok := sc.owned[name]; ok {
		return key
	}
	return name
}

// displayName maps a manager key back to the name the session used.
func (sc *sessionConn) displayName(key string) string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for name, k := range sc.owned {
		if k == key {
			return name
		}
	}
	return key
}

// ownedNames returns the session's registered connections as name -> manager key.
func (sc *sessionConn) ownedNames() map[string]string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	out := make(map[string]string, len(sc.owned))
	for n, k := range sc.owned {
		out[n] = k
	}
	return out
}

// reserve claims name for a new session-owned connection and returns its manager key.
func (sc *sessionConn) reserve(name string) (string, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.closed {
		return "", fmt.Errorf("MCP session has ended")
	}
	if _, ok := sc.owned[name]; ok {
		return "", ErrConnectionAlreadyExists
	}
	if len(sc.owned) >= maxSessionConnections {
		return "", fmt.Errorf("this MCP session already registered %d connections (limit %d)", len(sc.owned), maxSessionConnections)
	}
	key := sessionConnPrefix + sc.id + "/" + name
	sc.owned[name] = key
	return key, nil
}

// release forgets a reservation after a failed add.
func (sc *sessionConn) release(name string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	delete(sc.owned, name)
}

// isClosed reports whether the session has ended.
func (sc *sessionConn) isClosed() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.closed
}

// close marks the session ended and returns the manager keys of its connections.
func (sc *sessionConn) close() []string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.closed = true
	keys := make([]string, 0, len(sc.owned))
	for _, k := range sc.owned {
		keys = append(keys, k)
	}
	sc.owned = map[string]string{}
	return keys
}

// isSessionConnKey reports whether name is a session-owned manager key.
func isSessionConnKey(name string) bool {
	return strings.HasPrefix(name, sessionConnPrefix)
}

type sessionConnKey struct{}

// withSessionConn returns a context whose connection lookups use sc.
func withSessionConn(ctx context.Context, sc *sessionConn) context.Context {
	return context.WithValue(ctx, sessionConnKey{}, sc)
}

// sessionConnFrom returns the session connection state attached to ctx, or nil.
func sessionConnFrom(ctx context.Context) *sessionConn {
	sc, _ := ctx.Value(sessionConnKey{}).(*sessionConn)
	return sc
}

// activeNameLocked resolves the active connection name for ctx. Caller holds cm.mu.
// A session's choice is returned even if that connection has since been removed, so the
// session gets an error (see getDB) instead of silently running on another server.
func (cm *ConnectionManager) activeNameLocked(ctx context.Context) string {
	if sc := sessionConnFrom(ctx); sc != nil {
		if name := sc.get(); name != "" {
			return name
		}
	}
	return cm.activeConn
}

// ActiveFor returns the active database connection and its name for ctx.
func (cm *ConnectionManager) ActiveFor(ctx context.Context) (*sql.DB, string) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	name := cm.activeNameLocked(ctx)
	return cm.connections[name], name
}

// Has reports whether a connection with the given name is registered.
func (cm *ConnectionManager) Has(name string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	_, ok := cm.connections[name]
	return ok
}

// SetActiveFor switches the active connection for ctx: the MCP session's choice when ctx
// carries session state, otherwise the process-wide active connection.
func (cm *ConnectionManager) SetActiveFor(ctx context.Context, name string) error {
	sc := sessionConnFrom(ctx)
	if sc == nil {
		return cm.SetActive(name)
	}
	key := sc.resolve(name)
	if (isSessionConnKey(name) && key == name) || !cm.Has(key) {
		// Another session's connections are never addressable by their internal key.
		return fmt.Errorf("connection '%s' not found", name)
	}
	sc.set(key)
	return nil
}

// GetServerType returns the server type of the process-wide active connection.
func (cm *ConnectionManager) GetServerType() ServerType {
	return cm.ServerTypeFor(context.Background())
}

// ServerTypeFor returns the server type of the active connection for ctx.
func (cm *ConnectionManager) ServerTypeFor(ctx context.Context) ServerType {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if st, exists := cm.serverTypes[cm.activeNameLocked(ctx)]; exists {
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

// getServerType is a helper to get the active server type for ctx.
func getServerType(ctx context.Context) ServerType {
	if connManager == nil {
		return ServerTypeUnknown
	}
	return connManager.ServerTypeFor(ctx)
}
