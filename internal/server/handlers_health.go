package server

import (
	"net/http"

	"github.com/tgeorge06/atlaskb/internal/version"
)

type healthResponse struct {
	Status        string `json:"status"`
	DBConnected   bool   `json:"db_connected"`
	ReposIndexed  int    `json:"repos_indexed"`
	TotalEntities int    `json:"total_entities"`
	TotalFacts    int    `json:"total_facts"`
	Version       string `json:"version"`
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
	writeJSON(w, http.StatusOK, resp)
}
