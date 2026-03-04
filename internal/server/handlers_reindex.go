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
	RepoID  uuid.UUID
	Status  string // "running", "completed", "failed"
	Logs    []string
	IsBatch bool
	mu      sync.Mutex
}

func (j *indexJob) appendLog(msg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Logs = append(j.Logs, msg)
}

// Batch reindex state
var (
	batchMu    sync.Mutex
	activeBatch *batchState
)

type batchRepoStatus struct {
	RepoID   uuid.UUID `json:"repo_id"`
	RepoName string    `json:"repo_name"`
	Status   string    `json:"status"` // "pending", "running", "completed", "failed"
	Logs     []string  `json:"logs"`
}

type batchState struct {
	ID        string                      `json:"id"`
	RepoIDs   []uuid.UUID                 `json:"-"`
	Repos     map[uuid.UUID]*batchRepoStatus `json:"-"`
	Order     []uuid.UUID                 `json:"-"`
	Current   int                         `json:"current_index"`
	Force     bool                        `json:"force"`
	Done      bool                        `json:"done"`
	cancel    context.CancelFunc
	mu        sync.Mutex
}

type reindexRequest struct {
	Force       bool     `json:"force"`
	Phases      []string `json:"phases,omitempty"`
	Concurrency int      `json:"concurrency,omitempty"`
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

	// Check if batch is running for this repo
	batchMu.Lock()
	if activeBatch != nil && !activeBatch.Done {
		if _, inBatch := activeBatch.Repos[id]; inBatch {
			batchMu.Unlock()
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":  "repo is part of an active batch reindex",
				"status": "running",
			})
			return
		}
	}
	batchMu.Unlock()

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

		concurrency := s.cfg.Pipeline.Concurrency
		if req.Concurrency > 0 {
			concurrency = req.Concurrency
		}

		result, err := pipeline.Orchestrate(ctx, pipeline.OrchestratorConfig{
			RepoPath:          repo.LocalPath,
			Force:             force,
			Concurrency:       concurrency,
			ExtractionModel:   s.cfg.Pipeline.ExtractionModel,
			SynthesisModel:    s.cfg.Pipeline.SynthesisModel,
			Pool:              s.pool,
			LLM:               s.llm,
			Embedder:          s.embedder,
			Verbose:           false,
			GitLogLimit:       s.cfg.Pipeline.GitLogLimit,
			Phases:            req.Phases,
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

// --- Batch reindex endpoints ---

type batchReindexRequest struct {
	All     bool        `json:"all"`
	RepoIDs []uuid.UUID `json:"repo_ids"`
	Force   bool        `json:"force"`
}

func (s *Server) handleBatchReindex(w http.ResponseWriter, r *http.Request) {
	var req batchReindexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, NewBadRequest("invalid request body"))
		return
	}

	batchMu.Lock()
	if activeBatch != nil && !activeBatch.Done {
		batchMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "a batch reindex is already running",
		})
		return
	}
	batchMu.Unlock()

	repoStore := &models.RepoStore{Pool: s.pool}
	repos, err := repoStore.List(r.Context())
	if err != nil {
		writeError(w, NewInternal("listing repos: "+err.Error()))
		return
	}

	// Filter to requested repos or all
	var targets []models.Repo
	if req.All {
		targets = repos
	} else {
		idSet := map[uuid.UUID]bool{}
		for _, rid := range req.RepoIDs {
			idSet[rid] = true
		}
		for _, repo := range repos {
			if idSet[repo.ID] {
				targets = append(targets, repo)
			}
		}
	}

	if len(targets) == 0 {
		writeError(w, NewBadRequest("no repos to reindex"))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	batch := &batchState{
		ID:      uuid.New().String(),
		Force:   req.Force,
		Repos:   make(map[uuid.UUID]*batchRepoStatus, len(targets)),
		cancel:  cancel,
	}

	for _, repo := range targets {
		batch.RepoIDs = append(batch.RepoIDs, repo.ID)
		batch.Order = append(batch.Order, repo.ID)
		batch.Repos[repo.ID] = &batchRepoStatus{
			RepoID:   repo.ID,
			RepoName: repo.Name,
			Status:   "pending",
			Logs:     []string{},
		}
	}

	batchMu.Lock()
	activeBatch = batch
	batchMu.Unlock()

	go s.runBatch(ctx, batch, targets)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":   "started",
		"batch_id": batch.ID,
		"message":  fmt.Sprintf("Batch reindex started for %d repos", len(targets)),
	})
}

