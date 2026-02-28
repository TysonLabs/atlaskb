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

func (s *FactStore) SearchByVector(ctx context.Context, embedding pgvector.Vector, repoIDs []uuid.UUID, limit int) ([]Fact, error) {
	query := `SELECT id, entity_id, repo_id, claim, dimension, category, confidence, provenance, superseded_by, created_at, updated_at
		 FROM facts WHERE embedding IS NOT NULL AND superseded_by IS NULL`
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

func (s *FactStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM facts WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting facts: %w", err)
	}
	return nil
}
