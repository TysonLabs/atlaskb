package llm

import (
	"context"
	"encoding/json"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Response struct {
	Content    string `json:"content"`
	Model      string `json:"model"`
	InputTokens  int  `json:"input_tokens"`
	OutputTokens int  `json:"output_tokens"`
	StopReason   string `json:"stop_reason"`
}

type StreamChunk struct {
	Text  string
	Done  bool
	Error error
	Usage *StreamUsage // non-nil on the final chunk if the API reports usage
}

type StreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type JSONSchema struct {
	Name   string
	Schema json.RawMessage
}

type Client interface {
	Complete(ctx context.Context, model string, system string, messages []Message, maxTokens int, schema *JSONSchema) (*Response, error)
	CompleteStream(ctx context.Context, model string, system string, messages []Message, maxTokens int) (<-chan StreamChunk, error)
	GetContextWindow(ctx context.Context, model string) (int, error)
}
