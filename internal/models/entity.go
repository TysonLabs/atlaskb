package models

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

type EntityStore struct {
	Pool *pgxpool.Pool
}

func (s *EntityStore) Create(ctx context.Context, e *Entity) error {
	e.ID = uuid.New()
	e.CreatedAt = time.Now()
	e.UpdatedAt = e.CreatedAt

	_, err := s.Pool.Exec(ctx,
		`INSERT INTO entities (id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, name_normalized, signature, typeref, start_line, end_line, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		e.ID, e.RepoID, e.Kind, e.Name, e.QualifiedName, e.Path, e.Summary, e.Capabilities, e.Assumptions, NormalizeName(e.Name), e.Signature, e.TypeRef, e.StartLine, e.EndLine, e.CreatedAt, e.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting entity: %w", err)
	}
	return nil
}

func (s *EntityStore) Upsert(ctx context.Context, e *Entity) error {
	now := time.Now()
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO entities (id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, name_normalized, signature, typeref, start_line, end_line, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		 ON CONFLICT (repo_id, qualified_name) DO UPDATE SET
		   summary = COALESCE(EXCLUDED.summary, entities.summary),
		   capabilities = EXCLUDED.capabilities,
		   assumptions = EXCLUDED.assumptions,
		   name_normalized = EXCLUDED.name_normalized,
		   signature = COALESCE(EXCLUDED.signature, entities.signature),
		   typeref = COALESCE(EXCLUDED.typeref, entities.typeref),
		   start_line = COALESCE(EXCLUDED.start_line, entities.start_line),
		   end_line = COALESCE(EXCLUDED.end_line, entities.end_line),
		   updated_at = EXCLUDED.updated_at
		 RETURNING id`,
		uuid.New(), e.RepoID, e.Kind, e.Name, e.QualifiedName, e.Path, e.Summary, e.Capabilities, e.Assumptions, NormalizeName(e.Name), e.Signature, e.TypeRef, e.StartLine, e.EndLine, now, now,
	).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("upserting entity: %w", err)
	}
	e.UpdatedAt = now
	return nil
}

