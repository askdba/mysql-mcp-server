package main

import (
	"context"
	"encoding/json"
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

	oldCfg, oldCM := cfg, connManager
	cfg, connManager = c, cm
	t.Cleanup(func() {
		cfg, connManager = oldCfg, oldCM
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
