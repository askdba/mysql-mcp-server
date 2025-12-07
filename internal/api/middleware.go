// internal/api/middleware.go
package api

import (
	"context"
	"net/http"
	"time"
)

// DefaultRequestTimeout is the default timeout for HTTP requests.
const DefaultRequestTimeout = 60 * time.Second

// HandlerFunc is a function type for API handlers that returns data and error.
type HandlerFunc func(ctx context.Context, r *http.Request) (interface{}, error)

// WithCORS wraps a handler to add CORS headers and handle OPTIONS preflight.
func WithCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			WriteJSON(w, http.StatusOK, nil)
			return
		}

		next(w, r)
	}
}

// RequireMethod wraps a handler to require a specific HTTP method.
func RequireMethod(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			WriteJSON(w, http.StatusOK, nil)
			return
		}

		if r.Method != method {
			WriteMethodNotAllowed(w, method+" method required")
			return
		}

		next(w, r)
	}
}

// RequireGET wraps a handler to require GET method.
func RequireGET(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			WriteJSON(w, http.StatusOK, nil)
			return
		}

		// Allow GET and HEAD methods
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			WriteMethodNotAllowed(w, "GET method required")
			return
		}

		next(w, r)
	}
}

// RequirePOST wraps a handler to require POST method.
func RequirePOST(next http.HandlerFunc) http.HandlerFunc {
	return RequireMethod(http.MethodPost, next)
}

// WithTimeout wraps a handler to add a timeout to the request context.
func WithTimeout(timeout time.Duration, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}

// RequireFeature wraps a handler to check if a feature is enabled.
func RequireFeature(enabled bool, featureName string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			WriteJSON(w, http.StatusOK, nil)
			return
		}

		if !enabled {
			WriteNotFound(w, featureName+" not enabled")
			return
		}

		next(w, r)
	}
}

// RequireQueryParam returns middleware that checks a required query parameter is present.
func RequireQueryParam(paramName string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "OPTIONS" {
				WriteJSON(w, http.StatusOK, nil)
				return
			}

			if r.URL.Query().Get(paramName) == "" {
				WriteBadRequest(w, paramName+" parameter is required")
				return
			}

			next(w, r)
		}
	}
}

// RequireQueryParams returns middleware that checks multiple required query parameters are present.
func RequireQueryParams(paramNames []string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "OPTIONS" {
				WriteJSON(w, http.StatusOK, nil)
				return
			}

			for _, name := range paramNames {
				if r.URL.Query().Get(name) == "" {
					WriteBadRequest(w, name+" parameter is required")
					return
				}
			}

			next(w, r)
		}
	}
}

// Chain chains multiple middleware functions together.
func Chain(handler http.HandlerFunc, middlewares ...func(http.HandlerFunc) http.HandlerFunc) http.HandlerFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// Logger is a function type for logging HTTP requests.
// It receives method, path, status code, and duration.
type Logger func(method, path string, status int, duration time.Duration)

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// WithLogging returns middleware that logs HTTP requests using the provided logger.
func WithLogging(logger Logger) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status
			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			// Call the next handler
			next(wrapped, r)

			// Log the request (skip OPTIONS for cleaner logs)
			if r.Method != "OPTIONS" {
				logger(r.Method, r.URL.Path, wrapped.status, time.Since(start))
			}
		}
	}
}
