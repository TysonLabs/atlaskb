package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/models"
	"github.com/tgeorge06/atlaskb/internal/query"
)

type Server struct {
	pool     *pgxpool.Pool
	embedder embeddings.Client
}

func NewServer(pool *pgxpool.Pool, embedder embeddings.Client) *Server {
	return &Server{pool: pool, embedder: embedder}
}

// RegisterTools registers all AtlasKB MCP tools on the given go-sdk Server.
// This is used by both the stdio transport (Run) and the HTTP transport (web server).
func RegisterTools(srv *gomcp.Server, pool *pgxpool.Pool, embedder embeddings.Client) {
	s := &Server{pool: pool, embedder: embedder}

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "search_knowledge_base",
		Description: "Search the AtlasKB knowledge graph. mode=facts (default) returns individual fact results. mode=graph returns triplet-ranked (source, relationship, target) subgraph results showing how entities relate.",
	}, s.handleSearch)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "list_repos",
		Description: "List all repositories indexed in AtlasKB.",
	}, s.handleListRepos)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "get_conventions",
		Description: "Get coding conventions and patterns. When repo is specified, returns conventions for that repo. When omitted, returns conventions across all repos, tagged with repo name and deduplicated.",
	}, s.handleGetConventions)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "get_module_context",
		Description: "Get context about a specific module, file, or code entity. Returns the entity's summary, capabilities, assumptions, facts, and optionally its relationships.",
	}, s.handleGetModuleContext)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "get_service_contract",
		Description: "Get the public contract of a code entity: who depends on it and what invariants it exposes. Useful before modifying a module to understand downstream impact.",
	}, s.handleGetServiceContract)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "get_impact_analysis",
		Description: "Analyze the dependency graph around a code entity with N-hop traversal. Shows direct impacts, transitive dependency chains, and cross-repo effects. Use max_hops to control traversal depth (default 2, max 5).",
	}, s.handleGetImpactAnalysis)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "get_decision_context",
		Description: "Get architectural decisions linked to a code entity. Returns decision rationale, alternatives considered, and tradeoffs.",
	}, s.handleGetDecisionContext)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "get_task_context",
		Description: "Get a bundled context package for a coding task. Combines conventions, module context, service contracts, and decisions for a set of files in one call.",
	}, s.handleGetTaskContext)
}

func (s *Server) Run(ctx context.Context) error {
	srv := gomcp.NewServer(
		&gomcp.Implementation{Name: "atlaskb", Version: "0.1.0"},
		nil,
	)
	RegisterTools(srv, s.pool, s.embedder)
	return srv.Run(ctx, &gomcp.StdioTransport{})
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (s *Server) batchGetEntities(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*models.Entity, error) {
	if len(ids) == 0 {
		return make(map[uuid.UUID]*models.Entity), nil
	}
	store := &models.EntityStore{Pool: s.pool}
	entities, err := store.GetByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("batch fetching entities: %w", err)
	}
	m := make(map[uuid.UUID]*models.Entity, len(entities))
	for i := range entities {
		m[entities[i].ID] = &entities[i]
	}
	return m, nil
}

func (s *Server) resolveRepo(ctx context.Context, name string) (*models.Repo, error) {
	repoStore := &models.RepoStore{Pool: s.pool}
	r, err := repoStore.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("looking up repo: %w", err)
	}
	if r == nil {
		return nil, fmt.Errorf("repository %q not found", name)
	}
	return r, nil
}

func (s *Server) resolveEntity(ctx context.Context, repoID uuid.UUID, path string) (*models.Entity, error) {
	entityStore := &models.EntityStore{Pool: s.pool}

	// 1. Exact path match
	e, err := entityStore.FindByPath(ctx, repoID, path)
	if err != nil {
		return nil, err
	}
	if e != nil {
		return e, nil
	}

	// 2. Exact qualified_name match
	e, err = entityStore.FindByQualifiedName(ctx, repoID, path)
	if err != nil {
		return nil, err
	}
	if e != nil {
		return e, nil
	}

	// 3. Suffix-based path fallback (handles worktree paths, partial paths)
	e, err = entityStore.FindByPathSuffix(ctx, repoID, path)
	if err != nil {
		return nil, err
	}
	if e != nil {
		return e, nil
	}

	return nil, fmt.Errorf("entity %q not found", path)
}

func lookupRepoName(ctx context.Context, store *models.RepoStore, cache map[uuid.UUID]string, repoID uuid.UUID) string {
	if name, ok := cache[repoID]; ok {
		return name
	}
	if r, err := store.GetByID(ctx, repoID); err == nil && r != nil {
		cache[repoID] = r.Name
		return r.Name
	}
	cache[repoID] = ""
	return ""
}

func clampMaxResults(n, defaultN, maxN int) int {
	if n <= 0 {
		return defaultN
	}
	if n > maxN {
		return maxN
	}
	return n
}

func entityPath(e *models.Entity) string {
	if e.Path != nil {
		return *e.Path
	}
	return ""
}

// ── Input types ──────────────────────────────────────────────────────────────

