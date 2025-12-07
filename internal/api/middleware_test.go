// internal/api/middleware_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWithCORS(t *testing.T) {
	handler := WithCORS(func(w http.ResponseWriter, r *http.Request) {
		WriteSuccess(w, nil)
	})

	// Test OPTIONS request
	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("OPTIONS request should return 200, got %d", w.Code)
	}

	// Test GET request
	req = httptest.NewRequest("GET", "/api/test", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("missing CORS origin header")
	}
}

func TestRequireGET(t *testing.T) {
	handler := RequireGET(func(w http.ResponseWriter, r *http.Request) {
		WriteSuccess(w, "ok")
	})

	// Test GET request
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET request should return 200, got %d", w.Code)
	}

	// Test POST request (should fail)
	req = httptest.NewRequest("POST", "/api/test", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST request should return 405, got %d", w.Code)
	}

	// Test OPTIONS request (should pass for CORS)
	req = httptest.NewRequest("OPTIONS", "/api/test", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("OPTIONS request should return 200, got %d", w.Code)
	}
}

func TestRequirePOST(t *testing.T) {
	handler := RequirePOST(func(w http.ResponseWriter, r *http.Request) {
		WriteSuccess(w, "ok")
	})

	// Test POST request
	req := httptest.NewRequest("POST", "/api/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST request should return 200, got %d", w.Code)
	}

	// Test GET request (should fail)
	req = httptest.NewRequest("GET", "/api/test", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET request should return 405, got %d", w.Code)
	}
}

func TestRequireFeature(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		WriteSuccess(w, "feature enabled")
	}

	// Test with feature enabled
	enabledHandler := RequireFeature(true, "extended mode", handler)
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	enabledHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("enabled feature should return 200, got %d", w.Code)
	}

	// Test with feature disabled
	disabledHandler := RequireFeature(false, "extended mode", handler)
	req = httptest.NewRequest("GET", "/api/test", nil)
	w = httptest.NewRecorder()
	disabledHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("disabled feature should return 404, got %d", w.Code)
	}

	var resp Response
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "extended mode not enabled" {
		t.Errorf("unexpected error message: %s", resp.Error)
	}
}

func TestRequireQueryParam(t *testing.T) {
	handler := RequireQueryParam("database")(func(w http.ResponseWriter, r *http.Request) {
		WriteSuccess(w, r.URL.Query().Get("database"))
	})

	// Test with parameter present
	req := httptest.NewRequest("GET", "/api/test?database=mydb", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("request with param should return 200, got %d", w.Code)
	}

	// Test without parameter
	req = httptest.NewRequest("GET", "/api/test", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("request without param should return 400, got %d", w.Code)
	}
}

func TestRequireQueryParams(t *testing.T) {
	handler := RequireQueryParams([]string{"database", "table"})(func(w http.ResponseWriter, r *http.Request) {
		WriteSuccess(w, "ok")
	})

	// Test with all parameters
	req := httptest.NewRequest("GET", "/api/test?database=mydb&table=users", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("request with all params should return 200, got %d", w.Code)
	}

	// Test with missing parameter
	req = httptest.NewRequest("GET", "/api/test?database=mydb", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("request with missing param should return 400, got %d", w.Code)
	}
}

func TestChain(t *testing.T) {
	called := false
	handler := Chain(
		func(w http.ResponseWriter, r *http.Request) {
			called = true
			WriteSuccess(w, "ok")
		},
		RequireGET,
		WithCORS,
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Error("handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestWithLogging(t *testing.T) {
	var loggedMethod, loggedPath string
	var loggedStatus int

	logger := func(method, path string, status int, _ time.Duration) {
		loggedMethod = method
		loggedPath = path
		loggedStatus = status
	}

	handler := WithLogging(logger)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		WriteJSON(w, http.StatusCreated, map[string]string{"status": "created"})
	})

	// Test GET request
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if loggedMethod != "GET" {
		t.Errorf("expected method GET, got %s", loggedMethod)
	}
	if loggedPath != "/api/test" {
		t.Errorf("expected path /api/test, got %s", loggedPath)
	}
	if loggedStatus != http.StatusCreated {
		t.Errorf("expected status 201, got %d", loggedStatus)
	}

	// Test OPTIONS request (should not log)
	loggedMethod = ""
	req = httptest.NewRequest("OPTIONS", "/api/test", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	if loggedMethod != "" {
		t.Error("OPTIONS request should not be logged")
	}
}

func TestResponseWriterCapture(t *testing.T) {
	var capturedStatus int

	logger := func(_, _ string, status int, _ time.Duration) {
		capturedStatus = status
	}

	// Test default status (200)
	handler := WithLogging(logger)(func(w http.ResponseWriter, r *http.Request) {
		// No explicit WriteHeader call - should default to 200
		w.Write([]byte("ok"))
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if capturedStatus != http.StatusOK {
		t.Errorf("expected default status 200, got %d", capturedStatus)
	}

	// Test explicit 404
	handler = WithLogging(logger)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	req = httptest.NewRequest("GET", "/test", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	if capturedStatus != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", capturedStatus)
	}
}
