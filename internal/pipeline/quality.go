package pipeline

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type QualityScore struct {
	Overall           float64
	EntityCoverage    float64
	FactDensity       float64
	RelConnectivity   float64
	DimensionCoverage float64
	ParseSuccessRate  float64
	EntityCount       int
	FactCount         int
	RelationshipCount int
	DecisionCount     int

	// Raw diagnostic values for understanding the score
	NonDepEntities    int
	ExternalDepCount  int
	EntitiesWithFacts int
	EntitiesWithRels  int
	FactsPerEntity    float64
	DimensionBreakdown map[string]int
	EntityKindBreakdown map[string]int
	TotalJobs          int
	SuccessfulJobs     int
}

// QualityCounts holds the raw counts needed to compute a quality score.
// This struct allows ComputeQualityFromCounts to be a pure function testable without a database.
type QualityCounts struct {
	TotalEntities    int
	ExternalDepCount int
	TotalFacts       int
	TotalRels        int
	DecisionCount    int
	EntitiesWithFacts int
	EntitiesWithRels  int
	TotalJobs        int
	SuccessfulJobs   int
	ByDimension      map[string]int
	ByKind           map[string]int
}

// ComputeQualityFromCounts computes a quality score from raw counts (pure function, no DB).
func ComputeQualityFromCounts(c QualityCounts) *QualityScore {
	nonDepEntities := c.TotalEntities - c.ExternalDepCount
	if nonDepEntities < 0 {
		nonDepEntities = 0
	}

	factsPerEntity := 0.0
	if nonDepEntities > 0 {
		factsPerEntity = float64(c.TotalFacts) / float64(nonDepEntities)
	}

	qs := &QualityScore{
		EntityCount:         c.TotalEntities,
		FactCount:           c.TotalFacts,
		RelationshipCount:   c.TotalRels,
		DecisionCount:       c.DecisionCount,
		NonDepEntities:      nonDepEntities,
		ExternalDepCount:    c.ExternalDepCount,
		EntitiesWithFacts:   c.EntitiesWithFacts,
		EntitiesWithRels:    c.EntitiesWithRels,
		FactsPerEntity:      factsPerEntity,
		DimensionBreakdown:  c.ByDimension,
		EntityKindBreakdown: c.ByKind,
		TotalJobs:           c.TotalJobs,
		SuccessfulJobs:      c.SuccessfulJobs,
	}

	// 1. Entity coverage (30%): entities with ≥1 fact / non-dep entities
	if nonDepEntities > 0 {
		qs.EntityCoverage = math.Min(1.0, float64(c.EntitiesWithFacts)/float64(nonDepEntities)) * 100
	} else {
		qs.EntityCoverage = 0
	}

	// 2. Fact density (20%): penalize < 1.5 facts/entity or > 8 facts/entity
	if nonDepEntities > 0 {
		density := float64(c.TotalFacts) / float64(nonDepEntities)
		switch {
		case density >= 1.5 && density <= 8:
			qs.FactDensity = 100
		case density < 1.5:
			qs.FactDensity = (density / 1.5) * 100
		default: // density > 8
			// Linearly decrease from 100 at 8 to 50 at 16
			qs.FactDensity = math.Max(50, 100-(density-8)*6.25)
		}
	}

	// 3. Relationship connectivity (20%): entities with ≥1 relationship / non-dep entities
	if nonDepEntities > 0 {
		qs.RelConnectivity = math.Min(1.0, float64(c.EntitiesWithRels)/float64(nonDepEntities)) * 100
	}

	// 4. Dimension coverage (15%): % of {what, how, why, when} represented
	dimensionsPresent := 0
	for _, dim := range []string{"what", "how", "why", "when"} {
		if c.ByDimension[dim] > 0 {
			dimensionsPresent++
		}
	}
	qs.DimensionCoverage = float64(dimensionsPresent) / 4.0 * 100

	// 5. Parse success rate (15%): completed jobs / total jobs
	if c.TotalJobs > 0 {
		qs.ParseSuccessRate = float64(c.SuccessfulJobs) / float64(c.TotalJobs) * 100
	} else {
		qs.ParseSuccessRate = 100
	}

	// Weighted overall score
	qs.Overall = qs.EntityCoverage*0.30 +
		qs.FactDensity*0.20 +
		qs.RelConnectivity*0.20 +
		qs.DimensionCoverage*0.15 +
		qs.ParseSuccessRate*0.15

	// Clamp to [0, 100]
	qs.Overall = math.Max(0, math.Min(100, qs.Overall))

	return qs
}

