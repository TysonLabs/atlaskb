package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
	"github.com/tgeorge06/atlaskb/internal/query"
)

type askRequest struct {
	Question string   `json:"question"`
	RepoID   string   `json:"repo_id,omitempty"`
	RepoIDs  []string `json:"repo_ids,omitempty"`
	RepoName string   `json:"repo_name,omitempty"`
	TopK     int      `json:"top_k,omitempty"`
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var req askRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, NewBadRequest("invalid request body"))
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		writeError(w, NewBadRequest("question is required"))
		return
	}
	if req.TopK <= 0 {
		req.TopK = 40
	}

	repoIDs, err := s.resolveRepoIDs(r.Context(), req.RepoID, req.RepoIDs, req.RepoName)
	if err != nil {
		writeError(w, NewBadRequest(err.Error()))
		return
	}

	engine := query.NewEngine(s.pool, s.embedder)
	if s.llm != nil {
		engine.SetLLM(s.llm, s.cfg.Pipeline.ExtractionModel)
	}

	results, err := engine.Search(r.Context(), req.Question, repoIDs, req.TopK)
	if err != nil {
		writeError(w, NewInternal("search failed: "+err.Error()))
		return
	}

	// Set up SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, NewInternal("streaming not supported"))
		return
	}

	// Send retrieved facts as the first event
	factsJSON, _ := json.Marshal(results)
	fmt.Fprintf(w, "event: facts\ndata: %s\n\n", factsJSON)
	flusher.Flush()

	if len(results) == 0 {
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	synth := query.NewSynthesizer(s.llm, s.cfg.Pipeline.SynthesisModel)
	stream, err := synth.Synthesize(r.Context(), req.Question, results)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
		flusher.Flush()
		return
	}

	for chunk := range stream {
		if chunk.Error != nil {
			fmt.Fprintf(w, "event: error\ndata: %q\n\n", chunk.Error.Error())
			flusher.Flush()
			return
		}
		escaped, _ := json.Marshal(chunk.Text)
		fmt.Fprintf(w, "event: chunk\ndata: %s\n\n", escaped)
		flusher.Flush()
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, NewBadRequest("q parameter required"))
		return
	}

	// Parse repo_ids (comma-separated), repo_id (single), and repo_name
	var rawRepoIDs []string
	if rids := r.URL.Query().Get("repo_ids"); rids != "" {
		rawRepoIDs = strings.Split(rids, ",")
	}
	repoIDs, err := s.resolveRepoIDs(r.Context(), r.URL.Query().Get("repo_id"), rawRepoIDs, r.URL.Query().Get("repo_name"))
	if err != nil {
		writeError(w, NewBadRequest(err.Error()))
		return
	}

	limit := 20
	engine := query.NewEngine(s.pool, s.embedder)
	if s.llm != nil {
		engine.SetLLM(s.llm, s.cfg.Pipeline.ExtractionModel)
	}

	results, err := engine.Search(r.Context(), q, repoIDs, limit)
	if err != nil {
		writeError(w, NewInternal("search failed: "+err.Error()))
		return
	}
	if results == nil {
		results = []query.SearchResult{}
	}
	writeJSONWithETag(w, r, http.StatusOK, results)
}

// resolveRepoIDs merges repo_id, repo_ids, and repo_name into a deduplicated []uuid.UUID.
func (s *Server) resolveRepoIDs(ctx context.Context, repoID string, repoIDs []string, repoName string) ([]uuid.UUID, error) {
	seen := make(map[uuid.UUID]bool)
	var result []uuid.UUID

	// Single repo_id
	if repoID != "" {
		id, err := uuid.Parse(repoID)
		if err != nil {
			return nil, fmt.Errorf("invalid repo_id: %s", repoID)
		}
		seen[id] = true
		result = append(result, id)
	}

	// Multiple repo_ids
	for _, rid := range repoIDs {
		rid = strings.TrimSpace(rid)
		if rid == "" {
			continue
		}
		id, err := uuid.Parse(rid)
		if err != nil {
			return nil, fmt.Errorf("invalid repo_id in repo_ids: %s", rid)
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}

	// Resolve repo_name to ID
	if repoName != "" {
		repoStore := &models.RepoStore{Pool: s.pool}
		repo, err := repoStore.GetByName(ctx, repoName)
		if err != nil {
			return nil, fmt.Errorf("looking up repo_name %q: %w", repoName, err)
		}
		if repo == nil {
			return nil, fmt.Errorf("repo not found: %s", repoName)
		}
		if !seen[repo.ID] {
			seen[repo.ID] = true
			result = append(result, repo.ID)
		}
	}

	return result, nil
}
