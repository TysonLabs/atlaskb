package pipeline

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// Phase6Config configures the community detection phase.
type Phase6Config struct {
	RepoID         uuid.UUID
	RepoName       string
	Model          string
	Pool           *pgxpool.Pool
	LLM            llm.Client
	MinClusterSize int // default 3
}

// Phase6Stats reports what phase 6 produced.
type Phase6Stats struct {
	EntitiesInGraph int
	EdgesInGraph    int
	ClustersFound   int
	ClustersStored  int
	Modularity      float64
}

// RunPhase6 performs functional clustering using Louvain community detection.
func RunPhase6(ctx context.Context, cfg Phase6Config) (*Phase6Stats, error) {
	if cfg.MinClusterSize <= 0 {
		cfg.MinClusterSize = 3
	}

	entityStore := &models.EntityStore{Pool: cfg.Pool}
	relStore := &models.RelationshipStore{Pool: cfg.Pool}

	// 1. Load all entities for repo, excluding clusters and external deps
	allEntities, err := entityStore.ListByRepo(ctx, cfg.RepoID)
	if err != nil {
		return nil, fmt.Errorf("loading entities: %w", err)
	}

	var entities []models.Entity
	entityByID := make(map[uuid.UUID]models.Entity)
	for _, e := range allEntities {
		// Skip existing cluster entities
		if e.Kind == models.EntityCluster {
			continue
		}
		// Skip external dependencies (module entities with no path)
		if e.Kind == models.EntityModule && e.Path == nil {
			continue
		}
		entities = append(entities, e)
		entityByID[e.ID] = e
	}

	if len(entities) < cfg.MinClusterSize {
		return &Phase6Stats{EntitiesInGraph: len(entities)}, nil
	}

	// 2. Load all relationships for those entities, filtered to weighted kinds
	allRels, err := relStore.ListByRepo(ctx, cfg.RepoID)
	if err != nil {
		return nil, fmt.Errorf("loading relationships: %w", err)
	}

	// 3. Build graph
	graph := NewGraph(len(entities))
	for _, e := range entities {
		graph.AddNode(e.ID)
	}

	edgeCount := 0
	for _, r := range allRels {
		weight, ok := relWeights[r.Kind]
		if !ok {
			continue
		}
		// Only include edges where both endpoints are in our entity set
		if _, ok := entityByID[r.FromEntityID]; !ok {
			continue
		}
		if _, ok := entityByID[r.ToEntityID]; !ok {
			continue
		}
		graph.AddEdge(r.FromEntityID, r.ToEntityID, weight)
		edgeCount++
	}

	if edgeCount == 0 {
		return &Phase6Stats{EntitiesInGraph: len(entities), EdgesInGraph: 0}, nil
	}

	// 4. Run Louvain
	result := graph.louvainDetect(10, 1e-6)

	stats := &Phase6Stats{
		EntitiesInGraph: len(entities),
		EdgesInGraph:    edgeCount,
		ClustersFound:   result.NumCommunities,
		Modularity:      result.Modularity,
	}

	// 5. Group entities by community
	communities := make(map[int][]models.Entity)
	for nodeIdx, commID := range result.Communities {
		if nodeIdx < len(graph.nodeToID) {
			eid := graph.nodeToID[nodeIdx]
			if e, ok := entityByID[eid]; ok {
				communities[commID] = append(communities[commID], e)
			}
		}
	}

	// 6. Clean up stale clusters from prior runs
	oldClusters, err := entityStore.ListByRepoAndKind(ctx, cfg.RepoID, models.EntityCluster)
	if err != nil {
		log.Printf("[phase6] warn: listing old clusters: %v", err)
	} else {
		for _, old := range oldClusters {
			if err := entityStore.Delete(ctx, old.ID); err != nil {
				logVerboseF("[phase6] warn: deleting old cluster %s: %v", old.Name, err)
			}
		}
		if len(oldClusters) > 0 {
			log.Printf("[phase6] cleaned up %d stale clusters", len(oldClusters))
		}
	}

	// 7. For each community >= MinClusterSize: label, store cluster entity, store member_of relationships
	stored := 0
	for commID, members := range communities {
		if len(members) < cfg.MinClusterSize {
			continue
		}

		// Try LLM labeling first, fall back to keyword
		var label *ClusterLabel
		if cfg.LLM != nil && cfg.Model != "" {
			var llmErr error
			label, llmErr = LabelCluster(ctx, cfg.LLM, cfg.Model, members)
			if llmErr != nil {
				logVerboseF("[phase6] LLM labeling failed for community %d: %v, using keyword fallback", commID, llmErr)
				label = nil
			}
		}
		if label == nil {
			label = KeywordLabelCluster(members)
		}

		// Compute cohesion score: fraction of possible intra-cluster edges that exist
		cohesion := computeCohesion(graph, members)

		qualifiedName := fmt.Sprintf("cluster::%s", label.Label)
		description := label.Description
		if cohesion > 0 {
			description = fmt.Sprintf("%s (cohesion=%.2f)", description, cohesion)
		}

		clusterEntity := &models.Entity{
			RepoID:        cfg.RepoID,
			Kind:          models.EntityCluster,
			Name:          label.Label,
			QualifiedName: qualifiedName,
			Summary:       models.Ptr(description),
			Capabilities:  []string{label.Domain},
		}
		if err := entityStore.Upsert(ctx, clusterEntity); err != nil {
			logVerboseF("[phase6] warn: upserting cluster entity %s: %v", label.Label, err)
			continue
		}

		// Store member_of relationships
		for _, member := range members {
			rel := &models.Relationship{
				RepoID:       cfg.RepoID,
				FromEntityID: member.ID,
				ToEntityID:   clusterEntity.ID,
				Kind:         models.RelMemberOf,
				Description:  models.Ptr(fmt.Sprintf("%s is a member of cluster %s", member.Name, label.Label)),
				Strength:     models.StrengthStrong,
				Confidence:   models.ConfRelDeterministicOwns,
				Provenance: []models.Provenance{{
					SourceType: "file",
					Repo:       cfg.RepoName,
					Ref:        "phase6-clustering",
				}},
			}
			if err := relStore.Upsert(ctx, rel); err != nil {
				logVerboseF("[phase6] warn: creating member_of for %s: %v", member.Name, err)
			}
		}

		stored++
		log.Printf("[phase6] cluster %q: %d members, cohesion=%.2f", label.Label, len(members), cohesion)
	}

	stats.ClustersStored = stored
	return stats, nil
}

// computeCohesion calculates the fraction of possible intra-cluster edges that exist.
func computeCohesion(g *Graph, members []models.Entity) float64 {
	if len(members) < 2 {
		return 1.0
	}

	memberSet := make(map[int]bool)
	for _, m := range members {
		if idx, ok := g.idToNode[m.ID]; ok {
			memberSet[idx] = true
		}
	}

	existing := 0
	for idx := range memberSet {
		for neighbor := range g.adj[idx] {
			if memberSet[neighbor] && neighbor > idx {
				existing++
			}
		}
	}

	n := len(memberSet)
	possible := n * (n - 1) / 2
	if possible == 0 {
		return 0
	}
	return float64(existing) / float64(possible)
}
