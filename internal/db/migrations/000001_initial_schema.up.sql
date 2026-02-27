CREATE EXTENSION IF NOT EXISTS "vector";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Enums
CREATE TYPE entity_kind AS ENUM (
    'module', 'service', 'function', 'type', 'endpoint', 'concept', 'config'
);

CREATE TYPE fact_dimension AS ENUM ('what', 'how', 'why', 'when');

CREATE TYPE fact_category AS ENUM (
    'behavior', 'constraint', 'pattern', 'convention', 'debt', 'risk'
);

CREATE TYPE confidence_level AS ENUM ('high', 'medium', 'low');

CREATE TYPE relationship_kind AS ENUM (
    'depends_on', 'calls', 'implements', 'extends', 'produces', 'consumes',
    'replaced_by', 'tested_by', 'configured_by', 'owns'
);

CREATE TYPE relationship_strength AS ENUM ('strong', 'moderate', 'weak');

CREATE TYPE source_type AS ENUM (
    'file', 'commit', 'pr', 'issue', 'comment', 'adr', 'doc'
);

CREATE TYPE job_status AS ENUM (
    'pending', 'in_progress', 'completed', 'failed', 'skipped'
);

CREATE TYPE job_phase AS ENUM (
    'phase1', 'phase2', 'phase4', 'phase5', 'gitlog', 'embedding'
);

-- Repos
CREATE TABLE repos (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        TEXT NOT NULL,
    remote_url  TEXT,
    local_path  TEXT NOT NULL,
    default_branch TEXT NOT NULL DEFAULT 'main',
    last_commit_sha TEXT,
    last_indexed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(local_path)
);

-- Entities
CREATE TABLE entities (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id         UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    kind            entity_kind NOT NULL,
    name            TEXT NOT NULL,
    qualified_name  TEXT NOT NULL,
    path            TEXT,
    summary         TEXT,
    capabilities    TEXT[],
    assumptions     TEXT[],
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_entities_repo ON entities(repo_id);
CREATE INDEX idx_entities_kind ON entities(kind);
CREATE INDEX idx_entities_qualified ON entities(qualified_name);

-- Facts
CREATE TABLE facts (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    entity_id       UUID NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    repo_id         UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    claim           TEXT NOT NULL,
    dimension       fact_dimension NOT NULL,
    category        fact_category NOT NULL,
    confidence      confidence_level NOT NULL DEFAULT 'medium',
    provenance      JSONB NOT NULL DEFAULT '[]',
    embedding       vector(1024),
    superseded_by   UUID REFERENCES facts(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_facts_entity ON facts(entity_id);
CREATE INDEX idx_facts_repo ON facts(repo_id);
CREATE INDEX idx_facts_dimension ON facts(dimension);

-- HNSW index for vector similarity search
CREATE INDEX idx_facts_embedding ON facts
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- Decisions
CREATE TABLE decisions (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id         UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    summary         TEXT NOT NULL,
    description     TEXT NOT NULL,
    rationale       TEXT NOT NULL,
    alternatives    JSONB NOT NULL DEFAULT '[]',
    tradeoffs       TEXT[],
    provenance      JSONB NOT NULL DEFAULT '[]',
    made_at         TIMESTAMPTZ,
    still_valid     BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_decisions_repo ON decisions(repo_id);

-- Decision-entity junction
CREATE TABLE decision_entities (
    decision_id UUID NOT NULL REFERENCES decisions(id) ON DELETE CASCADE,
    entity_id   UUID NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    PRIMARY KEY (decision_id, entity_id)
);

-- Relationships
CREATE TABLE relationships (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id         UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    from_entity_id  UUID NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    to_entity_id    UUID NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    kind            relationship_kind NOT NULL,
    description     TEXT,
    strength        relationship_strength NOT NULL DEFAULT 'moderate',
    provenance      JSONB NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_relationships_repo ON relationships(repo_id);
CREATE INDEX idx_relationships_from ON relationships(from_entity_id);
CREATE INDEX idx_relationships_to ON relationships(to_entity_id);

-- Extraction Jobs
CREATE TABLE extraction_jobs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    repo_id         UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    phase           job_phase NOT NULL,
    target          TEXT NOT NULL,
    content_hash    TEXT,
    status          job_status NOT NULL DEFAULT 'pending',
    error_message   TEXT,
    tokens_used     INTEGER,
    cost_usd        NUMERIC(10,6),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_jobs_repo_phase ON extraction_jobs(repo_id, phase);
CREATE INDEX idx_jobs_status ON extraction_jobs(status);
CREATE UNIQUE INDEX idx_jobs_repo_phase_target ON extraction_jobs(repo_id, phase, target);
