package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// TokenEstimator counts tokens for a given text.
// This is intentionally small so we can swap implementations later if needed.
type TokenEstimator interface {
	Model() string
	Count(text string) (int, error)
}

type tiktokenEstimator struct {
	model string
	mu    sync.Mutex
	enc   *tiktoken.Tiktoken
}

func (e *tiktokenEstimator) Model() string { return e.model }

func (e *tiktokenEstimator) Count(text string) (int, error) {
	// tiktoken-go encoders are not documented as goroutine-safe; protect just in case.
	e.mu.Lock()
	defer e.mu.Unlock()

	toks := e.enc.Encode(text, nil, nil)
	return len(toks), nil
}

func NewTokenEstimator(model string) (TokenEstimator, error) {
	if model == "" {
		model = "cl100k_base"
	}
	enc, err := tiktoken.GetEncoding(model)
	if err != nil {
		return nil, fmt.Errorf("get encoding %q: %w", model, err)
	}
	return &tiktokenEstimator{model: model, enc: enc}, nil
}

type TokenUsage struct {
	InputEstimated  int    `json:"input_estimated"`
	OutputEstimated int    `json:"output_estimated"`
	TotalEstimated  int    `json:"total_estimated"`
	Model           string `json:"model,omitempty"`
}

// TokenEfficiency holds calculated efficiency metrics for token usage.
type TokenEfficiency struct {
	TokensPerRow    float64 `json:"tokens_per_row,omitempty"`
	IOEfficiency    float64 `json:"io_efficiency,omitempty"`
	CostEstimateUSD float64 `json:"cost_estimate_usd,omitempty"`
}

// Pricing per 1M tokens (GPT-4o as reference, Dec 2024)
const (
	costPerMillionInputTokens  = 2.50  // $2.50 per 1M input tokens
	costPerMillionOutputTokens = 10.00 // $10.00 per 1M output tokens
)

// CalculateEfficiency computes token efficiency metrics.
// Returns nil if token tracking is disabled or no meaningful data.
func CalculateEfficiency(inputTokens, outputTokens, rowCount int) *TokenEfficiency {
	if !tokenTracking {
		return nil
	}

	eff := &TokenEfficiency{}

	// Tokens per row (only if we have rows)
	if rowCount > 0 {
		eff.TokensPerRow = float64(outputTokens) / float64(rowCount)
		// Round to 2 decimal places
		eff.TokensPerRow = float64(int(eff.TokensPerRow*100)) / 100
	}

	// IO Efficiency ratio (output/input, higher = more data per token spent)
	if inputTokens > 0 {
		eff.IOEfficiency = float64(outputTokens) / float64(inputTokens)
		eff.IOEfficiency = float64(int(eff.IOEfficiency*100)) / 100
	}

	// Cost estimate in USD
	inputCost := float64(inputTokens) / 1_000_000 * costPerMillionInputTokens
	outputCost := float64(outputTokens) / 1_000_000 * costPerMillionOutputTokens
	eff.CostEstimateUSD = inputCost + outputCost
	// Round to 6 decimal places for micro-costs
	eff.CostEstimateUSD = float64(int(eff.CostEstimateUSD*1_000_000)) / 1_000_000

	return eff
}

const (
	// Keep estimation bounded so we don't accidentally serialize huge payloads.
	// This is only for *estimation*, not a hard limit on tool behavior.
	maxTokenEstimationBytes = 1 << 20 // 1 MiB
)

// errLimitExceeded is returned by limitedWriter when the cap is hit.
var errLimitExceeded = errors.New("size limit exceeded")

// limitedWriter wraps a bytes.Buffer and stops writing once the limit is reached.
type limitedWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		// Write only up to the limit, then return error to stop encoder.
		remaining := w.limit - w.buf.Len()
		if remaining > 0 {
			w.buf.Write(p[:remaining])
		}
		return len(p), errLimitExceeded
	}
	return w.buf.Write(p)
}

func estimateTokensForValue(v any) (int, error) {
	if !tokenTracking || tokenEstimator == nil {
		return 0, nil
	}

	// Use a size-limited writer to avoid allocating huge buffers for large payloads.
	buf := &bytes.Buffer{}
	lw := &limitedWriter{buf: buf, limit: maxTokenEstimationBytes}
	enc := json.NewEncoder(lw)

	err := enc.Encode(v)
	if errors.Is(err, errLimitExceeded) {
		// Payload exceeded the cap; use heuristic based on the limit.
		// We know it's at least maxTokenEstimationBytes, so estimate from that.
		return maxTokenEstimationBytes / 4, nil
	}
	if err != nil {
		return 0, err
	}

	return tokenEstimator.Count(buf.String())
}
