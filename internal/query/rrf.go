package query

import (
	"sort"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// RRFConfig controls the Reciprocal Rank Fusion merge behaviour.
type RRFConfig struct {
	K            int     // smoothing constant (default 60)
	VectorWeight float64 // multiplier for vector RRF scores (default 1.0)
	FTSWeight    float64 // multiplier for FTS RRF scores (default 1.0)
	VectorLimit  int     // max results from vector search (default 60)
	FTSLimit     int     // max results from FTS search (default 60)
}

// DefaultRRFConfig returns production defaults for RRF hybrid search.
func DefaultRRFConfig() RRFConfig {
	return RRFConfig{
		K:            60,
		VectorWeight: 1.0,
		FTSWeight:    1.0,
		VectorLimit:  60,
		FTSLimit:     60,
	}
}

// rrfResult holds one merged fact with provenance metadata from the RRF merge.
type rrfResult struct {
	Fact       models.ScoredFact
	RRFScore   float64
	InVector   bool
	InFTS      bool
	VectorRank int // 1-based rank in the vector list, 0 if absent
	FTSRank    int // 1-based rank in the FTS list, 0 if absent
}

// mergeRRF performs Reciprocal Rank Fusion over two ranked result lists.
// Each item receives score = weight / (k + rank) where rank is 1-based.
// Items appearing in both lists accumulate scores from both.
// The returned slice is sorted by fused score descending.
func mergeRRF(vectorResults, ftsResults []models.ScoredFact, cfg RRFConfig) []rrfResult {
	// Guard against zero-value config: apply defaults for critical fields
	if cfg.K == 0 {
		cfg.K = 60
	}
	if cfg.VectorWeight == 0 {
		cfg.VectorWeight = 1.0
	}
	if cfg.FTSWeight == 0 {
		cfg.FTSWeight = 1.0
	}
	type entry struct {
		fact       models.ScoredFact
		score      float64
		inVector   bool
		inFTS      bool
		vectorRank int
		ftsRank    int
	}

	merged := make(map[uuid.UUID]*entry)

	for rank, sf := range vectorResults {
		r := rank + 1 // 1-based
		score := cfg.VectorWeight / float64(cfg.K+r)
		e := &entry{
			fact:       sf,
			score:      score,
			inVector:   true,
			vectorRank: r,
		}
		merged[sf.ID] = e
	}

	for rank, sf := range ftsResults {
		r := rank + 1 // 1-based
		score := cfg.FTSWeight / float64(cfg.K+r)
		if e, ok := merged[sf.ID]; ok {
			e.score += score
			e.inFTS = true
			e.ftsRank = r
		} else {
			merged[sf.ID] = &entry{
				fact:    sf,
				score:   score,
				inFTS:   true,
				ftsRank: r,
			}
		}
	}

	results := make([]rrfResult, 0, len(merged))
	for _, e := range merged {
		results = append(results, rrfResult{
			Fact:       e.fact,
			RRFScore:   e.score,
			InVector:   e.inVector,
			InFTS:      e.inFTS,
			VectorRank: e.vectorRank,
			FTSRank:    e.ftsRank,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RRFScore > results[j].RRFScore
	})

	return results
}
