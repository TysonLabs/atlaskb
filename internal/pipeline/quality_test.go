package pipeline

import (
	"math"
	"testing"
)

func TestComputeQualityFromCounts_ZeroEntities(t *testing.T) {
	qs := ComputeQualityFromCounts(QualityCounts{
		TotalJobs:      10,
		SuccessfulJobs: 8,
		ByDimension:    map[string]int{},
		ByKind:         map[string]int{},
	})

	// With zero entities, only parse success rate contributes
	if qs.EntityCoverage != 0 {
		t.Errorf("EntityCoverage = %.1f, want 0", qs.EntityCoverage)
	}
	if qs.FactDensity != 0 {
		t.Errorf("FactDensity = %.1f, want 0", qs.FactDensity)
	}
	if qs.RelConnectivity != 0 {
		t.Errorf("RelConnectivity = %.1f, want 0", qs.RelConnectivity)
	}
	// Parse success: 8/10 = 80%
	if math.Abs(qs.ParseSuccessRate-80) > 0.1 {
		t.Errorf("ParseSuccessRate = %.1f, want 80", qs.ParseSuccessRate)
	}
	// Overall = 0*0.3 + 0*0.2 + 0*0.2 + 0*0.15 + 80*0.15 = 12
	if math.Abs(qs.Overall-12) > 0.1 {
		t.Errorf("Overall = %.1f, want 12", qs.Overall)
	}
}

func TestComputeQualityFromCounts_PerfectScore(t *testing.T) {
	qs := ComputeQualityFromCounts(QualityCounts{
		TotalEntities:    20,
		ExternalDepCount: 0,
		TotalFacts:       60, // 3.0 facts/entity — in sweet spot
		TotalRels:        30,
		EntitiesWithFacts: 20,
		EntitiesWithRels:  20,
		TotalJobs:         10,
		SuccessfulJobs:    10,
		ByDimension:       map[string]int{"what": 15, "how": 15, "why": 15, "when": 15},
		ByKind:            map[string]int{"function": 10, "type": 10},
	})

	if qs.EntityCoverage != 100 {
		t.Errorf("EntityCoverage = %.1f, want 100", qs.EntityCoverage)
	}
	if qs.FactDensity != 100 {
		t.Errorf("FactDensity = %.1f, want 100", qs.FactDensity)
	}
	if qs.RelConnectivity != 100 {
		t.Errorf("RelConnectivity = %.1f, want 100", qs.RelConnectivity)
	}
	if qs.DimensionCoverage != 100 {
		t.Errorf("DimensionCoverage = %.1f, want 100", qs.DimensionCoverage)
	}
	if qs.ParseSuccessRate != 100 {
		t.Errorf("ParseSuccessRate = %.1f, want 100", qs.ParseSuccessRate)
	}
	if qs.Overall != 100 {
		t.Errorf("Overall = %.1f, want 100", qs.Overall)
	}
}

func TestComputeQualityFromCounts_LowDensity(t *testing.T) {
	// 0.75 facts/entity — below 1.5 threshold
	qs := ComputeQualityFromCounts(QualityCounts{
		TotalEntities:    20,
		TotalFacts:       15, // 0.75 per entity
		EntitiesWithFacts: 15,
		EntitiesWithRels:  15,
		TotalJobs:         5,
		SuccessfulJobs:    5,
		ByDimension:       map[string]int{"what": 10, "how": 5},
		ByKind:            map[string]int{},
	})

	// density = 0.75, score = (0.75/1.5)*100 = 50
	if math.Abs(qs.FactDensity-50) > 0.1 {
		t.Errorf("FactDensity = %.1f, want 50", qs.FactDensity)
	}
}

func TestComputeQualityFromCounts_HighDensity(t *testing.T) {
	// 16 facts/entity — above 8.0 threshold, at the floor of 50
	qs := ComputeQualityFromCounts(QualityCounts{
		TotalEntities:    10,
		TotalFacts:       160, // 16 per entity
		EntitiesWithFacts: 10,
		TotalJobs:         1,
		SuccessfulJobs:    1,
		ByDimension:       map[string]int{"what": 160},
		ByKind:            map[string]int{},
	})

	// density = 16, score = max(50, 100-(16-8)*6.25) = max(50, 50) = 50
	if math.Abs(qs.FactDensity-50) > 0.1 {
		t.Errorf("FactDensity = %.1f, want 50", qs.FactDensity)
	}
}

func TestComputeQualityFromCounts_DensityBoundary_Low(t *testing.T) {
	// Exactly 1.5 facts/entity — should be 100
	qs := ComputeQualityFromCounts(QualityCounts{
		TotalEntities: 20,
		TotalFacts:    30, // 1.5 per entity
		TotalJobs:     1,
		SuccessfulJobs: 1,
		ByDimension:   map[string]int{},
		ByKind:        map[string]int{},
	})

	if qs.FactDensity != 100 {
		t.Errorf("FactDensity = %.1f, want 100", qs.FactDensity)
	}
}

func TestComputeQualityFromCounts_DensityBoundary_High(t *testing.T) {
	// Exactly 8.0 facts/entity — should be 100
	qs := ComputeQualityFromCounts(QualityCounts{
		TotalEntities: 10,
		TotalFacts:    80, // 8.0 per entity
		TotalJobs:     1,
		SuccessfulJobs: 1,
		ByDimension:   map[string]int{},
		ByKind:        map[string]int{},
	})

	if qs.FactDensity != 100 {
		t.Errorf("FactDensity = %.1f, want 100", qs.FactDensity)
	}
}

func TestComputeQualityFromCounts_PartialDimensions(t *testing.T) {
	// 2/4 dimensions present
	qs := ComputeQualityFromCounts(QualityCounts{
		TotalEntities: 10,
		TotalFacts:    30,
		TotalJobs:     1,
		SuccessfulJobs: 1,
		ByDimension:   map[string]int{"what": 20, "how": 10},
		ByKind:        map[string]int{},
	})

	if qs.DimensionCoverage != 50 {
		t.Errorf("DimensionCoverage = %.1f, want 50", qs.DimensionCoverage)
	}
}

func TestComputeQualityFromCounts_ExternalDepsExcluded(t *testing.T) {
	qs := ComputeQualityFromCounts(QualityCounts{
		TotalEntities:    30,
		ExternalDepCount: 20,
		TotalFacts:       30, // 3.0 per non-dep entity (10 non-dep)
		EntitiesWithFacts: 10,
		EntitiesWithRels:  10,
		TotalJobs:         1,
		SuccessfulJobs:    1,
		ByDimension:       map[string]int{"what": 30},
		ByKind:            map[string]int{},
	})

	if qs.NonDepEntities != 10 {
		t.Errorf("NonDepEntities = %d, want 10", qs.NonDepEntities)
	}
	if qs.ExternalDepCount != 20 {
		t.Errorf("ExternalDepCount = %d, want 20", qs.ExternalDepCount)
	}
	// Coverage should be based on 10 non-dep entities
	if qs.EntityCoverage != 100 {
		t.Errorf("EntityCoverage = %.1f, want 100", qs.EntityCoverage)
	}
}
