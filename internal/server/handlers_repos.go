package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type repoListItem struct {
	models.Repo
	EntityCount      int      `json:"entity_count"`
	FactCount        int      `json:"fact_count"`
	RelCount         int      `json:"relationship_count"`
	DecisionCount    int      `json:"decision_count"`
	QualityOverall   *float64 `json:"quality_overall,omitempty"`
}

func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	repoStore := &models.RepoStore{Pool: s.pool}
	repos, err := repoStore.List(r.Context())
	if err != nil {
		writeError(w, NewInternal("listing repos: "+err.Error()))
		return
	}

	items := make([]repoListItem, 0, len(repos))
	for _, repo := range repos {
		item := repoListItem{Repo: repo}

		entityTotal, _, err := (&models.EntityStore{Pool: s.pool}).CountByRepo(r.Context(), repo.ID)
		if err == nil {
			item.EntityCount = entityTotal
		}
		factTotal, _, err := (&models.FactStore{Pool: s.pool}).CountByRepo(r.Context(), repo.ID)
		if err == nil {
			item.FactCount = factTotal
		}
		relCount, err := (&models.RelationshipStore{Pool: s.pool}).CountByRepo(r.Context(), repo.ID)
		if err == nil {
			item.RelCount = relCount
		}
		decCount, err := (&models.DecisionStore{Pool: s.pool}).CountByRepo(r.Context(), repo.ID)
		if err == nil {
			item.DecisionCount = decCount
		}

		runStore := &models.IndexingRunStore{Pool: s.pool}
		latest, err := runStore.GetLatest(r.Context(), repo.ID)
		if err == nil && latest != nil {
			item.QualityOverall = latest.QualityOverall
		}

		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, items)
}

type repoDetailResponse struct {
	models.Repo
	EntityCount      int            `json:"entity_count"`
	EntityByKind     map[string]int `json:"entity_by_kind"`
	FactCount        int            `json:"fact_count"`
	FactByDimension  map[string]int `json:"fact_by_dimension"`
	RelCount         int            `json:"relationship_count"`
	DecisionCount    int            `json:"decision_count"`
	QualityOverall   *float64       `json:"quality_overall,omitempty"`
	QualityEntityCov *float64       `json:"quality_entity_cov,omitempty"`
	QualityFactDens  *float64       `json:"quality_fact_density,omitempty"`
	QualityRelConn   *float64       `json:"quality_rel_connect,omitempty"`
	QualityDimCov    *float64       `json:"quality_dim_coverage,omitempty"`
	QualityParseRate *float64       `json:"quality_parse_rate,omitempty"`
}

func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	repoStore := &models.RepoStore{Pool: s.pool}
	repo, err := repoStore.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("querying repo: "+err.Error()))
		return
	}
	if repo == nil {
		writeError(w, NewNotFound("repo not found"))
		return
	}

	resp := repoDetailResponse{Repo: *repo}

	entityTotal, byKind, err := (&models.EntityStore{Pool: s.pool}).CountByRepo(r.Context(), id)
	if err == nil {
		resp.EntityCount = entityTotal
		resp.EntityByKind = byKind
	}
	factTotal, byDim, err := (&models.FactStore{Pool: s.pool}).CountByRepo(r.Context(), id)
	if err == nil {
		resp.FactCount = factTotal
		resp.FactByDimension = byDim
	}
	relCount, err := (&models.RelationshipStore{Pool: s.pool}).CountByRepo(r.Context(), id)
	if err == nil {
		resp.RelCount = relCount
	}
	decCount, err := (&models.DecisionStore{Pool: s.pool}).CountByRepo(r.Context(), id)
	if err == nil {
		resp.DecisionCount = decCount
	}

	runStore := &models.IndexingRunStore{Pool: s.pool}
	latest, err := runStore.GetLatest(r.Context(), id)
	if err == nil && latest != nil {
		resp.QualityOverall = latest.QualityOverall
		resp.QualityEntityCov = latest.QualityEntityCov
		resp.QualityFactDens = latest.QualityFactDensity
		resp.QualityRelConn = latest.QualityRelConnect
		resp.QualityDimCov = latest.QualityDimCoverage
		resp.QualityParseRate = latest.QualityParseRate
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRepoIndexingRuns(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	runStore := &models.IndexingRunStore{Pool: s.pool}
	runs, err := runStore.ListByRepo(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("listing runs: "+err.Error()))
		return
	}
	if runs == nil {
		runs = []models.IndexingRun{}
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleRepoDecisions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	decStore := &models.DecisionStore{Pool: s.pool}
	decs, err := decStore.ListByRepo(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("listing decisions: "+err.Error()))
		return
	}
	if decs == nil {
		decs = []models.Decision{}
	}
	writeJSON(w, http.StatusOK, decs)
}

