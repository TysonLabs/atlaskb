package embeddings

import "context"

type Client interface {
	Embed(ctx context.Context, texts []string, model string) ([][]float32, error)
}

const (
	ModelVoyageCode3  = "voyage-code-3"
	ModelVoyage3Large = "voyage-3-large"

	MaxBatchSize = 128
)
