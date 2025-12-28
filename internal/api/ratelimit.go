// internal/api/ratelimit.go
package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter with per-IP tracking.
type RateLimiter struct {
	mu       sync.RWMutex
	buckets  map[string]*bucket
	rate     float64       // tokens per second
	burst    int           // max tokens (bucket size)
	cleanup  time.Duration // how often to clean up old buckets
	stopChan chan struct{}
}

// bucket represents a token bucket for a single client.
type bucket struct {
	tokens     float64
	lastUpdate time.Time
}

// NewRateLimiter creates a new rate limiter.
// rate: requests per second allowed
// burst: maximum burst size (bucket capacity)
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:  make(map[string]*bucket),
		rate:     rate,
		burst:    burst,
		cleanup:  5 * time.Minute,
		stopChan: make(chan struct{}),
	}

	// Start background cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request from the given IP is allowed.
// Returns true if allowed, false if rate limited.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	b, exists := rl.buckets[ip]
	if !exists {
		// New client, create bucket with full tokens
		rl.buckets[ip] = &bucket{
			tokens:     float64(rl.burst) - 1, // consume one token
			lastUpdate: now,
		}
		return true
	}

	// Calculate tokens to add based on time elapsed
	elapsed := now.Sub(b.lastUpdate).Seconds()
	b.tokens += elapsed * rl.rate
	b.lastUpdate = now

	// Cap tokens at burst limit
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}

	// Check if we have at least one token
	if b.tokens >= 1 {
		b.tokens--
		return true
	}

	return false
}

// cleanupLoop periodically removes stale buckets to prevent memory leaks.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup_stale()
		case <-rl.stopChan:
			return
		}
	}
}

// cleanup_stale removes buckets that haven't been used recently.
func (rl *RateLimiter) cleanup_stale() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	threshold := time.Now().Add(-rl.cleanup)
	for ip, b := range rl.buckets {
		if b.lastUpdate.Before(threshold) {
			delete(rl.buckets, ip)
		}
	}
}

// Stop stops the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopChan)
}

// Stats returns current rate limiter statistics.
func (rl *RateLimiter) Stats() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return map[string]interface{}{
		"active_clients": len(rl.buckets),
		"rate_per_sec":   rl.rate,
		"burst_size":     rl.burst,
	}
}

// getClientIP extracts the client IP from the request.
// It checks X-Forwarded-For and X-Real-IP headers first (for reverse proxies),
// then falls back to RemoteAddr.
func getClientIP(r *http.Request) string {
	remoteIPStr := strings.TrimSpace(r.RemoteAddr)
	remoteIP := net.ParseIP(remoteIPStr)
	if remoteIP == nil {
		// Fall back to RemoteAddr parsing (common "ip:port" case)
		if host, _, err := net.SplitHostPort(remoteIPStr); err == nil {
			remoteIP = net.ParseIP(host)
		}
	}

	// Only trust proxy headers when the direct peer is loopback.
	// Otherwise, clients can spoof X-Forwarded-For/X-Real-IP to bypass per-IP
	// rate limits and cause unbounded bucket growth.
	if remoteIP != nil && remoteIP.IsLoopback() {
		// Check X-Forwarded-For header (may contain multiple IPs)
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			first := xff
			if idx := strings.IndexByte(xff, ','); idx >= 0 {
				first = xff[:idx]
			}
			first = strings.TrimSpace(first)
			if ip := net.ParseIP(first); ip != nil {
				return first
			}
		}

		// Check X-Real-IP header
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			if ip := net.ParseIP(xri); ip != nil {
				return xri
			}
		}
	}

	// Fall back to RemoteAddr (best-effort)
	if remoteIP != nil {
		return remoteIP.String()
	}
	if host, _, err := net.SplitHostPort(remoteIPStr); err == nil {
		return host
	}
	return remoteIPStr
}

// WithRateLimit returns middleware that applies rate limiting.
// If the rate limiter is nil, requests pass through without limiting.
func WithRateLimit(rl *RateLimiter) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting if limiter is nil or for OPTIONS requests
			if rl == nil || r.Method == "OPTIONS" {
				next(w, r)
				return
			}

			ip := getClientIP(r)
			if !rl.Allow(ip) {
				w.Header().Set("Retry-After", "1")
				WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}

			next(w, r)
		}
	}
}
