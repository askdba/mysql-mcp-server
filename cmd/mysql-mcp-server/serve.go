// cmd/mysql-mcp-server/serve.go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// httpShutdownTimeout bounds graceful shutdown of the HTTP servers.
const httpShutdownTimeout = 30 * time.Second

// serveUntilSignal runs srv until SIGINT or SIGTERM, then shuts it down gracefully.
// It is shared by the REST API and the MCP HTTP transport.
func serveUntilSignal(srv *http.Server, certFile, keyFile string) error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	return serveUntil(srv, certFile, keyFile, stop)
}

// serveUntil runs srv (TLS when certFile is set) until stop receives a value, then calls
// srv.Shutdown with httpShutdownTimeout. It returns a listen/serve error, or nil after a
// shutdown.
func serveUntil(srv *http.Server, certFile, keyFile string, stop <-chan os.Signal) error {
	errCh := make(chan error, 1)
	go func() {
		if certFile != "" {
			errCh <- srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("HTTP server error: %w", err)
	case <-stop:
	}

	logInfo("Shutdown signal received, stopping server...", nil)
	ctx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logError("Server shutdown error", map[string]interface{}{"error": err.Error()})
	} else {
		logInfo("Server stopped gracefully", nil)
	}
	return nil
}
