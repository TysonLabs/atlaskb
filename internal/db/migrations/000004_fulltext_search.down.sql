DROP TRIGGER IF EXISTS trg_facts_claim_tsv ON facts;
DROP FUNCTION IF EXISTS facts_claim_tsv_trigger();
DROP INDEX IF EXISTS idx_facts_claim_tsv;
ALTER TABLE facts DROP COLUMN IF EXISTS claim_tsv;
