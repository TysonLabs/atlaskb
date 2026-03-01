package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func (s *Server) handleListCrossRepoLinks(w http.ResponseWriter, r *http.Request) {
	relStore := &models.RelationshipStore{Pool: s.pool}

	repoIDParam := r.URL.Query().Get("repo_id")
	var rels []models.CrossRepoRelationship
	var err error

	if repoIDParam != "" {
		repoID, parseErr := uuid.Parse(repoIDParam)
		if parseErr != nil {
			writeError(w, NewBadRequest("invalid repo_id"))
			return
		}
		rels, err = relStore.ListCrossRepoByRepo(r.Context(), repoID)
	} else {
		rels, err = relStore.ListAllCrossRepo(r.Context())
	}

	if err != nil {
		writeError(w, NewInternal("listing cross-repo links: "+err.Error()))
		return
	}
	if rels == nil {
		rels = []models.CrossRepoRelationship{}
	}
	writeJSON(w, http.StatusOK, rels)
}

func (s *Server) handleGetCrossRepoLink(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid id"))
		return
	}

	relStore := &models.RelationshipStore{Pool: s.pool}
	rel, err := relStore.GetCrossRepoByID(r.Context(), id)
	if err != nil {
		writeError(w, NewNotFound("cross-repo link not found"))
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

type createCrossRepoLinkRequest struct {
	FromEntityID string  `json:"from_entity_id"`
	ToEntityID   string  `json:"to_entity_id"`
	Kind         string  `json:"kind"`
	Strength     string  `json:"strength"`
	Description  *string `json:"description"`
}

func (s *Server) handleCreateCrossRepoLink(w http.ResponseWriter, r *http.Request) {
	var req createCrossRepoLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, NewBadRequest("invalid request body"))
		return
	}

	fromEntityID, err := uuid.Parse(req.FromEntityID)
	if err != nil {
		writeError(w, NewBadRequest("invalid from_entity_id"))
		return
	}
	toEntityID, err := uuid.Parse(req.ToEntityID)
	if err != nil {
		writeError(w, NewBadRequest("invalid to_entity_id"))
		return
	}

	if req.Kind == "" {
		writeError(w, NewBadRequest("kind is required"))
		return
	}
	if req.Strength == "" {
		req.Strength = models.StrengthModerate
	}

	// Resolve repo IDs from entities
	entityStore := &models.EntityStore{Pool: s.pool}
	fromEntity, err := entityStore.GetByID(r.Context(), fromEntityID)
	if err != nil || fromEntity == nil {
		writeError(w, NewNotFound("from entity not found"))
		return
	}
	toEntity, err := entityStore.GetByID(r.Context(), toEntityID)
	if err != nil || toEntity == nil {
		writeError(w, NewNotFound("to entity not found"))
		return
	}

	cr := &models.CrossRepoRelationship{
		FromEntityID: fromEntityID,
		ToEntityID:   toEntityID,
		FromRepoID:   fromEntity.RepoID,
		ToRepoID:     toEntity.RepoID,
		Kind:         req.Kind,
		Strength:     req.Strength,
		Description:  req.Description,
		Provenance: []models.Provenance{{
			SourceType: "manual",
		}},
	}

	relStore := &models.RelationshipStore{Pool: s.pool}
	if err := relStore.UpsertCrossRepo(r.Context(), cr); err != nil {
		writeError(w, NewInternal("creating cross-repo link: "+err.Error()))
		return
	}

	writeJSON(w, http.StatusCreated, cr)
}

func (s *Server) handleDeleteCrossRepoLink(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid id"))
		return
	}

	relStore := &models.RelationshipStore{Pool: s.pool}
	if err := relStore.DeleteCrossRepo(r.Context(), id); err != nil {
		writeError(w, NewInternal("deleting cross-repo link: "+err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
