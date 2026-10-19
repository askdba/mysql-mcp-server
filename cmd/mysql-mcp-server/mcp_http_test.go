package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askdba/mysql-mcp-server/internal/config"
)

// mcpHTTPFixture runs the MCP HTTP handler against two sqlmock connections ("a" active, "b").
type mcpHTTPFixture struct {
	srv   *httptest.Server
	mockA sqlmock.Sqlmock
	mockB sqlmock.Sqlmock
	cm    *ConnectionManager
}

func newMCPHTTPFixture(t *testing.T, mutate func(*config.Config)) *mcpHTTPFixture {
	t.Helper()

	dbA, mockA, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock a: %v", err)
	}
	dbB, mockB, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock b: %v", err)
	}
	cm := NewConnectionManager()
	cm.mu.Lock()
	cm.connections["a"] = dbA
	cm.configs["a"] = config.ConnectionConfig{Name: "a", DSN: "u:p@tcp(a:3306)/da"}
	cm.connections["b"] = dbB
	cm.configs["b"] = config.ConnectionConfig{Name: "b", DSN: "u:p@tcp(b:3306)/db"}
	cm.activeConn = "a"
	cm.mu.Unlock()

	c := &config.Config{Transport: config.TransportHTTP}
	c.MCPHTTPBind = config.DefaultMCPHTTPBind
	c.MCPHTTPPath = config.DefaultMCPHTTPPath
	c.MCPHTTPSessionTimeout = time.Minute
	if mutate != nil {
		mutate(c)
	}

	oldCfg, oldCM, oldTimeout := cfg, connManager, queryTimeout
	cfg, connManager, queryTimeout = c, cm, 5*time.Second
	t.Cleanup(func() {
		cfg, connManager, queryTimeout = oldCfg, oldCM, oldTimeout
		_ = dbA.Close()
		_ = dbB.Close()
	})

	handler, err := newMCPHTTPHandler(buildMCPServer(), c, nil, false)
	if err != nil {
		t.Fatalf("newMCPHTTPHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &mcpHTTPFixture{srv: srv, mockA: mockA, mockB: mockB, cm: cm}
}

// headerTransport adds a fixed header to every request.
type headerTransport struct {
	key, value string
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set(h.key, h.value)
	return http.DefaultTransport.RoundTrip(r)
}

// newBearerClient returns an HTTP client that sends "Authorization: Bearer <token>".
func newBearerClient(token string) *http.Client {
	return &http.Client{Transport: headerTransport{"Authorization", "Bearer " + token}}
}

func (f *mcpHTTPFixture) connect(t *testing.T, httpClient *http.Client) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   f.srv.URL + config.DefaultMCPHTTPPath,
		HTTPClient: httpClient,
		MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool[O any](t *testing.T, cs *mcp.ClientSession, name string, args any) O {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool %s returned tool error: %+v", name, res.Content)
	}
	var out O
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal %s output: %v", name, err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal %s output: %v", name, err)
	}
	return out
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestMCPHTTPToolsMatchStdio(t *testing.T) {
	f := newMCPHTTPFixture(t, nil)
	httpTools := toolNames(t, f.connect(t, nil))

	// Same server construction over an in-memory transport, as stdio uses it.
	clientT, serverT := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := buildMCPServer().Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "stdio-like"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	stdioTools := toolNames(t, cs)
	if !slices.Equal(httpTools, stdioTools) {
		t.Fatalf("tool sets differ:\nhttp:  %v\nstdio: %v", httpTools, stdioTools)
	}
	if !slices.Contains(httpTools, "run_query") || !slices.Contains(httpTools, "use_connection") {
		t.Fatalf("expected core tools over HTTP, got %v", httpTools)
	}
}

