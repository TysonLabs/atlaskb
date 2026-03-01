DROP INDEX IF EXISTS idx_entities_name_normalized;
ALTER TABLE entities DROP COLUMN IF EXISTS name_normalized;
DROP INDEX IF EXISTS idx_relationships_to_kind;
DROP INDEX IF EXISTS idx_relationships_from_kind;
DROP INDEX IF EXISTS idx_entities_name_trgm;
-- Note: not dropping pg_trgm extension as other things may depend on it
