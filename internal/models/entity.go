package models

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EntityStore struct {
	Pool *pgxpool.Pool
}

func (s *EntityStore) Create(ctx context.Context, e *Entity) error {
	e.ID = uuid.New()
	e.CreatedAt = time.Now()
	e.UpdatedAt = e.CreatedAt

	_, err := s.Pool.Exec(ctx,
		`INSERT INTO entities (id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		e.ID, e.RepoID, e.Kind, e.Name, e.QualifiedName, e.Path, e.Summary, e.Capabilities, e.Assumptions, e.CreatedAt, e.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting entity: %w", err)
	}
	return nil
}

func (s *EntityStore) Upsert(ctx context.Context, e *Entity) error {
	now := time.Now()
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO entities (id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (repo_id, qualified_name) DO UPDATE SET
		   summary = COALESCE(EXCLUDED.summary, entities.summary),
		   capabilities = EXCLUDED.capabilities,
		   assumptions = EXCLUDED.assumptions,
		   updated_at = EXCLUDED.updated_at
		 RETURNING id`,
		uuid.New(), e.RepoID, e.Kind, e.Name, e.QualifiedName, e.Path, e.Summary, e.Capabilities, e.Assumptions, now, now,
	).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("upserting entity: %w", err)
	}
	e.UpdatedAt = now
	return nil
}

func (s *EntityStore) FindByNameAndKind(ctx context.Context, repoID uuid.UUID, name, kind string) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND name = $2 AND kind = $3`, repoID, name, kind,
	)
	if err != nil {
		return nil, fmt.Errorf("finding entities by name/kind: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) Update(ctx context.Context, e *Entity) error {
	e.UpdatedAt = time.Now()
	_, err := s.Pool.Exec(ctx,
		`UPDATE entities SET summary = $2, capabilities = $3, assumptions = $4, updated_at = $5 WHERE id = $1`,
		e.ID, e.Summary, e.Capabilities, e.Assumptions, e.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("updating entity: %w", err)
	}
	return nil
}

func (s *EntityStore) GetByID(ctx context.Context, id uuid.UUID) (*Entity, error) {
	e := &Entity{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, created_at, updated_at
		 FROM entities WHERE id = $1`, id,
	).Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.CreatedAt, &e.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying entity: %w", err)
	}
	return e, nil
}

func (s *EntityStore) ListByRepo(ctx context.Context, repoID uuid.UUID) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, created_at, updated_at
		 FROM entities WHERE repo_id = $1 ORDER BY qualified_name`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing entities: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) ListByRepoAndKind(ctx context.Context, repoID uuid.UUID, kind string) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND kind = $2 ORDER BY qualified_name`, repoID, kind,
	)
	if err != nil {
		return nil, fmt.Errorf("listing entities by kind: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) FindByQualifiedName(ctx context.Context, repoID uuid.UUID, qualifiedName string) (*Entity, error) {
	e := &Entity{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND qualified_name = $2`, repoID, qualifiedName,
	).Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.CreatedAt, &e.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding entity: %w", err)
	}
	return e, nil
}

func (s *EntityStore) CountByRepo(ctx context.Context, repoID uuid.UUID) (total int, byKind map[string]int, err error) {
	byKind = make(map[string]int)
	rows, err := s.Pool.Query(ctx,
		`SELECT kind, COUNT(*) FROM entities WHERE repo_id = $1 GROUP BY kind`, repoID,
	)
	if err != nil {
		return 0, nil, fmt.Errorf("counting entities: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return 0, nil, fmt.Errorf("scanning entity count: %w", err)
		}
		byKind[kind] = count
		total += count
	}
	return total, byKind, nil
}

func (s *EntityStore) CountWithFacts(ctx context.Context, repoID uuid.UUID) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT e.id) FROM entities e JOIN facts f ON f.entity_id = e.id WHERE e.repo_id = $1 AND f.superseded_by IS NULL`, repoID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting entities with facts: %w", err)
	}
	return count, nil
}

func (s *EntityStore) CountWithRelationships(ctx context.Context, repoID uuid.UUID) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT e.id) FROM entities e JOIN relationships r ON (r.from_entity_id = e.id OR r.to_entity_id = e.id) WHERE e.repo_id = $1`, repoID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting entities with relationships: %w", err)
	}
	return count, nil
}

func (s *EntityStore) FindByPath(ctx context.Context, repoID uuid.UUID, path string) (*Entity, error) {
	e := &Entity{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND path = $2`, repoID, path,
	).Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.CreatedAt, &e.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding entity by path: %w", err)
	}
	return e, nil
}

func (s *EntityStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM entities WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting entities: %w", err)
	}
	return nil
}
