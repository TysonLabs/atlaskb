package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type SearchResult struct {
	Fact            models.Fact   `json:"fact"`
	Entity          models.Entity `json:"entity"`
	RepoName        string        `json:"repo_name"`
	Score           float64       `json:"score"`
	Source          string        `json:"source"` // "vector", "keyword", "expansion", "relationship"
	Stale           bool          `json:"stale"`
	StaleReasons    []string      `json:"stale_reasons,omitempty"`
	CommitsBehind   *int          `json:"commits_behind,omitempty"`
	Flagged         bool          `json:"flagged"`
	PendingFeedback int           `json:"pending_feedback,omitempty"`
}

type Engine struct {
	Pool     *pgxpool.Pool
	Embedder embeddings.Client
	LLM      llm.Client // optional, enables query decomposition
	Model    string     // LLM model for query decomposition
	Verbose  bool       // emit per-result score breakdowns via log.Printf
	RRF      RRFConfig  // Reciprocal Rank Fusion configuration
}

// scoreTrace tracks how a single result's score was built up.
type scoreTrace struct {
	factID        uuid.UUID
	entityName    string
	base          float64
	source        string
	rrfVectorRank int     // 1-based rank in vector list (0 if absent)
	rrfFTSRank    int     // 1-based rank in FTS list (0 if absent)
	entityBoost   float64 // multiplier from entity-name mention (1.0 if none)
	entitySim     float64 // similarity that drove the boost
	confidence    float64 // multiplier
	kindBias      float64 // multiplier
	category      float64 // multiplier
	repoAffinity  float64 // multiplier for same-repo boost (1.0 if no repo filter)
	overlap       float64 // additive bonus
	final         float64
}

func (t scoreTrace) String() string {
	s := fmt.Sprintf("  [%s] fact=%s entity=%s\n", t.source, t.factID.String()[:8], t.entityName)
	s += fmt.Sprintf("    base=%.4f", t.base)
	if t.rrfVectorRank > 0 {
		s += fmt.Sprintf("  vecRank=%d", t.rrfVectorRank)
	}
	if t.rrfFTSRank > 0 {
		s += fmt.Sprintf("  ftsRank=%d", t.rrfFTSRank)
	}
	if t.entityBoost != 1.0 {
		s += fmt.Sprintf("  entity_boost=×%.2f (sim=%.2f)", t.entityBoost, t.entitySim)
	}
	if t.confidence != 1.0 {
		s += fmt.Sprintf("  conf=×%.2f", t.confidence)
	}
	if t.kindBias != 1.0 {
		s += fmt.Sprintf("  kind=×%.2f", t.kindBias)
	}
	if t.category != 1.0 {
		s += fmt.Sprintf("  cat=×%.2f", t.category)
	}
	if t.repoAffinity != 0 && t.repoAffinity != 1.0 {
		s += fmt.Sprintf("  repo=×%.2f", t.repoAffinity)
	}
	if t.overlap > 0 {
		s += fmt.Sprintf("  overlap=+%.2f", t.overlap)
	}
	s += fmt.Sprintf("  → final=%.4f", t.final)
	return s
}

func NewEngine(pool *pgxpool.Pool, embedder embeddings.Client) *Engine {
	return &Engine{Pool: pool, Embedder: embedder, RRF: DefaultRRFConfig()}
}

// SetLLM enables query decomposition by providing an LLM client.
func (e *Engine) SetLLM(client llm.Client, model string) {
	e.LLM = client
	e.Model = model
}

