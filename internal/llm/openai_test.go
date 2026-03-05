package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOpenAIClientComplete(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got == "Bearer test-key" {
			sawAuth = true
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Fatalf("unexpected messages payload: %#v", req.Messages)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"content": "hello"},
					"finish_reason": "stop",
				},
			},
			"model": "qwen-test",
			"usage": map[string]any{
				"prompt_tokens":     12,
				"completion_tokens": 7,
			},
		})
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "test-key")
	c.maxRetries = 1
	resp, err := c.Complete(context.Background(), "qwen-test", "sys", []Message{{Role: "user", Content: "hi"}}, 128, nil)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "hello" || resp.Model != "qwen-test" || resp.InputTokens != 12 || resp.OutputTokens != 7 || resp.StopReason != "stop" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if !sawAuth {
		t.Fatal("authorization header not sent")
	}
}

func TestOpenAIClientCompleteHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "")
	c.maxRetries = 1
	_, err := c.Complete(context.Background(), "model", "", nil, 0, nil)
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("Complete() error = %v, want HTTP status error", err)
	}
}

func TestOpenAIClientCompleteStream(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"lo"}}]}`,
		``,
		`data: {"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(stream))
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "")
	ch, err := c.CompleteStream(context.Background(), "model", "", []Message{{Role: "user", Content: "x"}}, 64)
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}

	var text strings.Builder
	var done bool
	var usage *StreamUsage
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream chunk error: %v", chunk.Error)
		}
		if chunk.Text != "" {
			text.WriteString(chunk.Text)
		}
		if chunk.Done {
			done = true
			usage = chunk.Usage
		}
	}

	if text.String() != "Hello" {
		t.Fatalf("streamed text = %q, want Hello", text.String())
	}
	if !done {
		t.Fatal("did not receive done chunk")
	}
	if usage == nil || usage.PromptTokens != 5 || usage.CompletionTokens != 2 {
		t.Fatalf("usage = %+v, want prompt=5 completion=2", usage)
	}
}

func TestOpenAIClientGetContextWindowCaches(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "model-a", "max_model_len": 8192},
				{"id": "model-b", "max_model_len": 32768},
			},
		})
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "")
	got, err := c.GetContextWindow(context.Background(), "model-b")
	if err != nil {
		t.Fatalf("GetContextWindow() error = %v", err)
	}
	if got != 32768 {
		t.Fatalf("GetContextWindow() = %d, want 32768", got)
	}

	got, err = c.GetContextWindow(context.Background(), "model-b")
	if err != nil {
		t.Fatalf("GetContextWindow() cached error = %v", err)
	}
	if got != 32768 {
		t.Fatalf("cached GetContextWindow() = %d, want 32768", got)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("/v1/models calls = %d, want 1", calls)
	}
}

func TestOpenAIClientGetContextWindowFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "model-a", "max_model_len": 16384},
			},
		})
	}))
	defer srv.Close()

	c := NewOpenAIClient(srv.URL, "")
	got, err := c.GetContextWindow(context.Background(), "unknown-model")
	if err != nil {
		t.Fatalf("GetContextWindow() error = %v", err)
	}
	if got != 16384 {
		t.Fatalf("fallback GetContextWindow() = %d, want 16384", got)
	}
}
