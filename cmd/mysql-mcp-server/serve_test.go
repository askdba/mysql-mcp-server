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
	streamEnded := make(chan time.Duration, 1)
	start := time.Now()
	go func() {
		_, _ = io.Copy(io.Discard, bufio.NewReader(stream.Body))
		streamEnded <- time.Since(start)
	}()

	stop <- os.Interrupt
	select {
	case took := <-streamEnded:
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