func (e *Engine) Search(ctx context.Context, question string, repoIDs []uuid.UUID, limit int) ([]SearchResult, error) {
	if limit == 0 {
		limit = 40
	}

	// Step 1: Query decomposition — split compound questions into sub-queries
	queries := []string{question}
	if e.LLM != nil {
		subQueries, err := e.decomposeQuery(ctx, question)
		if err == nil && len(subQueries) > 1 {
			queries = subQueries
		}
	}

	factStore := &models.FactStore{Pool: e.Pool}
	entityStore := &models.EntityStore{Pool: e.Pool}
	repoStore := &models.RepoStore{Pool: e.Pool}
	relStore := &models.RelationshipStore{Pool: e.Pool}
	repoNameCache := make(map[uuid.UUID]string)
	repoStaleCache := make(map[uuid.UUID]models.RepoStaleness)
	lookupRepoStaleness := func(repoID uuid.UUID) models.RepoStaleness {
		if cached, ok := repoStaleCache[repoID]; ok {
			return cached
		}
		repo, err := repoStore.GetByID(ctx, repoID)
		if err != nil || repo == nil {
			return models.RepoStaleness{RepoID: repoID}
		}
		stale := models.ComputeRepoStaleness(ctx, *repo)
		repoStaleCache[repoID] = stale
		return stale
	}

	// Track seen fact IDs → index in allResults for dedup and cross-source fusion
	seen := make(map[uuid.UUID]int)
	var allResults []SearchResult
	// Per-result score traces for verbose logging (keyed by index in allResults)
	var traces []scoreTrace

	// Store the first query vector for reuse in entity expansion scoring
	var firstQueryVec *pgvector.Vector

	// Per-sub-query limit: distribute evenly, but ensure minimum coverage
	perQueryLimit := limit
	if len(queries) > 1 {
		perQueryLimit = limit / len(queries)
		if perQueryLimit < 10 {
			perQueryLimit = 10
		}
	}

	// Phase 2: Extract entity mentions from the question for boosting
	mentionedEntities := extractEntityMentions(ctx, question, entityStore, repoIDs, e.Verbose)
	if e.Verbose && len(mentionedEntities) > 0 {
		for eid, sim := range mentionedEntities {
			ent, _ := entityStore.GetByID(ctx, eid)
			name := eid.String()[:8]
			if ent != nil {
				name = ent.Name
			}
			log.Printf("[score] entity mention: %s (sim=%.2f)", name, sim)
		}
	}

	rrfCfg := e.RRF

	for _, q := range queries {
		// Step 2a: Vector similarity search
		vectors, err := e.Embedder.Embed(ctx, []string{q}, embeddings.DefaultModel)
		if err != nil {
			return nil, fmt.Errorf("embedding question: %w", err)
		}
		if len(vectors) == 0 || len(vectors[0]) == 0 {
			return nil, fmt.Errorf("empty embedding returned")
		}

		queryVec := pgvector.NewVector(vectors[0])
		if firstQueryVec == nil {
			firstQueryVec = &queryVec
		}
		vectorFacts, err := factStore.SearchByVector(ctx, queryVec, repoIDs, rrfCfg.VectorLimit)
		if err != nil {
			return nil, fmt.Errorf("vector search: %w", err)
		}

		// Step 2b: FTS search (non-fatal on error)
		ftsFacts, err := factStore.SearchByFTSRanked(ctx, q, repoIDs, rrfCfg.FTSLimit)
		if err != nil {
			// FTS failure is non-fatal — proceed with vector-only results
			ftsFacts = nil
		}

		// Step 2c: Reciprocal Rank Fusion merge
		merged := mergeRRF(vectorFacts, ftsFacts, rrfCfg)

		for _, mr := range merged {
			if _, ok := seen[mr.Fact.ID]; ok {
				continue
			}
			entity, err := entityStore.GetByID(ctx, mr.Fact.EntityID)
			if err != nil || entity == nil {
				continue
			}
			repoName := e.lookupRepoName(ctx, repoStore, repoNameCache, mr.Fact.RepoID)
			staleness := lookupRepoStaleness(mr.Fact.RepoID)

			// Determine source label
			source := "vector"
			if mr.InVector && mr.InFTS {
				source = "hybrid"
			} else if mr.InFTS {
				source = "fts"
			}

			seen[mr.Fact.ID] = len(allResults)
			allResults = append(allResults, SearchResult{
				Fact:          mr.Fact.Fact,
				Entity:        *entity,
				RepoName:      repoName,
				Score:         mr.RRFScore,
				Source:        source,
				Stale:         staleness.Stale,
				StaleReasons:  staleness.Reasons,
				CommitsBehind: staleness.CommitsBehind,
			})
			if e.Verbose {
				traces = append(traces, scoreTrace{
					factID: mr.Fact.ID, entityName: entity.Name,
					base: mr.RRFScore, source: source,
					rrfVectorRank: mr.VectorRank, rrfFTSRank: mr.FTSRank,
					entityBoost: 1.0, confidence: 1.0, kindBias: 1.0, category: 1.0,
				})
			}
		}
	}

	// Compute top RRF score for scaling expansion and relationship scores
	topRRFScore := 0.0
	for _, r := range allResults {
		if r.Score > topRRFScore {
			topRRFScore = r.Score
		}
	}
	if topRRFScore == 0 {
		topRRFScore = 1.0 / float64(rrfCfg.K+1) // fallback: score of rank-1 item
	}

	// Phase 2: Apply entity-name boost after initial retrieval
	const entityNameBoostFactor = 1.5
	for i := range allResults {
		if sim, ok := mentionedEntities[allResults[i].Entity.ID]; ok {
			boost := 1.0 + (entityNameBoostFactor-1.0)*sim // 1.0–1.5
			allResults[i].Score *= boost
			if e.Verbose {
				traces[i].entityBoost = boost
				traces[i].entitySim = sim
			}
		}
	}

	// Step 3: Entity expansion — for top entities found, fetch all their facts
	entityFactCounts := make(map[uuid.UUID]int)
	for _, r := range allResults {
		entityFactCounts[r.Entity.ID]++
	}

	// Expand entities that appeared in 2+ results (they're clearly relevant)
	expandedEntityIDs := make(map[uuid.UUID]bool)
	for eid, count := range entityFactCounts {
		if count >= 2 {
			expandedEntityIDs[eid] = true
		}
	}
	// Also expand top-scored entities even if they appear once
	if len(allResults) > 0 {
		topScore := allResults[0].Score
		for _, r := range allResults {
			if r.Score >= topScore*0.9 {
				expandedEntityIDs[r.Entity.ID] = true
			}
		}
	}
	// Phase 2: Force-expand mentioned entities into the expansion set
	for eid := range mentionedEntities {
		expandedEntityIDs[eid] = true
	}

	// Phase 3: Vector-scored expansion — each expanded fact gets its own similarity score
	if firstQueryVec != nil {
		for eid := range expandedEntityIDs {
			scoredExpFacts, err := factStore.SearchByVectorForEntity(ctx, *firstQueryVec, eid, 10)
			if err != nil {
				continue
			}
			for _, sf := range scoredExpFacts {
				if _, ok := seen[sf.ID]; ok {
					continue
				}
				entity, err := entityStore.GetByID(ctx, sf.EntityID)
				if err != nil || entity == nil {
					continue
				}
				repoName := e.lookupRepoName(ctx, repoStore, repoNameCache, sf.RepoID)
				staleness := lookupRepoStaleness(sf.RepoID)
				// Scale expansion score relative to top RRF score (0.85× discount)
				score := topRRFScore * sf.Score * 0.85
				seen[sf.ID] = len(allResults)
				allResults = append(allResults, SearchResult{
					Fact:          sf.Fact,
					Entity:        *entity,
					RepoName:      repoName,
					Score:         score,
					Source:        "expansion",
					Stale:         staleness.Stale,
					StaleReasons:  staleness.Reasons,
					CommitsBehind: staleness.CommitsBehind,
				})
				if e.Verbose {
					traces = append(traces, scoreTrace{
						factID: sf.ID, entityName: entity.Name,
						base: score, source: fmt.Sprintf("expansion(vec=%.3f×topRRF×0.85)", sf.Score),
						entityBoost: 1.0, confidence: 1.0, kindBias: 1.0, category: 1.0,
					})
				}
			}
		}
	}

	// Step 4: Relationship graph traversal — follow edges from top entities
	// Phase 4: Track entity → strength for strength-based scoring (relative to top RRF score)
	strengthScore := map[string]float64{
		models.StrengthStrong:   topRRFScore * 0.35,
		models.StrengthModerate: topRRFScore * 0.25,
		models.StrengthWeak:     topRRFScore * 0.15,
	}
	relEntityStrength := make(map[uuid.UUID]string) // entityID → best strength
	for eid := range expandedEntityIDs {
		rels, err := relStore.ListByEntity(ctx, eid)
		if err != nil {
			continue
		}
		for _, rel := range rels {
			other := rel.ToEntityID
			if rel.ToEntityID == eid {
				other = rel.FromEntityID
			}
			if expandedEntityIDs[other] {
				continue
			}
			// Keep the strongest relationship if multiple edges
			if existing, ok := relEntityStrength[other]; ok {
				if strengthScore[rel.Strength] <= strengthScore[existing] {
					continue
				}
			}
			relEntityStrength[other] = rel.Strength
		}
	}

	// Fetch key facts for related entities (limited to keep context manageable)
	relFactsPerEntity := 3
	for eid, strength := range relEntityStrength {
		facts, err := factStore.ListByEntity(ctx, eid)
		if err != nil {
			continue
		}
		// Phase 4: Use strength-based score instead of static 0.3
		score := strengthScore[strength]
		if score == 0 {
			score = topRRFScore * 0.15 // fallback for unknown strength
		}
		count := 0
		for _, f := range facts {
			if count >= relFactsPerEntity {
				break
			}
			if _, ok := seen[f.ID]; ok {
				continue
			}
			entity, err := entityStore.GetByID(ctx, f.EntityID)
			if err != nil || entity == nil {
				continue
			}
			repoName := e.lookupRepoName(ctx, repoStore, repoNameCache, f.RepoID)
			staleness := lookupRepoStaleness(f.RepoID)
			seen[f.ID] = len(allResults)
			allResults = append(allResults, SearchResult{
				Fact:          f,
				Entity:        *entity,
				RepoName:      repoName,
				Score:         score,
				Source:        "relationship",
				Stale:         staleness.Stale,
				StaleReasons:  staleness.Reasons,
				CommitsBehind: staleness.CommitsBehind,
			})
			if e.Verbose {
				traces = append(traces, scoreTrace{
					factID: f.ID, entityName: entity.Name,
					base: score, source: fmt.Sprintf("relationship(%s)", strength),
					entityBoost: 1.0, confidence: 1.0, kindBias: 1.0, category: 1.0,
				})
			}
			count++
		}
	}

	// Build target repo set for same-repo boosting
	targetRepoSet := make(map[uuid.UUID]bool, len(repoIDs))
	for _, rid := range repoIDs {
		targetRepoSet[rid] = true
	}

	// Post-processing multiplier pass: confidence, entity kind, category, claim overlap, repo affinity
	candidateTokens := extractCandidateTokens(question)
	for i := range allResults {
		r := &allResults[i]

		// Confidence multiplier
		confW := 1.0
		if w, ok := confidenceWeight[r.Fact.Confidence]; ok {
			confW = w
			r.Score *= w
		}

		// Entity kind bias
		kindW := 1.0
		if w, ok := entityKindBias[r.Entity.Kind]; ok {
			kindW = w
			r.Score *= w
		}

		// Category relevance
		catW := 1.0
		if w, ok := categoryRelevance[r.Fact.Category]; ok {
			catW = w
			r.Score *= w
		}

		// Same-repo affinity boost: when user specified target repo(s),
		// boost results from those repos and penalize cross-repo results.
		// This ensures the target repo's results surface above noise from
		// similarly-named entities in other repos.
		repoW := 1.0
		if len(targetRepoSet) > 0 {
			if targetRepoSet[r.Fact.RepoID] {
				repoW = 1.3 // 30% boost for target repo
			} else {
				repoW = 0.6 // 40% penalty for cross-repo
			}
			r.Score *= repoW
		}

		// Claim keyword overlap: +0.05 per matching token, max +0.1
		claimLower := strings.ToLower(r.Fact.Claim)
		overlapBonus := 0.0
		for _, tok := range candidateTokens {
			if strings.Contains(claimLower, strings.ToLower(tok)) {
				overlapBonus += 0.05
				if overlapBonus >= 0.1 {
					break
				}
			}
		}
		r.Score += overlapBonus

		if e.Verbose {
			traces[i].confidence = confW
			traces[i].kindBias = kindW
			traces[i].category = catW
			traces[i].repoAffinity = repoW
			traces[i].overlap = overlapBonus
			traces[i].final = r.Score
		}
	}

	// Sort by score descending and cap at limit
	if e.Verbose {
		// Sort traces in same order as results for aligned output
		type indexedTrace struct {
			idx   int
			trace scoreTrace
		}
		its := make([]indexedTrace, len(traces))
		for i, t := range traces {
			its[i] = indexedTrace{i, t}
		}
		sort.Slice(its, func(i, j int) bool {
			return allResults[its[i].idx].Score > allResults[its[j].idx].Score
		})
		log.Printf("[score] === Score breakdown (top 10 of %d) ===", len(allResults))
		shown := 10
		if len(its) < shown {
			shown = len(its)
		}
		for _, it := range its[:shown] {
			log.Print(it.trace.String())
		}
	}

	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	feedbackStore := &models.FactFeedbackStore{Pool: e.Pool}
	factIDs := make([]uuid.UUID, 0, len(allResults))
	for _, r := range allResults {
		factIDs = append(factIDs, r.Fact.ID)
	}
	if counts, err := feedbackStore.CountPendingByFactIDs(ctx, factIDs); err == nil {
		for i := range allResults {
			if n := counts[allResults[i].Fact.ID]; n > 0 {
				allResults[i].Flagged = true
				allResults[i].PendingFeedback = n
			}
		}
	}

	// Phase 5: Enforce repo diversity when not single-repo scoped
	if len(repoIDs) != 1 {
		allResults = enforceRepoDiversity(allResults, limit)
	}

	return allResults, nil
}

