package models

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExecutionFlow represents a detected execution flow through the call graph,
// starting from an entry point and tracing through called functions via BFS.
type ExecutionFlow struct {
	ID            uuid.UUID   `json:"id"`
	RepoID        uuid.UUID   `json:"repo_id"`
	EntryEntityID uuid.UUID   `json:"entry_entity_id"`
	Label         string      `json:"label"`
	StepEntityIDs []uuid.UUID `json:"step_entity_ids"`
	StepNames     []string    `json:"step_names"`
	Depth         int         `json:"depth"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

type FlowStore struct {
	Pool *pgxpool.Pool
}

// Upsert inserts a new execution flow or updates an existing one keyed on (repo_id, entry_entity_id).
func (s *FlowStore) Upsert(ctx context.Context, f *ExecutionFlow) error {
	now := time.Now()
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO execution_flows (id, repo_id, entry_entity_id, label, step_entity_ids, step_names, depth, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (repo_id, entry_entity_id) DO UPDATE SET
		   label = EXCLUDED.label,
		   step_entity_ids = EXCLUDED.step_entity_ids,
		   step_names = EXCLUDED.step_names,
		   depth = EXCLUDED.depth,
		   updated_at = EXCLUDED.updated_at
		 RETURNING id, created_at`,
		uuid.New(), f.RepoID, f.EntryEntityID, f.Label, f.StepEntityIDs, f.StepNames, f.Depth, now, now,
	).Scan(&f.ID, &f.CreatedAt)
	if err != nil {
		return fmt.Errorf("upserting execution flow: %w", err)
	}
	f.UpdatedAt = now
	return nil
}

// DeleteByRepo removes all execution flows for a given repository.
func (s *FlowStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM execution_flows WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting execution flows: %w", err)
	}
	return nil
}

// ListByRepo returns execution flows for a repo, ordered by depth descending.
func (s *FlowStore) ListByRepo(ctx context.Context, repoID uuid.UUID, limit int) ([]ExecutionFlow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, entry_entity_id, label, step_entity_ids, step_names, depth, created_at, updated_at
		 FROM execution_flows WHERE repo_id = $1 ORDER BY depth DESC LIMIT $2`, repoID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing execution flows: %w", err)
	}
	defer rows.Close()

	var flows []ExecutionFlow
	for rows.Next() {
		var f ExecutionFlow
		if err := rows.Scan(&f.ID, &f.RepoID, &f.EntryEntityID, &f.Label, &f.StepEntityIDs, &f.StepNames, &f.Depth, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning execution flow: %w", err)
		}
		flows = append(flows, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating execution flows: %w", err)
	}
	return flows, nil
}

// FindByEntity returns all flows where the given entity ID appears in step_entity_ids (uses GIN index).
func (s *FlowStore) FindByEntity(ctx context.Context, entityID uuid.UUID, limit int) ([]ExecutionFlow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, entry_entity_id, label, step_entity_ids, step_names, depth, created_at, updated_at
		 FROM execution_flows WHERE $1 = ANY(step_entity_ids) ORDER BY depth DESC LIMIT $2`, entityID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("finding execution flows by entity: %w", err)
	}
	defer rows.Close()

	var flows []ExecutionFlow
	for rows.Next() {
		var f ExecutionFlow
		if err := rows.Scan(&f.ID, &f.RepoID, &f.EntryEntityID, &f.Label, &f.StepEntityIDs, &f.StepNames, &f.Depth, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning execution flow: %w", err)
		}
		flows = append(flows, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating execution flows: %w", err)
	}
	return flows, nil
}
