package llm

import "context"

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
}

type Client interface {
	Complete(ctx context.Context, model string, system string, messages []Message, maxTokens int) (*Response, error)
	CompleteStream(ctx context.Context, model string, system string, messages []Message, maxTokens int) (<-chan StreamChunk, error)
}
