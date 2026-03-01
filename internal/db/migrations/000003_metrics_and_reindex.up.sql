-- Indexing runs: persist metrics from each pipeline execution
CREATE TABLE indexing_runs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id         UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    commit_sha      TEXT,
    mode            TEXT NOT NULL DEFAULT 'full',  -- 'full', 'incremental', 'retry'
    model_extraction TEXT,
    model_synthesis  TEXT,
    concurrency     INTEGER,

    -- Phase stats
    files_total     INTEGER,
    files_analyzed  INTEGER,
    files_skipped   INTEGER,
    entities_created INTEGER,
    facts_created   INTEGER,
    rels_created    INTEGER,
    decisions_created INTEGER,

    -- Backfill stats
    orphan_entities  INTEGER,
    backfill_facts   INTEGER,
    backfill_rels    INTEGER,

    -- Tokens & cost
    total_tokens    INTEGER,
    total_cost_usd  NUMERIC(10,6),

    -- Quality snapshot
    quality_overall       NUMERIC(5,2),
    quality_entity_cov    NUMERIC(5,2),
    quality_fact_density  NUMERIC(5,2),
    quality_rel_connect   NUMERIC(5,2),
    quality_dim_coverage  NUMERIC(5,2),
    quality_parse_rate    NUMERIC(5,2),

    -- Timing
    duration_ms     BIGINT,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_indexing_runs_repo ON indexing_runs(repo_id);

-- Add model_used and attempt_count to extraction_jobs
ALTER TABLE extraction_jobs ADD COLUMN IF NOT EXISTS model_used TEXT;
ALTER TABLE extraction_jobs ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 1;
