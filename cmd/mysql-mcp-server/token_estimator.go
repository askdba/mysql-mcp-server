package main

import (
	"encoding/json"
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

const (
	// Keep estimation bounded so we don't accidentally serialize huge payloads.
	// This is only for *estimation*, not a hard limit on tool behavior.
	maxTokenEstimationBytes = 1 << 20 // 1 MiB
)

func estimateTokensForValue(v any) (int, error) {
	if !tokenTracking || tokenEstimator == nil {
		return 0, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	if len(b) > maxTokenEstimationBytes {
		// If it's too large to safely estimate with tokenizer, fall back to a
		// conservative heuristic: ~4 bytes per token.
		return len(b) / 4, nil
	}
	return tokenEstimator.Count(string(b))
}

