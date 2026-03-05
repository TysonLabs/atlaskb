package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type createFeedbackRequest struct {
	FactID     string  `json:"fact_id"`
	Reason     string  `json:"reason"`
	Correction *string `json:"correction,omitempty"`
}

func (s *Server) handleCreateFeedback(w http.ResponseWriter, r *http.Request) {
	var req createFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, NewBadRequest("invalid request body"))
		return
	}
	if strings.TrimSpace(req.FactID) == "" || strings.TrimSpace(req.Reason) == "" {
		writeError(w, NewBadRequest("fact_id and reason are required"))
		return
	}
	factID, err := uuid.Parse(req.FactID)
	if err != nil {
		writeError(w, NewBadRequest("invalid fact_id"))
		return
	}

	factStore := &models.FactStore{Pool: s.pool}
	fact, err := factStore.GetByID(r.Context(), factID)
	if err != nil {
		writeError(w, NewInternal("loading fact: "+err.Error()))
		return
	}
	if fact == nil {
		writeError(w, NewNotFound("fact not found"))
		return
	}

	fb := &models.FactFeedback{
		FactID:     fact.ID,
		RepoID:     fact.RepoID,
		Reason:     strings.TrimSpace(req.Reason),
		Correction: req.Correction,
		Status:     models.FeedbackPending,
	}
	fbStore := &models.FactFeedbackStore{Pool: s.pool}
	if err := fbStore.Create(r.Context(), fb); err != nil {
		writeError(w, NewInternal("creating feedback: "+err.Error()))
		return
	}
	if err := factStore.UpdateConfidence(r.Context(), fact.ID, models.ConfidenceLow); err != nil {
		writeError(w, NewInternal("lowering fact confidence: "+err.Error()))
		return
	}
	queued := queueReanalysisTargetsServer(r.Context(), s.pool, *fact)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":             fb.ID,
		"status":         fb.Status,
		"fact_id":        fb.FactID,
		"queued_targets": queued,
	})
}

func (s *Server) handleListFeedback(w http.ResponseWriter, r *http.Request) {
	var repoID *uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("repo_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			writeError(w, NewBadRequest("invalid repo_id"))
			return
		}
		repoID = &parsed
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	store := &models.FactFeedbackStore{Pool: s.pool}
	items, err := store.List(r.Context(), repoID, status, 200)
	if err != nil {
		writeError(w, NewInternal("listing feedback: "+err.Error()))
		return
	}
	if items == nil {
		items = []models.FactFeedback{}
	}
	writeJSON(w, http.StatusOK, items)
}

type resolveFeedbackRequest struct {
	Outcome *string `json:"outcome,omitempty"`
}

func (s *Server) handleResolveFeedback(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid feedback id"))
		return
	}
	var req resolveFeedbackRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	store := &models.FactFeedbackStore{Pool: s.pool}
	if existing, err := store.GetByID(r.Context(), id); err != nil {
		writeError(w, NewInternal("loading feedback: "+err.Error()))
		return
	} else if existing == nil {
		writeError(w, NewNotFound("feedback not found"))
		return
	}
	if err := store.Resolve(r.Context(), id, req.Outcome); err != nil {
		writeError(w, NewInternal("resolving feedback: "+err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": models.FeedbackResolved})
}

func queueReanalysisTargetsServer(ctx context.Context, pool *pgxpool.Pool, fact models.Fact) []string {
	jobStore := &models.JobStore{Pool: pool}
	seen := map[string]bool{}
	var queued []string

	for _, prov := range fact.Provenance {
		target := strings.TrimSpace(prov.Ref)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true

		existing, _ := jobStore.GetByTarget(ctx, fact.RepoID, models.PhasePhase2, target)
		if existing != nil {
			_, _ = pool.Exec(ctx,
				`UPDATE extraction_jobs SET status = 'pending', error_message = NULL, started_at = NULL, completed_at = NULL, updated_at = now()
				 WHERE id = $1`, existing.ID)
			queued = append(queued, target)
			continue
		}
		_ = jobStore.Create(ctx, &models.ExtractionJob{
			RepoID: fact.RepoID,
			Phase:  models.PhasePhase2,
			Target: target,
			Status: models.JobPending,
		})
		queued = append(queued, target)
	}
	return queued
}
