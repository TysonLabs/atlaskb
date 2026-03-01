package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type SearchResult struct {
	Fact     models.Fact   `json:"fact"`
	Entity   models.Entity `json:"entity"`
	RepoName string        `json:"repo_name"`
	Score    float64       `json:"score"`
	Source   string        `json:"source"` // "vector", "keyword", "expansion", "relationship"
}

type Engine struct {
	Pool     *pgxpool.Pool
	Embedder embeddings.Client
	LLM      llm.Client // optional, enables query decomposition
	Model    string     // LLM model for query decomposition
}

func NewEngine(pool *pgxpool.Pool, embedder embeddings.Client) *Engine {
	return &Engine{Pool: pool, Embedder: embedder}
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

	// Track seen fact IDs to deduplicate across sub-queries and search types
	seen := make(map[uuid.UUID]bool)
	var allResults []SearchResult

	// Per-sub-query limit: distribute evenly, but ensure minimum coverage
	perQueryLimit := limit
	if len(queries) > 1 {
		perQueryLimit = limit / len(queries)
		if perQueryLimit < 10 {
			perQueryLimit = 10
		}
	}

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
		scoredFacts, err := factStore.SearchByVector(ctx, queryVec, repoIDs, perQueryLimit)
		if err != nil {
			return nil, fmt.Errorf("vector search: %w", err)
		}

		for _, sf := range scoredFacts {
			if seen[sf.ID] {
				continue
			}
			seen[sf.ID] = true
			entity, err := entityStore.GetByID(ctx, sf.EntityID)
			if err != nil || entity == nil {
				continue
			}
			repoName := e.lookupRepoName(ctx, repoStore, repoNameCache, sf.RepoID)
			allResults = append(allResults, SearchResult{
				Fact:     sf.Fact,
				Entity:   *entity,
				RepoName: repoName,
				Score:    sf.Score,
				Source:   "vector",
			})
		}

		// Step 2b: Keyword/BM25 search (hybrid)
		keywordFacts, err := factStore.SearchByKeyword(ctx, q, repoIDs, perQueryLimit/2)
		if err != nil {
			// Keyword search failure is non-fatal (column may not exist yet)
			keywordFacts = nil
		}

		for _, f := range keywordFacts {
			if seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			entity, err := entityStore.GetByID(ctx, f.EntityID)
			if err != nil || entity == nil {
				continue
			}
			repoName := e.lookupRepoName(ctx, repoStore, repoNameCache, f.RepoID)
			allResults = append(allResults, SearchResult{
				Fact:     f,
				Entity:   *entity,
				RepoName: repoName,
				Score:    0.5, // keyword results get a moderate default score
				Source:   "keyword",
			})
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

	for eid := range expandedEntityIDs {
		facts, err := factStore.ListByEntity(ctx, eid)
		if err != nil {
			continue
		}
		for _, f := range facts {
			if seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			entity, err := entityStore.GetByID(ctx, f.EntityID)
			if err != nil || entity == nil {
				continue
			}
			repoName := e.lookupRepoName(ctx, repoStore, repoNameCache, f.RepoID)
			allResults = append(allResults, SearchResult{
				Fact:     f,
				Entity:   *entity,
				RepoName: repoName,
				Score:    0.4, // expansion results get a lower default score
				Source:   "expansion",
			})
		}
	}

	// Step 4: Relationship graph traversal — follow edges from top entities
	relEntityIDs := make(map[uuid.UUID]bool)
	for eid := range expandedEntityIDs {
		rels, err := relStore.ListByEntity(ctx, eid)
		if err != nil {
			continue
		}
		for _, rel := range rels {
			// Collect the other end of the relationship
			other := rel.ToEntityID
			if rel.ToEntityID == eid {
				other = rel.FromEntityID
			}
			if !expandedEntityIDs[other] {
				relEntityIDs[other] = true
			}
		}
	}

	// Fetch key facts for related entities (limited to keep context manageable)
	relFactsPerEntity := 3
	for eid := range relEntityIDs {
		facts, err := factStore.ListByEntity(ctx, eid)
		if err != nil {
			continue
		}
		count := 0
		for _, f := range facts {
			if count >= relFactsPerEntity {
				break
			}
			if seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			entity, err := entityStore.GetByID(ctx, f.EntityID)
			if err != nil || entity == nil {
				continue
			}
			repoName := e.lookupRepoName(ctx, repoStore, repoNameCache, f.RepoID)
			allResults = append(allResults, SearchResult{
				Fact:     f,
				Entity:   *entity,
				RepoName: repoName,
				Score:    0.3, // relationship-traversed results get lowest score
				Source:   "relationship",
			})
			count++
		}
	}

	// Sort by score descending and cap at limit
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	return allResults, nil
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
