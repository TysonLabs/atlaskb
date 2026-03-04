package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
		Name: "search_knowledge_base",
		Description: `Search the code knowledge graph using natural language. Use this to answer questions like "how does auth work?" or "what patterns are used for error handling?".
Returns facts (assertions about code) with their source entity, confidence, and provenance.
mode=facts (default): returns individual ranked facts. mode=graph: returns (entity)-[relationship]->(entity) triplets showing how code entities relate.
Use this when you don't know which file or entity to look at — it searches across all indexed knowledge.`,
	}, s.handleSearch)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "list_repos",
		Description: "List all repositories indexed in AtlasKB with their names and last indexed timestamps. Call this first to discover available repo names for use with other tools.",
	}, s.handleListRepos)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_conventions",
		Description: `Get coding conventions and patterns (e.g. error handling style, naming conventions, import patterns, auth approach).
Use this to understand HOW code should be written in a repo — before writing new code or reviewing existing code.
When repo is omitted, returns org-wide conventions across all repos (useful for understanding cross-repo standards).`,
	}, s.handleGetConventions)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_module_context",
		Description: `Get detailed context about a specific file, function, type, or module. Returns its summary, capabilities, assumptions, and facts.
Use this when you need to understand what a specific piece of code does before modifying or using it.
Set depth=deep to also get its relationships (what it calls, what calls it, what it depends on).
The path parameter accepts file paths (e.g. "internal/server/handlers.go") or qualified names (e.g. "github.com/foo/bar.Handler").`,
	}, s.handleGetModuleContext)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_service_contract",
		Description: `Get the public contract of a code entity: who depends on it and what invariants it must maintain.
Use this BEFORE modifying a function, type, or module to understand what would break — shows all downstream dependents and behavioral contracts.
Critical for safe refactoring and API changes.`,
	}, s.handleGetServiceContract)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_impact_analysis",
		Description: `Analyze the dependency graph around a code entity with N-hop traversal. Shows direct impacts and transitive dependency chains.
Use this to answer "what would be affected if I change X?" — traces through calls, depends_on, imports, and other relationship types.
Returns direct impacts (1 hop) and transitive paths (multi-hop chains) with affected repos listed.`,
	}, s.handleGetImpactAnalysis)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_decision_context",
		Description: `Get architectural decisions linked to a code entity — why it was built this way, what alternatives were considered, and what tradeoffs were accepted.
Use this when you need to understand the rationale behind existing code, or before proposing changes that might conflict with past decisions.`,
	}, s.handleGetDecisionContext)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_task_context",
		Description: `RECOMMENDED: Start here for any coding task. Bundles conventions, module context, service contracts, decisions, and staleness info for a set of files in one call.
Prefer this over calling get_module_context, get_service_contract, get_conventions, and get_decision_context separately — it combines all of them efficiently.
Pass the files you plan to read or modify. Returns everything you need to understand the code and write changes safely.`,
	}, s.handleGetTaskContext)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_execution_flows",
		Description: `Get detected execution flows (call chains) through the codebase — shows how functions call each other in sequence from entry points.
Use this to understand runtime behavior: "what happens when HandleRequest is called?" or "what functions does ProcessOrder invoke?".
Use the 'through' parameter to filter to flows passing through a specific function.`,
	}, s.handleGetExecutionFlows)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_functional_clusters",
		Description: `Get functional clusters — groups of code entities that form cohesive areas based on their call/dependency relationships.
Use this to understand the high-level architecture: what are the major subsystems and which files/functions belong to each.
Useful for onboarding to a new codebase or planning large refactors.`,
	}, s.handleGetFunctionalClusters)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_repo_overview",
		Description: `Get a high-level architectural overview of a repository with entity, fact, relationship, and decision counts.
Use this to quickly understand what a repo does, its major components, and its scale — before diving into specific files or entities.`,
	}, s.handleGetRepoOverview)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "search_entities",
		Description: `Search for code entities (functions, types, modules, services, endpoints, configs) by name or kind.
Use this to find specific code symbols — e.g. "find all endpoints" or "find functions matching Handler".
Returns entity name, kind, file path, and summary. Click through to get_module_context for full details.`,
	}, s.handleSearchEntities)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name: "get_entity_source",
		Description: `Read the source code of a file from an indexed repository. Returns the raw file content.
Use this when you need to see the actual implementation — after using get_module_context to understand what a file does.
The path is relative to the repo root (e.g. "internal/server/handlers.go"). Files larger than 500KB are truncated.`,
	}, s.handleGetEntitySource)
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
	Query string `json:"query" jsonschema:"Natural language question or keyword search (e.g. 'how does authentication work?' or 'error handling patterns')"`
	Repo  string `json:"repo,omitempty" jsonschema:"Filter to a specific repository name (omit to search all repos)"`
	Mode  string `json:"mode,omitempty" jsonschema:"Search mode: facts (default) returns ranked assertions, graph returns entity-relationship triplets"`
	Limit int    `json:"limit,omitempty" jsonschema:"Max results to return (default 20, max 50)"`
}

