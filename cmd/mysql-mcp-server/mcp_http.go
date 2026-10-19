// cmd/mysql-mcp-server/mcp_http.go
package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askdba/mysql-mcp-server/internal/api"
	"github.com/askdba/mysql-mcp-server/internal/config"
)

// ===== MCP Streamable HTTP transport (MYSQL_MCP_TRANSPORT=http) =====

// sessionConnMiddleware attaches per-MCP-session connection state to every request, so
// use_connection / add_connection switch only the calling session's active connection.
// State is dropped when the session ends.
func sessionConnMiddleware() mcp.Middleware {
	var sessions sync.Map // *mcp.ServerSession -> *sessionConn
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if ss, ok := req.GetSession().(*mcp.ServerSession); ok && ss != nil {
				v, ok := sessions.Load(ss) // fast path: no allocation for known sessions
				if !ok {
					var loaded bool
					if v, loaded = sessions.LoadOrStore(ss, &sessionConn{}); !loaded {
						go func() {
							_ = ss.Wait()
							sessions.Delete(ss)
						}()
					}
				}
				ctx = withSessionConn(ctx, v.(*sessionConn))
			}
			return next(ctx, method, req)
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
	return handler, nil
}

// mcpHTTPShutdownGrace is how long in-flight requests may finish after SIGINT/SIGTERM
// before open MCP streams are cancelled.
const mcpHTTPShutdownGrace = 5 * time.Second

// startMCPHTTPServer serves MCP over Streamable HTTP until SIGINT/SIGTERM.
func startMCPHTTPServer(server *mcp.Server, c *config.Config, tokenCardEnabled bool) {
	var rateLimiter *api.RateLimiter
	if c.RateLimitEnabled {
		rateLimiter = api.NewRateLimiter(c.RateLimitRPS, c.RateLimitBurst)
		defer rateLimiter.Stop()
	}

	handler, err := newMCPHTTPHandler(server, c, rateLimiter, tokenCardEnabled)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	addr := net.JoinHostPort(c.MCPHTTPBind, strconv.Itoa(c.HTTPPort))
	useTLS := c.MCPHTTPTLSCert != ""
	// Request contexts derive from baseCtx. Shutdown never ends open server-sent event
	// streams by itself, so after a grace period for in-flight tool calls it cancels them.
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: Streamable HTTP keeps server-sent event streams open.
		// Tool calls are bounded by the query timeout instead.
		IdleTimeout: 120 * time.Second,
		BaseContext: func(net.Listener) context.Context { return baseCtx },
	}
	srv.RegisterOnShutdown(func() { time.AfterFunc(mcpHTTPShutdownGrace, cancelBase) })

	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	if !config.IsLoopbackBind(c.MCPHTTPBind) && !useTLS {
		logWarn("MCP HTTP transport is listening on a non-loopback address without TLS (MYSQL_MCP_HTTP_ALLOW_INSECURE=1); terminate TLS in front of it", map[string]interface{}{
			"bind": c.MCPHTTPBind,
		})
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		logInfo("MCP Streamable HTTP server starting", map[string]interface{}{
			"url":       fmt.Sprintf("%s://%s%s", scheme, addr, c.MCPHTTPPath),
			"auth":      c.MCPHTTPToken != "",
			"tls":       useTLS,
			"stateless": c.MCPHTTPStateless,
			"version":   Version,
		})
		var err error
		if useTLS {
			err = srv.ListenAndServeTLS(c.MCPHTTPTLSCert, c.MCPHTTPTLSKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("MCP HTTP server error: %v", err)
		}
	}()

	<-stop
	logInfo("Shutdown signal received, stopping MCP HTTP server...", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logError("MCP HTTP server shutdown error", map[string]interface{}{"error": err.Error()})
	} else {
		logInfo("MCP HTTP server stopped gracefully", nil)
	}
}
