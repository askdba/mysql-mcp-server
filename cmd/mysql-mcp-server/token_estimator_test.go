package main

import (
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
	largeString := strings.Repeat("x", maxTokenEstimationBytes+1000)
	largePayload := map[string]string{"data": largeString}

	n, err := estimateTokensForValue(largePayload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should use heuristic: len(json) / 4
	// The JSON will be slightly larger than the string due to {"data":"..."}
	expectedMin := (maxTokenEstimationBytes + 1000) / 4
	if n < expectedMin {
		t.Errorf("expected at least %d tokens (heuristic), got %d", expectedMin, n)
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

