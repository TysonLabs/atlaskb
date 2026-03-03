package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(r.URL.Query().Get("repo_id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo_id"))
		return
	}
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		writeError(w, NewBadRequest("path is required"))
		return
	}

	repoStore := &models.RepoStore{Pool: s.pool}
	repo, err := repoStore.GetByID(r.Context(), repoID)
	if err != nil || repo == nil {
		writeError(w, NewNotFound("repo not found"))
		return
	}

	// SECURITY: Resolve and validate that the requested path is within the repo
	absRepo, err := filepath.Abs(repo.LocalPath)
	if err != nil {
		writeError(w, NewInternal("failed to resolve repo path"))
		return
	}
	fullPath := filepath.Join(absRepo, filepath.Clean(filePath))
	if !strings.HasPrefix(fullPath, absRepo) {
		writeError(w, NewBadRequest("path outside repository"))
		return
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		writeError(w, NewNotFound("file not found"))
		return
	}

	// Limit response size to prevent huge files
	if len(content) > 500_000 {
		content = content[:500_000]
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"path":    filePath,
		"content": string(content),
	})
}
