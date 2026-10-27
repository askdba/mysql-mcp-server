package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/askdba/mysql-mcp-server/internal/config"
)

// freeAddr returns a currently free 127.0.0.1 address.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func waitForServer(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server at %s did not start: %v", url, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestServeUntilStopsOnSignal(t *testing.T) {
	addr := freeAddr(t)
	srv := &http.Server{Addr: addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})}
	stop := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- serveUntil(srv, "", "", stop) }()
	waitForServer(t, "http://"+addr+"/")

	stop <- os.Interrupt
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveUntil after stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveUntil did not return after stop")
	}
	if _, err := http.Get("http://" + addr + "/"); err == nil {
		t.Error("server still accepting connections after shutdown")
	}
}

func TestServeUntilReturnsListenError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	srv := &http.Server{Addr: l.Addr().String(), Handler: http.NotFoundHandler()}
	if err := serveUntil(srv, "", "", make(chan os.Signal)); err == nil || !strings.Contains(err.Error(), "HTTP server error") {
		t.Fatalf("expected listen error for a busy port, got %v", err)
	}
}

func TestBodyReadDeadlineStopsSlowBodies(t *testing.T) {
	readErr := make(chan error, 1)
	srv := httptest.NewServer(withBodyReadDeadline(200*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readErr <- err
	})))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Promise 100 bytes, send one, then stall.
	fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{")

	select {
	case err := <-readErr:
		if err == nil {
			t.Fatal("expected the stalled body read to fail")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("slow body was not cut off by the read deadline")
	}
}

// TestBodyReadDeadlineClosesStalledConnection checks the client side: a stalled client
// must get a response and a closed connection, not a connection held open forever.
func TestBodyReadDeadlineClosesStalledConnection(t *testing.T) {
	srv := httptest.NewServer(withBodyReadDeadline(200*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "body read timeout", http.StatusBadRequest)
			return
		}
	})))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST /mcp HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	resp, err := io.ReadAll(conn) // returns when the server closes the connection
	if err != nil {
		t.Fatalf("connection not closed by the server within 5s (read: %v, got %q)", err, resp)
	}
	if !strings.HasPrefix(string(resp), "HTTP/1.1 400") {
		t.Errorf("stalled client should get 400, got %q", resp)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("connection closed after %v", took)
	}
}

func TestBodyReadDeadlineClearedAfterBody(t *testing.T) {
	const timeout = 150 * time.Millisecond
	srv := httptest.NewServer(withBodyReadDeadline(timeout, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// A long tool call: well past the body deadline, the request must stay alive.
		select {
		case <-time.After(4 * timeout):
		case <-r.Context().Done():
			http.Error(w, "request context cancelled", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, "done")
	})))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(b) != "done" {
		t.Fatalf("long response after body read: %d %q", resp.StatusCode, b)
	}
}

func TestMCPHTTPServerShutdownEndsOpenStreams(t *testing.T) {
	f := newMCPHTTPFixture(t, nil) // sets globals; its own httptest server is unused here
	_ = f
	oldGrace := mcpHTTPShutdownGrace
	mcpHTTPShutdownGrace = 200 * time.Millisecond
	defer func() { mcpHTTPShutdownGrace = oldGrace }()

	host, port, _ := net.SplitHostPort(freeAddr(t))
	c := *cfg
	c.MCPHTTPBind = host
	fmt.Sscan(port, &c.HTTPPort)

	srv, release, err := newMCPHTTPServer(buildMCPServer(), &c, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	stop := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- serveUntil(srv, "", "", stop) }()
	base := "http://" + srv.Addr
	waitForServer(t, base+"/health")

	// Open a session and its standalone server-sent event stream.
	resp := postInitialize(t, base+config.DefaultMCPHTTPPath, nil)
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("no Mcp-Session-Id")
	}
	note, _ := http.NewRequest(http.MethodPost, base+config.DefaultMCPHTTPPath, strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	note.Header.Set("Content-Type", "application/json")
	note.Header.Set("Accept", "application/json, text/event-stream")
	note.Header.Set("Mcp-Session-Id", sid)
	if r, err := http.DefaultClient.Do(note); err == nil {
		r.Body.Close()
	}
	get, _ := http.NewRequest(http.MethodGet, base+config.DefaultMCPHTTPPath, nil)
	get.Header.Set("Accept", "text/event-stream")
	get.Header.Set("Mcp-Session-Id", sid)
	stream, err := http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK || !strings.HasPrefix(stream.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("standalone SSE stream not opened: %d %q", stream.StatusCode, stream.Header.Get("Content-Type"))
	}
	streamEnded := make(chan time.Duration, 1)
	start := time.Now()
	go func() {
		_, _ = io.Copy(io.Discard, bufio.NewReader(stream.Body))
		streamEnded <- time.Since(start)
	}()

	stop <- os.Interrupt
	select {
	case took := <-streamEnded:
		if took < mcpHTTPShutdownGrace {
			t.Errorf("open stream ended after %v, before the %v grace period", took, mcpHTTPShutdownGrace)
		}
		if took > 2*time.Second {
			t.Errorf("open stream ended %v after shutdown began, want about the %v grace period", took, mcpHTTPShutdownGrace)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel the open stream after the grace period")
	}
	// http.Server.Shutdown may still wait up to 5s for client connections that were
	// opened but never sent a request (StateNew); it must not wait the full 30s timeout.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not finish")
	}
}

