package main

import "testing"

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

