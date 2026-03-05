package server

import (
	"net/http"

	"github.com/tgeorge06/atlaskb/internal/telemetry"
)

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, telemetry.SnapshotMetrics())
}