// A client that stalls its request body must not hold shutdown for the body read timeout:
// cancelling the base context does not unblock the read, so remaining connections are
// closed shortly after the grace period.
func TestMCPHTTPServerShutdownClosesStalledBody(t *testing.T) {
	f := newMCPHTTPFixture(t, nil)
	_ = f
	oldGrace, oldForce := mcpHTTPShutdownGrace, mcpHTTPShutdownForce
	mcpHTTPShutdownGrace, mcpHTTPShutdownForce = 200*time.Millisecond, 100*time.Millisecond
	defer func() { mcpHTTPShutdownGrace, mcpHTTPShutdownForce = oldGrace, oldForce }()

	host, port, _ := net.SplitHostPort(freeAddr(t))
	c := *cfg
	c.MCPHTTPBind = host
	fmt.Sscan(port, &c.HTTPPort)
	srv, release, err := newMCPHTTPServer(buildMCPServer(), &c, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	stop := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() { done <- serveUntil(srv, "", "", stop) }()
	waitForServer(t, "http://"+srv.Addr+"/health")

	conn, err := net.Dial("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Expect: 100-continue makes net/http answer "100 Continue" when the handler first
	// reads the body, so shutdown starts only once the handler is in the body read.
	fmt.Fprint(conn, "POST /mcp HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nExpect: 100-continue\r\nContent-Length: 100\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	if status, err := br.ReadString('\n'); err != nil || !strings.HasPrefix(status, "HTTP/1.1 100 Continue") {
		t.Fatalf("body read did not start: status=%q err=%v", status, err)
	}
	if line, err := br.ReadString('\n'); err != nil || line != "\r\n" {
		t.Fatalf("invalid 100-continue response: %q err=%v", line, err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	fmt.Fprint(conn, "{")

	start := time.Now()
	stop <- os.Interrupt
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took > 3*time.Second {
			t.Errorf("shutdown took %v with a stalled body, want about grace + force", took)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a stalled request body held shutdown")
	}
}

func TestPreflightHTTPListener(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	_, busyPort, _ := net.SplitHostPort(busy.Addr().String())
	port := func(s string) int { n := 0; fmt.Sscan(s, &n); return n }

	mcpCfg := func(p int) *config.Config {
		return &config.Config{Transport: config.TransportHTTP, MCPHTTPBind: "127.0.0.1", HTTPPort: p}
	}
	if err := preflightHTTPListener(mcpCfg(port(busyPort))); err == nil || !strings.Contains(err.Error(), "cannot listen") {
		t.Errorf("busy port: %v", err)
	}

	_, freePort, _ := net.SplitHostPort(freeAddr(t))
	if err := preflightHTTPListener(mcpCfg(port(freePort))); err != nil {
		t.Errorf("free port: %v", err)
	}

	bad := mcpCfg(port(freePort))
	bad.MCPHTTPTLSCert, bad.MCPHTTPTLSKey = "/nonexistent/cert.pem", "/nonexistent/key.pem"
	if err := preflightHTTPListener(bad); err == nil || !strings.Contains(err.Error(), "TLS certificate/key") {
		t.Errorf("missing TLS files: %v", err)
	}

	// REST mode listens on all interfaces (":port"): a port taken there is reported too.
	wild, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer wild.Close()
	_, wildPort, _ := net.SplitHostPort(wild.Addr().String())
	if err := preflightHTTPListener(&config.Config{HTTPMode: true, HTTPPort: port(wildPort)}); err == nil || !strings.Contains(err.Error(), "cannot listen") {
		t.Errorf("REST busy port: %v", err)
	}
}