func ComputeQuality(ctx context.Context, pool *pgxpool.Pool, repoID uuid.UUID) (*QualityScore, error) {
	entityStore := &models.EntityStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}

	// Count entities (total and by kind)
	totalEntities, byKind, err := entityStore.CountByRepo(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("counting entities: %w", err)
	}

	// Non-dependency entities (exclude external modules without a path)
	var externalDepCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM entities WHERE repo_id = $1 AND kind = 'module' AND path IS NULL`, repoID,
	).Scan(&externalDepCount)
	if err != nil {
		externalDepCount = 0
	}

	// Count facts (total and by dimension)
	totalFacts, byDimension, err := factStore.CountByRepo(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("counting facts: %w", err)
	}

	// Count relationships
	totalRels, err := relStore.CountByRepo(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("counting relationships: %w", err)
	}

	// Count decisions
	var decisionCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM decisions WHERE repo_id = $1`, repoID,
	).Scan(&decisionCount)
	if err != nil {
		decisionCount = 0
	}

	// Entities with facts
	entitiesWithFacts, err := entityStore.CountWithFacts(ctx, repoID)
	if err != nil {
		entitiesWithFacts = 0
	}

	// Entities with relationships
	entitiesWithRels, err := entityStore.CountWithRelationships(ctx, repoID)
	if err != nil {
		entitiesWithRels = 0
	}

	// Parse success rate from jobs
	var totalJobs, firstAttemptSuccesses int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'completed' AND error_message IS NULL) FROM extraction_jobs WHERE repo_id = $1`, repoID,
	).Scan(&totalJobs, &firstAttemptSuccesses)
	if err != nil {
		totalJobs = 1
		firstAttemptSuccesses = 1
	}

	return ComputeQualityFromCounts(QualityCounts{
		TotalEntities:    totalEntities,
		ExternalDepCount: externalDepCount,
		TotalFacts:       totalFacts,
		TotalRels:        totalRels,
		DecisionCount:    decisionCount,
		EntitiesWithFacts: entitiesWithFacts,
		EntitiesWithRels:  entitiesWithRels,
		TotalJobs:        totalJobs,
		SuccessfulJobs:   firstAttemptSuccesses,
		ByDimension:      byDimension,
		ByKind:           byKind,
	}), nil
}

func FormatQualityScore(qs *QualityScore) string {
	return fmt.Sprintf("Quality score: %.0f/100 (entities: %.0f, facts: %.0f, relationships: %.0f, dimensions: %.0f, parsing: %.0f)",
		qs.Overall, qs.EntityCoverage, qs.FactDensity, qs.RelConnectivity, qs.DimensionCoverage, qs.ParseSuccessRate)
}

func FormatQualityDetails(qs *QualityScore) string {
	s := fmt.Sprintf("  --- Quality Score Breakdown ---\n")
	s += fmt.Sprintf("  Overall: %.1f/100\n", qs.Overall)
	s += fmt.Sprintf("\n")

	// Entity coverage
	s += fmt.Sprintf("  Entity Coverage (weight 30%%): %.1f/100\n", qs.EntityCoverage)
	s += fmt.Sprintf("    %d total entities, %d external deps, %d non-dep entities\n",
		qs.EntityCount, qs.ExternalDepCount, qs.NonDepEntities)
	s += fmt.Sprintf("    %d/%d non-dep entities have ≥1 fact\n", qs.EntitiesWithFacts, qs.NonDepEntities)
	if len(qs.EntityKindBreakdown) > 0 {
		s += fmt.Sprintf("    By kind: ")
		first := true
		for kind, count := range qs.EntityKindBreakdown {
			if !first {
				s += ", "
			}
			s += fmt.Sprintf("%s=%d", kind, count)
			first = false
		}
		s += "\n"
	}

	// Fact density
	s += fmt.Sprintf("\n  Fact Density (weight 20%%): %.1f/100\n", qs.FactDensity)
	s += fmt.Sprintf("    %.1f facts/entity (target: 1.5–8.0)\n", qs.FactsPerEntity)
	s += fmt.Sprintf("    %d total facts, %d decisions\n", qs.FactCount, qs.DecisionCount)
	if len(qs.DimensionBreakdown) > 0 {
		s += fmt.Sprintf("    By dimension: what=%d, how=%d, why=%d, when=%d\n",
			qs.DimensionBreakdown["what"], qs.DimensionBreakdown["how"],
			qs.DimensionBreakdown["why"], qs.DimensionBreakdown["when"])
	}

	// Relationship connectivity
	s += fmt.Sprintf("\n  Relationship Connectivity (weight 20%%): %.1f/100\n", qs.RelConnectivity)
	s += fmt.Sprintf("    %d/%d non-dep entities have ≥1 relationship\n", qs.EntitiesWithRels, qs.NonDepEntities)
	s += fmt.Sprintf("    %d total relationships\n", qs.RelationshipCount)

	// Dimension coverage
	s += fmt.Sprintf("\n  Dimension Coverage (weight 15%%): %.1f/100\n", qs.DimensionCoverage)
	dims := []string{"what", "how", "why", "when"}
	present := 0
	for _, d := range dims {
		if qs.DimensionBreakdown[d] > 0 {
			present++
		}
	}
	s += fmt.Sprintf("    %d/4 dimensions represented\n", present)

	// Parse success rate
	s += fmt.Sprintf("\n  Parse Success Rate (weight 15%%): %.1f/100\n", qs.ParseSuccessRate)
	s += fmt.Sprintf("    %d/%d jobs succeeded on first attempt\n", qs.SuccessfulJobs, qs.TotalJobs)

	return s
}
