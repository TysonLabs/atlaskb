-- Safety-net unique constraints for dedup
-- Primary dedup is LLM-based; these are fallback constraints.

-- First, deduplicate existing entities: keep the most recently updated row per (repo_id, qualified_name).
DELETE FROM entities e
USING entities e2
WHERE e.repo_id = e2.repo_id
  AND e.qualified_name = e2.qualified_name
  AND e.id <> e2.id
  AND e.updated_at < e2.updated_at;

-- Entities: one entity per qualified_name per repo
ALTER TABLE entities
    ADD CONSTRAINT uq_entities_repo_qualified_name UNIQUE (repo_id, qualified_name);

-- Deduplicate existing relationships: keep most recent per (repo_id, from, to, kind).
DELETE FROM relationships r
USING relationships r2
WHERE r.repo_id = r2.repo_id
  AND r.from_entity_id = r2.from_entity_id
  AND r.to_entity_id = r2.to_entity_id
  AND r.kind = r2.kind
  AND r.id <> r2.id
  AND r.created_at < r2.created_at;

-- Relationships: one relationship per (from, to, kind) per repo
ALTER TABLE relationships
    ADD CONSTRAINT uq_relationships_repo_from_to_kind UNIQUE (repo_id, from_entity_id, to_entity_id, kind);
