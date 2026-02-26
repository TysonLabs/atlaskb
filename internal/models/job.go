package models

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type JobStore struct {
	Pool *pgxpool.Pool
}

func (s *JobStore) Create(ctx context.Context, j *ExtractionJob) error {
	j.ID = uuid.New()
	j.CreatedAt = time.Now()
	j.UpdatedAt = j.CreatedAt

	_, err := s.Pool.Exec(ctx,
		`INSERT INTO extraction_jobs (id, repo_id, phase, target, content_hash, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (repo_id, phase, target) DO NOTHING`,
		j.ID, j.RepoID, j.Phase, j.Target, j.ContentHash, j.Status, j.CreatedAt, j.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting job: %w", err)
	}
	return nil
}

// ClaimNext atomically claims the next pending job for a phase using FOR UPDATE SKIP LOCKED.
func (s *JobStore) ClaimNext(ctx context.Context, repoID uuid.UUID, phase string) (*ExtractionJob, error) {
	j := &ExtractionJob{}
	now := time.Now()
	err := s.Pool.QueryRow(ctx,
		`UPDATE extraction_jobs SET status = 'in_progress', started_at = $3, updated_at = $3
		 WHERE id = (
		   SELECT id FROM extraction_jobs
		   WHERE repo_id = $1 AND phase = $2 AND status = 'pending'
		   ORDER BY created_at
		   FOR UPDATE SKIP LOCKED
		   LIMIT 1
		 )
		 RETURNING id, repo_id, phase, target, content_hash, status, error_message, tokens_used, cost_usd, started_at, completed_at, created_at, updated_at`,
		repoID, phase, now,
	).Scan(&j.ID, &j.RepoID, &j.Phase, &j.Target, &j.ContentHash, &j.Status, &j.ErrorMessage, &j.TokensUsed, &j.CostUSD, &j.StartedAt, &j.CompletedAt, &j.CreatedAt, &j.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claiming job: %w", err)
	}
	return j, nil
}

func (s *JobStore) Complete(ctx context.Context, id uuid.UUID, tokensUsed int, costUSD float64) error {
	now := time.Now()
	_, err := s.Pool.Exec(ctx,
		`UPDATE extraction_jobs SET status = 'completed', tokens_used = $2, cost_usd = $3, completed_at = $4, updated_at = $4
		 WHERE id = $1`,
		id, tokensUsed, costUSD, now,
	)
	if err != nil {
		return fmt.Errorf("completing job: %w", err)
	}
	return nil
}

func (s *JobStore) Fail(ctx context.Context, id uuid.UUID, errMsg string) error {
	now := time.Now()
	_, err := s.Pool.Exec(ctx,
		`UPDATE extraction_jobs SET status = 'failed', error_message = $2, completed_at = $3, updated_at = $3
		 WHERE id = $1`,
		id, errMsg, now,
	)
	if err != nil {
		return fmt.Errorf("failing job: %w", err)
	}
	return nil
}

func (s *JobStore) ResetFailed(ctx context.Context, repoID uuid.UUID, phase string) (int64, error) {
	tag, err := s.Pool.Exec(ctx,
		`UPDATE extraction_jobs SET status = 'pending', error_message = NULL, started_at = NULL, completed_at = NULL, updated_at = now()
		 WHERE repo_id = $1 AND ($2 = '' OR phase = $2::job_phase) AND status = 'failed'`,
		repoID, phase,
	)
	if err != nil {
		return 0, fmt.Errorf("resetting failed jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *JobStore) CountByStatus(ctx context.Context, repoID uuid.UUID, phase string) (map[string]int, error) {
	query := `SELECT status, COUNT(*) FROM extraction_jobs WHERE repo_id = $1`
	args := []any{repoID}
	if phase != "" {
		query += ` AND phase = $2`
		args = append(args, phase)
	}
	query += ` GROUP BY status`

	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("counting jobs: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scanning count: %w", err)
		}
		counts[status] = count
	}
	return counts, nil
}

func (s *JobStore) ListFailed(ctx context.Context, repoID uuid.UUID) ([]ExtractionJob, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, phase, target, content_hash, status, error_message, tokens_used, cost_usd, started_at, completed_at, created_at, updated_at
		 FROM extraction_jobs WHERE repo_id = $1 AND status = 'failed' ORDER BY phase, target`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing failed jobs: %w", err)
	}
	defer rows.Close()

	var jobs []ExtractionJob
	for rows.Next() {
		var j ExtractionJob
		if err := rows.Scan(&j.ID, &j.RepoID, &j.Phase, &j.Target, &j.ContentHash, &j.Status, &j.ErrorMessage, &j.TokensUsed, &j.CostUSD, &j.StartedAt, &j.CompletedAt, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning job: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (s *JobStore) GetByTarget(ctx context.Context, repoID uuid.UUID, phase, target string) (*ExtractionJob, error) {
	j := &ExtractionJob{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, phase, target, content_hash, status, error_message, tokens_used, cost_usd, started_at, completed_at, created_at, updated_at
		 FROM extraction_jobs WHERE repo_id = $1 AND phase = $2 AND target = $3`,
		repoID, phase, target,
	).Scan(&j.ID, &j.RepoID, &j.Phase, &j.Target, &j.ContentHash, &j.Status, &j.ErrorMessage, &j.TokensUsed, &j.CostUSD, &j.StartedAt, &j.CompletedAt, &j.CreatedAt, &j.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying job: %w", err)
	}
	return j, nil
}

func (s *JobStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM extraction_jobs WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting jobs: %w", err)
	}
	return nil
}
