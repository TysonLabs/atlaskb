package models

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IndexingRunStore struct {
	Pool *pgxpool.Pool
}

func (s *IndexingRunStore) Create(ctx context.Context, r *IndexingRun) error {
	r.ID = uuid.New()
	r.StartedAt = time.Now()
	r.CreatedAt = r.StartedAt

	_, err := s.Pool.Exec(ctx,
		`INSERT INTO indexing_runs (id, repo_id, commit_sha, mode, model_extraction, model_synthesis, concurrency, started_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		r.ID, r.RepoID, r.CommitSHA, r.Mode, r.ModelExtraction, r.ModelSynthesis, r.Concurrency, r.StartedAt, r.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting indexing run: %w", err)
	}
	return nil
}

func (s *IndexingRunStore) Complete(ctx context.Context, r *IndexingRun) error {
	now := time.Now()
	r.CompletedAt = &now

	_, err := s.Pool.Exec(ctx,
		`UPDATE indexing_runs SET
			files_total = $2, files_analyzed = $3, files_skipped = $4,
			entities_created = $5, facts_created = $6, rels_created = $7, decisions_created = $8,
			orphan_entities = $9, backfill_facts = $10, backfill_rels = $11,
			total_tokens = $12, total_cost_usd = $13,
			quality_overall = $14, quality_entity_cov = $15, quality_fact_density = $16,
			quality_rel_connect = $17, quality_dim_coverage = $18, quality_parse_rate = $19,
			duration_ms = $20, completed_at = $21
		 WHERE id = $1`,
		r.ID,
		r.FilesTotal, r.FilesAnalyzed, r.FilesSkipped,
		r.EntitiesCreated, r.FactsCreated, r.RelsCreated, r.DecisionsCreated,
		r.OrphanEntities, r.BackfillFacts, r.BackfillRels,
		r.TotalTokens, r.TotalCostUSD,
		r.QualityOverall, r.QualityEntityCov, r.QualityFactDensity,
		r.QualityRelConnect, r.QualityDimCoverage, r.QualityParseRate,
		r.DurationMS, now,
	)
	if err != nil {
		return fmt.Errorf("completing indexing run: %w", err)
	}
	return nil
}

func (s *IndexingRunStore) GetLatest(ctx context.Context, repoID uuid.UUID) (*IndexingRun, error) {
	r := &IndexingRun{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, commit_sha, mode, model_extraction, model_synthesis, concurrency,
			files_total, files_analyzed, files_skipped,
			entities_created, facts_created, rels_created, decisions_created,
			orphan_entities, backfill_facts, backfill_rels,
			total_tokens, total_cost_usd,
			quality_overall, quality_entity_cov, quality_fact_density,
			quality_rel_connect, quality_dim_coverage, quality_parse_rate,
			duration_ms, started_at, completed_at, created_at
		 FROM indexing_runs WHERE repo_id = $1 ORDER BY created_at DESC LIMIT 1`, repoID,
	).Scan(
		&r.ID, &r.RepoID, &r.CommitSHA, &r.Mode, &r.ModelExtraction, &r.ModelSynthesis, &r.Concurrency,
		&r.FilesTotal, &r.FilesAnalyzed, &r.FilesSkipped,
		&r.EntitiesCreated, &r.FactsCreated, &r.RelsCreated, &r.DecisionsCreated,
		&r.OrphanEntities, &r.BackfillFacts, &r.BackfillRels,
		&r.TotalTokens, &r.TotalCostUSD,
		&r.QualityOverall, &r.QualityEntityCov, &r.QualityFactDensity,
		&r.QualityRelConnect, &r.QualityDimCoverage, &r.QualityParseRate,
		&r.DurationMS, &r.StartedAt, &r.CompletedAt, &r.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying latest indexing run: %w", err)
	}
	return r, nil
}

func (s *IndexingRunStore) ListByRepo(ctx context.Context, repoID uuid.UUID) ([]IndexingRun, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, commit_sha, mode, model_extraction, model_synthesis, concurrency,
			files_total, files_analyzed, files_skipped,
			entities_created, facts_created, rels_created, decisions_created,
			orphan_entities, backfill_facts, backfill_rels,
			total_tokens, total_cost_usd,
			quality_overall, quality_entity_cov, quality_fact_density,
			quality_rel_connect, quality_dim_coverage, quality_parse_rate,
			duration_ms, started_at, completed_at, created_at
		 FROM indexing_runs WHERE repo_id = $1 ORDER BY created_at DESC`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing indexing runs: %w", err)
	}
	defer rows.Close()

	var runs []IndexingRun
	for rows.Next() {
		var r IndexingRun
		if err := rows.Scan(
			&r.ID, &r.RepoID, &r.CommitSHA, &r.Mode, &r.ModelExtraction, &r.ModelSynthesis, &r.Concurrency,
			&r.FilesTotal, &r.FilesAnalyzed, &r.FilesSkipped,
			&r.EntitiesCreated, &r.FactsCreated, &r.RelsCreated, &r.DecisionsCreated,
			&r.OrphanEntities, &r.BackfillFacts, &r.BackfillRels,
			&r.TotalTokens, &r.TotalCostUSD,
			&r.QualityOverall, &r.QualityEntityCov, &r.QualityFactDensity,
			&r.QualityRelConnect, &r.QualityDimCoverage, &r.QualityParseRate,
			&r.DurationMS, &r.StartedAt, &r.CompletedAt, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning indexing run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, nil
}