type listReposInput struct{}

type getConventionsInput struct {
	Repo       string `json:"repo,omitempty" jsonschema:"Repository name (optional — omit to get conventions from all repos)"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Max results to return (default 50, max 200)"`
}

type getModuleContextInput struct {
	Repo       string `json:"repo" jsonschema:"Repository name (required)"`
	Path       string `json:"path" jsonschema:"File path (e.g. 'internal/server/handlers.go') or qualified name (e.g. 'github.com/foo/bar.Handler')"`
	Depth      string `json:"depth,omitempty" jsonschema:"shallow (default) or deep — deep includes all relationships (calls, imports, depends_on)"`
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
	MinConfidence float32  `json:"min_confidence,omitempty" jsonschema:"Minimum confidence threshold (0.0-1.0) to filter relationships"`
}

type getDecisionContextInput struct {
	Repo       string `json:"repo" jsonschema:"Repository name (required)"`
	Path       string `json:"path" jsonschema:"File path or qualified name of the entity (required)"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Max results to return (default 50, max 200)"`
}

type getTaskContextInput struct {
	Repo       string   `json:"repo" jsonschema:"Repository name (required)"`
	Files      []string `json:"files" jsonschema:"File paths you plan to read or modify (e.g. ['internal/server/handlers.go', 'internal/models/user.go']). Max 20 files."`
	Depth      string   `json:"depth,omitempty" jsonschema:"shallow (default) or deep — deep includes all relationships per file"`
	MaxResults int      `json:"max_results,omitempty" jsonschema:"Max results per sub-query (default 50, max 200)"`
}

type getExecutionFlowsInput struct {
	Repo       string `json:"repo" jsonschema:"Repository name (required)"`
	Through    string `json:"through,omitempty" jsonschema:"Filter to flows containing this function name"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Max flows to return (default 20, max 50)"`
}

type getFunctionalClustersInput struct {
	Repo       string `json:"repo" jsonschema:"Repository name (required)"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Max results to return (default 50, max 200)"`
}

type getRepoOverviewInput struct {
	Repo string `json:"repo" jsonschema:"Repository name (required)"`
}

type searchEntitiesInput struct {
	Repo       string `json:"repo,omitempty" jsonschema:"Filter to a specific repository name (omit to search all repos)"`
	Query      string `json:"query,omitempty" jsonschema:"Search by name (e.g. 'Handler', 'UserService') — matches partial names"`
	Kind       string `json:"kind,omitempty" jsonschema:"Filter by entity kind: function, type, module, service, endpoint, config, concept, cluster"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Max results to return (default 20, max 100)"`
	Offset     int    `json:"offset,omitempty" jsonschema:"Offset for pagination (default 0)"`
}

type getEntitySourceInput struct {
	Repo string `json:"repo" jsonschema:"Repository name (required)"`
	Path string `json:"path" jsonschema:"File path relative to repo root (e.g. 'internal/server/handlers.go')"`
}

// ── Response types ───────────────────────────────────────────────────────────