func (s *EntityStore) FindByNameAndKind(ctx context.Context, repoID uuid.UUID, name, kind string) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND name = $2 AND kind = $3`, repoID, name, kind,
	)
	if err != nil {
		return nil, fmt.Errorf("finding entities by name/kind: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) FindByName(ctx context.Context, repoID uuid.UUID, name string) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND name = $2`, repoID, name,
	)
	if err != nil {
		return nil, fmt.Errorf("finding entities by name: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) Update(ctx context.Context, e *Entity) error {
	e.UpdatedAt = time.Now()
	_, err := s.Pool.Exec(ctx,
		`UPDATE entities SET summary = $2, capabilities = $3, assumptions = $4, signature = COALESCE($5, signature), typeref = COALESCE($6, typeref), start_line = COALESCE($7, start_line), end_line = COALESCE($8, end_line), updated_at = $9 WHERE id = $1`,
		e.ID, e.Summary, e.Capabilities, e.Assumptions, e.Signature, e.TypeRef, e.StartLine, e.EndLine, e.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("updating entity: %w", err)
	}
	return nil
}

func (s *EntityStore) GetByID(ctx context.Context, id uuid.UUID) (*Entity, error) {
	e := &Entity{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE id = $1`, id,
	).Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying entity: %w", err)
	}
	return e, nil
}

// GetByIDs fetches multiple entities by their IDs in a single query.
func (s *EntityStore) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]Entity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE id = ANY($1)`, ids,
	)
	if err != nil {
		return nil, fmt.Errorf("fetching entities by IDs: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) ListByRepo(ctx context.Context, repoID uuid.UUID) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 ORDER BY qualified_name`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing entities: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) ListByRepoAndKind(ctx context.Context, repoID uuid.UUID, kind string) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND kind = $2 ORDER BY qualified_name`, repoID, kind,
	)
	if err != nil {
		return nil, fmt.Errorf("listing entities by kind: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) FindByQualifiedName(ctx context.Context, repoID uuid.UUID, qualifiedName string) (*Entity, error) {
	e := &Entity{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND qualified_name = $2`, repoID, qualifiedName,
	).Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding entity: %w", err)
	}
	return e, nil
}

// ListOrphans returns entities that have no facts (orphaned entities).
func (s *EntityStore) ListOrphans(ctx context.Context, repoID uuid.UUID) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT e.id, e.repo_id, e.kind, e.name, e.qualified_name, e.path, e.summary, e.capabilities, e.assumptions, e.signature, e.typeref, e.start_line, e.end_line, e.created_at, e.updated_at
		 FROM entities e
		 LEFT JOIN facts f ON f.entity_id = e.id AND f.superseded_by IS NULL
		 WHERE e.repo_id = $1 AND e.path IS NOT NULL AND f.id IS NULL
		 ORDER BY e.path, e.qualified_name`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing orphan entities: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning orphan entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

// ListWithoutRelationships returns entities that have no relationships (isolated entities).
func (s *EntityStore) ListWithoutRelationships(ctx context.Context, repoID uuid.UUID) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT e.id, e.repo_id, e.kind, e.name, e.qualified_name, e.path, e.summary, e.capabilities, e.assumptions, e.signature, e.typeref, e.start_line, e.end_line, e.created_at, e.updated_at
		 FROM entities e
		 LEFT JOIN relationships r1 ON r1.from_entity_id = e.id
		 LEFT JOIN relationships r2 ON r2.to_entity_id = e.id
		 WHERE e.repo_id = $1 AND e.path IS NOT NULL AND r1.id IS NULL AND r2.id IS NULL
		 ORDER BY e.path, e.qualified_name`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing entities without relationships: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) CountByRepo(ctx context.Context, repoID uuid.UUID) (total int, byKind map[string]int, err error) {
	byKind = make(map[string]int)
	rows, err := s.Pool.Query(ctx,
		`SELECT kind, COUNT(*) FROM entities WHERE repo_id = $1 GROUP BY kind`, repoID,
	)
	if err != nil {
		return 0, nil, fmt.Errorf("counting entities: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind string
		var count int
		if err := rows.Scan(&kind, &count); err != nil {
			return 0, nil, fmt.Errorf("scanning entity count: %w", err)
		}
		byKind[kind] = count
		total += count
	}
	return total, byKind, nil
}

func (s *EntityStore) CountWithFacts(ctx context.Context, repoID uuid.UUID) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT e.id) FROM entities e JOIN facts f ON f.entity_id = e.id WHERE e.repo_id = $1 AND f.superseded_by IS NULL`, repoID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting entities with facts: %w", err)
	}
	return count, nil
}

func (s *EntityStore) CountWithRelationships(ctx context.Context, repoID uuid.UUID) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT e.id) FROM entities e JOIN relationships r ON (r.from_entity_id = e.id OR r.to_entity_id = e.id) WHERE e.repo_id = $1`, repoID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting entities with relationships: %w", err)
	}
	return count, nil
}

func (s *EntityStore) FindByPath(ctx context.Context, repoID uuid.UUID, path string) (*Entity, error) {
	e := &Entity{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND path = $2`, repoID, path,
	).Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding entity by path: %w", err)
	}
	return e, nil
}

// FindByPathSuffix finds the first entity whose path ends with the given suffix.
// Used as a fallback when exact path match fails (e.g. worktree-indexed paths).
func (s *EntityStore) FindByPathSuffix(ctx context.Context, repoID uuid.UUID, suffix string) (*Entity, error) {
	e := &Entity{}
	pattern := "%/" + suffix
	err := s.Pool.QueryRow(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND path LIKE $2 LIMIT 1`, repoID, pattern,
	).Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding entity by path suffix: %w", err)
	}
	return e, nil
}

// ListByPathSuffix returns all entities whose path ends with the given suffix.
func (s *EntityStore) ListByPathSuffix(ctx context.Context, repoID uuid.UUID, suffix string) ([]Entity, error) {
	pattern := "%/" + suffix
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND path LIKE $2`, repoID, pattern)
	if err != nil {
		return nil, fmt.Errorf("listing entities by path suffix: %w", err)
	}
	defer rows.Close()
	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entities = append(entities, e)
	}
	return entities, nil
}

func (s *EntityStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM entities WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting entities: %w", err)
	}
	return nil
}

