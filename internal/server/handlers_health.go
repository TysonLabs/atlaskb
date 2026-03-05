package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tgeorge06/atlaskb/internal/version"
)

type healthResponse struct {
	Status              string `json:"status"`
	Readiness           string `json:"readiness"`
	DBConnected         bool   `json:"db_connected"`
	LLMReachable        bool   `json:"llm_reachable"`
	EmbeddingsReachable bool   `json:"embeddings_reachable"`
	ReposIndexed        int    `json:"repos_indexed"`
	TotalEntities       int    `json:"total_entities"`
	TotalFacts          int    `json:"total_facts"`
	Version             string `json:"version"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{Version: version.Version}

	err := s.pool.QueryRow(r.Context(),
		`SELECT
			(SELECT COUNT(*) FROM repos),
			(SELECT COUNT(*) FROM entities),
			(SELECT COUNT(*) FROM facts)`,
	).Scan(&resp.ReposIndexed, &resp.TotalEntities, &resp.TotalFacts)

	if err != nil {
		resp.Status = "degraded"
		resp.DBConnected = false
		writeJSON(w, http.StatusServiceUnavailable, resp)
		return
	}

	resp.Status = "ok"
	resp.DBConnected = true
	resp.LLMReachable = checkHTTPDependency(r.Context(), s.cfg.LLM.BaseURL+"/v1/models", http.MethodGet, nil)
	if s.cfg.Embeddings.BaseURL == s.cfg.LLM.BaseURL {
		resp.EmbeddingsReachable = resp.LLMReachable
	} else {
		payload := []byte(`{"input":["health"],"model":"` + s.cfg.Embeddings.Model + `"}`)
		resp.EmbeddingsReachable = checkHTTPDependency(r.Context(), s.cfg.Embeddings.BaseURL+"/v1/embeddings", http.MethodPost, payload)
	}
	if resp.LLMReachable && resp.EmbeddingsReachable {
		resp.Readiness = "ready"
	} else {
		resp.Status = "degraded"
		resp.Readiness = "degraded"
	}
	writeJSON(w, http.StatusOK, resp)
}

func checkHTTPDependency(ctx context.Context, url, method string, body []byte) bool {
	if strings.TrimSpace(url) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return false
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}
