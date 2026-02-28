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

func (s *Server) Run(ctx context.Context) error {
	srv := gomcp.NewServer(
		&gomcp.Implementation{Name: "atlaskb", Version: "0.1.0"},
		nil,
	)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "search_knowledge_base",
		Description: "Search the AtlasKB knowledge graph for facts about an indexed codebase. Returns entities, claims, and metadata ranked by relevance.",
	}, s.handleSearch)

	gomcp.AddTool(srv, &gomcp.Tool{
		Name:        "list_repos",
		Description: "List all repositories indexed in AtlasKB.",
	}, s.handleListRepos)

	return srv.Run(ctx, &gomcp.StdioTransport{})
}

// Tool input types

type searchInput struct {
	Query string `json:"query" jsonschema:"Natural language search query"`
	Repo  string `json:"repo,omitempty" jsonschema:"Filter by repository name"`
	Limit int    `json:"limit,omitempty" jsonschema:"Max results to return (default 20, max 50)"`
}

type listReposInput struct{}

// Response types

type searchResultItem struct {
	Entity     string  `json:"entity"`
	EntityKind string  `json:"entity_kind"`
	Path       string  `json:"path,omitempty"`
	Claim      string  `json:"claim"`
	Dimension  string  `json:"dimension"`
	Category   string  `json:"category"`
	Confidence string  `json:"confidence"`
	Score      float64 `json:"score"`
}

type repoItem struct {
	Name          string     `json:"name"`
	RemoteURL     *string    `json:"remote_url,omitempty"`
	LocalPath     string     `json:"local_path"`
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
}

func (s *Server) handleSearch(ctx context.Context, req *gomcp.CallToolRequest, args searchInput) (*gomcp.CallToolResult, any, error) {
	if args.Query == "" {
		return errorResult("query parameter is required"), nil, nil
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
		repoStore := &models.RepoStore{Pool: s.pool}
		repos, err := repoStore.List(ctx)
		if err != nil {
			return errorResult(fmt.Sprintf("listing repos: %v", err)), nil, nil
		}
		for _, r := range repos {
			if r.Name == args.Repo {
				repoIDs = append(repoIDs, r.ID)
			}
		}
		if len(repoIDs) == 0 {
			return errorResult(fmt.Sprintf("repository %q not found", args.Repo)), nil, nil
		}
	}

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
