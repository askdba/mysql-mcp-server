package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewTokenEstimatorAndCount(t *testing.T) {
	est, err := NewTokenEstimator("cl100k_base")
	if err != nil {
		t.Fatalf("NewTokenEstimator failed: %v", err)
	}
	n, err := est.Count(`{"hello":"world"}`)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if n <= 0 {
		t.Fatalf("expected token count > 0, got %d", n)
	}
}

func TestNewTokenEstimatorDefaultModel(t *testing.T) {
	// Empty model should default to cl100k_base
	est, err := NewTokenEstimator("")
	if err != nil {
		t.Fatalf("NewTokenEstimator with empty model failed: %v", err)
	}
	if est.Model() != "cl100k_base" {
		t.Errorf("expected model 'cl100k_base', got %q", est.Model())
	}
}

func TestNewTokenEstimatorInvalidModel(t *testing.T) {
	_, err := NewTokenEstimator("invalid_model_xyz")
	if err == nil {
		t.Fatal("expected error for invalid model, got nil")
	}
}

func TestTokenEstimatorModel(t *testing.T) {
	est, err := NewTokenEstimator("cl100k_base")
	if err != nil {
		t.Fatalf("NewTokenEstimator failed: %v", err)
	}
	if est.Model() != "cl100k_base" {
		t.Errorf("expected model 'cl100k_base', got %q", est.Model())
	}
}

func TestTokenEstimatorVariousInputs(t *testing.T) {
	est, err := NewTokenEstimator("cl100k_base")
	if err != nil {
		t.Fatalf("NewTokenEstimator failed: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		minCount int
	}{
		{"empty string", "", 0},
		{"single word", "hello", 1},
		{"sentence", "The quick brown fox jumps over the lazy dog.", 5},
		{"SQL query", "SELECT id, name, email FROM users WHERE active = 1 LIMIT 100", 10},
		{"JSON object", `{"database":"testdb","tables":["users","orders"],"count":42}`, 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := est.Count(tc.input)
			if err != nil {
				t.Fatalf("Count failed: %v", err)
			}
			if n < tc.minCount {
				t.Errorf("expected at least %d tokens, got %d", tc.minCount, n)
			}
		})
	}
}

func TestEstimateTokensForValueDisabled(t *testing.T) {
	// Save and restore global state
	origTracking := tokenTracking
	origEstimator := tokenEstimator
	defer func() {
		tokenTracking = origTracking
		tokenEstimator = origEstimator
	}()

	// When tracking is disabled, should return 0
	tokenTracking = false
	tokenEstimator = nil

	n, err := estimateTokensForValue(map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 tokens when tracking disabled, got %d", n)
	}
}

func TestEstimateTokensForValueEnabled(t *testing.T) {
	// Save and restore global state
	origTracking := tokenTracking
	origEstimator := tokenEstimator
	defer func() {
		tokenTracking = origTracking
		tokenEstimator = origEstimator
	}()

	// Enable tracking
	tokenTracking = true
	est, err := NewTokenEstimator("cl100k_base")
	if err != nil {
		t.Fatalf("NewTokenEstimator failed: %v", err)
	}
	tokenEstimator = est

	// Test with a simple value
	n, err := estimateTokensForValue(map[string]interface{}{
		"database": "testdb",
		"tables":   []string{"users", "orders"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n <= 0 {
		t.Errorf("expected positive token count, got %d", n)
	}
}

func TestEstimateTokensForValueLargePayload(t *testing.T) {
	// Save and restore global state
	origTracking := tokenTracking
	origEstimator := tokenEstimator
	defer func() {
		tokenTracking = origTracking
		tokenEstimator = origEstimator
	}()

	// Enable tracking
	tokenTracking = true
	est, err := NewTokenEstimator("cl100k_base")
	if err != nil {
		t.Fatalf("NewTokenEstimator failed: %v", err)
	}
	tokenEstimator = est

	// Create a payload larger than maxTokenEstimationBytes (1MB)
	// The function should fall back to heuristic (~4 bytes per token)
	// The limited writer caps allocation at maxTokenEstimationBytes.
	largeString := strings.Repeat("x", maxTokenEstimationBytes+1000)
	largePayload := map[string]string{"data": largeString}

	n, err := estimateTokensForValue(largePayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should use heuristic based on the cap: maxTokenEstimationBytes / 4
	// Since we stop serializing early, we don't know the actual size.
	expected := maxTokenEstimationBytes / 4
	if n != expected {
		t.Errorf("expected %d tokens (heuristic from cap), got %d", expected, n)
	}
}

func TestTokenUsageStruct(t *testing.T) {
	usage := TokenUsage{
		InputEstimated:  100,
		OutputEstimated: 200,
		TotalEstimated:  300,
		Model:           "cl100k_base",
	}

	if usage.InputEstimated != 100 {
		t.Errorf("expected InputEstimated=100, got %d", usage.InputEstimated)
	}
	if usage.OutputEstimated != 200 {
		t.Errorf("expected OutputEstimated=200, got %d", usage.OutputEstimated)
	}
	if usage.TotalEstimated != 300 {
		t.Errorf("expected TotalEstimated=300, got %d", usage.TotalEstimated)
	}
	if usage.Model != "cl100k_base" {
		t.Errorf("expected Model='cl100k_base', got %q", usage.Model)
	}
}

func TestLimitedWriterStopsEarly(t *testing.T) {
	buf := &bytes.Buffer{}
	lw := &limitedWriter{buf: buf, limit: 100}

	// Write data that fits within the limit
	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error for small write: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes written, got %d", n)
	}
	if buf.Len() != 5 {
		t.Errorf("expected buffer len 5, got %d", buf.Len())
	}

	// Write data that exceeds the limit
	largeData := strings.Repeat("x", 200)
	_, err = lw.Write([]byte(largeData))
	if err != errLimitExceeded {
		t.Fatalf("expected errLimitExceeded, got %v", err)
	}

	// Buffer should be capped at the limit
	if buf.Len() != 100 {
		t.Errorf("expected buffer capped at 100, got %d", buf.Len())
	}
}

func TestLimitedWriterNoAllocationBeyondCap(t *testing.T) {
	// This test verifies that the buffer doesn't grow beyond the limit,
	// preventing memory spikes for large payloads.
	buf := &bytes.Buffer{}
	limit := 1024 // 1KB limit
	lw := &limitedWriter{buf: buf, limit: limit}

	// Try to write 10KB of data in chunks
	chunk := strings.Repeat("a", 500)
	for i := 0; i < 20; i++ {
		_, err := lw.Write([]byte(chunk))
		if err == errLimitExceeded {
			break
		}
	}

	// Buffer should never exceed the limit
	if buf.Len() > limit {
		t.Errorf("buffer exceeded limit: got %d, limit %d", buf.Len(), limit)
	}
}
