ALTER TABLE entities DROP CONSTRAINT IF EXISTS uq_entities_repo_qualified_name;
ALTER TABLE relationships DROP CONSTRAINT IF EXISTS uq_relationships_repo_from_to_kind;
