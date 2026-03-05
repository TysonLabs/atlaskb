ALTER TABLE indexing_runs
  ADD COLUMN IF NOT EXISTS parse_fallbacks INTEGER,
  ADD COLUMN IF NOT EXISTS unresolved_refs INTEGER;
