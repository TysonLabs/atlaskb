CREATE UNIQUE INDEX IF NOT EXISTS idx_cross_repo_unique
  ON cross_repo_relationships(from_entity_id, to_entity_id, kind);
