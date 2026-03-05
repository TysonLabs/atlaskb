ALTER TABLE indexing_runs
  DROP COLUMN IF EXISTS parse_fallbacks,
  DROP COLUMN IF EXISTS unresolved_refs;