type searchResultItem struct {
	Entity     string             `json:"entity"`
	EntityKind string             `json:"entity_kind"`
	Path       string             `json:"path,omitempty"`
	Repo       string             `json:"repo,omitempty"`
	Claim      string             `json:"claim"`
	Dimension  string             `json:"dimension"`
	Category   string             `json:"category"`
	Confidence string             `json:"confidence"`
	Score      float64            `json:"score"`
	Provenance []models.Provenance `json:"provenance,omitempty"`
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
	Name          string   `json:"name"`
	QualifiedName string   `json:"qualified_name,omitempty"`
	Kind          string   `json:"kind"`
	Path          string   `json:"path,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Signature     string   `json:"signature,omitempty"`
	Returns       string   `json:"returns,omitempty"`
	StartLine     *int     `json:"start_line,omitempty"`
	EndLine       *int     `json:"end_line,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
	Assumptions   []string `json:"assumptions,omitempty"`
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
	Confidence       float32 `json:"confidence,omitempty"`
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
	Confidence       float32 `json:"confidence,omitempty"`
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
	Confidence       float32 `json:"confidence"`
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

type relatedFileHint struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type taskModuleContext struct {
	Path         string                   `json:"path"`
	Entities     []entitySummary          `json:"entities,omitempty"`
	Imports      []string                 `json:"imports,omitempty"`
	RelatedFiles []relatedFileHint        `json:"related_files,omitempty"`
	Context      *moduleContextResponse   `json:"context,omitempty"`
	Contract     *serviceContractResponse `json:"contract,omitempty"`
	Decisions    []decisionItem           `json:"decisions,omitempty"`
	Errors       []string                 `json:"errors,omitempty"`
}

type stalenessInfo struct {
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
	IndexedCommit *string    `json:"indexed_commit,omitempty"`
	HeadCommit    *string    `json:"head_commit,omitempty"`
	CommitsBehind *int       `json:"commits_behind,omitempty"`
}

type taskContextResponse struct {
	Repo        string              `json:"repo"`
	Conventions []conventionItem    `json:"conventions"`
	Modules     []taskModuleContext `json:"modules"`
	Staleness   stalenessInfo       `json:"staleness"`
}

type repoOverviewResponse struct {
	Name              string     `json:"name"`
	Overview          string     `json:"overview"`
	EntityCount       int        `json:"entity_count"`
	FactCount         int        `json:"fact_count"`
	RelationshipCount int        `json:"relationship_count"`
	DecisionCount     int        `json:"decision_count"`
	LastIndexedAt     *time.Time `json:"last_indexed_at,omitempty"`
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
			Provenance: r.Fact.Provenance,
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
			ri := relationshipItem{RelationshipKind: r.Kind, Confidence: r.Confidence}
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
		di := dependentItem{RelationshipKind: r.Kind, Confidence: r.Confidence}
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
		if f.Category == models.CategoryBehavior || f.Category == models.CategoryConstraint || f.Category == models.CategoryContract {
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
		MinConfidence: args.MinConfidence,
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
		ii := impactItem{RelationshipKind: r.Kind, Confidence: r.Confidence}
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

	// Fetch conventions once for the repo (capped at 20 for soldiers)
	factStore := &models.FactStore{Pool: s.pool}
	convLimit := limit
	if convLimit > 20 {
		convLimit = 20
	}
	convFacts, err := factStore.ListByRepoAndCategory(ctx, repo.ID, []string{models.CategoryConvention, models.CategoryPattern}, convLimit)
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
	entityStore := &models.EntityStore{Pool: s.pool}
	repoStore := &models.RepoStore{Pool: s.pool}
	relStore := &models.RelationshipStore{Pool: s.pool}
	decisionStore := &models.DecisionStore{Pool: s.pool}
	repoNameCache := make(map[uuid.UUID]string)
	modules := make([]taskModuleContext, 0, len(args.Files))
	for _, filePath := range args.Files {
		mc := taskModuleContext{Path: filePath}

		entity, resolveErr := s.resolveEntity(ctx, repo.ID, filePath)

		// List all entities in this file (table of contents for the soldier)
		// This works even when resolveEntity fails — ListByPath checks the path column directly
		fileEntities, _ := entityStore.ListByPath(ctx, repo.ID, filePath)
		if len(fileEntities) == 0 {
			// Suffix fallback: handles cases where indexed path has different prefix
			fileEntities, _ = entityStore.ListByPathSuffix(ctx, repo.ID, filePath)
		}
		if len(fileEntities) == 0 && entity != nil && entity.Path != nil && *entity.Path != filePath {
			fileEntities, _ = entityStore.ListByPath(ctx, repo.ID, *entity.Path)
		}

		// If resolveEntity failed but we found file entities, use the first as primary
		if resolveErr != nil && len(fileEntities) > 0 {
			entity = &fileEntities[0]
			resolveErr = nil
		}

		if resolveErr != nil {
			mc.Errors = append(mc.Errors, fmt.Sprintf("resolving entity: %v", resolveErr))
			modules = append(modules, mc)
			continue
		}
		if len(fileEntities) > 0 {
			mc.Entities = make([]entitySummary, 0, len(fileEntities))
			for i := range fileEntities {
				mc.Entities = append(mc.Entities, toEntitySummary(&fileEntities[i]))
			}

			// Collect imports and related files from relationships on file entities
			fileEntityIDs := make([]uuid.UUID, 0, len(fileEntities))
			for _, fe := range fileEntities {
				fileEntityIDs = append(fileEntityIDs, fe.ID)
			}
			fileEntityIDSet := make(map[uuid.UUID]bool, len(fileEntityIDs))
			for _, id := range fileEntityIDs {
				fileEntityIDSet[id] = true
			}

			importSet := make(map[string]bool)
			relatedMap := make(map[string]string) // path -> reason
			for _, feID := range fileEntityIDs {
				rels, err := relStore.ListByEntityLimited(ctx, feID, 50)
				if err != nil {
					continue
				}
				for _, r := range rels {
					otherID := r.ToEntityID
					direction := "outgoing"
					if r.FromEntityID != feID {
						otherID = r.FromEntityID
						direction = "incoming"
					}

					// Collect imports
					if r.Kind == "imports" && direction == "outgoing" {
						other, _ := entityStore.GetByID(ctx, otherID)
						if other != nil {
							importSet[other.QualifiedName] = true
						}
						continue
					}

					// Collect related files (skip entities in same file)
					if !fileEntityIDSet[otherID] {
						other, _ := entityStore.GetByID(ctx, otherID)
						if other != nil && other.Path != nil && *other.Path != filePath {
							reason := r.Kind
							if direction == "incoming" {
								reason = "depended on by " + other.Name
							} else {
								reason = "depends on " + other.Name
							}
							if _, exists := relatedMap[*other.Path]; !exists {
								relatedMap[*other.Path] = reason
							}
						}
					}
				}
			}

			if len(importSet) > 0 {
				mc.Imports = make([]string, 0, len(importSet))
				for imp := range importSet {
					mc.Imports = append(mc.Imports, imp)
				}
			}
			if len(relatedMap) > 0 {
				mc.RelatedFiles = make([]relatedFileHint, 0, len(relatedMap))
				for path, reason := range relatedMap {
					mc.RelatedFiles = append(mc.RelatedFiles, relatedFileHint{
						Path:   path,
						Reason: reason,
					})
				}
			}
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
						ri := relationshipItem{RelationshipKind: r.Kind, Confidence: r.Confidence}
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
					di := dependentItem{RelationshipKind: r.Kind, Confidence: r.Confidence}
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
					if f.Category == models.CategoryBehavior || f.Category == models.CategoryConstraint || f.Category == models.CategoryContract {
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

	staleness := stalenessInfo{
		LastIndexedAt: repo.LastIndexedAt,
		IndexedCommit: repo.LastCommitSHA,
	}

	// Try to compute commit distance from git if repo path is accessible
	if repo.LocalPath != "" && repo.LastCommitSHA != nil {
		if headOut, err := exec.CommandContext(ctx, "git", "-C", repo.LocalPath, "rev-parse", "HEAD").Output(); err == nil {
			head := strings.TrimSpace(string(headOut))
			staleness.HeadCommit = &head
			if head != *repo.LastCommitSHA {
				countCmd := exec.CommandContext(ctx, "git", "-C", repo.LocalPath, "rev-list", "--count", *repo.LastCommitSHA+"..HEAD")
				if countOut, err := countCmd.Output(); err == nil {
					if n, err := strconv.Atoi(strings.TrimSpace(string(countOut))); err == nil {
						staleness.CommitsBehind = &n
					}
				}
			} else {
				zero := 0
				staleness.CommitsBehind = &zero
			}
		}
	}

	resp := taskContextResponse{
		Repo:        repo.Name,
		Conventions: conventions,
		Modules:     modules,
		Staleness:   staleness,
	}

	return jsonResult(resp), nil, nil
}

// ── Shared utilities ─────────────────────────────────────────────────────────

func toEntitySummary(e *models.Entity) entitySummary {
	summary := ""
	if e.Summary != nil {
		summary = *e.Summary
	}
	sig := ""
	if e.Signature != nil {
		sig = *e.Signature
	}
	returns := ""
	if e.TypeRef != nil {
		returns = *e.TypeRef
	}
	return entitySummary{
		Name:          e.Name,
		QualifiedName: e.QualifiedName,
		Kind:          e.Kind,
		Path:          entityPath(e),
		Summary:       summary,
		Signature:     sig,
		Returns:       returns,
		StartLine:     e.StartLine,
		EndLine:       e.EndLine,
		Capabilities:  e.Capabilities,
		Assumptions:   e.Assumptions,
	}
}

// ── Execution flows handler ──────────────────────────────────────────────────

type flowResultItem struct {
	EntryPoint string   `json:"entry_point"`
	Label      string   `json:"label"`
	Steps      []string `json:"steps"`
	Depth      int      `json:"depth"`
}

func (s *Server) handleGetExecutionFlows(ctx context.Context, req *gomcp.CallToolRequest, args getExecutionFlowsInput) (*gomcp.CallToolResult, any, error) {
	if args.Repo == "" {
		return errorResult("repo is required"), nil, nil
	}
	repo, err := s.resolveRepo(ctx, args.Repo)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	limit := clampMaxResults(args.MaxResults, 20, 50)
	flowStore := &models.FlowStore{Pool: s.pool}

	var flows []models.ExecutionFlow

	if args.Through != "" {
		// Find entities matching the "through" name, then find flows containing them
		entityStore := &models.EntityStore{Pool: s.pool}
		entities, err := entityStore.FindByName(ctx, repo.ID, args.Through)
		if err != nil {
			return errorResult(fmt.Sprintf("finding entity: %v", err)), nil, nil
		}
		if len(entities) == 0 {
			return jsonResult([]flowResultItem{}), nil, nil
		}

		// Collect flows from all matching entities, dedup by flow ID
		seen := make(map[uuid.UUID]bool)
		for _, e := range entities {
			eFlows, err := flowStore.FindByEntity(ctx, e.ID, limit)
			if err != nil {
				continue
			}
			for _, f := range eFlows {
				if !seen[f.ID] {
					seen[f.ID] = true
					flows = append(flows, f)
				}
			}
		}
		// Cap at limit
		if len(flows) > limit {
			flows = flows[:limit]
		}
	} else {
		flows, err = flowStore.ListByRepo(ctx, repo.ID, limit)
		if err != nil {
			return errorResult(fmt.Sprintf("listing flows: %v", err)), nil, nil
		}
	}

	// Batch-fetch entry point entities for display names
	entryIDs := make([]uuid.UUID, 0, len(flows))
	for _, f := range flows {
		entryIDs = append(entryIDs, f.EntryEntityID)
	}
	entryMap, _ := s.batchGetEntities(ctx, entryIDs)

	items := make([]flowResultItem, 0, len(flows))
	for _, f := range flows {
		entryName := ""
		if e, ok := entryMap[f.EntryEntityID]; ok {
			entryName = e.QualifiedName
		}
		items = append(items, flowResultItem{
			EntryPoint: entryName,
			Label:      f.Label,
			Steps:      f.StepNames,
			Depth:      f.Depth,
		})
	}

	return jsonResult(items), nil, nil
}

// ── Functional Clusters ─────────────────────────────────────────────────────

type clusterMemberItem struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
}

type clusterItem struct {
	Label       string              `json:"label"`
	Description string              `json:"description"`
	MemberCount int                 `json:"member_count"`
	Members     []clusterMemberItem `json:"members"`
}

type functionalClustersResponse struct {
	Clusters []clusterItem `json:"clusters"`
}

func (s *Server) handleGetFunctionalClusters(ctx context.Context, req *gomcp.CallToolRequest, args getFunctionalClustersInput) (*gomcp.CallToolResult, any, error) {
	if args.Repo == "" {
		return errorResult("repo parameter is required"), nil, nil
	}

	repo, err := s.resolveRepo(ctx, args.Repo)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	limit := clampMaxResults(args.MaxResults, 50, 200)

	entityStore := &models.EntityStore{Pool: s.pool}
	relStore := &models.RelationshipStore{Pool: s.pool}

	// Find all cluster entities for this repo
	clusters, err := entityStore.ListByRepoAndKind(ctx, repo.ID, models.EntityCluster)
	if err != nil {
		return errorResult(fmt.Sprintf("listing clusters: %v", err)), nil, nil
	}

	if len(clusters) == 0 {
		return jsonResult(functionalClustersResponse{Clusters: []clusterItem{}}), nil, nil
	}

	var items []clusterItem
	for _, cluster := range clusters {
		if len(items) >= limit {
			break
		}

		// Find member_of relationships pointing TO this cluster
		rels, err := relStore.ListDependentsOf(ctx, cluster.ID, 200)
		if err != nil {
			continue
		}

		// Filter to only member_of relationships
		var memberIDs []uuid.UUID
		for _, r := range rels {
			if r.Kind == models.RelMemberOf {
				memberIDs = append(memberIDs, r.FromEntityID)
			}
		}

		// Batch fetch member entities
		memberEntities, err := entityStore.GetByIDs(ctx, memberIDs)
		if err != nil {
			continue
		}

		members := make([]clusterMemberItem, 0, len(memberEntities))
		for _, m := range memberEntities {
			item := clusterMemberItem{
				Name: m.QualifiedName,
				Kind: m.Kind,
			}
			if m.Path != nil {
				item.Path = *m.Path
			}
			members = append(members, item)
		}

		description := ""
		if cluster.Summary != nil {
			description = *cluster.Summary
		}

		items = append(items, clusterItem{
			Label:       cluster.Name,
			Description: description,
			MemberCount: len(members),
			Members:     members,
		})
	}

	return jsonResult(functionalClustersResponse{Clusters: items}), nil, nil
}

// ── Repo Overview ────────────────────────────────────────────────────────────

func (s *Server) handleGetRepoOverview(ctx context.Context, req *gomcp.CallToolRequest, args getRepoOverviewInput) (*gomcp.CallToolResult, any, error) {
	if args.Repo == "" {
		return errorResult("repo parameter is required"), nil, nil
	}

	repo, err := s.resolveRepo(ctx, args.Repo)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	overview := ""
	if repo.Overview != nil {
		overview = *repo.Overview
	}

	// Query counts for entities, facts, relationships, decisions
	var entityCount, factCount, relCount, decisionCount int

	err = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM entities WHERE repo_id = $1`, repo.ID).Scan(&entityCount)
	if err != nil {
		return errorResult(fmt.Sprintf("counting entities: %v", err)), nil, nil
	}

	err = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM facts WHERE repo_id = $1`, repo.ID).Scan(&factCount)
	if err != nil {
		return errorResult(fmt.Sprintf("counting facts: %v", err)), nil, nil
	}

	err = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM relationships WHERE repo_id = $1`, repo.ID).Scan(&relCount)
	if err != nil {
		return errorResult(fmt.Sprintf("counting relationships: %v", err)), nil, nil
	}

	err = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM decisions WHERE repo_id = $1`, repo.ID).Scan(&decisionCount)
	if err != nil {
		return errorResult(fmt.Sprintf("counting decisions: %v", err)), nil, nil
	}

	resp := repoOverviewResponse{
		Name:              repo.Name,
		Overview:          overview,
		EntityCount:       entityCount,
		FactCount:         factCount,
		RelationshipCount: relCount,
		DecisionCount:     decisionCount,
		LastIndexedAt:     repo.LastIndexedAt,
	}

	return jsonResult(resp), nil, nil
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

// ── search_entities handler ──────────────────────────────────────────────────

type entityResultItem struct {
	Name          string   `json:"name"`
	QualifiedName string   `json:"qualified_name"`
	Kind          string   `json:"kind"`
	Path          string   `json:"path,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Repo          string   `json:"repo,omitempty"`
}

func (s *Server) handleSearchEntities(ctx context.Context, _ *gomcp.CallToolRequest, params searchEntitiesInput) (*gomcp.CallToolResult, any, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	var repoID *uuid.UUID
	var repoName string
	if params.Repo != "" {
		repo, err := s.resolveRepo(ctx, params.Repo)
		if err != nil {
			return errorResult(err.Error()), nil, nil
		}
		repoID = &repo.ID
		repoName = repo.Name
	}

	entityStore := &models.EntityStore{Pool: s.pool}
	result, err := entityStore.SearchByName(ctx, repoID, params.Query, params.Kind, limit, offset)
	if err != nil {
		return errorResult(fmt.Sprintf("searching entities: %v", err)), nil, nil
	}

	// Resolve repo names for display
	repoNames := map[uuid.UUID]string{}
	if repoID != nil {
		repoNames[*repoID] = repoName
	} else {
		repoStore := &models.RepoStore{Pool: s.pool}
		for _, e := range result.Items {
			if _, ok := repoNames[e.RepoID]; !ok {
				r, err := repoStore.GetByID(ctx, e.RepoID)
				if err == nil && r != nil {
					repoNames[e.RepoID] = r.Name
				}
			}
		}
	}

	items := make([]entityResultItem, len(result.Items))
	for i, e := range result.Items {
		var path, summary string
		if e.Path != nil {
			path = *e.Path
		}
		if e.Summary != nil {
			summary = *e.Summary
		}
		items[i] = entityResultItem{
			Name:          e.Name,
			QualifiedName: e.QualifiedName,
			Kind:          e.Kind,
			Path:          path,
			Summary:       summary,
			Repo:          repoNames[e.RepoID],
		}
	}

	type response struct {
		Items []entityResultItem `json:"items"`
		Total int                `json:"total"`
	}
	out := response{Items: items, Total: result.Total}
	return jsonResult(out), nil, nil
}

// ── get_entity_source handler ────────────────────────────────────────────────

func (s *Server) handleGetEntitySource(ctx context.Context, _ *gomcp.CallToolRequest, params getEntitySourceInput) (*gomcp.CallToolResult, any, error) {
	if params.Repo == "" {
		return errorResult("repo is required"), nil, nil
	}
	if params.Path == "" {
		return errorResult("path is required"), nil, nil
	}

	repo, err := s.resolveRepo(ctx, params.Repo)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	// SECURITY: Resolve and validate that the requested path is within the repo
	absRepo, err := filepath.Abs(repo.LocalPath)
	if err != nil {
		return errorResult("failed to resolve repo path"), nil, nil
	}
	fullPath := filepath.Join(absRepo, filepath.Clean(params.Path))
	if !strings.HasPrefix(fullPath, absRepo) {
		return errorResult("path outside repository"), nil, nil
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return errorResult(fmt.Sprintf("file not found: %s", params.Path)), nil, nil
	}

	// Limit response size
	if len(content) > 500_000 {
		content = content[:500_000]
	}

	type response struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	out := response{Path: params.Path, Content: string(content)}
	return jsonResult(out), nil, nil
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
