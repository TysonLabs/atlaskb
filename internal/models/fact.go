package models

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

type FactStore struct {
	Pool *pgxpool.Pool
}

func (s *FactStore) Create(ctx context.Context, f *Fact) error {
	f.ID = uuid.New()
	f.CreatedAt = time.Now()
	f.UpdatedAt = f.CreatedAt

	provJSON, err := json.Marshal(f.Provenance)
	if err != nil {
		return fmt.Errorf("marshaling provenance: %w", err)
	}

	_, err = s.Pool.Exec(ctx,
		`INSERT INTO facts (id, entity_id, repo_id, claim, dimension, category, confidence, provenance, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		f.ID, f.EntityID, f.RepoID, f.Claim, f.Dimension, f.Category, f.Confidence, provJSON, f.CreatedAt, f.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting fact: %w", err)
	}
	return nil
}

func (s *FactStore) UpdateEmbedding(ctx context.Context, id uuid.UUID, embedding pgvector.Vector) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE facts SET embedding = $2, updated_at = now() WHERE id = $1`,
		id, embedding,
	)
	if err != nil {
		return fmt.Errorf("updating embedding: %w", err)
	}
	return nil
}

func (s *FactStore) GetByID(ctx context.Context, id uuid.UUID) (*Fact, error) {
	f := &Fact{}
	var provJSON []byte
	err := s.Pool.QueryRow(ctx,
		`SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at
		 FROM facts WHERE id = $1`, id,
	).Scan(&f.ID, &f.EntityID, &f.RepoID, &f.Claim, &f.Dimension, &f.Category, &f.Confidence, &provJSON, &f.SupersededBy, &f.CreatedAt, &f.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying fact: %w", err)
	}
	if err := json.Unmarshal(provJSON, &f.Provenance); err != nil {
		return nil, fmt.Errorf("unmarshaling provenance: %w", err)
	}
	return f, nil
}

