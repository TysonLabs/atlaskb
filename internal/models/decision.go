package models

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DecisionStore struct {
	Pool *pgxpool.Pool
}

func (s *DecisionStore) Create(ctx context.Context, d *Decision) error {
	d.ID = uuid.New()
	d.CreatedAt = time.Now()
	d.UpdatedAt = d.CreatedAt

	altJSON, err := json.Marshal(d.Alternatives)
	if err != nil {
		return fmt.Errorf("marshaling alternatives: %w", err)
	}
	provJSON, err := json.Marshal(d.Provenance)
	if err != nil {
		return fmt.Errorf("marshaling provenance: %w", err)
	}

	_, err = s.Pool.Exec(ctx,
		`INSERT INTO decisions (id, repo_id, summary, description, rationale, alternatives, tradeoffs, provenance, made_at, still_valid, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		d.ID, d.RepoID, d.Summary, d.Description, d.Rationale, altJSON, d.Tradeoffs, provJSON, d.MadeAt, d.StillValid, d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting decision: %w", err)
	}
	return nil
}

func (s *DecisionStore) LinkEntities(ctx context.Context, decisionID uuid.UUID, entityIDs []uuid.UUID) error {
	for _, eid := range entityIDs {
		_, err := s.Pool.Exec(ctx,
			`INSERT INTO decision_entities (decision_id, entity_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			decisionID, eid,
		)
		if err != nil {
			return fmt.Errorf("linking decision to entity: %w", err)
		}
	}
	return nil
}

func (s *DecisionStore) GetByID(ctx context.Context, id uuid.UUID) (*Decision, error) {
	d := &Decision{}
	var altJSON, provJSON []byte
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, summary, description, rationale, alternatives, tradeoffs, provenance, made_at, still_valid, created_at, updated_at
		 FROM decisions WHERE id = $1`, id,
	).Scan(&d.ID, &d.RepoID, &d.Summary, &d.Description, &d.Rationale, &altJSON, &d.Tradeoffs, &provJSON, &d.MadeAt, &d.StillValid, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying decision: %w", err)
	}
	if err := json.Unmarshal(altJSON, &d.Alternatives); err != nil {
		return nil, fmt.Errorf("unmarshaling alternatives: %w", err)
	}
	if err := json.Unmarshal(provJSON, &d.Provenance); err != nil {
		return nil, fmt.Errorf("unmarshaling provenance: %w", err)
	}
	return d, nil
}

func (s *DecisionStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM decisions WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting decisions: %w", err)
	}
	return nil
}

func (s *DecisionStore) ListByEntity(ctx context.Context, entityID uuid.UUID, limit int) ([]Decision, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT d.id, d.repo_id, d.summary, d.description, d.rationale, d.alternatives, d.tradeoffs, d.provenance, d.made_at, d.still_valid, d.created_at, d.updated_at
		 FROM decisions d
		 JOIN decision_entities de ON de.decision_id = d.id
		 WHERE de.entity_id = $1
		 ORDER BY d.created_at DESC LIMIT $2`, entityID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing decisions by entity: %w", err)
	}
	defer rows.Close()

	var decisions []Decision
	for rows.Next() {
		var d Decision
		var altJSON, provJSON []byte
		if err := rows.Scan(&d.ID, &d.RepoID, &d.Summary, &d.Description, &d.Rationale, &altJSON, &d.Tradeoffs, &provJSON, &d.MadeAt, &d.StillValid, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning decision: %w", err)
		}
		if err := json.Unmarshal(altJSON, &d.Alternatives); err != nil {
			return nil, fmt.Errorf("unmarshaling alternatives: %w", err)
		}
		if err := json.Unmarshal(provJSON, &d.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}

func (s *DecisionStore) CountByRepo(ctx context.Context, repoID uuid.UUID) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM decisions WHERE repo_id = $1`, repoID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting decisions: %w", err)
	}
	return count, nil
}

func (s *DecisionStore) ListByRepo(ctx context.Context, repoID uuid.UUID) ([]Decision, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, summary, description, rationale, alternatives, tradeoffs, provenance, made_at, still_valid, created_at, updated_at
		 FROM decisions WHERE repo_id = $1 ORDER BY created_at DESC`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing decisions: %w", err)
	}
	defer rows.Close()

	var decisions []Decision
	for rows.Next() {
		var d Decision
		var altJSON, provJSON []byte
		if err := rows.Scan(&d.ID, &d.RepoID, &d.Summary, &d.Description, &d.Rationale, &altJSON, &d.Tradeoffs, &provJSON, &d.MadeAt, &d.StillValid, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning decision: %w", err)
		}
		if err := json.Unmarshal(altJSON, &d.Alternatives); err != nil {
			return nil, fmt.Errorf("unmarshaling alternatives: %w", err)
		}
		if err := json.Unmarshal(provJSON, &d.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}