// ListDistinctPaths returns all distinct file paths for entities in a repo.
func (s *EntityStore) ListDistinctPaths(ctx context.Context, repoID uuid.UUID) ([]string, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT DISTINCT path FROM entities WHERE repo_id = $1 AND path IS NOT NULL`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing distinct paths: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scanning path: %w", err)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

type EntitySearchResult struct {
	Items []Entity `json:"items"`
	Total int      `json:"total"`
}

func (s *EntityStore) SearchByName(ctx context.Context, repoID *uuid.UUID, query, kind string, limit, offset int) (*EntitySearchResult, error) {
	where := "WHERE 1=1"
	args := []any{}
	argIdx := 1

	if repoID != nil {
		where += fmt.Sprintf(" AND repo_id = $%d", argIdx)
		args = append(args, *repoID)
		argIdx++
	}
	if kind != "" {
		where += fmt.Sprintf(" AND kind = $%d", argIdx)
		args = append(args, kind)
		argIdx++
	}
	if query != "" {
		where += fmt.Sprintf(" AND (name ILIKE $%d OR qualified_name ILIKE $%d)", argIdx, argIdx)
		args = append(args, "%"+query+"%")
		argIdx++
	}

	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	err := s.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM entities "+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("counting entities: %w", err)
	}

	sql := fmt.Sprintf(
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities %s ORDER BY qualified_name LIMIT $%d OFFSET $%d`, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("searching entities: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	if entities == nil {
		entities = []Entity{}
	}
	return &EntitySearchResult{Items: entities, Total: total}, nil
}

// EntityWithSimilarity pairs an entity with a pg_trgm similarity score.
type EntityWithSimilarity struct {
	Entity
	Similarity float64
}

// SearchFuzzy finds entities with names similar to the given name using pg_trgm.
// If repoID is non-nil, results are scoped to that repo. threshold controls minimum
// similarity (0.0 to 1.0, recommended 0.3+).
func (s *EntityStore) SearchFuzzy(ctx context.Context, name string, repoID *uuid.UUID, threshold float64, limit int) ([]EntityWithSimilarity, error) {
	if threshold <= 0 {
		threshold = 0.3
	}
	if limit <= 0 {
		limit = 10
	}

	normalized := NormalizeName(name)

	where := "WHERE similarity(name_normalized, $1) >= $2"
	args := []any{normalized, threshold}
	argIdx := 3

	if repoID != nil {
		where += fmt.Sprintf(" AND repo_id = $%d", argIdx)
		args = append(args, *repoID)
		argIdx++
	}

	query := fmt.Sprintf(
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at,
		        similarity(name_normalized, $1) AS sim
		 FROM entities %s
		 ORDER BY sim DESC LIMIT $%d`, where, argIdx)
	args = append(args, limit)

	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fuzzy searching entities: %w", err)
	}
	defer rows.Close()

	var results []EntityWithSimilarity
	for rows.Next() {
		var ews EntityWithSimilarity
		if err := rows.Scan(&ews.ID, &ews.RepoID, &ews.Kind, &ews.Name, &ews.QualifiedName, &ews.Path,
			&ews.Summary, &ews.Capabilities, &ews.Assumptions, &ews.Signature, &ews.TypeRef, &ews.StartLine, &ews.EndLine, &ews.CreatedAt, &ews.UpdatedAt, &ews.Similarity); err != nil {
			return nil, fmt.Errorf("scanning fuzzy entity: %w", err)
		}
		results = append(results, ews)
	}
	return results, nil
}

// NormalizeName normalizes an entity name for fuzzy comparison:
// strips separators (_-spaces), lowercases, and collapses camelCase.
func NormalizeName(name string) string {
	// Remove common separators
	var b []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '_' || c == '-' || c == ' ' {
			continue
		}
		// Lowercase
		if c >= 'A' && c <= 'Z' {
			b = append(b, c+32)
		} else {
			b = append(b, c)
		}
	}
	return string(b)
}

// ListByPath returns all entities for a given file path in a repo, ordered by start_line.
func (s *EntityStore) ListByPath(ctx context.Context, repoID uuid.UUID, path string) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND path = $2 ORDER BY start_line NULLS LAST, qualified_name`, repoID, path,
	)
	if err != nil {
		return nil, fmt.Errorf("listing entities by path: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

// Delete removes a single entity by ID, cascading to facts and relationships.
func (s *EntityStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM facts WHERE entity_id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting facts for entity %s: %w", id, err)
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM relationships WHERE from_entity_id = $1 OR to_entity_id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting relationships for entity %s: %w", id, err)
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM entities WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting entity %s: %w", id, err)
	}
	return nil
}

// ScoredEntity wraps an Entity with its cosine similarity score from vector search.
type ScoredEntity struct {
	Entity
	Score float64
}

// UpdateSummaryEmbedding sets the summary_embedding vector for an entity.
func (s *EntityStore) UpdateSummaryEmbedding(ctx context.Context, id uuid.UUID, embedding pgvector.Vector) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE entities SET summary_embedding = $2 WHERE id = $1`,
		id, embedding,
	)
	if err != nil {
		return fmt.Errorf("updating summary embedding: %w", err)
	}
	return nil
}

// ListByRepoWithoutSummaryEmbedding returns entities that have a summary but no summary embedding.
func (s *EntityStore) ListByRepoWithoutSummaryEmbedding(ctx context.Context, repoID uuid.UUID) ([]Entity, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at
		 FROM entities WHERE repo_id = $1 AND summary IS NOT NULL AND summary_embedding IS NULL`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing entities without summary embedding: %w", err)
	}
	defer rows.Close()

	var entities []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.RepoID, &e.Kind, &e.Name, &e.QualifiedName, &e.Path, &e.Summary, &e.Capabilities, &e.Assumptions, &e.Signature, &e.TypeRef, &e.StartLine, &e.EndLine, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning entity: %w", err)
		}
		entities = append(entities, e)
	}
	return entities, nil
}

