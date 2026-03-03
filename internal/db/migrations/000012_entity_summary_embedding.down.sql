DROP INDEX IF EXISTS idx_entities_summary_embedding;
ALTER TABLE entities DROP COLUMN IF EXISTS summary_embedding;
