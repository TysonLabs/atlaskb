package llm

import "context"

type MockClient struct {
	CompleteFunc       func(ctx context.Context, model string, system string, messages []Message, maxTokens int, schema *JSONSchema) (*Response, error)
	CompleteStreamFunc func(ctx context.Context, model string, system string, messages []Message, maxTokens int) (<-chan StreamChunk, error)
}

func (m *MockClient) Complete(ctx context.Context, model string, system string, messages []Message, maxTokens int, schema *JSONSchema) (*Response, error) {
	if m.CompleteFunc != nil {
		return m.CompleteFunc(ctx, model, system, messages, maxTokens, schema)
	}
	return &Response{
		Content:      "{}",
		Model:        model,
		InputTokens:  100,
		OutputTokens: 50,
		StopReason:   "end_turn",
	}, nil
}

func (m *MockClient) CompleteStream(ctx context.Context, model string, system string, messages []Message, maxTokens int) (<-chan StreamChunk, error) {
	if m.CompleteStreamFunc != nil {
		return m.CompleteStreamFunc(ctx, model, system, messages, maxTokens)
	}
	ch := make(chan StreamChunk, 2)
	ch <- StreamChunk{Text: "mock response"}
	ch <- StreamChunk{Done: true}
	close(ch)
	return ch, nil
}
