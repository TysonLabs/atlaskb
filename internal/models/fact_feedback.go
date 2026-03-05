package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FactFeedbackStore struct {
	Pool *pgxpool.Pool
}

type FeedbackSubmissionResult struct {
	Feedback      FactFeedback
	QueuedTargets []string
}

func (s *FactFeedbackStore) Create(ctx context.Context, fb *FactFeedback) error {
	fb.ID = uuid.New()
	fb.CreatedAt = time.Now()
	if fb.Status == "" {
		fb.Status = FeedbackPending
	}
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO fact_feedback (id, fact_id, repo_id, reason, correction, status, outcome, created_at, resolved_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		fb.ID, fb.FactID, fb.RepoID, fb.Reason, fb.Correction, fb.Status, fb.Outcome, fb.CreatedAt, fb.ResolvedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting feedback: %w", err)
	}
	return nil
}

func (s *FactFeedbackStore) Resolve(ctx context.Context, id uuid.UUID, outcome *string) error {
	now := time.Now()
	_, err := s.Pool.Exec(ctx,
		`UPDATE fact_feedback
		 SET status = $2, outcome = $3, resolved_at = $4
		 WHERE id = $1`,
		id, FeedbackResolved, outcome, now,
	)
	if err != nil {
		return fmt.Errorf("resolving feedback: %w", err)
	}
	return nil
}

func (s *FactFeedbackStore) List(ctx context.Context, repoID *uuid.UUID, status string, limit int) ([]FactFeedback, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, fact_id, repo_id, reason, correction, status, outcome, created_at, resolved_at
		FROM fact_feedback WHERE 1=1`
	args := []any{}
	arg := 1
	if repoID != nil {
		query += fmt.Sprintf(" AND repo_id = $%d", arg)
		args = append(args, *repoID)
		arg++
	}
	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", arg)
		args = append(args, status)
		arg++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", arg)
	args = append(args, limit)

	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing feedback: %w", err)
	}
	defer rows.Close()

	var out []FactFeedback
	for rows.Next() {
		var fb FactFeedback
		if err := rows.Scan(&fb.ID, &fb.FactID, &fb.RepoID, &fb.Reason, &fb.Correction, &fb.Status, &fb.Outcome, &fb.CreatedAt, &fb.ResolvedAt); err != nil {
			return nil, fmt.Errorf("scanning feedback: %w", err)
		}
		out = append(out, fb)
	}
	return out, nil
}

func (s *FactFeedbackStore) ListPendingByRepo(ctx context.Context, repoID uuid.UUID) ([]FactFeedback, error) {
	return s.List(ctx, &repoID, FeedbackPending, 1000)
}

func (s *FactFeedbackStore) CountPendingByFactIDs(ctx context.Context, factIDs []uuid.UUID) (map[uuid.UUID]int, error) {
	out := make(map[uuid.UUID]int, len(factIDs))
	if len(factIDs) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT fact_id, COUNT(*) FROM fact_feedback
		 WHERE fact_id = ANY($1) AND status = $2
		 GROUP BY fact_id`, factIDs, FeedbackPending)
	if err != nil {
		return nil, fmt.Errorf("counting pending feedback by fact: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var factID uuid.UUID
		var n int
		if err := rows.Scan(&factID, &n); err != nil {
			return nil, fmt.Errorf("scanning feedback count: %w", err)
		}
		out[factID] = n
	}
	return out, nil
}

func (s *FactFeedbackStore) GetByID(ctx context.Context, id uuid.UUID) (*FactFeedback, error) {
	var fb FactFeedback
	err := s.Pool.QueryRow(ctx,
		`SELECT id, fact_id, repo_id, reason, correction, status, outcome, created_at, resolved_at
		 FROM fact_feedback WHERE id = $1`, id,
	).Scan(&fb.ID, &fb.FactID, &fb.RepoID, &fb.Reason, &fb.Correction, &fb.Status, &fb.Outcome, &fb.CreatedAt, &fb.ResolvedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying feedback: %w", err)
	}
	return &fb, nil
}

// SubmitFactFeedback stores feedback, lowers fact confidence, and queues
// provenance targets for phase2 re-analysis in one transaction.
func SubmitFactFeedback(ctx context.Context, pool *pgxpool.Pool, fact *Fact, reason string, correction *string) (*FeedbackSubmissionResult, error) {
	if fact == nil {
		return nil, fmt.Errorf("fact is required")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reason is required")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	fb := FactFeedback{
		ID:         uuid.New(),
		FactID:     fact.ID,
		RepoID:     fact.RepoID,
		Reason:     reason,
		Correction: correction,
		Status:     FeedbackPending,
		CreatedAt:  time.Now(),
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO fact_feedback (id, fact_id, repo_id, reason, correction, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		fb.ID, fb.FactID, fb.RepoID, fb.Reason, fb.Correction, fb.Status, fb.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert feedback: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE facts SET confidence = $2, updated_at = now() WHERE id = $1`,
		fact.ID, ConfidenceLow,
	); err != nil {
		return nil, fmt.Errorf("lower fact confidence: %w", err)
	}

	seen := map[string]bool{}
	var queued []string
	for _, prov := range fact.Provenance {
		target := strings.TrimSpace(prov.Ref)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true

		var existingID uuid.UUID
		var status string
		err := tx.QueryRow(ctx,
			`SELECT id, status FROM extraction_jobs WHERE repo_id = $1 AND phase = $2 AND target = $3`,
			fact.RepoID, PhasePhase2, target,
		).Scan(&existingID, &status)
		if err == nil {
			switch status {
			case JobInProgress:
				// Do not reset currently running jobs.
			case JobPending:
				queued = append(queued, target)
			default:
				tag, updErr := tx.Exec(ctx,
					`UPDATE extraction_jobs
					 SET status = 'pending', error_message = NULL, started_at = NULL, completed_at = NULL, updated_at = now()
					 WHERE id = $1 AND status <> 'in_progress'`,
					existingID,
				)
				if updErr != nil {
					return nil, fmt.Errorf("reset existing job: %w", updErr)
				}
				if tag.RowsAffected() > 0 {
					queued = append(queued, target)
				}
			}
			continue
		}
		if err != pgx.ErrNoRows {
			return nil, fmt.Errorf("load existing job: %w", err)
		}

		now := time.Now()
		if _, err := tx.Exec(ctx,
			`INSERT INTO extraction_jobs (id, repo_id, phase, target, status, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $6)
			 ON CONFLICT (repo_id, phase, target) DO NOTHING`,
			uuid.New(), fact.RepoID, PhasePhase2, target, JobPending, now,
		); err != nil {
			return nil, fmt.Errorf("queue reanalysis job: %w", err)
		}
		queued = append(queued, target)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit feedback tx: %w", err)
	}
	return &FeedbackSubmissionResult{Feedback: fb, QueuedTargets: queued}, nil
}