// Post-processing scoring multipliers
var confidenceWeight = map[string]float64{
	"high": 1.0, "medium": 0.85, "low": 0.7,
}
var entityKindBias = map[string]float64{
	"function": 1.15, "type": 1.15, "endpoint": 1.15, "config": 1.15,
	"module": 0.85, "service": 0.85, "concept": 0.85,
}
var categoryRelevance = map[string]float64{
	"behavior": 1.0, "pattern": 1.0, "convention": 1.0,
	"constraint": 0.95, "risk": 0.8, "debt": 0.7,
}

// camelCaseRe matches CamelCase boundaries for entity name candidate detection.
var camelCaseRe = regexp.MustCompile(`[a-z][A-Z]`)

// extractCandidateTokens returns notable tokens from a question string.
// Tokens are considered candidates if they are CamelCase, contain underscores, or are >6 chars.
func extractCandidateTokens(question string) []string {
	tokens := strings.FieldsFunc(question, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '?' || r == '!' || r == '.' || r == ';' || r == ':'
	})

	var candidates []string
	for _, tok := range tokens {
		tok = strings.Trim(tok, "'\"()")
		if tok == "" {
			continue
		}
		isCamel := camelCaseRe.MatchString(tok)
		hasUnderscore := strings.Contains(tok, "_")
		isLong := len(tok) > 6
		if isCamel || hasUnderscore || isLong {
			candidates = append(candidates, tok)
		}
	}
	return candidates
}

