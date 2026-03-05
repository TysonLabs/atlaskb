package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

	submission, err := models.SubmitFactFeedback(r.Context(), s.pool, fact, req.Reason, req.Correction)
	if err != nil {
		writeError(w, NewInternal("submitting feedback: "+err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":             submission.Feedback.ID,
		"status":         submission.Feedback.Status,
		"fact_id":        submission.Feedback.FactID,
		"queued_targets": submission.QueuedTargets,
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
