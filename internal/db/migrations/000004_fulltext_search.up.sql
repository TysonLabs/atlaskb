-- Add full-text search support to facts table for hybrid keyword+vector search

-- Add tsvector column for pre-computed text search vectors
ALTER TABLE facts ADD COLUMN claim_tsv tsvector;

-- Create GIN index for fast full-text search
CREATE INDEX idx_facts_claim_tsv ON facts USING GIN (claim_tsv);

-- Populate from existing data
UPDATE facts SET claim_tsv = to_tsvector('english', claim) WHERE claim_tsv IS NULL;

-- Auto-update trigger: keep claim_tsv in sync when claim changes
CREATE OR REPLACE FUNCTION facts_claim_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.claim_tsv := to_tsvector('english', NEW.claim);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_facts_claim_tsv
    BEFORE INSERT OR UPDATE OF claim ON facts
    FOR EACH ROW
    EXECUTE FUNCTION facts_claim_tsv_trigger();
