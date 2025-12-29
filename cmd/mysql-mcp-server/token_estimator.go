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
