// internal/api/response_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"message": "hello"}

	WriteSuccess(w, data)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Error("expected success to be true")
	}
	if resp.Error != "" {
		t.Errorf("expected no error, got %s", resp.Error)
	}
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
	}{
		{"bad request", http.StatusBadRequest, "invalid input"},
		{"internal error", http.StatusInternalServerError, "database error"},
		{"not found", http.StatusNotFound, "resource not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()

			WriteError(w, tt.status, tt.message)

			if w.Code != tt.status {
				t.Errorf("expected status %d, got %d", tt.status, w.Code)
			}

			var resp Response
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if resp.Success {
				t.Error("expected success to be false")
			}
			if resp.Error != tt.message {
				t.Errorf("expected error %q, got %q", tt.message, resp.Error)
			}
		})
	}
}

func TestWriteBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	WriteBadRequest(w, "invalid parameter")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestWriteInternalError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteInternalError(w, "server error")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestWriteNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	WriteNotFound(w, "not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestWriteMethodNotAllowed(t *testing.T) {
	w := httptest.NewRecorder()
	WriteMethodNotAllowed(w, "POST required")

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestCORSHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	WriteSuccess(w, nil)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS origin header")
	}
	if w.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" {
		t.Error("expected CORS methods header")
	}
	if w.Header().Get("Access-Control-Allow-Headers") != "Content-Type" {
		t.Error("expected CORS headers header")
	}
}