type searchInput struct {
	Query string `json:"query" jsonschema:"Natural language search query"`
	Repo  string `json:"repo,omitempty" jsonschema:"Filter by repository name"`
	Mode  string `json:"mode,omitempty" jsonschema:"Search mode: facts (default, individual fact ranking) or graph (triplet-ranked subgraph search)"`
	Limit int    `json:"limit,omitempty" jsonschema:"Max results to return (default 20, max 50)"`
}

type listReposInput struct{}

type getConventionsInput struct {
	Repo       string `json:"repo,omitempty" jsonschema:"Repository name (optional — omit to get conventions from all repos)"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Max results to return (default 50, max 200)"`
}

type getModuleContextInput struct {
	Repo       string `json:"repo" jsonschema:"Repository name (required)"`
	Path       string `json:"path" jsonschema:"File path or qualified name of the entity (required)"`
	Depth      string `json:"depth,omitempty" jsonschema:"shallow (default) or deep - deep includes relationships"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Max results to return (default 50, max 200)"`
}

type getServiceContractInput struct {
	Repo       string `json:"repo" jsonschema:"Repository name (required)"`
	Path       string `json:"path" jsonschema:"File path or qualified name of the entity (required)"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Max results to return (default 50, max 200)"`
}

type getImpactAnalysisInput struct {
	Repo       string   `json:"repo" jsonschema:"Repository name (required)"`
	Path       string   `json:"path" jsonschema:"File path or qualified name of the entity (required)"`
	MaxHops    int      `json:"max_hops,omitempty" jsonschema:"Max traversal depth (default 2, max 5)"`
	RelKinds   []string `json:"rel_kinds,omitempty" jsonschema:"Filter by relationship kinds (e.g. depends_on, calls)"`
	MaxResults int      `json:"max_results,omitempty" jsonschema:"Max results to return (default 50, max 200)"`
}

type getDecisionContextInput struct {
	Repo       string `json:"repo" jsonschema:"Repository name (required)"`
	Path       string `json:"path" jsonschema:"File path or qualified name of the entity (required)"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Max results to return (default 50, max 200)"`
}

type getTaskContextInput struct {
	Repo       string   `json:"repo" jsonschema:"Repository name (required)"`
	Files      []string `json:"files" jsonschema:"List of file paths or qualified names (required)"`
	Depth      string   `json:"depth,omitempty" jsonschema:"shallow (default) or deep"`
	MaxResults int      `json:"max_results,omitempty" jsonschema:"Max results per sub-query (default 50, max 200)"`
}

// ── Response types ───────────────────────────────────────────────────────────

type searchResultItem struct {
	Entity     string  `json:"entity"`
	EntityKind string  `json:"entity_kind"`
	Path       string  `json:"path,omitempty"`
	Repo       string  `json:"repo,omitempty"`
	Claim      string  `json:"claim"`
	Dimension  string  `json:"dimension"`
	Category   string  `json:"category"`
	Confidence string  `json:"confidence"`
	Score      float64 `json:"score"`
}

type tripletResultItem struct {
	Source           string   `json:"source"`
	SourceKind       string   `json:"source_kind"`
	SourcePath       string   `json:"source_path,omitempty"`
	SourceRepo       string   `json:"source_repo,omitempty"`
	RelationshipKind string   `json:"relationship_kind"`
	RelDescription   string   `json:"rel_description,omitempty"`
	Target           string   `json:"target"`
	TargetKind       string   `json:"target_kind"`
	TargetPath       string   `json:"target_path,omitempty"`
	TargetRepo       string   `json:"target_repo,omitempty"`
	Score            float64  `json:"score"`
	SourceFacts      []string `json:"source_facts,omitempty"`
	TargetFacts      []string `json:"target_facts,omitempty"`
}

type repoItem struct {
	Name          string     `json:"name"`
	RemoteURL     *string    `json:"remote_url,omitempty"`
	LocalPath     string     `json:"local_path"`
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
}

type conventionItem struct {
	Claim      string `json:"claim"`
	Dimension  string `json:"dimension"`
	Confidence string `json:"confidence"`
	Entity     string `json:"entity"`
	EntityKind string `json:"entity_kind"`
	Path       string `json:"path,omitempty"`
	Repo       string `json:"repo,omitempty"`
}

