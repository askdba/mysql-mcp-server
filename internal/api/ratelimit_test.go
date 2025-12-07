// internal/api/ratelimit_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	// Create a rate limiter: 10 requests/second, burst of 5
	rl := NewRateLimiter(10, 5)
	defer rl.Stop()

	ip := "192.168.1.1"

	// First 5 requests should be allowed (burst)
	for i := 0; i < 5; i++ {
		if !rl.Allow(ip) {
			t.Errorf("request %d should be allowed within burst", i+1)
		}
	}

	// 6th request should be denied (burst exhausted)
	if rl.Allow(ip) {
		t.Error("request 6 should be denied after burst exhausted")
	}

	// Wait for token refill (100ms for 1 token at 10/sec)
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow(ip) {
		t.Error("request should be allowed after token refill")
	}
}

func TestRateLimiter_MultipleClients(t *testing.T) {
	rl := NewRateLimiter(10, 2)
	defer rl.Stop()

	ip1 := "192.168.1.1"
	ip2 := "192.168.1.2"

	// Exhaust tokens for ip1
	rl.Allow(ip1)
	rl.Allow(ip1)

	// ip1 should be denied
	if rl.Allow(ip1) {
		t.Error("ip1 should be denied after exhausting tokens")
	}

	// ip2 should still be allowed (separate bucket)
	if !rl.Allow(ip2) {
		t.Error("ip2 should be allowed (separate bucket)")
	}
}

func TestRateLimiter_Stats(t *testing.T) {
	rl := NewRateLimiter(100, 50)
	defer rl.Stop()

	// Make some requests
	rl.Allow("1.1.1.1")
	rl.Allow("2.2.2.2")
	rl.Allow("3.3.3.3")

	stats := rl.Stats()

	if stats["active_clients"].(int) != 3 {
		t.Errorf("expected 3 active clients, got %v", stats["active_clients"])
	}
	if stats["rate_per_sec"].(float64) != 100 {
		t.Errorf("expected rate 100, got %v", stats["rate_per_sec"])
	}
	if stats["burst_size"].(int) != 50 {
		t.Errorf("expected burst 50, got %v", stats["burst_size"])
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xForwarded string
		xRealIP    string
		expected   string
	}{
		{
			name:       "remote addr only",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "x-forwarded-for single",
			remoteAddr: "127.0.0.1:12345",
			xForwarded: "203.0.113.195",
			expected:   "203.0.113.195",
		},
		{
			name:       "x-forwarded-for multiple",
			remoteAddr: "127.0.0.1:12345",
			xForwarded: "203.0.113.195, 70.41.3.18, 150.172.238.178",
			expected:   "203.0.113.195",
		},
		{
			name:       "x-real-ip",
			remoteAddr: "127.0.0.1:12345",
			xRealIP:    "203.0.113.195",
			expected:   "203.0.113.195",
		},
		{
			name:       "x-forwarded-for takes precedence",
			remoteAddr: "127.0.0.1:12345",
			xForwarded: "203.0.113.195",
			xRealIP:    "10.0.0.1",
			expected:   "203.0.113.195",
		},
		{
			name:       "remote addr without port",
			remoteAddr: "192.168.1.1",
			expected:   "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwarded)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			got := getClientIP(req)
			if got != tt.expected {
				t.Errorf("getClientIP() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestWithRateLimit(t *testing.T) {
	rl := NewRateLimiter(10, 2)
	defer rl.Stop()

	handler := WithRateLimit(rl)(func(w http.ResponseWriter, r *http.Request) {
		WriteSuccess(w, "ok")
	})

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d should return 200, got %d", i+1, w.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("request 3 should return 429, got %d", w.Code)
	}

	// Check Retry-After header
	if w.Header().Get("Retry-After") != "1" {
		t.Error("expected Retry-After header")
	}
}

func TestWithRateLimit_NilLimiter(t *testing.T) {
	// When rate limiter is nil, requests should pass through
	handler := WithRateLimit(nil)(func(w http.ResponseWriter, r *http.Request) {
		WriteSuccess(w, "ok")
	})

	// Many requests should all succeed
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d should return 200 when limiter is nil, got %d", i+1, w.Code)
		}
	}
}

func TestWithRateLimit_OptionsPassThrough(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	defer rl.Stop()

	handler := WithRateLimit(rl)(func(w http.ResponseWriter, r *http.Request) {
		WriteSuccess(w, "ok")
	})

	// Exhaust the rate limit
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler(w, req)

	// OPTIONS request should pass through even when rate limited
	req = httptest.NewRequest("OPTIONS", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("OPTIONS request should bypass rate limit, got %d", w.Code)
	}
}