type createRepoRequest struct {
	Name          string   `json:"name"`
	LocalPath     string   `json:"local_path"`
	DefaultBranch string   `json:"default_branch"`
	ExcludeDirs   []string `json:"exclude_dirs"`
}

func (s *Server) handleCreateRepo(w http.ResponseWriter, r *http.Request) {
	var req createRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, NewBadRequest("invalid request body"))
		return
	}

	if req.Name == "" || req.LocalPath == "" {
		writeError(w, NewBadRequest("name and local_path are required"))
		return
	}

	// Resolve and validate path
	absPath, err := filepath.Abs(req.LocalPath)
	if err != nil {
		writeError(w, NewBadRequest("invalid path: "+err.Error()))
		return
	}
	info, err := os.Stat(absPath)
	if err != nil || !info.IsDir() {
		writeError(w, NewBadRequest("path does not exist or is not a directory"))
		return
	}
	// Check it's a git repo
	gitDir := filepath.Join(absPath, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		writeError(w, NewBadRequest("path is not a git repository"))
		return
	}

	// Check for duplicate path
	repoStore := &models.RepoStore{Pool: s.pool}
	existing, _ := repoStore.GetByPath(r.Context(), absPath)
	if existing != nil {
		writeError(w, NewBadRequest("a repo with this path already exists"))
		return
	}

	branch := req.DefaultBranch
	if branch == "" {
		branch = "main"
	}

	repo := &models.Repo{
		Name:          req.Name,
		LocalPath:     absPath,
		DefaultBranch: branch,
		ExcludeDirs:   req.ExcludeDirs,
	}
	if err := repoStore.Create(r.Context(), repo); err != nil {
		writeError(w, NewInternal("creating repo: "+err.Error()))
		return
	}

	writeJSON(w, http.StatusCreated, repo)
}

type updateRepoRequest struct {
	Name        *string  `json:"name"`
	ExcludeDirs []string `json:"exclude_dirs"`
}

type updateRepoResponse struct {
	models.Repo
	ReindexRequired bool `json:"reindex_required"`
}

func (s *Server) handleUpdateRepo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	var req updateRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, NewBadRequest("invalid request body"))
		return
	}

	repoStore := &models.RepoStore{Pool: s.pool}
	repo, err := repoStore.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("querying repo: "+err.Error()))
		return
	}
	if repo == nil {
		writeError(w, NewNotFound("repo not found"))
		return
	}

	// Detect if exclude_dirs changed (triggers reindex warning)
	reindexRequired := false
	oldDirs := repo.ExcludeDirs

	if req.Name != nil {
		repo.Name = *req.Name
	}
	if req.ExcludeDirs != nil {
		repo.ExcludeDirs = req.ExcludeDirs
	}

	if repo.LastIndexedAt != nil && !stringSliceEqual(oldDirs, repo.ExcludeDirs) {
		reindexRequired = true
	}

	if err := repoStore.Update(r.Context(), repo); err != nil {
		writeError(w, NewInternal("updating repo: "+err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, updateRepoResponse{
		Repo:            *repo,
		ReindexRequired: reindexRequired,
	})
}

func (s *Server) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	repoStore := &models.RepoStore{Pool: s.pool}
	repo, err := repoStore.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("querying repo: "+err.Error()))
		return
	}
	if repo == nil {
		writeError(w, NewNotFound("repo not found"))
		return
	}

	// FK constraints have ON DELETE CASCADE, so this removes all related data
	if err := repoStore.Delete(r.Context(), id); err != nil {
		writeError(w, NewInternal("deleting repo: "+err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
