package embeddings

import "context"

type Client interface {
	Embed(ctx context.Context, texts []string, model string) ([][]float32, error)
}

const (
	DefaultModel = "mxbai-embed-large-v1"

	MaxBatchSize = 128
)
