package models

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RelationshipStore struct {
	Pool *pgxpool.Pool
}

func (s *RelationshipStore) Create(ctx context.Context, r *Relationship) error {
	r.ID = uuid.New()
	r.CreatedAt = time.Now()

	provJSON, err := json.Marshal(r.Provenance)
	if err != nil {
		return fmt.Errorf("marshaling provenance: %w", err)
	}

	_, err = s.Pool.Exec(ctx,
		`INSERT INTO relationships (id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		r.ID, r.RepoID, r.FromEntityID, r.ToEntityID, r.Kind, r.Description, r.Strength, provJSON, r.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting relationship: %w", err)
	}
	return nil
}

func (s *RelationshipStore) ListByEntity(ctx context.Context, entityID uuid.UUID) ([]Relationship, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at
		 FROM relationships WHERE from_entity_id = $1 OR to_entity_id = $1 ORDER BY kind`, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing relationships: %w", err)
	}
	defer rows.Close()

	var rels []Relationship
	for rows.Next() {
		var r Relationship
		var provJSON []byte
		if err := rows.Scan(&r.ID, &r.RepoID, &r.FromEntityID, &r.ToEntityID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning relationship: %w", err)
		}
		if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		rels = append(rels, r)
	}
	return rels, nil
}

func (s *RelationshipStore) ListByRepo(ctx context.Context, repoID uuid.UUID) ([]Relationship, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at
		 FROM relationships WHERE repo_id = $1 ORDER BY kind`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing relationships by repo: %w", err)
	}
	defer rows.Close()

	var rels []Relationship
	for rows.Next() {
		var r Relationship
		var provJSON []byte
		if err := rows.Scan(&r.ID, &r.RepoID, &r.FromEntityID, &r.ToEntityID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning relationship: %w", err)
		}
		if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		rels = append(rels, r)
	}
	return rels, nil
}

func (s *RelationshipStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM relationships WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting relationships: %w", err)
	}
	return nil
}
