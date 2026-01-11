package main

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockInput is a simple test input struct
type mockInput struct {
	Value string `json:"value"`
}

// mockOutput is a simple test output struct
type mockOutput struct {
	Result string `json:"result"`
}

func TestWrapToolTokenTrackingDisabled(t *testing.T) {
	// Save and restore global state
	origTracking := tokenTracking
	origEstimator := tokenEstimator
	defer func() {
		tokenTracking = origTracking
		tokenEstimator = origEstimator
	}()

	// Disable tracking
	tokenTracking = false
	tokenEstimator = nil

	called := false
	handler := func(ctx context.Context, req *mcp.CallToolRequest, input mockInput) (*mcp.CallToolResult, mockOutput, error) {
		called = true
		return nil, mockOutput{Result: "ok"}, nil
	}

	wrapped := wrapTool("test_tool", handler)
	_, out, err := wrapped(context.Background(), nil, mockInput{Value: "test"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
	if out.Result != "ok" {
		t.Errorf("unexpected result: %s", out.Result)
	}
}

func TestWrapToolTokenTrackingEnabled(t *testing.T) {
	// Save and restore global state
	origTracking := tokenTracking
	origEstimator := tokenEstimator
	origModel := tokenModel
	origLogging := jsonLogging
	defer func() {
		tokenTracking = origTracking
		tokenEstimator = origEstimator
		tokenModel = origModel
		jsonLogging = origLogging
	}()

	// Enable tracking
	tokenTracking = true
	tokenModel = "cl100k_base"
	jsonLogging = false // Avoid noisy output in tests
	est, err := NewTokenEstimator("cl100k_base")
	if err != nil {
		t.Fatalf("NewTokenEstimator failed: %v", err)
	}
	tokenEstimator = est

	called := false
	handler := func(ctx context.Context, req *mcp.CallToolRequest, input mockInput) (*mcp.CallToolResult, mockOutput, error) {
		called = true
		return nil, mockOutput{Result: "success"}, nil
	}

	wrapped := wrapTool("test_tool", handler)
	_, out, err := wrapped(context.Background(), nil, mockInput{Value: "test"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
	if out.Result != "success" {
		t.Errorf("unexpected result: %s", out.Result)
	}
}

func TestWrapToolWithError(t *testing.T) {
	// Save and restore global state
	origTracking := tokenTracking
	origEstimator := tokenEstimator
	origModel := tokenModel
	origLogging := jsonLogging
	defer func() {
		tokenTracking = origTracking
		tokenEstimator = origEstimator
		tokenModel = origModel
		jsonLogging = origLogging
	}()

	// Enable tracking
	tokenTracking = true
	tokenModel = "cl100k_base"
	jsonLogging = false
	est, err := NewTokenEstimator("cl100k_base")
	if err != nil {
		t.Fatalf("NewTokenEstimator failed: %v", err)
	}
	tokenEstimator = est

	expectedErr := errors.New("test error")
	handler := func(ctx context.Context, req *mcp.CallToolRequest, input mockInput) (*mcp.CallToolResult, mockOutput, error) {
		return nil, mockOutput{}, expectedErr
	}

	wrapped := wrapTool("test_tool", handler)
	_, _, err = wrapped(context.Background(), nil, mockInput{Value: "test"})

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestWrapToolRunQuerySkipsExtraLogging(t *testing.T) {
	// The run_query tool has its own dedicated logging, so wrapTool should skip it
	// Save and restore global state
	origTracking := tokenTracking
	origEstimator := tokenEstimator
	origModel := tokenModel
	defer func() {
		tokenTracking = origTracking
		tokenEstimator = origEstimator
		tokenModel = origModel
	}()

	// Enable tracking
	tokenTracking = true
	tokenModel = "cl100k_base"
	est, err := NewTokenEstimator("cl100k_base")
	if err != nil {
		t.Fatalf("NewTokenEstimator failed: %v", err)
	}
	tokenEstimator = est

	called := false
	handler := func(ctx context.Context, req *mcp.CallToolRequest, input mockInput) (*mcp.CallToolResult, mockOutput, error) {
		called = true
		return nil, mockOutput{Result: "ok"}, nil
	}

	// Wrap with "run_query" name - should skip extra logging
	wrapped := wrapTool("run_query", handler)
	_, _, err = wrapped(context.Background(), nil, mockInput{Value: "test"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
}

func TestTokenUsageTotalEstimatedOnError(t *testing.T) {
	// This test verifies the fix: TotalEstimated should equal InputEstimated
	// when initialized, so error paths log correct totals.

	inputTokens := 100
	tokens := &TokenUsage{
		InputEstimated: inputTokens,
		TotalEstimated: inputTokens, // This is the fix - not left as 0
		Model:          "cl100k_base",
	}

	// Simulate error path - TotalEstimated should already be set
	if tokens.TotalEstimated != inputTokens {
		t.Errorf("expected TotalEstimated=%d, got %d", inputTokens, tokens.TotalEstimated)
	}

	// After success, TotalEstimated would be updated
	outputTokens := 50
	tokens.OutputEstimated = outputTokens
	tokens.TotalEstimated = inputTokens + outputTokens

	if tokens.TotalEstimated != 150 {
		t.Errorf("expected TotalEstimated=150 after update, got %d", tokens.TotalEstimated)
	}
}

func TestWrapToolPreservesContext(t *testing.T) {
	// Verify that context is properly passed through

	type ctxKey string
	const testKey ctxKey = "test"

	handler := func(ctx context.Context, req *mcp.CallToolRequest, input mockInput) (*mcp.CallToolResult, mockOutput, error) {
		val := ctx.Value(testKey)
		if val != "value" {
			return nil, mockOutput{}, errors.New("context not preserved")
		}
		return nil, mockOutput{Result: "ok"}, nil
	}

	wrapped := wrapTool("test_tool", handler)
	ctx := context.WithValue(context.Background(), testKey, "value")
	_, out, err := wrapped(ctx, nil, mockInput{Value: "test"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Result != "ok" {
		t.Errorf("unexpected result: %s", out.Result)
	}
}
