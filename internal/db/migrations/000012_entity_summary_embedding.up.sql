ALTER TABLE entities ADD COLUMN summary_embedding vector(1024);
CREATE INDEX idx_entities_summary_embedding ON entities
    USING hnsw (summary_embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);