// extractEntityMentions identifies entity names mentioned in the question and returns
// a map of entityID → similarity score for boosting.
func extractEntityMentions(ctx context.Context, question string, entityStore *models.EntityStore, repoIDs []uuid.UUID, verbose bool) map[uuid.UUID]float64 {
	result := make(map[uuid.UUID]float64)

	candidates := extractCandidateTokens(question)
	if verbose {
		log.Printf("[entity-mention] candidates: %v", candidates)
	}

	for _, candidate := range candidates {
		// Search within each target repo (or all repos if unscoped)
		var searchRepoIDs []*uuid.UUID
		if len(repoIDs) > 0 {
			for _, rid := range repoIDs {
				rid := rid
				searchRepoIDs = append(searchRepoIDs, &rid)
			}
		} else {
			searchRepoIDs = []*uuid.UUID{nil}
		}

		var allMatches []models.EntityWithSimilarity
		for _, rid := range searchRepoIDs {
			matches, err := entityStore.SearchFuzzy(ctx, candidate, rid, 0.4, 5)
			if err != nil {
				if verbose {
					log.Printf("[entity-mention] candidate=%q error=%v", candidate, err)
				}
				continue
			}
			allMatches = append(allMatches, matches...)
		}

		if verbose {
			if len(allMatches) == 0 {
				// Show near-misses at lower threshold for debugging
				nearMisses, _ := entityStore.SearchFuzzy(ctx, candidate, nil, 0.3, 3)
				if len(nearMisses) > 0 {
					for _, m := range nearMisses {
						log.Printf("[entity-mention] candidate=%q NEAR-MISS: %s (sim=%.3f, threshold=0.4)", candidate, m.Name, m.Similarity)
					}
				} else {
					log.Printf("[entity-mention] candidate=%q no matches even at 0.3", candidate)
				}
			} else {
				for _, m := range allMatches {
					log.Printf("[entity-mention] candidate=%q matched: %s (sim=%.3f, repo=%s)", candidate, m.Name, m.Similarity, m.RepoID.String()[:8])
				}
			}
		}

		for _, m := range allMatches {
			if m.Similarity > result[m.ID] {
				result[m.ID] = m.Similarity
			}
		}
	}

	return result
}

