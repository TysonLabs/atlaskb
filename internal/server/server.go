package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/mcp"
)

type Server struct {
	pool     *pgxpool.Pool
	embedder embeddings.Client
	llm      llm.Client
	cfg      config.Config
	router   chi.Router
	webFS    fs.FS
}

func New(pool *pgxpool.Pool, embedder embeddings.Client, llmClient llm.Client, cfg config.Config, webFS fs.FS) *Server {
	s := &Server{
		pool:     pool,
		embedder: embedder,
		llm:      llmClient,
		cfg:      cfg,
		webFS:    webFS,
	}
	s.router = s.buildRouter()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(recoveryMiddleware)
	r.Use(loggingMiddleware)
	r.Use(corsMiddleware)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)

		r.Get("/stats", s.handleStats)
		r.Get("/stats/recent-runs", s.handleRecentRuns)

		r.Get("/repos", s.handleListRepos)
		r.Post("/repos", s.handleCreateRepo)
		r.Get("/repos/{id}", s.handleGetRepo)
		r.Put("/repos/{id}", s.handleUpdateRepo)
		r.Delete("/repos/{id}", s.handleDeleteRepo)
		r.Post("/repos/{id}/reindex", s.handleReindex)
		r.Get("/repos/{id}/reindex/status", s.handleReindexStatus)
		r.Get("/repos/{id}/indexing-runs", s.handleRepoIndexingRuns)
		r.Get("/repos/{id}/decisions", s.handleRepoDecisions)

		r.Get("/entities", s.handleListEntities)
		r.Get("/entities/{id}", s.handleGetEntity)
		r.Get("/entities/{id}/facts", s.handleEntityFacts)
		r.Get("/entities/{id}/relationships", s.handleEntityRelationships)
		r.Get("/entities/{id}/decisions", s.handleEntityDecisions)

		r.Get("/graph/repo/{id}", s.handleRepoGraph)
		r.Get("/graph/entity/{id}", s.handleEntityGraph)
		r.Get("/graph/multi", s.handleMultiRepoGraph)

		r.Get("/cross-repo/links", s.handleListCrossRepoLinks)
		r.Get("/cross-repo/links/{id}", s.handleGetCrossRepoLink)
		r.Post("/cross-repo/links", s.handleCreateCrossRepoLink)
		r.Delete("/cross-repo/links/{id}", s.handleDeleteCrossRepoLink)

		r.Post("/indexing/batch", s.handleBatchReindex)
		r.Get("/indexing/batch/status", s.handleBatchStatus)
		r.Post("/indexing/batch/cancel", s.handleBatchCancel)
		r.Get("/indexing/jobs", s.handleListIndexingJobs)
		r.Get("/indexing/history", s.handleIndexingHistory)

		r.Post("/ask", s.handleAsk)
		r.Get("/search", s.handleSearch)

		r.Get("/chats", s.handleListChats)
		r.Post("/chats", s.handleCreateChat)
		r.Get("/chats/{id}", s.handleGetChat)
		r.Put("/chats/{id}", s.handleUpdateChat)
		r.Delete("/chats/{id}", s.handleDeleteChat)
		r.Post("/chats/{id}/messages", s.handleChatMessage)
	})

	// MCP over Streamable HTTP — allows N agents to connect over HTTP
	mcpSrv := gomcp.NewServer(&gomcp.Implementation{Name: "atlaskb", Version: "0.1.0"}, nil)
	mcp.RegisterTools(mcpSrv, s.pool, s.embedder)
	mcpHandler := gomcp.NewStreamableHTTPHandler(func(*http.Request) *gomcp.Server {
		return mcpSrv
	}, nil)
	r.Handle("/mcp", mcpHandler)
	r.Handle("/mcp/*", mcpHandler)

	// SPA fallback — serve static files, fall back to index.html
	s.serveSPA(r)

	return r
}

func (s *Server) serveSPA(r chi.Router) {
	if s.webFS == nil {
		return
	}

	fileServer := http.FileServerFS(s.webFS)

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		// Try to serve the file directly
		if f, err := s.webFS.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fall back to index.html for SPA routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