// SearchBySummaryVector performs vector similarity search on entity summaries.
// Returns entities whose summary embedding has cosine similarity >= 0.4 with the query vector.
func (s *EntityStore) SearchBySummaryVector(ctx context.Context, embedding pgvector.Vector, repoIDs []uuid.UUID, limit int) ([]ScoredEntity, error) {
	query := `SELECT id, repo_id, kind, name, qualified_name, path, summary, capabilities, assumptions, signature, typeref, start_line, end_line, created_at, updated_at,
		 1 - (summary_embedding <=> $1) AS score
		 FROM entities WHERE summary_embedding IS NOT NULL AND 1 - (summary_embedding <=> $1) >= 0.4`
	args := []any{embedding}
	argIdx := 2

	if len(repoIDs) > 0 {
		query += fmt.Sprintf(" AND repo_id = ANY($%d)", argIdx)
		args = append(args, repoIDs)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY summary_embedding <=> $1 LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("summary vector search: %w", err)
	}
	defer rows.Close()

	var results []ScoredEntity
	for rows.Next() {
		var se ScoredEntity
		if err := rows.Scan(&se.ID, &se.RepoID, &se.Kind, &se.Name, &se.QualifiedName, &se.Path, &se.Summary, &se.Capabilities, &se.Assumptions, &se.Signature, &se.TypeRef, &se.StartLine, &se.EndLine, &se.CreatedAt, &se.UpdatedAt, &se.Score); err != nil {
			return nil, fmt.Errorf("scanning scored entity: %w", err)
		}
		results = append(results, se)
	}
	return results, nil
}

// MaxSummarySimilarity returns the maximum cosine similarity between the query vector
// and summary embeddings for each entity ID. Used as a fallback in triplet scoring
// when entities have no matching facts.
func (s *EntityStore) MaxSummarySimilarity(ctx context.Context, queryVec pgvector.Vector, entityIDs []uuid.UUID) (map[uuid.UUID]float64, error) {
	if len(entityIDs) == 0 {
		return make(map[uuid.UUID]float64), nil
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT id, 1 - (summary_embedding <=> $1) AS score
		 FROM entities
		 WHERE id = ANY($2) AND summary_embedding IS NOT NULL`,
		queryVec, entityIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("computing max summary similarity: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]float64, len(entityIDs))
	for rows.Next() {
		var eid uuid.UUID
		var score float64
		if err := rows.Scan(&eid, &score); err != nil {
			return nil, fmt.Errorf("scanning summary similarity: %w", err)
		}
		result[eid] = score
	}
	return result, nil
}

// DeleteByPath deletes all entities (and cascading facts/relationships) for a given path in a repo.
func (s *EntityStore) DeleteByPath(ctx context.Context, repoID uuid.UUID, path string) error {
	// Delete facts and relationships first (entities FK-referenced by facts.entity_id and relationships.from/to)
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM facts WHERE entity_id IN (SELECT id FROM entities WHERE repo_id = $1 AND path = $2)`, repoID, path)
	if err != nil {
		return fmt.Errorf("deleting facts for path %s: %w", path, err)
	}
	_, err = s.Pool.Exec(ctx,
		`DELETE FROM relationships WHERE from_entity_id IN (SELECT id FROM entities WHERE repo_id = $1 AND path = $2)
		 OR to_entity_id IN (SELECT id FROM entities WHERE repo_id = $1 AND path = $2)`, repoID, path)
	if err != nil {
		return fmt.Errorf("deleting relationships for path %s: %w", path, err)
	}
	_, err = s.Pool.Exec(ctx,
		`DELETE FROM entities WHERE repo_id = $1 AND path = $2`, repoID, path)
	if err != nil {
		return fmt.Errorf("deleting entities for path %s: %w", path, err)
	}
	return nil
}
