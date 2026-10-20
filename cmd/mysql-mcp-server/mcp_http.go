// cmd/mysql-mcp-server/mcp_http.go
package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askdba/mysql-mcp-server/internal/api"
	"github.com/askdba/mysql-mcp-server/internal/config"
)

// ===== MCP Streamable HTTP transport (MYSQL_MCP_TRANSPORT=http) =====

// sessionConnMiddleware attaches per-MCP-session connection state to every request, so
// use_connection / add_connection switch only the calling session's active connection.
// When the session ends, its state is dropped and the connections it registered with
// add_connection are closed.
func sessionConnMiddleware() mcp.Middleware {
	var sessions sync.Map // *mcp.ServerSession -> *sessionConn
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if ss, ok := req.GetSession().(*mcp.ServerSession); ok && ss != nil {
				v, ok := sessions.Load(ss) // fast path: no allocation for known sessions
				if !ok {
					var loaded bool
					if v, loaded = sessions.LoadOrStore(ss, newSessionConn()); !loaded {
						sc := v.(*sessionConn)
						go func() {
							_ = ss.Wait()
							sessions.Delete(ss)
							closeSessionConnections(sc)
						}()
					}
				}
				ctx = withSessionConn(ctx, v.(*sessionConn))
			}
			return next(ctx, method, req)
		}
	}
}

// closeSessionConnections removes the connections a finished MCP session registered.
func closeSessionConnections(sc *sessionConn) {
	for _, key := range sc.close() {
		if connManager == nil {
			return
		}
		if err := connManager.RemoveConnection(key); err == nil {
			logInfo("closed session connection", map[string]interface{}{"connection": key})
		}
	}
}

