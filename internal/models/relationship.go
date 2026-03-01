package models

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RelationshipStore struct {
	Pool *pgxpool.Pool
}

func (s *RelationshipStore) Create(ctx context.Context, r *Relationship) error {
	r.ID = uuid.New()
	r.CreatedAt = time.Now()

	provJSON, err := json.Marshal(r.Provenance)
	if err != nil {
		return fmt.Errorf("marshaling provenance: %w", err)
	}

	_, err = s.Pool.Exec(ctx,
		`INSERT INTO relationships (id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		r.ID, r.RepoID, r.FromEntityID, r.ToEntityID, r.Kind, r.Description, r.Strength, provJSON, r.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting relationship: %w", err)
	}
	return nil
}

func (s *RelationshipStore) Upsert(ctx context.Context, r *Relationship) error {
	r.CreatedAt = time.Now()

	provJSON, err := json.Marshal(r.Provenance)
	if err != nil {
		return fmt.Errorf("marshaling provenance: %w", err)
	}

	err = s.Pool.QueryRow(ctx,
		`INSERT INTO relationships (id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (repo_id, from_entity_id, to_entity_id, kind) DO UPDATE SET
		   description = COALESCE(EXCLUDED.description, relationships.description),
		   strength = EXCLUDED.strength,
		   provenance = EXCLUDED.provenance
		 RETURNING id`,
		uuid.New(), r.RepoID, r.FromEntityID, r.ToEntityID, r.Kind, r.Description, r.Strength, provJSON, r.CreatedAt,
	).Scan(&r.ID)
	if err != nil {
		return fmt.Errorf("upserting relationship: %w", err)
	}
	return nil
}

func (s *RelationshipStore) ListByEntity(ctx context.Context, entityID uuid.UUID) ([]Relationship, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at
		 FROM relationships WHERE from_entity_id = $1 OR to_entity_id = $1 ORDER BY kind`, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing relationships: %w", err)
	}
	defer rows.Close()

	var rels []Relationship
	for rows.Next() {
		var r Relationship
		var provJSON []byte
		if err := rows.Scan(&r.ID, &r.RepoID, &r.FromEntityID, &r.ToEntityID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning relationship: %w", err)
		}
		if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		rels = append(rels, r)
	}
	return rels, nil
}

func (s *RelationshipStore) ListByRepo(ctx context.Context, repoID uuid.UUID) ([]Relationship, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at
		 FROM relationships WHERE repo_id = $1 ORDER BY kind`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing relationships by repo: %w", err)
	}
	defer rows.Close()

	var rels []Relationship
	for rows.Next() {
		var r Relationship
		var provJSON []byte
		if err := rows.Scan(&r.ID, &r.RepoID, &r.FromEntityID, &r.ToEntityID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning relationship: %w", err)
		}
		if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		rels = append(rels, r)
	}
	return rels, nil
}

func (s *RelationshipStore) ListDependentsOf(ctx context.Context, entityID uuid.UUID, limit int) ([]Relationship, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at
		 FROM relationships WHERE to_entity_id = $1
		 ORDER BY kind LIMIT $2`, entityID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("listing dependents: %w", err)
	}
	defer rows.Close()

	var rels []Relationship
	for rows.Next() {
		var r Relationship
		var provJSON []byte
		if err := rows.Scan(&r.ID, &r.RepoID, &r.FromEntityID, &r.ToEntityID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning relationship: %w", err)
		}
		if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		rels = append(rels, r)
	}
	return rels, nil
}

func (s *RelationshipStore) CountByRepo(ctx context.Context, repoID uuid.UUID) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM relationships WHERE repo_id = $1`, repoID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting relationships: %w", err)
	}
	return count, nil
}

func (s *RelationshipStore) DeleteByRepo(ctx context.Context, repoID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM relationships WHERE repo_id = $1`, repoID)
	if err != nil {
		return fmt.Errorf("deleting relationships: %w", err)
	}
	return nil
}

