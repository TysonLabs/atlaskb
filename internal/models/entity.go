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
		 ON CONFLICT (id) DO UPDATE SET
		   summary = EXCLUDED.summary,
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

func (s *EntityStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM entities WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting entities: %w", err)
	}
	return nil
}
