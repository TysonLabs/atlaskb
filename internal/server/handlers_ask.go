package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/query"
)

type askRequest struct {
	Question string `json:"question"`
	RepoID   string `json:"repo_id,omitempty"`
	TopK     int    `json:"top_k,omitempty"`
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

	var repoIDs []uuid.UUID
	if req.RepoID != "" {
		id, err := uuid.Parse(req.RepoID)
		if err != nil {
			writeError(w, NewBadRequest("invalid repo_id"))
			return
		}
		repoIDs = []uuid.UUID{id}
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

	var repoIDs []uuid.UUID
	if rid := r.URL.Query().Get("repo_id"); rid != "" {
		id, err := uuid.Parse(rid)
		if err != nil {
			writeError(w, NewBadRequest("invalid repo_id"))
			return
		}
		repoIDs = []uuid.UUID{id}
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
	writeJSON(w, http.StatusOK, results)
}
