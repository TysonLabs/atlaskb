CREATE TABLE IF NOT EXISTS cross_repo_relationships (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    from_entity_id  UUID NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    to_entity_id    UUID NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    from_repo_id    UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    to_repo_id      UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    kind            relationship_kind NOT NULL,
    description     TEXT,
    strength        relationship_strength NOT NULL DEFAULT 'moderate',
    provenance      JSONB NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_cross_repo CHECK (from_repo_id != to_repo_id)
);

CREATE INDEX IF NOT EXISTS idx_cross_repo_from ON cross_repo_relationships(from_entity_id);
CREATE INDEX IF NOT EXISTS idx_cross_repo_to ON cross_repo_relationships(to_entity_id);
CREATE INDEX IF NOT EXISTS idx_cross_repo_from_repo ON cross_repo_relationships(from_repo_id);
CREATE INDEX IF NOT EXISTS idx_cross_repo_to_repo ON cross_repo_relationships(to_repo_id);