// requireBearerToken rejects requests without "Authorization: Bearer <token>".
// An empty token disables the check (loopback-only deployments; see config.ValidateTransport).
func requireBearerToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := strings.Fields(r.Header.Get("Authorization"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "bearer") ||
			subtle.ConstantTimeCompare([]byte(fields[1]), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mysql-mcp-server"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mcpCORSAllowHeaders are the request headers browser MCP clients send on the endpoint.
const mcpCORSAllowHeaders = "Authorization, Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID"

// withMCPCORS answers CORS for the configured browser origins. It runs before the token
// check, because a preflight OPTIONS request never carries the Authorization header.
// Requests from other origins get no CORS headers (cross-origin protection rejects them).
func withMCPCORS(allowed []string, next http.Handler) http.Handler {
	if len(allowed) == 0 {
		return next
	}
	origins := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		origins[strings.TrimRight(o, "/")] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := origins[origin]; !ok || origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Add("Vary", "Origin")
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", mcpCORSAllowHeaders)
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withAllowedHosts replaces the SDK's DNS-rebinding check when a reverse proxy forwards
// public Host names to a loopback listener: a request arriving on a loopback address must
// carry a localhost Host or one of the configured names.
func withAllowedHosts(allowed []string, next http.Handler) http.Handler {
	hosts := make(map[string]struct{}, len(allowed))
	for _, h := range allowed {
		hosts[strings.ToLower(strings.TrimSpace(h))] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok && local != nil && isLoopbackHostPort(local.String()) {
			host := strings.ToLower(r.Host)
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if _, ok := hosts[host]; !ok && !config.IsLoopbackBind(host) {
				http.Error(w, fmt.Sprintf("Forbidden: invalid Host header %q", r.Host), http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackHostPort reports whether an "ip:port" address is on a loopback interface.
func isLoopbackHostPort(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return config.IsLoopbackBind(host)
}

// newMCPHTTPHandler builds the handler for the MCP HTTP transport:
//   - c.MCPHTTPPath: MCP Streamable HTTP (bearer token, cross-origin protection,
//     and the SDK's DNS-rebinding protection for localhost)
//   - /health: unauthenticated health check
//   - /api/metrics/tokens: token metrics (same bearer token as MCP)
//   - /status: Token Card page, only when tokenCardEnabled and no token is configured
//     (a browser cannot send the bearer token)
func newMCPHTTPHandler(server *mcp.Server, c *config.Config, rl *api.RateLimiter, tokenCardEnabled bool) (http.Handler, error) {
	server.AddReceivingMiddleware(sessionConnMiddleware())

	var mcpHandler http.Handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:      c.MCPHTTPStateless,
			SessionTimeout: c.MCPHTTPSessionTimeout,
			// With allowed hosts configured, withAllowedHosts performs the same check
			// but also accepts the proxy's public names.
			DisableLocalhostProtection: len(c.MCPHTTPAllowedHosts) > 0,
		},
	)
	if len(c.MCPHTTPAllowedHosts) > 0 {
		mcpHandler = withAllowedHosts(c.MCPHTTPAllowedHosts, mcpHandler)
	}

	// Cross-origin protection wraps the handler, as the SDK recommends; its
	// StreamableHTTPOptions.CrossOriginProtection field is deprecated.
	cop := http.NewCrossOriginProtection()
	for _, origin := range c.MCPHTTPAllowedOrigins {
		if err := cop.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("invalid MYSQL_MCP_HTTP_ALLOWED_ORIGINS entry %q: %w", origin, err)
		}
	}

	mux := http.NewServeMux()
	mux.Handle(c.MCPHTTPPath, withMCPCORS(c.MCPHTTPAllowedOrigins, requireBearerToken(c.MCPHTTPToken, cop.Handler(mcpHandler))))
	mux.HandleFunc("/health", httpHealth)
	mux.Handle("/api/metrics/tokens", requireBearerToken(c.MCPHTTPToken, http.HandlerFunc(httpMetricsTokens)))
	if tokenCardEnabled && c.MCPHTTPToken == "" {
		mux.HandleFunc("/status", httpStatusPage)
	}

	handler := api.WithLogging(httpLogger)(mux.ServeHTTP)
	handler = api.WithRateLimit(rl)(handler)
	return withBodyReadDeadline(mcpHTTPBodyReadTimeout, handler), nil
}

// mcpHTTPShutdownGrace is how long in-flight requests may finish after SIGINT/SIGTERM
// before open MCP streams are cancelled.
var mcpHTTPShutdownGrace = 5 * time.Second

// mcpHTTPBodyReadTimeout bounds how long a client may take to send a request body.
var mcpHTTPBodyReadTimeout = 30 * time.Second

// withBodyReadDeadline limits the time to read a request body, so a client cannot hold a
// connection open by trickling bytes. http.Server.ReadTimeout cannot be used: it would also
// apply while long tool calls run and to server-sent event streams. The deadline is set when
// the request starts and cleared as soon as the body has been read.
func withBodyReadDeadline(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody {
			next.ServeHTTP(w, r)
			return
		}
		rc := http.NewResponseController(w)
		if err := rc.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			next.ServeHTTP(w, r) // writer without deadline support (e.g. recorders in tests)
			return
		}
		r.Body = &deadlineBody{ReadCloser: r.Body, rc: rc}
		next.ServeHTTP(w, r)
	})
}

// deadlineBody clears the connection read deadline once the body is fully read (or fails).
// Clearing also applies to the server's background read that watches for client
// disconnects, so long-running responses are not cut off.
type deadlineBody struct {
	io.ReadCloser
	rc   *http.ResponseController
	once sync.Once
}

func (b *deadlineBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.clear()
	}
	return n, err
}

func (b *deadlineBody) Close() error {
	b.clear()
	return b.ReadCloser.Close()
}

func (b *deadlineBody) clear() {
	b.once.Do(func() { _ = b.rc.SetReadDeadline(time.Time{}) })
}

// newMCPHTTPServer builds the http.Server for the MCP HTTP transport. Request contexts
// derive from a base context: Shutdown never ends open server-sent event streams by itself,
// so mcpHTTPShutdownGrace after shutdown starts, the base context is cancelled. The
// returned func releases the base context.
func newMCPHTTPServer(server *mcp.Server, c *config.Config, rl *api.RateLimiter, tokenCardEnabled bool) (*http.Server, func(), error) {
	handler, err := newMCPHTTPHandler(server, c, rl, tokenCardEnabled)
	if err != nil {
		return nil, nil, err
	}
	baseCtx, cancelBase := context.WithCancel(context.Background())
	srv := &http.Server{
		Addr:              net.JoinHostPort(c.MCPHTTPBind, strconv.Itoa(c.HTTPPort)),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// No ReadTimeout/WriteTimeout: Streamable HTTP keeps server-sent event streams open
		// and tool calls may run long (bounded by the query timeout). Request bodies are
		// bounded by withBodyReadDeadline and the SDK's MaxRequestBodyBytes.
		IdleTimeout: 120 * time.Second,
		BaseContext: func(net.Listener) context.Context { return baseCtx },
	}
	srv.RegisterOnShutdown(func() { time.AfterFunc(mcpHTTPShutdownGrace, cancelBase) })
	return srv, cancelBase, nil
}

// startMCPHTTPServer serves MCP over Streamable HTTP until SIGINT/SIGTERM.
func startMCPHTTPServer(server *mcp.Server, c *config.Config, tokenCardEnabled bool) {
	var rateLimiter *api.RateLimiter
	if c.RateLimitEnabled {
		rateLimiter = api.NewRateLimiter(c.RateLimitRPS, c.RateLimitBurst)
		defer rateLimiter.Stop()
	}

	srv, release, err := newMCPHTTPServer(server, c, rateLimiter, tokenCardEnabled)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	defer release()

	useTLS := c.MCPHTTPTLSCert != ""
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	if !config.IsLoopbackBind(c.MCPHTTPBind) && !useTLS {
		logWarn("MCP HTTP transport is listening on a non-loopback address without TLS (MYSQL_MCP_HTTP_ALLOW_INSECURE=1); terminate TLS in front of it", map[string]interface{}{
			"bind": c.MCPHTTPBind,
		})
	}
	logInfo("MCP Streamable HTTP server starting", map[string]interface{}{
		"url":       fmt.Sprintf("%s://%s%s", scheme, srv.Addr, c.MCPHTTPPath),
		"auth":      c.MCPHTTPToken != "",
		"tls":       useTLS,
		"stateless": c.MCPHTTPStateless,
		"version":   Version,
	})

	if err := serveUntilSignal(srv, c.MCPHTTPTLSCert, c.MCPHTTPTLSKey); err != nil {
		log.Fatalf("MCP %v", err)
	}
}