func (s *FactStore) ListByEntity(ctx context.Context, entityID uuid.UUID) ([]Fact, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at
		 FROM facts WHERE entity_id = $1 AND superseded_by IS NULL ORDER BY dimension, category`, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing facts: %w", err)
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var f Fact
		var provJSON []byte
		if err := rows.Scan(&f.ID, &f.EntityID, &f.RepoID, &f.Claim, &f.Dimension, &f.Category, &f.Confidence, &provJSON, &f.SupersededBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning fact: %w", err)
		}
		if err := json.Unmarshal(provJSON, &f.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, nil
}

// ListByEntityLimited returns up to `limit` active facts for an entity.
func (s *FactStore) ListByEntityLimited(ctx context.Context, entityID uuid.UUID, limit int) ([]Fact, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at
		 FROM facts WHERE entity_id = $1 AND superseded_by IS NULL ORDER BY dimension, category LIMIT $2`, entityID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing facts: %w", err)
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var f Fact
		var provJSON []byte
		if err := rows.Scan(&f.ID, &f.EntityID, &f.RepoID, &f.Claim, &f.Dimension, &f.Category, &f.Confidence, &provJSON, &f.SupersededBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning fact: %w", err)
		}
		if err := json.Unmarshal(provJSON, &f.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, nil
}

func (s *FactStore) ListByRepoWithoutEmbedding(ctx context.Context, repoID uuid.UUID) ([]Fact, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at
		 FROM facts WHERE repo_id = $1 AND embedding IS NULL AND superseded_by IS NULL`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing facts without embedding: %w", err)
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var f Fact
		var provJSON []byte
		if err := rows.Scan(&f.ID, &f.EntityID, &f.RepoID, &f.Claim, &f.Dimension, &f.Category, &f.Confidence, &provJSON, &f.SupersededBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning fact: %w", err)
		}
		if err := json.Unmarshal(provJSON, &f.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, nil
}

// ScoredFact wraps a Fact with its cosine similarity score from vector search.
type ScoredFact struct {
	Fact
	Score float64
}

func (s *FactStore) SearchByVector(ctx context.Context, embedding pgvector.Vector, repoIDs []uuid.UUID, limit int) ([]ScoredFact, error) {
	query := `SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at,
		 1 - (embedding <=> $1) AS score
		 FROM facts WHERE embedding IS NOT NULL AND superseded_by IS NULL AND 1 - (embedding <=> $1) >= 0.4`
	args := []any{embedding}
	argIdx := 2

	if len(repoIDs) > 0 {
		query += fmt.Sprintf(" AND repo_id = ANY($%d)", argIdx)
		args = append(args, repoIDs)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY embedding <=> $1 LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close()

	var facts []ScoredFact
	for rows.Next() {
		var f ScoredFact
		var provJSON []byte
		if err := rows.Scan(&f.ID, &f.EntityID, &f.RepoID, &f.Claim, &f.Dimension, &f.Category, &f.Confidence, &provJSON, &f.SupersededBy, &f.CreatedAt, &f.UpdatedAt, &f.Score); err != nil {
			return nil, fmt.Errorf("scanning fact: %w", err)
		}
		if err := json.Unmarshal(provJSON, &f.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, nil
}

// SearchByVectorForEntity performs vector search scoped to a single entity.
// Returns up to `limit` ScoredFact results ordered by similarity. No minimum
// similarity threshold — the expansion discount handles low-relevance results.
func (s *FactStore) SearchByVectorForEntity(ctx context.Context, embedding pgvector.Vector, entityID uuid.UUID, limit int) ([]ScoredFact, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at,
		 1 - (embedding <=> $1) AS score
		 FROM facts WHERE embedding IS NOT NULL AND superseded_by IS NULL AND entity_id = $2
		 ORDER BY embedding <=> $1 LIMIT $3`,
		embedding, entityID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("vector search for entity: %w", err)
	}
	defer rows.Close()

	var facts []ScoredFact
	for rows.Next() {
		var f ScoredFact
		var provJSON []byte
		if err := rows.Scan(&f.ID, &f.EntityID, &f.RepoID, &f.Claim, &f.Dimension, &f.Category, &f.Confidence, &provJSON, &f.SupersededBy, &f.CreatedAt, &f.UpdatedAt, &f.Score); err != nil {
			return nil, fmt.Errorf("scanning fact: %w", err)
		}
		if err := json.Unmarshal(provJSON, &f.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, nil
}

// SearchByKeyword performs full-text search on fact claims using PostgreSQL ts_query.
// Returns ScoredFact with ts_rank scores for proper ranking.
func (s *FactStore) SearchByKeyword(ctx context.Context, query string, repoIDs []uuid.UUID, limit int) ([]ScoredFact, error) {
	sql := `SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at,
		 ts_rank(claim_tsv, websearch_to_tsquery('english', $1)) AS score
		 FROM facts WHERE claim_tsv @@ websearch_to_tsquery('english', $1) AND superseded_by IS NULL`
	args := []any{query}
	argIdx := 2

	if len(repoIDs) > 0 {
		sql += fmt.Sprintf(" AND repo_id = ANY($%d)", argIdx)
		args = append(args, repoIDs)
		argIdx++
	}

	sql += fmt.Sprintf(" ORDER BY score DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}
	defer rows.Close()

	var facts []ScoredFact
	for rows.Next() {
		var f ScoredFact
		var provJSON []byte
		if err := rows.Scan(&f.ID, &f.EntityID, &f.RepoID, &f.Claim, &f.Dimension, &f.Category, &f.Confidence, &provJSON, &f.SupersededBy, &f.CreatedAt, &f.UpdatedAt, &f.Score); err != nil {
			return nil, fmt.Errorf("scanning fact: %w", err)
		}
		if err := json.Unmarshal(provJSON, &f.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, nil
}

func (s *FactStore) SetSupersededBy(ctx context.Context, factID, supersededByID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE facts SET superseded_by = $2, updated_at = now() WHERE id = $1`,
		factID, supersededByID,
	)
	if err != nil {
		return fmt.Errorf("setting superseded_by: %w", err)
	}
	return nil
}

func (s *FactStore) CountByRepo(ctx context.Context, repoID uuid.UUID) (total int, byDimension map[string]int, err error) {
	byDimension = make(map[string]int)
	rows, err := s.Pool.Query(ctx,
		`SELECT dimension, COUNT(*) FROM facts WHERE repo_id = $1 AND superseded_by IS NULL GROUP BY dimension`, repoID,
	)
	if err != nil {
		return 0, nil, fmt.Errorf("counting facts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dim string
		var count int
		if err := rows.Scan(&dim, &count); err != nil {
			return 0, nil, fmt.Errorf("scanning fact count: %w", err)
		}
		byDimension[dim] = count
		total += count
	}
	return total, byDimension, nil
}

func (s *FactStore) ListByRepoAndCategory(ctx context.Context, repoID uuid.UUID, categories []string, limit int) ([]Fact, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at
		 FROM facts WHERE repo_id = $1 AND category = ANY($2)
		 AND superseded_by IS NULL AND confidence != 'low'
		 ORDER BY dimension LIMIT $3`, repoID, categories, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing facts by category: %w", err)
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var f Fact
		var provJSON []byte
		if err := rows.Scan(&f.ID, &f.EntityID, &f.RepoID, &f.Claim, &f.Dimension, &f.Category, &f.Confidence, &provJSON, &f.SupersededBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning fact: %w", err)
		}
		if err := json.Unmarshal(provJSON, &f.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, nil
}

// MaxSimilarityByEntity returns the maximum cosine similarity score between the given
// query vector and facts for each entity ID. Used for triplet-ranked search scoring.
func (s *FactStore) MaxSimilarityByEntity(ctx context.Context, queryVec pgvector.Vector, entityIDs []uuid.UUID) (map[uuid.UUID]float64, error) {
	if len(entityIDs) == 0 {
		return make(map[uuid.UUID]float64), nil
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT entity_id, MAX(1 - (embedding <=> $1)) AS score
		 FROM facts
		 WHERE entity_id = ANY($2) AND embedding IS NOT NULL AND superseded_by IS NULL
		 GROUP BY entity_id`, queryVec, entityIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("computing max similarity by entity: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]float64, len(entityIDs))
	for rows.Next() {
		var eid uuid.UUID
		var score float64
		if err := rows.Scan(&eid, &score); err != nil {
			return nil, fmt.Errorf("scanning similarity score: %w", err)
		}
		result[eid] = score
	}
	return result, nil
}

// ListByRepoAndCategoryAllRepos returns convention/pattern facts across all repos.
func (s *FactStore) ListByRepoAndCategoryAllRepos(ctx context.Context, categories []string, limit int) ([]Fact, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at
		 FROM facts WHERE category = ANY($1)
		 AND superseded_by IS NULL AND confidence != 'low'
		 ORDER BY dimension LIMIT $2`, categories, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing facts by category (all repos): %w", err)
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var f Fact
		var provJSON []byte
		if err := rows.Scan(&f.ID, &f.EntityID, &f.RepoID, &f.Claim, &f.Dimension, &f.Category, &f.Confidence, &provJSON, &f.SupersededBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning fact: %w", err)
		}
		if err := json.Unmarshal(provJSON, &f.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, nil
}

func (s *FactStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM facts WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting facts: %w", err)
	}
	return nil
}
