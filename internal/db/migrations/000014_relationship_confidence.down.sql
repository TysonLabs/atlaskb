DROP INDEX IF EXISTS idx_relationships_confidence;
DROP INDEX IF EXISTS idx_cross_repo_relationships_confidence;
ALTER TABLE relationships DROP COLUMN IF EXISTS confidence;
ALTER TABLE cross_repo_relationships DROP COLUMN IF EXISTS confidence;
