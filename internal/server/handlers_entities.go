package server

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func (s *Server) handleListEntities(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var repoID *uuid.UUID
	if rid := q.Get("repo_id"); rid != "" {
		id, err := uuid.Parse(rid)
		if err != nil {
			writeError(w, NewBadRequest("invalid repo_id"))
			return
		}
		repoID = &id
	}

	store := &models.EntityStore{Pool: s.pool}
	result, err := store.SearchByName(r.Context(), repoID, q.Get("q"), q.Get("kind"), limit, offset)
	if err != nil {
		writeError(w, NewInternal("searching entities: "+err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetEntity(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid entity id"))
		return
	}

	store := &models.EntityStore{Pool: s.pool}
	entity, err := store.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("querying entity: "+err.Error()))
		return
	}
	if entity == nil {
		writeError(w, NewNotFound("entity not found"))
		return
	}

	factStore := &models.FactStore{Pool: s.pool}
	facts, _ := factStore.ListByEntity(r.Context(), id)
	if facts == nil {
		facts = []models.Fact{}
	}

	relStore := &models.RelationshipStore{Pool: s.pool}
	rels, _ := relStore.ListByEntity(r.Context(), id)

	type relWithNames struct {
		models.Relationship
		FromEntityName string `json:"from_entity_name"`
		ToEntityName   string `json:"to_entity_name"`
	}
	resolvedRels := make([]relWithNames, 0, len(rels))
	for _, rel := range rels {
		rn := relWithNames{Relationship: rel}
		if from, err := store.GetByID(r.Context(), rel.FromEntityID); err == nil && from != nil {
			rn.FromEntityName = from.QualifiedName
		}
		if to, err := store.GetByID(r.Context(), rel.ToEntityID); err == nil && to != nil {
			rn.ToEntityName = to.QualifiedName
		}
		resolvedRels = append(resolvedRels, rn)
	}

	resp := struct {
		*models.Entity
		Facts         []models.Fact  `json:"facts"`
		Relationships []relWithNames `json:"relationships"`
	}{
		Entity:        entity,
		Facts:         facts,
		Relationships: resolvedRels,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleEntityFacts(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid entity id"))
		return
	}

	store := &models.FactStore{Pool: s.pool}
	facts, err := store.ListByEntity(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("listing facts: "+err.Error()))
		return
	}
	if facts == nil {
		facts = []models.Fact{}
	}
	writeJSON(w, http.StatusOK, facts)
}

func (s *Server) handleEntityRelationships(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid entity id"))
		return
	}

	relStore := &models.RelationshipStore{Pool: s.pool}
	rels, err := relStore.ListByEntity(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("listing relationships: "+err.Error()))
		return
	}

	entityStore := &models.EntityStore{Pool: s.pool}
	type relWithNames struct {
		models.Relationship
		FromEntityName string `json:"from_entity_name"`
		ToEntityName   string `json:"to_entity_name"`
	}

	resolved := make([]relWithNames, 0, len(rels))
	for _, rel := range rels {
		rn := relWithNames{Relationship: rel}
		if from, err := entityStore.GetByID(r.Context(), rel.FromEntityID); err == nil && from != nil {
			rn.FromEntityName = from.QualifiedName
		}
		if to, err := entityStore.GetByID(r.Context(), rel.ToEntityID); err == nil && to != nil {
			rn.ToEntityName = to.QualifiedName
		}
		resolved = append(resolved, rn)
	}
	writeJSON(w, http.StatusOK, resolved)
}

func (s *Server) handleEntityDecisions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid entity id"))
		return
	}

	store := &models.DecisionStore{Pool: s.pool}
	decs, err := store.ListByEntity(r.Context(), id, 50)
	if err != nil {
		writeError(w, NewInternal("listing decisions: "+err.Error()))
		return
	}
	if decs == nil {
		decs = []models.Decision{}
	}
	writeJSON(w, http.StatusOK, decs)
}