func TestMCPHTTPUseConnectionIsPerSession(t *testing.T) {
	f := newMCPHTTPFixture(t, nil)
	f.mockB.ExpectQuery("SELECT DATABASE\\(\\)").WillReturnRows(sqlmock.NewRows([]string{"DATABASE()"}).AddRow("db"))

	s1 := f.connect(t, nil)
	s2 := f.connect(t, nil)

	used := callTool[UseConnectionOutput](t, s1, "use_connection", map[string]any{"name": "b"})
	if !used.Success || used.Active != "b" {
		t.Fatalf("use_connection: %+v", used)
	}

	if got := callTool[ListConnectionsOutput](t, s1, "list_connections", map[string]any{}).Active; got != "b" {
		t.Errorf("session 1 active = %q, want b", got)
	}
	if got := callTool[ListConnectionsOutput](t, s2, "list_connections", map[string]any{}).Active; got != "a" {
		t.Errorf("session 2 active = %q, want a (must not see session 1's switch)", got)
	}
	if _, name := f.cm.GetActive(); name != "a" {
		t.Errorf("process-wide active = %q, want a", name)
	}
	if err := f.mockB.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestMCPHTTPUseConnectionUnknownName(t *testing.T) {
	f := newMCPHTTPFixture(t, nil)
	out := callTool[UseConnectionOutput](t, f.connect(t, nil), "use_connection", map[string]any{"name": "nope"})
	if out.Success || !strings.Contains(out.Message, "not found") {
		t.Fatalf("expected not found, got %+v", out)
	}
}

func TestMCPHTTPBearerToken(t *testing.T) {
	f := newMCPHTTPFixture(t, func(c *config.Config) { c.MCPHTTPToken = "s3cret" })

	for name, auth := range map[string]string{"missing": "", "wrong": "Bearer nope", "basic": "Basic s3cret"} {
		req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/mcp", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s token: status %d, want 401", name, resp.StatusCode)
		}
		if !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s token: missing WWW-Authenticate header", name)
		}
	}

	// Metrics share the MCP token; /health stays open.
	resp, err := http.Get(f.srv.URL + "/api/metrics/tokens")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/api/metrics/tokens without token: %d, want 401", resp.StatusCode)
	}
	resp, err = http.Get(f.srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/health: %d, want 200", resp.StatusCode)
	}

	cs := f.connect(t, newBearerClient("s3cret"))
	if len(toolNames(t, cs)) == 0 {
		t.Fatal("expected tools with a valid token")
	}
}

