DROP TABLE IF EXISTS indexing_runs;

ALTER TABLE extraction_jobs DROP COLUMN IF EXISTS model_used;
ALTER TABLE extraction_jobs DROP COLUMN IF EXISTS attempt_count;