// CrossRepoRelationship extends Relationship with explicit from/to repo IDs.
type CrossRepoRelationship struct {
	ID           uuid.UUID    `json:"id"`
	FromEntityID uuid.UUID    `json:"from_entity_id"`
	ToEntityID   uuid.UUID    `json:"to_entity_id"`
	FromRepoID   uuid.UUID    `json:"from_repo_id"`
	ToRepoID     uuid.UUID    `json:"to_repo_id"`
	Kind         string       `json:"kind"`
	Description  *string      `json:"description,omitempty"`
	Strength     string       `json:"strength"`
	Provenance   []Provenance `json:"provenance"`
	CreatedAt    time.Time    `json:"created_at"`
}

// CreateCrossRepo creates a relationship between entities in different repos.
func (s *RelationshipStore) CreateCrossRepo(ctx context.Context, cr *CrossRepoRelationship) error {
	cr.ID = uuid.New()
	cr.CreatedAt = time.Now()

	provJSON, err := json.Marshal(cr.Provenance)
	if err != nil {
		return fmt.Errorf("marshaling provenance: %w", err)
	}

	_, err = s.Pool.Exec(ctx,
		`INSERT INTO cross_repo_relationships (id, from_entity_id, to_entity_id, from_repo_id, to_repo_id, kind, description, strength, provenance, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		cr.ID, cr.FromEntityID, cr.ToEntityID, cr.FromRepoID, cr.ToRepoID, cr.Kind, cr.Description, cr.Strength, provJSON, cr.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting cross-repo relationship: %w", err)
	}
	return nil
}

// UpsertCrossRepo creates or updates a cross-repo relationship using the unique index.
func (s *RelationshipStore) UpsertCrossRepo(ctx context.Context, cr *CrossRepoRelationship) error {
	cr.CreatedAt = time.Now()

	provJSON, err := json.Marshal(cr.Provenance)
	if err != nil {
		return fmt.Errorf("marshaling provenance: %w", err)
	}

	err = s.Pool.QueryRow(ctx,
		`INSERT INTO cross_repo_relationships (id, from_entity_id, to_entity_id, from_repo_id, to_repo_id, kind, description, strength, provenance, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (from_entity_id, to_entity_id, kind) DO UPDATE SET
		   provenance = EXCLUDED.provenance,
		   strength = EXCLUDED.strength,
		   description = COALESCE(EXCLUDED.description, cross_repo_relationships.description)
		 RETURNING id`,
		uuid.New(), cr.FromEntityID, cr.ToEntityID, cr.FromRepoID, cr.ToRepoID, cr.Kind, cr.Description, cr.Strength, provJSON, cr.CreatedAt,
	).Scan(&cr.ID)
	if err != nil {
		return fmt.Errorf("upserting cross-repo relationship: %w", err)
	}
	return nil
}

// ListAllCrossRepo returns all cross-repo relationships.
func (s *RelationshipStore) ListAllCrossRepo(ctx context.Context) ([]CrossRepoRelationship, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, from_entity_id, to_entity_id, from_repo_id, to_repo_id, kind, description, strength, provenance, created_at
		 FROM cross_repo_relationships ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing all cross-repo relationships: %w", err)
	}
	defer rows.Close()

	var rels []CrossRepoRelationship
	for rows.Next() {
		var r CrossRepoRelationship
		var provJSON []byte
		if err := rows.Scan(&r.ID, &r.FromEntityID, &r.ToEntityID, &r.FromRepoID, &r.ToRepoID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning cross-repo relationship: %w", err)
		}
		if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		rels = append(rels, r)
	}
	return rels, nil
}

// GetCrossRepoByID returns a single cross-repo relationship by ID.
func (s *RelationshipStore) GetCrossRepoByID(ctx context.Context, id uuid.UUID) (*CrossRepoRelationship, error) {
	var r CrossRepoRelationship
	var provJSON []byte
	err := s.Pool.QueryRow(ctx,
		`SELECT id, from_entity_id, to_entity_id, from_repo_id, to_repo_id, kind, description, strength, provenance, created_at
		 FROM cross_repo_relationships WHERE id = $1`, id,
	).Scan(&r.ID, &r.FromEntityID, &r.ToEntityID, &r.FromRepoID, &r.ToRepoID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting cross-repo relationship: %w", err)
	}
	if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
		return nil, fmt.Errorf("unmarshaling provenance: %w", err)
	}
	return &r, nil
}

// DeleteCrossRepo deletes a cross-repo relationship by ID.
func (s *RelationshipStore) DeleteCrossRepo(ctx context.Context, id uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM cross_repo_relationships WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting cross-repo relationship: %w", err)
	}
	return nil
}

// ListCrossRepoByEntity returns all cross-repo relationships involving an entity.
func (s *RelationshipStore) ListCrossRepoByEntity(ctx context.Context, entityID uuid.UUID) ([]CrossRepoRelationship, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, from_entity_id, to_entity_id, from_repo_id, to_repo_id, kind, description, strength, provenance, created_at
		 FROM cross_repo_relationships WHERE from_entity_id = $1 OR to_entity_id = $1 ORDER BY kind`, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing cross-repo relationships: %w", err)
	}
	defer rows.Close()

	var rels []CrossRepoRelationship
	for rows.Next() {
		var r CrossRepoRelationship
		var provJSON []byte
		if err := rows.Scan(&r.ID, &r.FromEntityID, &r.ToEntityID, &r.FromRepoID, &r.ToRepoID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning cross-repo relationship: %w", err)
		}
		if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		rels = append(rels, r)
	}
	return rels, nil
}

// ListCrossRepoByRepo returns all cross-repo relationships involving a repo.
func (s *RelationshipStore) ListCrossRepoByRepo(ctx context.Context, repoID uuid.UUID) ([]CrossRepoRelationship, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, from_entity_id, to_entity_id, from_repo_id, to_repo_id, kind, description, strength, provenance, created_at
		 FROM cross_repo_relationships WHERE from_repo_id = $1 OR to_repo_id = $1 ORDER BY kind`, repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing cross-repo relationships by repo: %w", err)
	}
	defer rows.Close()

	var rels []CrossRepoRelationship
	for rows.Next() {
		var r CrossRepoRelationship
		var provJSON []byte
		if err := rows.Scan(&r.ID, &r.FromEntityID, &r.ToEntityID, &r.FromRepoID, &r.ToRepoID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning cross-repo relationship: %w", err)
		}
		if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		rels = append(rels, r)
	}
	return rels, nil
}

// TraverseFromEntity performs N-hop bidirectional graph traversal from a seed entity
// using a recursive CTE. Returns a Subgraph with all discovered entities, relationships,
// and optionally facts within the specified hop distance.
func (s *RelationshipStore) TraverseFromEntity(ctx context.Context, seedID uuid.UUID, opts TraversalOptions) (*Subgraph, error) {
	if opts.MaxHops <= 0 {
		opts.MaxHops = 3
	}
	if opts.MaxHops > 5 {
		opts.MaxHops = 5
	}
	if opts.MaxEntities <= 0 {
		opts.MaxEntities = 200
	}
	if opts.FactsPerEntity <= 0 {
		opts.FactsPerEntity = 10
	}

	// Build relationship kind filter clause
	relKindFilter := ""
	args := []any{seedID, opts.MaxHops, opts.MaxEntities}
	argIdx := 4

	if len(opts.RelKinds) > 0 {
		relKindFilter = fmt.Sprintf(" AND r.kind = ANY($%d)", argIdx)
		args = append(args, opts.RelKinds)
		argIdx++
	}

	// Build the source tables — either just relationships, or UNION with cross_repo_relationships
	relSource := "relationships"
	if opts.CrossRepo {
		relSource = `(
			SELECT id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at
			FROM relationships
			UNION ALL
			SELECT id, from_repo_id AS repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at
			FROM cross_repo_relationships
		)`
	}

	query := fmt.Sprintf(`
		WITH RECURSIVE graph AS (
			-- Base case: the seed entity at depth 0
			SELECT $1::uuid AS entity_id, 0 AS depth
			UNION
			-- Recursive step: follow relationships bidirectionally
			SELECT
				CASE WHEN r.from_entity_id = g.entity_id THEN r.to_entity_id
				     ELSE r.from_entity_id END AS entity_id,
				g.depth + 1 AS depth
			FROM graph g
			JOIN %s r ON (r.from_entity_id = g.entity_id OR r.to_entity_id = g.entity_id)%s
			WHERE g.depth < $2
		)
		SELECT DISTINCT ON (entity_id) entity_id, depth
		FROM graph
		ORDER BY entity_id, depth ASC
		LIMIT $3
	`, relSource, relKindFilter)

	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("traversing graph: %w", err)
	}
	defer rows.Close()

	entityDepths := make(map[uuid.UUID]int)
	var entityIDs []uuid.UUID
	for rows.Next() {
		var eid uuid.UUID
		var depth int
		if err := rows.Scan(&eid, &depth); err != nil {
			return nil, fmt.Errorf("scanning traversal result: %w", err)
		}
		entityDepths[eid] = depth
		entityIDs = append(entityIDs, eid)
	}

	if len(entityIDs) == 0 {
		return &Subgraph{
			SeedEntityID:  seedID,
			Entities:      make(map[uuid.UUID]Entity),
			Relationships: nil,
			Facts:         make(map[uuid.UUID][]Fact),
			Depths:        make(map[uuid.UUID]int),
		}, nil
	}

	// Batch-fetch all discovered entities
	entityStore := &EntityStore{Pool: s.Pool}
	entities, err := entityStore.GetByIDs(ctx, entityIDs)
	if err != nil {
		return nil, fmt.Errorf("fetching entities: %w", err)
	}

	entityMap := make(map[uuid.UUID]Entity, len(entities))
	for _, e := range entities {
		entityMap[e.ID] = e
	}

	// Fetch all relationships between discovered entities
	rels, err := s.listRelationshipsBetween(ctx, entityIDs, opts.CrossRepo)
	if err != nil {
		return nil, fmt.Errorf("fetching relationships: %w", err)
	}

	// Filter by rel kinds if specified
	if len(opts.RelKinds) > 0 {
		kindSet := make(map[string]bool, len(opts.RelKinds))
		for _, k := range opts.RelKinds {
			kindSet[k] = true
		}
		filtered := rels[:0]
		for _, r := range rels {
			if kindSet[r.Kind] {
				filtered = append(filtered, r)
			}
		}
		rels = filtered
	}

	subgraph := &Subgraph{
		SeedEntityID:  seedID,
		Entities:      entityMap,
		Relationships: rels,
		Facts:         make(map[uuid.UUID][]Fact),
		Depths:        entityDepths,
	}

	// Optionally fetch facts for discovered entities
	if opts.IncludeFacts {
		factStore := &FactStore{Pool: s.Pool}
		for _, eid := range entityIDs {
			facts, err := factStore.ListByEntityLimited(ctx, eid, opts.FactsPerEntity)
			if err != nil {
				continue
			}
			subgraph.Facts[eid] = facts
		}
	}

	return subgraph, nil
}

// listRelationshipsBetween fetches all relationships where both endpoints are in the given entity set.
func (s *RelationshipStore) listRelationshipsBetween(ctx context.Context, entityIDs []uuid.UUID, crossRepo bool) ([]Relationship, error) {
	query := `SELECT id, repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at
		 FROM relationships
		 WHERE from_entity_id = ANY($1) AND to_entity_id = ANY($1)`

	if crossRepo {
		query += `
		 UNION ALL
		 SELECT id, from_repo_id AS repo_id, from_entity_id, to_entity_id, kind, description, strength, provenance, created_at
		 FROM cross_repo_relationships
		 WHERE from_entity_id = ANY($1) AND to_entity_id = ANY($1)`
	}

	rows, err := s.Pool.Query(ctx, query, entityIDs)
	if err != nil {
		return nil, fmt.Errorf("listing relationships between entities: %w", err)
	}
	defer rows.Close()

	var rels []Relationship
	for rows.Next() {
		var r Relationship
		var provJSON []byte
		if err := rows.Scan(&r.ID, &r.RepoID, &r.FromEntityID, &r.ToEntityID, &r.Kind, &r.Description, &r.Strength, &provJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning relationship: %w", err)
		}
		if err := json.Unmarshal(provJSON, &r.Provenance); err != nil {
			return nil, fmt.Errorf("unmarshaling provenance: %w", err)
		}
		rels = append(rels, r)
	}
	return rels, nil
}