type entitySummary struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Path         string   `json:"path,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Assumptions  []string `json:"assumptions,omitempty"`
}

type factItem struct {
	Claim      string `json:"claim"`
	Dimension  string `json:"dimension"`
	Category   string `json:"category"`
	Confidence string `json:"confidence"`
}

type relationshipItem struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Path             string `json:"path,omitempty"`
	Repo             string `json:"repo,omitempty"`
	RelationshipKind string `json:"relationship_kind"`
	Direction        string `json:"direction,omitempty"`
}

type moduleContextResponse struct {
	Entity        entitySummary    `json:"entity"`
	Facts         []factItem       `json:"facts"`
	Relationships []relationshipItem `json:"relationships,omitempty"`
}

type dependentItem struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Path             string `json:"path,omitempty"`
	Repo             string `json:"repo,omitempty"`
	RelationshipKind string `json:"relationship_kind"`
}

type serviceContractResponse struct {
	Entity     entitySummary  `json:"entity"`
	Dependents []dependentItem `json:"dependents"`
	Invariants []factItem     `json:"invariants"`
}

type impactItem struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Path             string `json:"path,omitempty"`
	Repo             string `json:"repo,omitempty"`
	Direction        string `json:"direction"`
	RelationshipKind string `json:"relationship_kind"`
}

type transitivePathItem struct {
	Path []pathNode `json:"path"`
}

type pathNode struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Path             string `json:"path,omitempty"`
	RelationshipKind string `json:"relationship_kind,omitempty"`
}

type impactAnalysisResponse struct {
	Entity          entitySummary        `json:"entity"`
	DirectImpacts   []impactItem         `json:"direct_impacts"`
	TransitivePaths []transitivePathItem `json:"transitive_paths,omitempty"`
	AffectedRepos   []string             `json:"affected_repos,omitempty"`
}

type decisionItem struct {
	Summary      string              `json:"summary"`
	Description  string              `json:"description"`
	Rationale    string              `json:"rationale"`
	Alternatives []models.Alternative `json:"alternatives,omitempty"`
	Tradeoffs    []string            `json:"tradeoffs,omitempty"`
	StillValid   bool                `json:"still_valid"`
}

type decisionContextResponse struct {
	Entity    entitySummary  `json:"entity"`
	Decisions []decisionItem `json:"decisions"`
}

type taskModuleContext struct {
	Path      string                   `json:"path"`
	Context   *moduleContextResponse   `json:"context,omitempty"`
	Contract  *serviceContractResponse `json:"contract,omitempty"`
	Decisions []decisionItem           `json:"decisions,omitempty"`
	Errors    []string                 `json:"errors,omitempty"`
}

type stalenessInfo struct {
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
	IndexedCommit *string    `json:"indexed_commit,omitempty"`
}

type taskContextResponse struct {
	Repo        string              `json:"repo"`
	Conventions []conventionItem    `json:"conventions"`
	Modules     []taskModuleContext `json:"modules"`
	Staleness   stalenessInfo       `json:"staleness"`
}

// ── Existing handlers ────────────────────────────────────────────────────────

func (s *Server) handleSearch(ctx context.Context, req *gomcp.CallToolRequest, args searchInput) (*gomcp.CallToolResult, any, error) {
	if args.Query == "" {
		return errorResult("query parameter is required"), nil, nil
	}
	if args.Mode != "" && args.Mode != "facts" && args.Mode != "graph" {
		return errorResult(fmt.Sprintf("invalid mode %q: must be \"facts\" or \"graph\"", args.Mode)), nil, nil
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	var repoIDs []uuid.UUID
	if args.Repo != "" {
		repo, err := s.resolveRepo(ctx, args.Repo)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		repoIDs = append(repoIDs, repo.ID)
	}

	// Graph mode: triplet-ranked search
	if args.Mode == "graph" {
		engine := query.NewEngine(s.pool, s.embedder)
		triplets, err := engine.SearchTriplets(ctx, args.Query, repoIDs, query.TripletSearchOptions{
			MaxTriplets:    limit,
			IncludeFacts:   true,
			FactsPerEntity: 3,
		})
		if err != nil {
			return errorResult(fmt.Sprintf("graph search failed: %v", err)), nil, nil
		}

		repoStore := &models.RepoStore{Pool: s.pool}
		repoNameCache := make(map[uuid.UUID]string)

		items := make([]tripletResultItem, 0, len(triplets))
		for _, t := range triplets {
			sourceRepo := lookupRepoName(ctx, repoStore, repoNameCache, t.Source.RepoID)
			targetRepo := lookupRepoName(ctx, repoStore, repoNameCache, t.Target.RepoID)
			item := tripletResultItem{
				Source:           t.Source.Name,
				SourceKind:       t.Source.Kind,
				SourcePath:       entityPath(&t.Source),
				SourceRepo:       sourceRepo,
				RelationshipKind: t.Relationship.Kind,
				Target:           t.Target.Name,
				TargetKind:       t.Target.Kind,
				TargetPath:       entityPath(&t.Target),
				TargetRepo:       targetRepo,
				Score:            t.Score,
			}
			if t.Relationship.Description != nil {
				item.RelDescription = *t.Relationship.Description
			}
			for _, f := range t.SourceFacts {
				item.SourceFacts = append(item.SourceFacts, f.Claim)
			}
			for _, f := range t.TargetFacts {
				item.TargetFacts = append(item.TargetFacts, f.Claim)
			}
			items = append(items, item)
		}
		return jsonResult(items), nil, nil
	}

	// Default: facts mode
	engine := query.NewEngine(s.pool, s.embedder)
	results, err := engine.Search(ctx, args.Query, repoIDs, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("search failed: %v", err)), nil, nil
	}

	items := make([]searchResultItem, 0, len(results))
	for _, r := range results {
		path := ""
		if r.Entity.Path != nil {
			path = *r.Entity.Path
		}
		items = append(items, searchResultItem{
			Entity:     r.Entity.Name,
			EntityKind: r.Entity.Kind,
			Path:       path,
			Repo:       r.RepoName,
			Claim:      r.Fact.Claim,
			Dimension:  r.Fact.Dimension,
			Category:   r.Fact.Category,
			Confidence: r.Fact.Confidence,
			Score:      r.Score,
		})
	}

	return jsonResult(items), nil, nil
}

func (s *Server) handleListRepos(ctx context.Context, req *gomcp.CallToolRequest, args listReposInput) (*gomcp.CallToolResult, any, error) {
	repoStore := &models.RepoStore{Pool: s.pool}
	repos, err := repoStore.List(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("listing repos: %v", err)), nil, nil
	}

	items := make([]repoItem, 0, len(repos))
	for _, r := range repos {
		items = append(items, repoItem{
			Name:          r.Name,
			RemoteURL:     r.RemoteURL,
			LocalPath:     r.LocalPath,
			LastIndexedAt: r.LastIndexedAt,
		})
	}

	return jsonResult(items), nil, nil
}

// ── New tool handlers ────────────────────────────────────────────────────────

func (s *Server) handleGetConventions(ctx context.Context, req *gomcp.CallToolRequest, args getConventionsInput) (*gomcp.CallToolResult, any, error) {
	limit := clampMaxResults(args.MaxResults, 50, 200)
	factStore := &models.FactStore{Pool: s.pool}

	var facts []models.Fact

	if args.Repo != "" {
		// Single-repo mode (original behavior)
		repo, err := s.resolveRepo(ctx, args.Repo)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		facts, err = factStore.ListByRepoAndCategory(ctx, repo.ID, []string{models.CategoryConvention, models.CategoryPattern}, limit)
		if err != nil {
			return errorResult(fmt.Sprintf("listing conventions: %v", err)), nil, nil
		}
	} else {
		// Org-wide mode: query across all repos
		var err error
		facts, err = factStore.ListByRepoAndCategoryAllRepos(ctx, []string{models.CategoryConvention, models.CategoryPattern}, limit*2)
		if err != nil {
			return errorResult(fmt.Sprintf("listing org-wide conventions: %v", err)), nil, nil
		}
	}

	// Batch fetch all referenced entities
	entityIDs := make([]uuid.UUID, 0, len(facts))
	for _, f := range facts {
		entityIDs = append(entityIDs, f.EntityID)
	}
	entityMap, err := s.batchGetEntities(ctx, entityIDs)
	if err != nil {
		return errorResult(fmt.Sprintf("fetching entities: %v", err)), nil, nil
	}

	repoStore := &models.RepoStore{Pool: s.pool}
	repoNameCache := make(map[uuid.UUID]string)

	items := make([]conventionItem, 0, len(facts))
	seen := make(map[string]bool) // deduplicate similar claims across repos
	for _, f := range facts {
		// Deduplicate: skip if we've seen a very similar claim
		claimKey := f.Dimension + ":" + models.NormalizeName(f.Claim)
		if seen[claimKey] {
			continue
		}
		seen[claimKey] = true

		item := conventionItem{
			Claim:      f.Claim,
			Dimension:  f.Dimension,
			Confidence: f.Confidence,
		}
		if e, ok := entityMap[f.EntityID]; ok && e != nil {
			item.Entity = e.Name
			item.EntityKind = e.Kind
			item.Path = entityPath(e)
		}

		// Tag with repo name in org-wide mode
		if args.Repo == "" {
			item.Repo = lookupRepoName(ctx, repoStore, repoNameCache, f.RepoID)
		}

		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}

	return jsonResult(items), nil, nil
}

func (s *Server) handleGetModuleContext(ctx context.Context, req *gomcp.CallToolRequest, args getModuleContextInput) (*gomcp.CallToolResult, any, error) {
	if args.Repo == "" {
		return errorResult("repo parameter is required"), nil, nil
	}
	if args.Path == "" {
		return errorResult("path parameter is required"), nil, nil
	}
	if args.Depth != "" && args.Depth != "shallow" && args.Depth != "deep" {
		return errorResult(fmt.Sprintf("invalid depth %q: must be \"shallow\" or \"deep\"", args.Depth)), nil, nil
	}

	repo, err := s.resolveRepo(ctx, args.Repo)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	entity, err := s.resolveEntity(ctx, repo.ID, args.Path)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	limit := clampMaxResults(args.MaxResults, 50, 200)

	factStore := &models.FactStore{Pool: s.pool}
	facts, err := factStore.ListByEntityLimited(ctx, entity.ID, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("listing facts: %v", err)), nil, nil
	}

	factItems := make([]factItem, 0, len(facts))
	for _, f := range facts {
		factItems = append(factItems, factItem{
			Claim:      f.Claim,
			Dimension:  f.Dimension,
			Category:   f.Category,
			Confidence: f.Confidence,
		})
	}

	resp := moduleContextResponse{
		Entity: toEntitySummary(entity),
		Facts:  factItems,
	}

	if args.Depth == "deep" {
		relStore := &models.RelationshipStore{Pool: s.pool}
		rels, err := relStore.ListByEntityLimited(ctx, entity.ID, limit)
		if err != nil {
			return errorResult(fmt.Sprintf("listing relationships: %v", err)), nil, nil
		}

		// Batch fetch other-end entities
		otherIDs := make([]uuid.UUID, 0, len(rels))
		for _, r := range rels {
			if r.FromEntityID == entity.ID {
				otherIDs = append(otherIDs, r.ToEntityID)
			} else {
				otherIDs = append(otherIDs, r.FromEntityID)
			}
		}
		otherMap, err := s.batchGetEntities(ctx, otherIDs)
		if err != nil {
			return errorResult(fmt.Sprintf("fetching related entities: %v", err)), nil, nil
		}

		repoStore := &models.RepoStore{Pool: s.pool}
		repoNameCache := make(map[uuid.UUID]string)
		relItems := make([]relationshipItem, 0, len(rels))
		for _, r := range rels {
			ri := relationshipItem{RelationshipKind: r.Kind}
			if r.FromEntityID == entity.ID {
				ri.Direction = "outgoing"
				if other, ok := otherMap[r.ToEntityID]; ok && other != nil {
					ri.Name = other.Name
					ri.Kind = other.Kind
					ri.Path = entityPath(other)
					ri.Repo = lookupRepoName(ctx, repoStore, repoNameCache, other.RepoID)
				}
			} else {
				ri.Direction = "incoming"
				if other, ok := otherMap[r.FromEntityID]; ok && other != nil {
					ri.Name = other.Name
					ri.Kind = other.Kind
					ri.Path = entityPath(other)
					ri.Repo = lookupRepoName(ctx, repoStore, repoNameCache, other.RepoID)
				}
			}
			relItems = append(relItems, ri)
		}
		resp.Relationships = relItems
	}

	return jsonResult(resp), nil, nil
}

func (s *Server) handleGetServiceContract(ctx context.Context, req *gomcp.CallToolRequest, args getServiceContractInput) (*gomcp.CallToolResult, any, error) {
	if args.Repo == "" {
		return errorResult("repo parameter is required"), nil, nil
	}
	if args.Path == "" {
		return errorResult("path parameter is required"), nil, nil
	}

	repo, err := s.resolveRepo(ctx, args.Repo)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	entity, err := s.resolveEntity(ctx, repo.ID, args.Path)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	limit := clampMaxResults(args.MaxResults, 50, 200)

	relStore := &models.RelationshipStore{Pool: s.pool}
	rels, err := relStore.ListDependentsOf(ctx, entity.ID, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("listing dependents: %v", err)), nil, nil
	}

	// Batch fetch dependent entities
	depIDs := make([]uuid.UUID, 0, len(rels))
	for _, r := range rels {
		depIDs = append(depIDs, r.FromEntityID)
	}
	depMap, err := s.batchGetEntities(ctx, depIDs)
	if err != nil {
		return errorResult(fmt.Sprintf("fetching dependent entities: %v", err)), nil, nil
	}

	repoStore := &models.RepoStore{Pool: s.pool}
	repoNameCache := make(map[uuid.UUID]string)
	dependents := make([]dependentItem, 0, len(rels))
	for _, r := range rels {
		di := dependentItem{RelationshipKind: r.Kind}
		if other, ok := depMap[r.FromEntityID]; ok && other != nil {
			di.Name = other.Name
			di.Kind = other.Kind
			di.Path = entityPath(other)
			di.Repo = lookupRepoName(ctx, repoStore, repoNameCache, other.RepoID)
		}
		dependents = append(dependents, di)
	}

	factStore := &models.FactStore{Pool: s.pool}
	allFacts, err := factStore.ListByEntityLimited(ctx, entity.ID, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("listing facts: %v", err)), nil, nil
	}

	invariants := make([]factItem, 0)
	for _, f := range allFacts {
		if f.Category == models.CategoryBehavior || f.Category == models.CategoryConstraint {
			invariants = append(invariants, factItem{
				Claim:      f.Claim,
				Dimension:  f.Dimension,
				Category:   f.Category,
				Confidence: f.Confidence,
			})
			if len(invariants) >= limit {
				break
			}
		}
	}

	resp := serviceContractResponse{
		Entity:     toEntitySummary(entity),
		Dependents: dependents,
		Invariants: invariants,
	}

	return jsonResult(resp), nil, nil
}

func (s *Server) handleGetImpactAnalysis(ctx context.Context, req *gomcp.CallToolRequest, args getImpactAnalysisInput) (*gomcp.CallToolResult, any, error) {
	if args.Repo == "" {
		return errorResult("repo parameter is required"), nil, nil
	}
	if args.Path == "" {
		return errorResult("path parameter is required"), nil, nil
	}

	repo, err := s.resolveRepo(ctx, args.Repo)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	entity, err := s.resolveEntity(ctx, repo.ID, args.Path)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	maxHops := args.MaxHops
	if maxHops <= 0 {
		maxHops = 2
	}
	if maxHops > 5 {
		maxHops = 5
	}

	limit := clampMaxResults(args.MaxResults, 50, 200)

	relStore := &models.RelationshipStore{Pool: s.pool}

	// Use N-hop traversal
	opts := models.TraversalOptions{
		MaxHops:     maxHops,
		RelKinds:    args.RelKinds,
		MaxEntities: limit,
		CrossRepo:   true,
	}
	subgraph, err := relStore.TraverseFromEntity(ctx, entity.ID, opts)
	if err != nil {
		return errorResult(fmt.Sprintf("traversing graph: %v", err)), nil, nil
	}

	// Shared repo name cache for impact items and affected repos
	repoStore := &models.RepoStore{Pool: s.pool}
	repoNameCache := make(map[uuid.UUID]string)

	// Build direct impacts (1-hop, backward compatible)
	directImpacts := make([]impactItem, 0)
	for _, r := range subgraph.Relationships {
		if r.FromEntityID != entity.ID && r.ToEntityID != entity.ID {
			continue // not a direct relationship
		}
		ii := impactItem{RelationshipKind: r.Kind}
		if r.FromEntityID == entity.ID {
			ii.Direction = "depends_on"
			if r.Kind == models.RelTestedBy {
				ii.Direction = "tested_by"
			}
			if other, ok := subgraph.Entities[r.ToEntityID]; ok {
				ii.Name = other.Name
				ii.Kind = other.Kind
				ii.Path = entityPath(&other)
				ii.Repo = lookupRepoName(ctx, repoStore, repoNameCache, other.RepoID)
			}
		} else {
			ii.Direction = "depended_by"
			if other, ok := subgraph.Entities[r.FromEntityID]; ok {
				ii.Name = other.Name
				ii.Kind = other.Kind
				ii.Path = entityPath(&other)
				ii.Repo = lookupRepoName(ctx, repoStore, repoNameCache, other.RepoID)
			}
		}
		directImpacts = append(directImpacts, ii)
	}

	// Build transitive paths using BFS parent-pointer tracing
	transitivePaths := buildTransitivePaths(entity.ID, subgraph)

	// Collect affected repos (reuses the shared repoNameCache populated above)
	repoSet := make(map[string]bool)
	for _, e := range subgraph.Entities {
		if e.ID == entity.ID {
			continue
		}
		name := lookupRepoName(ctx, repoStore, repoNameCache, e.RepoID)
		if name != "" && name != repo.Name {
			repoSet[name] = true
		}
	}
	var affectedRepos []string
	for name := range repoSet {
		affectedRepos = append(affectedRepos, name)
	}

	resp := impactAnalysisResponse{
		Entity:          toEntitySummary(entity),
		DirectImpacts:   directImpacts,
		TransitivePaths: transitivePaths,
		AffectedRepos:   affectedRepos,
	}

	return jsonResult(resp), nil, nil
}

// buildTransitivePaths traces paths from the seed entity to all entities at depth > 1
// using BFS parent-pointer reconstruction on the subgraph.
func buildTransitivePaths(seedID uuid.UUID, sg *models.Subgraph) []transitivePathItem {
	if sg == nil || len(sg.Relationships) == 0 {
		return nil
	}

	// Build adjacency list from relationships
	type edge struct {
		neighbor uuid.UUID
		relKind  string
	}
	adj := make(map[uuid.UUID][]edge)
	for _, r := range sg.Relationships {
		adj[r.FromEntityID] = append(adj[r.FromEntityID], edge{r.ToEntityID, r.Kind})
		adj[r.ToEntityID] = append(adj[r.ToEntityID], edge{r.FromEntityID, r.Kind})
	}

	// BFS from seed to discover shortest paths
	type bfsEntry struct {
		id      uuid.UUID
		parent  uuid.UUID
		relKind string
	}
	visited := map[uuid.UUID]bool{seedID: true}
	parent := make(map[uuid.UUID]bfsEntry) // child -> parent info
	queue := []uuid.UUID{seedID}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, e := range adj[curr] {
			if !visited[e.neighbor] {
				visited[e.neighbor] = true
				parent[e.neighbor] = bfsEntry{id: e.neighbor, parent: curr, relKind: e.relKind}
				queue = append(queue, e.neighbor)
			}
		}
	}

	// Reconstruct paths for entities at depth > 1
	var paths []transitivePathItem
	for eid, depth := range sg.Depths {
		if depth <= 1 || eid == seedID {
			continue
		}

		// Trace back from eid to seed
		var reversePath []pathNode
		curr := eid
		for curr != seedID {
			entry, ok := parent[curr]
			if !ok {
				break
			}
			if e, ok := sg.Entities[curr]; ok {
				reversePath = append(reversePath, pathNode{
					Name:             e.Name,
					Kind:             e.Kind,
					Path:             entityPath(&e),
					RelationshipKind: entry.relKind,
				})
			}
			curr = entry.parent
		}

		if len(reversePath) == 0 {
			continue
		}

		// Add seed at the beginning, reverse the rest
		seedEntity := sg.Entities[seedID]
		nodes := make([]pathNode, 0, len(reversePath)+1)
		nodes = append(nodes, pathNode{
			Name: seedEntity.Name,
			Kind: seedEntity.Kind,
			Path: entityPath(&seedEntity),
		})
		for i := len(reversePath) - 1; i >= 0; i-- {
			nodes = append(nodes, reversePath[i])
		}

		paths = append(paths, transitivePathItem{Path: nodes})
	}

	return paths
}

func (s *Server) handleGetDecisionContext(ctx context.Context, req *gomcp.CallToolRequest, args getDecisionContextInput) (*gomcp.CallToolResult, any, error) {
	if args.Repo == "" {
		return errorResult("repo parameter is required"), nil, nil
	}
	if args.Path == "" {
		return errorResult("path parameter is required"), nil, nil
	}

	repo, err := s.resolveRepo(ctx, args.Repo)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	entity, err := s.resolveEntity(ctx, repo.ID, args.Path)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	limit := clampMaxResults(args.MaxResults, 50, 200)

	decisionStore := &models.DecisionStore{Pool: s.pool}
	decisions, err := decisionStore.ListByEntity(ctx, entity.ID, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("listing decisions: %v", err)), nil, nil
	}

	items := make([]decisionItem, 0, len(decisions))
	for _, d := range decisions {
		items = append(items, decisionItem{
			Summary:      d.Summary,
			Description:  d.Description,
			Rationale:    d.Rationale,
			Alternatives: d.Alternatives,
			Tradeoffs:    d.Tradeoffs,
			StillValid:   d.StillValid,
		})
	}

	resp := decisionContextResponse{
		Entity:    toEntitySummary(entity),
		Decisions: items,
	}

	return jsonResult(resp), nil, nil
}

func (s *Server) handleGetTaskContext(ctx context.Context, req *gomcp.CallToolRequest, args getTaskContextInput) (*gomcp.CallToolResult, any, error) {
	if args.Repo == "" {
		return errorResult("repo parameter is required"), nil, nil
	}
	if len(args.Files) == 0 {
		return errorResult("files parameter is required"), nil, nil
	}
	if len(args.Files) > 20 {
		return errorResult(fmt.Sprintf("too many files: max 20, got %d", len(args.Files))), nil, nil
	}
	if args.Depth != "" && args.Depth != "shallow" && args.Depth != "deep" {
		return errorResult(fmt.Sprintf("invalid depth %q: must be \"shallow\" or \"deep\"", args.Depth)), nil, nil
	}

	repo, err := s.resolveRepo(ctx, args.Repo)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	limit := clampMaxResults(args.MaxResults, 50, 200)

	// Fetch conventions once for the repo
	factStore := &models.FactStore{Pool: s.pool}
	convFacts, err := factStore.ListByRepoAndCategory(ctx, repo.ID, []string{models.CategoryConvention, models.CategoryPattern}, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("listing conventions: %v", err)), nil, nil
	}

	// Batch fetch convention entities
	convEntityIDs := make([]uuid.UUID, 0, len(convFacts))
	for _, f := range convFacts {
		convEntityIDs = append(convEntityIDs, f.EntityID)
	}
	convEntityMap, err := s.batchGetEntities(ctx, convEntityIDs)
	if err != nil {
		return errorResult(fmt.Sprintf("fetching convention entities: %v", err)), nil, nil
	}

	conventions := make([]conventionItem, 0, len(convFacts))
	for _, f := range convFacts {
		item := conventionItem{
			Claim:      f.Claim,
			Dimension:  f.Dimension,
			Confidence: f.Confidence,
		}
		if e, ok := convEntityMap[f.EntityID]; ok && e != nil {
			item.Entity = e.Name
			item.EntityKind = e.Kind
			item.Path = entityPath(e)
		}
		conventions = append(conventions, item)
	}

	// Fetch per-file context
	repoStore := &models.RepoStore{Pool: s.pool}
	relStore := &models.RelationshipStore{Pool: s.pool}
	decisionStore := &models.DecisionStore{Pool: s.pool}
	repoNameCache := make(map[uuid.UUID]string)
	modules := make([]taskModuleContext, 0, len(args.Files))
	for _, filePath := range args.Files {
		mc := taskModuleContext{Path: filePath}

		entity, err := s.resolveEntity(ctx, repo.ID, filePath)
		if err != nil {
			mc.Errors = append(mc.Errors, fmt.Sprintf("resolving entity: %v", err))
			modules = append(modules, mc)
			continue
		}

		// Module context
		facts, err := factStore.ListByEntityLimited(ctx, entity.ID, limit)
		if err != nil {
			mc.Errors = append(mc.Errors, fmt.Sprintf("listing facts: %v", err))
		}
		factItems := make([]factItem, 0, len(facts))
		for _, f := range facts {
			factItems = append(factItems, factItem{
				Claim:      f.Claim,
				Dimension:  f.Dimension,
				Category:   f.Category,
				Confidence: f.Confidence,
			})
		}

		modCtx := &moduleContextResponse{
			Entity: toEntitySummary(entity),
			Facts:  factItems,
		}

		if args.Depth == "deep" {
			rels, err := relStore.ListByEntityLimited(ctx, entity.ID, limit)
			if err != nil {
				mc.Errors = append(mc.Errors, fmt.Sprintf("listing relationships: %v", err))
			} else {
				// Batch fetch other-end entities
				otherIDs := make([]uuid.UUID, 0, len(rels))
				for _, r := range rels {
					if r.FromEntityID == entity.ID {
						otherIDs = append(otherIDs, r.ToEntityID)
					} else {
						otherIDs = append(otherIDs, r.FromEntityID)
					}
				}
				otherMap, batchErr := s.batchGetEntities(ctx, otherIDs)
				if batchErr != nil {
					mc.Errors = append(mc.Errors, fmt.Sprintf("fetching related entities: %v", batchErr))
				} else {
					relItems := make([]relationshipItem, 0, len(rels))
					for _, r := range rels {
						ri := relationshipItem{RelationshipKind: r.Kind}
						if r.FromEntityID == entity.ID {
							ri.Direction = "outgoing"
							if other, ok := otherMap[r.ToEntityID]; ok && other != nil {
								ri.Name = other.Name
								ri.Kind = other.Kind
								ri.Path = entityPath(other)
								ri.Repo = lookupRepoName(ctx, repoStore, repoNameCache, other.RepoID)
							}
						} else {
							ri.Direction = "incoming"
							if other, ok := otherMap[r.FromEntityID]; ok && other != nil {
								ri.Name = other.Name
								ri.Kind = other.Kind
								ri.Path = entityPath(other)
								ri.Repo = lookupRepoName(ctx, repoStore, repoNameCache, other.RepoID)
							}
						}
						relItems = append(relItems, ri)
					}
					modCtx.Relationships = relItems
				}
			}
		}

		mc.Context = modCtx

		// Service contract
		depRels, err := relStore.ListDependentsOf(ctx, entity.ID, limit)
		if err != nil {
			mc.Errors = append(mc.Errors, fmt.Sprintf("listing dependents: %v", err))
		} else if len(depRels) > 0 {
			// Batch fetch dependent entities
			depIDs := make([]uuid.UUID, 0, len(depRels))
			for _, r := range depRels {
				depIDs = append(depIDs, r.FromEntityID)
			}
			depMap, batchErr := s.batchGetEntities(ctx, depIDs)
			if batchErr != nil {
				mc.Errors = append(mc.Errors, fmt.Sprintf("fetching dependent entities: %v", batchErr))
			} else {
				dependents := make([]dependentItem, 0, len(depRels))
				for _, r := range depRels {
					di := dependentItem{RelationshipKind: r.Kind}
					if other, ok := depMap[r.FromEntityID]; ok && other != nil {
						di.Name = other.Name
						di.Kind = other.Kind
						di.Path = entityPath(other)
						di.Repo = lookupRepoName(ctx, repoStore, repoNameCache, other.RepoID)
					}
					dependents = append(dependents, di)
				}

				invariants := make([]factItem, 0)
				for _, f := range facts {
					if f.Category == models.CategoryBehavior || f.Category == models.CategoryConstraint {
						invariants = append(invariants, factItem{
							Claim:      f.Claim,
							Dimension:  f.Dimension,
							Category:   f.Category,
							Confidence: f.Confidence,
						})
					}
				}

				mc.Contract = &serviceContractResponse{
					Entity:     toEntitySummary(entity),
					Dependents: dependents,
					Invariants: invariants,
				}
			}
		}

		// Decisions
		decisions, err := decisionStore.ListByEntity(ctx, entity.ID, limit)
		if err != nil {
			mc.Errors = append(mc.Errors, fmt.Sprintf("listing decisions: %v", err))
		} else if len(decisions) > 0 {
			mc.Decisions = make([]decisionItem, 0, len(decisions))
			for _, d := range decisions {
				mc.Decisions = append(mc.Decisions, decisionItem{
					Summary:      d.Summary,
					Description:  d.Description,
					Rationale:    d.Rationale,
					Alternatives: d.Alternatives,
					Tradeoffs:    d.Tradeoffs,
					StillValid:   d.StillValid,
				})
			}
		}

		modules = append(modules, mc)
	}

	resp := taskContextResponse{
		Repo:        repo.Name,
		Conventions: conventions,
		Modules:     modules,
		Staleness: stalenessInfo{
			LastIndexedAt: repo.LastIndexedAt,
			IndexedCommit: repo.LastCommitSHA,
		},
	}

	return jsonResult(resp), nil, nil
}

// ── Shared utilities ─────────────────────────────────────────────────────────

func toEntitySummary(e *models.Entity) entitySummary {
	summary := ""
	if e.Summary != nil {
		summary = *e.Summary
	}
	return entitySummary{
		Name:         e.Name,
		Kind:         e.Kind,
		Path:         entityPath(e),
		Summary:      summary,
		Capabilities: e.Capabilities,
		Assumptions:  e.Assumptions,
	}
}

func jsonResult(v any) *gomcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("marshaling result: %v", err))
	}
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{
			&gomcp.TextContent{Text: string(data)},
		},
	}
}

func errorResult(msg string) *gomcp.CallToolResult {
	r := &gomcp.CallToolResult{
		Content: []gomcp.Content{
			&gomcp.TextContent{Text: msg},
		},
	}
	r.SetError(fmt.Errorf("%s", msg))
	return r
}
