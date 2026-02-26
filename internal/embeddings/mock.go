package embeddings

import "context"

type MockClient struct {
	EmbedFunc func(ctx context.Context, texts []string, model string) ([][]float32, error)
}

func (m *MockClient) Embed(ctx context.Context, texts []string, model string) ([][]float32, error) {
	if m.EmbedFunc != nil {
		return m.EmbedFunc(ctx, texts, model)
	}
	// Return zero vectors of dimension 1024
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = make([]float32, 1024)
	}
	return result, nil
}
