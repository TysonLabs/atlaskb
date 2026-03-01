package pipeline

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tgeorge06/atlaskb/internal/llm"
)

// LLMRetryConfig controls retry behavior for LLM calls.
type LLMRetryConfig struct {
	MaxRetries int           // Maximum number of attempts (default 3)
	BaseDelay  time.Duration // Initial delay between retries (default 1s)
}

var DefaultRetryConfig = LLMRetryConfig{
	MaxRetries: 3,
	BaseDelay:  time.Second,
}

// callLLMWithRetry calls the LLM with exponential backoff on failure.
// Returns the response, number of attempts made, and any error.
func callLLMWithRetry(ctx context.Context, client llm.Client, model, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema, retryCfg LLMRetryConfig) (*llm.Response, int, error) {
	if retryCfg.MaxRetries <= 0 {
		retryCfg.MaxRetries = DefaultRetryConfig.MaxRetries
	}
	if retryCfg.BaseDelay <= 0 {
		retryCfg.BaseDelay = DefaultRetryConfig.BaseDelay
	}

	var lastErr error
	for attempt := 1; attempt <= retryCfg.MaxRetries; attempt++ {
		resp, err := client.Complete(ctx, model, system, messages, maxTokens, schema)
		if err == nil {
			return resp, attempt, nil
		}

		lastErr = err
		if attempt < retryCfg.MaxRetries {
			delay := retryCfg.BaseDelay * time.Duration(1<<(attempt-1)) // 1s, 2s, 4s
			log.Printf("[retry] LLM attempt %d/%d failed: %v, retrying in %s", attempt, retryCfg.MaxRetries, err, delay)

			select {
			case <-ctx.Done():
				return nil, attempt, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return nil, retryCfg.MaxRetries, fmt.Errorf("LLM call failed after %d attempts: %w", retryCfg.MaxRetries, lastErr)
}
