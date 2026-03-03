CREATE TABLE IF NOT EXISTS execution_flows (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id         UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    entry_entity_id UUID NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    label           TEXT NOT NULL,
    step_entity_ids UUID[] NOT NULL,
    step_names      TEXT[] NOT NULL,
    depth           INTEGER NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(repo_id, entry_entity_id)
);
CREATE INDEX IF NOT EXISTS idx_execution_flows_repo ON execution_flows(repo_id);
CREATE INDEX IF NOT EXISTS idx_execution_flows_entry ON execution_flows(entry_entity_id);
CREATE INDEX IF NOT EXISTS idx_execution_flows_steps ON execution_flows USING gin(step_entity_ids);
