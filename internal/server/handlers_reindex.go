package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	ghpkg "github.com/tgeorge06/atlaskb/internal/github"
	"github.com/tgeorge06/atlaskb/internal/models"
	"github.com/tgeorge06/atlaskb/internal/pipeline"
)

// In-flight indexing jobs tracked by repo ID.
var (
	indexingMu   sync.Mutex
	indexingJobs = map[uuid.UUID]*indexJob{}
)

type indexJob struct {
	RepoID uuid.UUID
	Status string // "running", "completed", "failed"
	Logs   []string
	mu     sync.Mutex
}

func (j *indexJob) appendLog(msg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Logs = append(j.Logs, msg)
}

type reindexRequest struct {
	Force bool `json:"force"`
}

func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	var req reindexRequest
	json.NewDecoder(r.Body).Decode(&req) // optional body, defaults fine

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

	// Check if already running
	indexingMu.Lock()
	if existing, ok := indexingJobs[id]; ok && existing.Status == "running" {
		indexingMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":  "indexing already in progress",
			"status": "running",
		})
		return
	}

	job := &indexJob{RepoID: id, Status: "running"}
	indexingJobs[id] = job
	indexingMu.Unlock()

	// Run pipeline in background
	go func() {
		ctx := context.Background()
		job.appendLog(fmt.Sprintf("Starting indexing for %s...", repo.Name))

		force := req.Force
		// If repo has never been indexed, always force
		if repo.LastIndexedAt == nil {
			force = true
		}

		var ghClient *ghpkg.Client
		if s.cfg.GitHub.Token != "" {
			ghClient = ghpkg.NewClient(s.cfg.GitHub)
		}

		result, err := pipeline.Orchestrate(ctx, pipeline.OrchestratorConfig{
			RepoPath:          repo.LocalPath,
			Force:             force,
			Concurrency:       s.cfg.Pipeline.Concurrency,
			ExtractionModel:   s.cfg.Pipeline.ExtractionModel,
			SynthesisModel:    s.cfg.Pipeline.SynthesisModel,
			Pool:              s.pool,
			LLM:               s.llm,
			Embedder:          s.embedder,
			Verbose:           false,
			ProgressFunc:      job.appendLog,
			GlobalExcludeDirs: s.cfg.Pipeline.GlobalExcludeDirs,
			GitHubClient:      ghClient,
			GitHubMaxPRs:      s.cfg.GitHub.MaxPRs,
			GitHubPRBatchSize: s.cfg.GitHub.PRBatchSize,
		})

		indexingMu.Lock()
		defer indexingMu.Unlock()

		if err != nil {
			job.Status = "failed"
			job.appendLog(fmt.Sprintf("Indexing failed: %v", err))
			log.Printf("[reindex] %s failed: %v", repo.Name, err)
			return
		}

		job.Status = "completed"
		msg := fmt.Sprintf("Indexing complete in %s", result.Duration.Round(1e9)) // round to seconds
		if result.Phase2Stats != nil {
			msg += fmt.Sprintf(" — %d files, %d entities, %d facts",
				result.Phase2Stats.FilesProcessed, result.Phase2Stats.EntitiesCreated, result.Phase2Stats.FactsCreated)
		}
		job.appendLog(msg)
		log.Printf("[reindex] %s completed in %s", repo.Name, result.Duration)
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "started",
		"message": fmt.Sprintf("Indexing started for %s", repo.Name),
	})
}

func (s *Server) handleReindexStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	indexingMu.Lock()
	job, exists := indexingJobs[id]
	indexingMu.Unlock()

	if !exists {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "idle",
			"logs":   []string{},
		})
		return
	}

	job.mu.Lock()
	status := job.Status
	logs := make([]string, len(job.Logs))
	copy(logs, job.Logs)
	job.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status": status,
		"logs":   logs,
	})
}