func (s *Server) runBatch(ctx context.Context, batch *batchState, repos []models.Repo) {
	for i, repo := range repos {
		if ctx.Err() != nil {
			// Cancelled — mark remaining as failed
			batch.mu.Lock()
			for j := i; j < len(repos); j++ {
				if rs, ok := batch.Repos[repos[j].ID]; ok && rs.Status == "pending" {
					rs.Status = "failed"
					rs.Logs = append(rs.Logs, "Cancelled")
				}
			}
			batch.mu.Unlock()
			break
		}

		batch.mu.Lock()
		batch.Current = i
		rs := batch.Repos[repo.ID]
		rs.Status = "running"
		rs.Logs = append(rs.Logs, fmt.Sprintf("Starting indexing for %s...", repo.Name))
		batch.mu.Unlock()

		// Also register in indexingJobs so per-repo status polling works
		job := &indexJob{RepoID: repo.ID, Status: "running", IsBatch: true}
		indexingMu.Lock()
		indexingJobs[repo.ID] = job
		indexingMu.Unlock()

		progressFn := func(msg string) {
			job.appendLog(msg)
			batch.mu.Lock()
			rs.Logs = append(rs.Logs, msg)
			batch.mu.Unlock()
		}

		progressFn(fmt.Sprintf("Starting indexing for %s...", repo.Name))

		force := batch.Force
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
			GitLogLimit:       s.cfg.Pipeline.GitLogLimit,
			ProgressFunc:      progressFn,
			GlobalExcludeDirs: s.cfg.Pipeline.GlobalExcludeDirs,
			GitHubClient:      ghClient,
			GitHubMaxPRs:      s.cfg.GitHub.MaxPRs,
			GitHubPRBatchSize: s.cfg.GitHub.PRBatchSize,
		})

		indexingMu.Lock()
		if err != nil {
			job.Status = "failed"
			job.appendLog(fmt.Sprintf("Indexing failed: %v", err))
			batch.mu.Lock()
			rs.Status = "failed"
			rs.Logs = append(rs.Logs, fmt.Sprintf("Indexing failed: %v", err))
			batch.mu.Unlock()
			log.Printf("[batch-reindex] %s failed: %v", repo.Name, err)
		} else {
			job.Status = "completed"
			msg := fmt.Sprintf("Indexing complete in %s", result.Duration.Round(1e9))
			if result.Phase2Stats != nil {
				msg += fmt.Sprintf(" — %d files, %d entities, %d facts",
					result.Phase2Stats.FilesProcessed, result.Phase2Stats.EntitiesCreated, result.Phase2Stats.FactsCreated)
			}
			job.appendLog(msg)
			batch.mu.Lock()
			rs.Status = "completed"
			rs.Logs = append(rs.Logs, msg)
			batch.mu.Unlock()
			log.Printf("[batch-reindex] %s completed in %s", repo.Name, result.Duration)
		}
		indexingMu.Unlock()
	}

	batch.mu.Lock()
	batch.Done = true
	batch.mu.Unlock()
	log.Printf("[batch-reindex] batch %s complete", batch.ID)
}

func (s *Server) handleBatchStatus(w http.ResponseWriter, r *http.Request) {
	batchMu.Lock()
	batch := activeBatch
	batchMu.Unlock()

	if batch == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"active": false,
		})
		return
	}

	batch.mu.Lock()
	defer batch.mu.Unlock()

	completed := 0
	failed := 0
	repoStatuses := make([]batchRepoStatus, 0, len(batch.Order))
	for _, rid := range batch.Order {
		rs := batch.Repos[rid]
		repoStatuses = append(repoStatuses, *rs)
		switch rs.Status {
		case "completed":
			completed++
		case "failed":
			failed++
		}
	}

	// For the currently running repo, include its logs from indexingJobs
	// (which has more up-to-date progress from ProgressFunc)
	if !batch.Done && batch.Current < len(batch.Order) {
		currentID := batch.Order[batch.Current]
		indexingMu.Lock()
		if job, ok := indexingJobs[currentID]; ok {
			job.mu.Lock()
			repoStatuses[batch.Current].Logs = make([]string, len(job.Logs))
			copy(repoStatuses[batch.Current].Logs, job.Logs)
			job.mu.Unlock()
		}
		indexingMu.Unlock()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"active":        !batch.Done,
		"id":            batch.ID,
		"total":         len(batch.Order),
		"completed":     completed,
		"failed":        failed,
		"current_index": batch.Current,
		"force":         batch.Force,
		"repos":         repoStatuses,
	})
}

