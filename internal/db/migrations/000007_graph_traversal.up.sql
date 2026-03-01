-- Enable pg_trgm for fuzzy text matching
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Trigram index on entity names for fuzzy search
CREATE INDEX IF NOT EXISTS idx_entities_name_trgm ON entities USING gin (name gin_trgm_ops);

-- Composite indexes for efficient relationship traversal
CREATE INDEX IF NOT EXISTS idx_relationships_from_kind ON relationships(from_entity_id, kind);
CREATE INDEX IF NOT EXISTS idx_relationships_to_kind ON relationships(to_entity_id, kind);

-- Normalized name column for fast dedup matching
ALTER TABLE entities ADD COLUMN IF NOT EXISTS name_normalized TEXT;
UPDATE entities SET name_normalized = lower(regexp_replace(name, '[_\-\s]+', '', 'g'));
CREATE INDEX IF NOT EXISTS idx_entities_name_normalized ON entities(name_normalized);
