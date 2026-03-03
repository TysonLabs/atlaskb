ALTER TABLE relationships ADD COLUMN IF NOT EXISTS confidence REAL;
ALTER TABLE cross_repo_relationships ADD COLUMN IF NOT EXISTS confidence REAL;

UPDATE relationships SET confidence = CASE
    WHEN strength = 'strong' THEN 0.90
    WHEN strength = 'moderate' THEN 0.75
    WHEN strength = 'weak' THEN 0.55
    ELSE 0.75 END
WHERE confidence IS NULL;

UPDATE cross_repo_relationships SET confidence = CASE
    WHEN strength = 'strong' THEN 0.75
    WHEN strength = 'moderate' THEN 0.65
    WHEN strength = 'weak' THEN 0.50
    ELSE 0.65 END
WHERE confidence IS NULL;

CREATE INDEX idx_relationships_confidence ON relationships(confidence);
CREATE INDEX idx_cross_repo_relationships_confidence ON cross_repo_relationships(confidence);