// enforceRepoDiversity ensures no single repo dominates the results.
// maxPerRepo = max(limit/3, 5). Backfills remaining slots from overflow in score order.
func enforceRepoDiversity(results []SearchResult, limit int) []SearchResult {
	if len(results) == 0 {
		return results
	}

	maxPerRepo := limit / 3
	if maxPerRepo < 5 {
		maxPerRepo = 5
	}

	// Results are already sorted by score
	repoCounts := make(map[uuid.UUID]int)
	var kept []SearchResult
	var overflow []SearchResult

	for _, r := range results {
		if repoCounts[r.Fact.RepoID] < maxPerRepo {
			kept = append(kept, r)
			repoCounts[r.Fact.RepoID]++
		} else {
			overflow = append(overflow, r)
		}
	}

	// Backfill remaining slots from overflow
	remaining := limit - len(kept)
	for i := 0; i < len(overflow) && remaining > 0; i++ {
		kept = append(kept, overflow[i])
		remaining--
	}

	return kept
}

// decomposeQuery uses the LLM to split a compound question into focused sub-queries.
func (e *Engine) decomposeQuery(ctx context.Context, question string) ([]string, error) {
	system := `You split compound questions about codebases into focused sub-queries for a knowledge base search.
Return a JSON array of strings. Each sub-query should target one specific aspect.
If the question is already focused on a single topic, return it as-is in a single-element array.
Maximum 4 sub-queries. Keep each sub-query concise.`

	resp, err := e.LLM.Complete(ctx, e.Model, system, []llm.Message{
		{Role: "user", Content: question},
	}, 256, nil)
	if err != nil {
		return nil, err
	}

	// Parse JSON array from response
	content := strings.TrimSpace(resp.Content)
	// Strip markdown code fences if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var queries []string
	if err := json.Unmarshal([]byte(content), &queries); err != nil {
		// If parsing fails, just use the original question
		return []string{question}, nil
	}

	if len(queries) == 0 {
		return []string{question}, nil
	}
	if len(queries) > 4 {
		queries = queries[:4]
	}

	return queries, nil
}

func (e *Engine) lookupRepoName(ctx context.Context, store *models.RepoStore, cache map[uuid.UUID]string, repoID uuid.UUID) string {
	if name, ok := cache[repoID]; ok {
		return name
	}
	repo, err := store.GetByID(ctx, repoID)
	if err == nil && repo != nil {
		cache[repoID] = repo.Name
		return repo.Name
	}
	return ""
}