func TestMCPHTTPRejectsCrossOriginAndRebinding(t *testing.T) {
	f := newMCPHTTPFixture(t, nil)
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"0"}}}`

	post := func(mod func(*http.Request)) int {
		req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		mod(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := post(func(r *http.Request) {
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	}); code != http.StatusForbidden {
		t.Errorf("cross-origin POST: %d, want 403", code)
	}
	if code := post(func(r *http.Request) { r.Host = "evil.example" }); code != http.StatusForbidden {
		t.Errorf("DNS-rebinding Host header: %d, want 403", code)
	}
	if code := post(func(*http.Request) {}); code != http.StatusOK {
		t.Errorf("same-origin initialize: %d, want 200", code)
	}
}

func TestMCPHTTPAllowedOrigin(t *testing.T) {
	f := newMCPHTTPFixture(t, func(c *config.Config) { c.MCPHTTPAllowedOrigins = []string{"https://app.example"} })
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"0"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("trusted origin: %d, want 200", resp.StatusCode)
	}
}

func TestNewMCPHTTPHandlerInvalidOrigin(t *testing.T) {
	c := &config.Config{MCPHTTPPath: "/mcp", MCPHTTPAllowedOrigins: []string{"not a url"}}
	if _, err := newMCPHTTPHandler(mcp.NewServer(&mcp.Implementation{Name: "x"}, nil), c, nil, false); err == nil {
		t.Fatal("expected error for invalid origin")
	}
}

func TestMCPHTTPStatelessDoesNotLeakSessions(t *testing.T) {
	f := newMCPHTTPFixture(t, func(c *config.Config) { c.MCPHTTPStateless = true })
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_connections","arguments":{}}}`
	post := func() {
		req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), `"active":"a"`) {
			t.Fatalf("stateless tools/call: %d %s", resp.StatusCode, b)
		}
	}
	post() // warm up connection pools
	http.DefaultClient.CloseIdleConnections()
	base := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		post()
	}
	http.DefaultClient.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > base+5 {
		if time.Now().After(deadline) {
			t.Fatalf("goroutines grew from %d to %d after 50 stateless requests", base, runtime.NumGoroutine())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func postInitialize(t *testing.T, url string, mod func(*http.Request)) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"0"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if mod != nil {
		mod(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestMCPHTTPAllowedHostsForReverseProxy(t *testing.T) {
	// Without allowed hosts, a proxy's public Host on a loopback listener is rejected.
	f := newMCPHTTPFixture(t, nil)
	if code := postInitialize(t, f.srv.URL+"/mcp", func(r *http.Request) { r.Host = "mcp.example.com" }).StatusCode; code != http.StatusForbidden {
		t.Fatalf("proxy Host without allowlist: %d, want 403", code)
	}

	f = newMCPHTTPFixture(t, func(c *config.Config) { c.MCPHTTPAllowedHosts = []string{"MCP.example.com"} })
	if code := postInitialize(t, f.srv.URL+"/mcp", func(r *http.Request) { r.Host = "mcp.example.com:443" }).StatusCode; code != http.StatusOK {
		t.Errorf("allowed proxy Host: %d, want 200", code)
	}
	if code := postInitialize(t, f.srv.URL+"/mcp", func(r *http.Request) { r.Host = "evil.example" }).StatusCode; code != http.StatusForbidden {
		t.Errorf("unlisted Host: %d, want 403", code)
	}
	if code := postInitialize(t, f.srv.URL+"/mcp", nil).StatusCode; code != http.StatusOK {
		t.Errorf("localhost Host: %d, want 200", code)
	}
}

func TestMCPHTTPCORSPreflightBeforeAuth(t *testing.T) {
	f := newMCPHTTPFixture(t, func(c *config.Config) {
		c.MCPHTTPToken = "s3cret"
		c.MCPHTTPAllowedOrigins = []string{"https://app.example"}
	})

	preflight := func(origin string) *http.Response {
		req, _ := http.NewRequest(http.MethodOptions, f.srv.URL+"/mcp", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	resp := preflight("https://app.example")
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("allowed preflight: %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Errorf("Access-Control-Allow-Headers = %q", resp.Header.Get("Access-Control-Allow-Headers"))
	}

	resp = preflight("https://evil.example")
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("disallowed origin must not get CORS headers")
	}

	resp = postInitialize(t, f.srv.URL+"/mcp", func(r *http.Request) {
		r.Header.Set("Origin", "https://app.example")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Authorization", "Bearer s3cret")
	})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Errorf("allowed origin POST: %d ACAO=%q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Expose-Headers"), "Mcp-Session-Id") {
		t.Error("Mcp-Session-Id must be exposed to browser clients")
	}
}

func TestMCPHTTPRemovedSessionConnectionErrors(t *testing.T) {
	f := newMCPHTTPFixture(t, nil)
	f.mockB.ExpectQuery("SELECT DATABASE\\(\\)").WillReturnRows(sqlmock.NewRows([]string{"DATABASE()"}).AddRow("db"))
	s1 := f.connect(t, nil)
	if out := callTool[UseConnectionOutput](t, s1, "use_connection", map[string]any{"name": "b"}); !out.Success {
		t.Fatalf("use_connection: %+v", out)
	}
	if err := f.cm.RemoveConnection("b"); err != nil {
		t.Fatal(err)
	}

	res, err := s1.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_databases", Arguments: map[string]any{}})
	if err == nil && !res.IsError {
		t.Fatal("expected an error after the session's connection was removed, not a silent fallback")
	}
	msg := fmt.Sprint(err)
	if res != nil {
		b, _ := json.Marshal(res.Content)
		msg += string(b)
	}
	if !strings.Contains(msg, "no longer exists") {
		t.Errorf("error should explain the removed connection, got %s", msg)
	}
	if err := f.mockA.ExpectationsWereMet(); err != nil {
		t.Errorf("query must not run on the process-wide connection: %v", err)
	}
}

func TestGetDBWithoutActiveConnectionReturnsError(t *testing.T) {
	old := connManager
	connManager = NewConnectionManager()
	defer func() { connManager = old }()
	err := getDB(context.Background()).PingContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no active MySQL connection") {
		t.Fatalf("expected no-active-connection error, got %v", err)
	}
	if getDB(context.Background()) != getDB(context.Background()) {
		t.Error("missing-connection handle should be cached")
	}
}

func TestSessionConnReserveResolveClose(t *testing.T) {
	sc := newSessionConn()
	other := newSessionConn()
	if sc.id == "" || sc.id == other.id {
		t.Fatalf("session ids must be unique and non-empty: %q %q", sc.id, other.id)
	}
	key, err := sc.reserve("staging")
	if err != nil || !isSessionConnKey(key) || !strings.HasSuffix(key, "/staging") {
		t.Fatalf("reserve: key=%q err=%v", key, err)
	}
	if _, err := sc.reserve("staging"); err == nil {
		t.Error("duplicate name in one session must fail")
	}
	if k2, err := other.reserve("staging"); err != nil || k2 == key {
		t.Errorf("another session may reuse the name with its own key: %q %v", k2, err)
	}
	if sc.resolve("staging") != key || sc.resolve("prod") != "prod" || sc.displayName(key) != "staging" {
		t.Error("resolve/displayName mismatch")
	}
	for i := 1; i < maxSessionConnections; i++ {
		if _, err := sc.reserve(fmt.Sprintf("c%d", i)); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
	if _, err := sc.reserve("one-too-many"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("expected per-session limit, got %v", err)
	}
	if keys := sc.close(); len(keys) != maxSessionConnections {
		t.Errorf("close returned %d keys, want %d", len(keys), maxSessionConnections)
	}
	if _, err := sc.reserve("late"); err == nil {
		t.Error("reserve after close must fail")
	}
}

func TestMCPHTTPSessionOwnedConnectionsAreIsolated(t *testing.T) {
	f := newMCPHTTPFixture(t, nil)

	// Simulate what add_connection registers for two sessions.
	s1, s2 := newSessionConn(), newSessionConn()
	k1, _ := s1.reserve("extra")
	k2, _ := s2.reserve("extra")
	for _, k := range []string{k1, k2} {
		db, _, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		f.cm.mu.Lock()
		f.cm.connections[k] = db
		f.cm.configs[k] = config.ConnectionConfig{Name: k, DSN: "u:p@tcp(x:3306)/x"}
		f.cm.mu.Unlock()
	}
	ctx1 := withSessionConn(context.Background(), s1)
	if err := f.cm.SetActiveFor(ctx1, "extra"); err != nil {
		t.Fatal(err)
	}

	_, out, err := toolListConnections(ctx1, nil, ListConnectionsInput{})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range out.Connections {
		names = append(names, c.Name)
		if isSessionConnKey(c.Name) {
			t.Errorf("internal key leaked into list_connections: %q", c.Name)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"a", "b", "extra"}) || out.Active != "extra" {
		t.Errorf("session 1 sees %v active=%q, want [a b extra] active=extra", names, out.Active)
	}

	// A session cannot address another session's connection, even by its internal key.
	ctx3 := withSessionConn(context.Background(), newSessionConn())
	for _, name := range []string{"extra", k1} {
		if err := f.cm.SetActiveFor(ctx3, name); err == nil {
			t.Errorf("session 3 switched to %q owned by another session", name)
		}
	}
	_, out3, _ := toolListConnections(ctx3, nil, ListConnectionsInput{})
	if len(out3.Connections) != 2 {
		t.Errorf("session 3 should only see process-wide connections, got %+v", out3.Connections)
	}

	// Ending session 1 closes its connection and leaves session 2's alone.
	closeSessionConnections(s1)
	if f.cm.Has(k1) || !f.cm.Has(k2) {
		t.Errorf("after session 1 ends: has k1=%v (want false), has k2=%v (want true)", f.cm.Has(k1), f.cm.Has(k2))
	}
}

func TestAddConnectionHTTPGuards(t *testing.T) {
	cm := NewConnectionManager()
	ctx := withSessionConn(context.Background(), newSessionConn())

	_, _, err := toolAddConnection(ctx, nil, AddConnectionInput{Name: "x", DSN: "user:p@tcp(h:3306)/d"}, cm, &config.Config{MCPHTTPStateless: true})
	if err == nil || !strings.Contains(err.Error(), "stateless") {
		t.Errorf("stateless mode: %v", err)
	}
	_, _, err = toolAddConnection(ctx, nil, AddConnectionInput{Name: sessionConnPrefix + "abc/x", DSN: "user:p@tcp(h:3306)/d"}, cm, &config.Config{})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("reserved prefix: %v", err)
	}
	db, _, _ := sqlmock.New()
	defer db.Close()
	cm.mu.Lock()
	cm.connections["prod"] = db
	cm.configs["prod"] = config.ConnectionConfig{Name: "prod"}
	cm.mu.Unlock()
	_, _, err = toolAddConnection(ctx, nil, AddConnectionInput{Name: "prod", DSN: "user:p@tcp(h:3306)/d"}, cm, &config.Config{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("shadowing a process-wide name: %v", err)
	}
}
