package query

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// ScoredTriplet represents a (source, relationship, target) triplet scored as a unit.
type ScoredTriplet struct {
	Source       models.Entity       `json:"source"`
	Relationship models.Relationship `json:"relationship"`
	Target       models.Entity       `json:"target"`
	SourceFacts  []models.Fact       `json:"source_facts,omitempty"`
	TargetFacts  []models.Fact       `json:"target_facts,omitempty"`
	Score        float64             `json:"score"`
}

// TripletSearchOptions configures triplet-ranked search.
type TripletSearchOptions struct {
	SeedLimit      int  // max seed facts from vector search (default 15)
	TraversalHops  int  // hops from each seed entity (default 2)
	MaxTriplets    int  // max triplets to return (default 20)
	IncludeFacts   bool // attach top facts per entity
	FactsPerEntity int  // max facts per entity (default 5)
}

// SearchTriplets performs triplet-ranked search: embed query, find seed entities via
// vector search, traverse their neighborhoods, then score (source, rel, target) triplets.
func (e *Engine) SearchTriplets(ctx context.Context, question string, repoIDs []uuid.UUID, opts TripletSearchOptions) ([]ScoredTriplet, error) {
	if opts.SeedLimit <= 0 {
		opts.SeedLimit = 15
	}
	if opts.TraversalHops <= 0 {
		opts.TraversalHops = 2
	}
	if opts.MaxTriplets <= 0 {
		opts.MaxTriplets = 20
	}
	if opts.FactsPerEntity <= 0 {
		opts.FactsPerEntity = 5
	}

	// Step 1: Embed query and vector search for seed facts
	vectors, err := e.Embedder.Embed(ctx, []string{question}, embeddings.DefaultModel)
	if err != nil {
		return nil, fmt.Errorf("embedding question: %w", err)
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}
	queryVec := pgvector.NewVector(vectors[0])

	factStore := &models.FactStore{Pool: e.Pool}
	seedFacts, err := factStore.SearchByVector(ctx, queryVec, repoIDs, opts.SeedLimit)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	if len(seedFacts) == 0 {
		return nil, nil
	}

	// Step 2: Collect seed entity IDs and traverse neighborhoods
	seedEntityIDs := make(map[uuid.UUID]bool)
	for _, sf := range seedFacts {
		seedEntityIDs[sf.EntityID] = true
	}

	relStore := &models.RelationshipStore{Pool: e.Pool}
	allEntities := make(map[uuid.UUID]models.Entity)
	var allRels []models.Relationship
	seenRels := make(map[uuid.UUID]bool)

	for eid := range seedEntityIDs {
		subgraph, err := relStore.TraverseFromEntity(ctx, eid, models.TraversalOptions{
			MaxHops:     opts.TraversalHops,
			MaxEntities: 50,
		})
		if err != nil {
			continue
		}
		for id, entity := range subgraph.Entities {
			allEntities[id] = entity
		}
		for _, r := range subgraph.Relationships {
			if !seenRels[r.ID] {
				seenRels[r.ID] = true
				allRels = append(allRels, r)
			}
		}
	}

	if len(allRels) == 0 {
		return nil, nil
	}

	// Step 3: Batch-compute max similarity scores for all discovered entities
	entityIDs := make([]uuid.UUID, 0, len(allEntities))
	for id := range allEntities {
		entityIDs = append(entityIDs, id)
	}

	simScores, err := factStore.MaxSimilarityByEntity(ctx, queryVec, entityIDs)
	if err != nil {
		return nil, fmt.Errorf("computing similarity scores: %w", err)
	}

	// Fallback: use summary embedding similarity for entities with no matching facts
	entityStore := &models.EntityStore{Pool: e.Pool}
	summaryScores, _ := entityStore.MaxSummarySimilarity(ctx, queryVec, entityIDs)
	for eid, score := range summaryScores {
		if _, hasFact := simScores[eid]; !hasFact {
			simScores[eid] = score * 0.8 // discount vs fact-based similarity
		}
	}

	// Build target repo set for same-repo boosting
	targetRepoSet := make(map[uuid.UUID]bool, len(repoIDs))
	for _, rid := range repoIDs {
		targetRepoSet[rid] = true
	}

	// Step 4: Score each relationship as a triplet
	triplets := make([]ScoredTriplet, 0, len(allRels))
	for _, r := range allRels {
		source, sourceOK := allEntities[r.FromEntityID]
		target, targetOK := allEntities[r.ToEntityID]
		if !sourceOK || !targetOK {
			continue
		}

		sourceScore := simScores[r.FromEntityID]
		targetScore := simScores[r.ToEntityID]

		// Relationship score: average of endpoint scores weighted by strength
		strengthMultiplier := 0.5
		switch r.Strength {
		case models.StrengthStrong:
			strengthMultiplier = 1.0
		case models.StrengthModerate:
			strengthMultiplier = 0.7
		case models.StrengthWeak:
			strengthMultiplier = 0.4
		}
		relScore := (sourceScore + targetScore) / 2 * strengthMultiplier

		tripletScore := 0.4*sourceScore + 0.2*relScore + 0.4*targetScore

		// Same-repo affinity: boost triplets where both endpoints are in the target repo
		if len(targetRepoSet) > 0 {
			sourceInRepo := targetRepoSet[source.RepoID]
			targetInRepo := targetRepoSet[target.RepoID]
			if sourceInRepo && targetInRepo {
				tripletScore *= 1.3
			} else if sourceInRepo || targetInRepo {
				tripletScore *= 1.0 // no change for mixed
			} else {
				tripletScore *= 0.6 // penalize fully cross-repo
			}
		}

		triplets = append(triplets, ScoredTriplet{
			Source:       source,
			Relationship: r,
			Target:       target,
			Score:        tripletScore,
		})
	}

	// Sort by score descending
	sort.Slice(triplets, func(i, j int) bool {
		return triplets[i].Score > triplets[j].Score
	})

	if len(triplets) > opts.MaxTriplets {
		triplets = triplets[:opts.MaxTriplets]
	}

	// Step 5: Optionally attach facts
	if opts.IncludeFacts {
		for i := range triplets {
			sourceFacts, _ := factStore.ListByEntityLimited(ctx, triplets[i].Source.ID, opts.FactsPerEntity)
			targetFacts, _ := factStore.ListByEntityLimited(ctx, triplets[i].Target.ID, opts.FactsPerEntity)
			triplets[i].SourceFacts = sourceFacts
			triplets[i].TargetFacts = targetFacts
		}
	}

	return triplets, nil
}
