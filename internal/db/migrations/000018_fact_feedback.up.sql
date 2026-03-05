CREATE TABLE IF NOT EXISTS fact_feedback (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  fact_id UUID NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
  repo_id UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
  reason TEXT NOT NULL,
  correction TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  outcome TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_fact_feedback_fact ON fact_feedback(fact_id);
CREATE INDEX IF NOT EXISTS idx_fact_feedback_repo_status ON fact_feedback(repo_id, status);