func (s *Server) handleBatchCancel(w http.ResponseWriter, r *http.Request) {
	batchMu.Lock()
	batch := activeBatch
	batchMu.Unlock()

	if batch == nil || batch.Done {
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "no active batch",
		})
		return
	}

	batch.cancel()
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "cancelling",
	})
}

func (s *Server) handleListIndexingJobs(w http.ResponseWriter, r *http.Request) {
	indexingMu.Lock()
	defer indexingMu.Unlock()

	type jobSummary struct {
		RepoID    uuid.UUID `json:"repo_id"`
		RepoName  string    `json:"repo_name"`
		Status    string    `json:"status"`
		LatestLog string    `json:"latest_log"`
		IsBatch   bool      `json:"is_batch"`
	}

	// Resolve repo names
	repoStore := &models.RepoStore{Pool: s.pool}
	repos, _ := repoStore.List(r.Context())
	nameMap := map[uuid.UUID]string{}
	for _, repo := range repos {
		nameMap[repo.ID] = repo.Name
	}

	jobs := make([]jobSummary, 0)
	for id, job := range indexingJobs {
		job.mu.Lock()
		latest := ""
		if len(job.Logs) > 0 {
			latest = job.Logs[len(job.Logs)-1]
		}
		jobs = append(jobs, jobSummary{
			RepoID:    id,
			RepoName:  nameMap[id],
			Status:    job.Status,
			LatestLog: latest,
			IsBatch:   job.IsBatch,
		})
		job.mu.Unlock()
	}

	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleIndexingHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(),
		`SELECT ir.id, ir.repo_id, ir.commit_sha, ir.mode, ir.model_extraction, ir.model_synthesis, ir.concurrency,
			ir.files_total, ir.files_analyzed, ir.files_skipped,
			ir.entities_created, ir.facts_created, ir.rels_created, ir.decisions_created,
			ir.orphan_entities, ir.backfill_facts, ir.backfill_rels,
			ir.total_tokens, ir.total_cost_usd,
			ir.quality_overall, ir.quality_entity_cov, ir.quality_fact_density,
			ir.quality_rel_connect, ir.quality_dim_coverage, ir.quality_parse_rate,
			ir.duration_ms, ir.started_at, ir.completed_at, ir.created_at,
			r.name
		 FROM indexing_runs ir
		 JOIN repos r ON r.id = ir.repo_id
		 ORDER BY ir.created_at DESC
		 LIMIT 50`)
	if err != nil {
		writeError(w, NewInternal("querying indexing history: "+err.Error()))
		return
	}
	defer rows.Close()

	var runs []recentRunResponse
	for rows.Next() {
		var rr recentRunResponse
		if err := rows.Scan(
			&rr.ID, &rr.RepoID, &rr.CommitSHA, &rr.Mode, &rr.ModelExtraction, &rr.ModelSynthesis, &rr.Concurrency,
			&rr.FilesTotal, &rr.FilesAnalyzed, &rr.FilesSkipped,
			&rr.EntitiesCreated, &rr.FactsCreated, &rr.RelsCreated, &rr.DecisionsCreated,
			&rr.OrphanEntities, &rr.BackfillFacts, &rr.BackfillRels,
			&rr.TotalTokens, &rr.TotalCostUSD,
			&rr.QualityOverall, &rr.QualityEntityCov, &rr.QualityFactDensity,
			&rr.QualityRelConnect, &rr.QualityDimCoverage, &rr.QualityParseRate,
			&rr.DurationMS, &rr.StartedAt, &rr.CompletedAt, &rr.CreatedAt,
			&rr.RepoName,
		); err != nil {
			writeError(w, NewInternal("scanning run: "+err.Error()))
			return
		}
		runs = append(runs, rr)
	}
	if runs == nil {
		runs = []recentRunResponse{}
	}
	writeJSON(w, http.StatusOK, runs)
}
