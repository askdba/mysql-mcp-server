package main

import (
	"bytes"
	"encoding/json"
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

func TestCalculateEfficiency(t *testing.T) {
	// Enable token tracking for tests
	oldTracking := tokenTracking
	tokenTracking = true
	defer func() { tokenTracking = oldTracking }()

	tests := []struct {
		name         string
		inputTokens  int
		outputTokens int
		rowCount     int
		wantPerRow   float64
		wantIO       float64
		wantCostMin  float64
		wantCostMax  float64
	}{
		{
			name:         "typical query",
			inputTokens:  17,
			outputTokens: 50,
			rowCount:     5,
			wantPerRow:   10.0,
			wantIO:       2.94,
			wantCostMin:  0.0,
			wantCostMax:  0.001,
		},
		{
			name:         "large result",
			inputTokens:  100,
			outputTokens: 2500,
			rowCount:     10,
			wantPerRow:   250.0,
			wantIO:       25.0,
			wantCostMin:  0.02,
			wantCostMax:  0.03,
		},
		{
			name:         "zero rows",
			inputTokens:  50,
			outputTokens: 10,
			rowCount:     0,
			wantPerRow:   0.0, // should be 0 when no rows
			wantIO:       0.2,
			wantCostMin:  0.0,
			wantCostMax:  0.001,
		},
		{
			name:         "zero input",
			inputTokens:  0,
			outputTokens: 100,
			rowCount:     5,
			wantPerRow:   20.0,
			wantIO:       0.0, // should be 0 when no input
			wantCostMin:  0.0,
			wantCostMax:  0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eff := CalculateEfficiency(tt.inputTokens, tt.outputTokens, tt.rowCount)
			if eff == nil {
				t.Fatal("CalculateEfficiency returned nil")
			}

			if eff.TokensPerRow != tt.wantPerRow {
				t.Errorf("TokensPerRow = %v, want %v", eff.TokensPerRow, tt.wantPerRow)
			}

			if eff.IOEfficiency != tt.wantIO {
				t.Errorf("IOEfficiency = %v, want %v", eff.IOEfficiency, tt.wantIO)
			}

			if eff.CostEstimateUSD < tt.wantCostMin || eff.CostEstimateUSD > tt.wantCostMax {
				t.Errorf("CostEstimateUSD = %v, want between %v and %v",
					eff.CostEstimateUSD, tt.wantCostMin, tt.wantCostMax)
			}
		})
	}
}

func TestCalculateEfficiencyDisabled(t *testing.T) {
	// Disable token tracking
	oldTracking := tokenTracking
	tokenTracking = false
	defer func() { tokenTracking = oldTracking }()

	eff := CalculateEfficiency(100, 500, 10)
	if eff != nil {
		t.Error("expected nil when token tracking is disabled")
	}
}

func TestTokenEfficiencyStruct(t *testing.T) {
	eff := TokenEfficiency{
		TokensPerRow:    10.5,
		IOEfficiency:    2.5,
		CostEstimateUSD: 0.000125,
	}

	// Verify JSON marshaling
	data, err := json.Marshal(eff)
	if err != nil {
		t.Fatalf("failed to marshal TokenEfficiency: %v", err)
	}

	var parsed TokenEfficiency
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal TokenEfficiency: %v", err)
	}

	if parsed.TokensPerRow != eff.TokensPerRow {
		t.Errorf("TokensPerRow mismatch: got %v, want %v", parsed.TokensPerRow, eff.TokensPerRow)
	}
	if parsed.IOEfficiency != eff.IOEfficiency {
		t.Errorf("IOEfficiency mismatch: got %v, want %v", parsed.IOEfficiency, eff.IOEfficiency)
	}
	if parsed.CostEstimateUSD != eff.CostEstimateUSD {
		t.Errorf("CostEstimateUSD mismatch: got %v, want %v", parsed.CostEstimateUSD, eff.CostEstimateUSD)
	}
}
