package query

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type SearchResult struct {
	Fact   models.Fact   `json:"fact"`
	Entity models.Entity `json:"entity"`
	Score  float64       `json:"score"`
}

type Engine struct {
	Pool     *pgxpool.Pool
	Embedder embeddings.Client
}

func NewEngine(pool *pgxpool.Pool, embedder embeddings.Client) *Engine {
	return &Engine{Pool: pool, Embedder: embedder}
}

func (e *Engine) Search(ctx context.Context, question string, repoIDs []uuid.UUID, limit int) ([]SearchResult, error) {
	if limit == 0 {
		limit = 20
	}

	// Embed the question
	vectors, err := e.Embedder.Embed(ctx, []string{question}, embeddings.DefaultModel)
	if err != nil {
		return nil, fmt.Errorf("embedding question: %w", err)
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	queryVec := pgvector.NewVector(vectors[0])

	// Vector similarity search
	factStore := &models.FactStore{Pool: e.Pool}
	facts, err := factStore.SearchByVector(ctx, queryVec, repoIDs, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	// Enrich with entity data
	entityStore := &models.EntityStore{Pool: e.Pool}
	var results []SearchResult
	for _, f := range facts {
		entity, err := entityStore.GetByID(ctx, f.EntityID)
		if err != nil || entity == nil {
			continue
		}
		results = append(results, SearchResult{
			Fact:   f,
			Entity: *entity,
		})
	}

	return results, nil
}
