package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type clusterResponse struct {
	models.Entity
	Members []models.Entity `json:"members"`
}

func (s *Server) handleRepoClusters(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	entityStore := &models.EntityStore{Pool: s.pool}
	clusters, err := entityStore.ListByRepoAndKind(r.Context(), id, "cluster")
	if err != nil {
		writeError(w, NewInternal("listing clusters: "+err.Error()))
		return
	}

	relStore := &models.RelationshipStore{Pool: s.pool}

	// Batch fetch: one query for ALL relationships in this repo
	allRels, err := relStore.ListByRepo(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("listing relationships: "+err.Error()))
		return
	}

	// Build a set of cluster IDs for fast lookup
	clusterIDs := make(map[uuid.UUID]struct{}, len(clusters))
	for _, c := range clusters {
		clusterIDs[c.ID] = struct{}{}
	}

	// Filter to member_of relationships targeting a cluster, group by cluster ID
	clusterMembers := make(map[uuid.UUID][]uuid.UUID)
	var allMemberIDs []uuid.UUID
	seen := make(map[uuid.UUID]struct{})
	for _, rel := range allRels {
		if rel.Kind != "member_of" {
			continue
		}
		if _, ok := clusterIDs[rel.ToEntityID]; !ok {
			continue
		}
		clusterMembers[rel.ToEntityID] = append(clusterMembers[rel.ToEntityID], rel.FromEntityID)
		if _, ok := seen[rel.FromEntityID]; !ok {
			seen[rel.FromEntityID] = struct{}{}
			allMemberIDs = append(allMemberIDs, rel.FromEntityID)
		}
	}

	// One batch fetch for ALL member entities
	memberMap := make(map[uuid.UUID]models.Entity)
	if len(allMemberIDs) > 0 {
		members, err := entityStore.GetByIDs(r.Context(), allMemberIDs)
		if err != nil {
			writeError(w, NewInternal("fetching member entities: "+err.Error()))
			return
		}
		for _, m := range members {
			memberMap[m.ID] = m
		}
	}

	// Assemble response
	result := make([]clusterResponse, 0, len(clusters))
	for _, cluster := range clusters {
		resp := clusterResponse{Entity: cluster, Members: []models.Entity{}}
		for _, mid := range clusterMembers[cluster.ID] {
			if m, ok := memberMap[mid]; ok {
				resp.Members = append(resp.Members, m)
			}
		}
		result = append(result, resp)
	}

	writeJSON(w, http.StatusOK, result)
}

type flowResponse struct {
	models.ExecutionFlow
	EntryEntity *models.Entity `json:"entry_entity,omitempty"`
}

func (s *Server) handleRepoFlows(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	flowStore := &models.FlowStore{Pool: s.pool}
	flows, err := flowStore.ListByRepo(r.Context(), id, 50)
	if err != nil {
		writeError(w, NewInternal("listing flows: "+err.Error()))
		return
	}
	if flows == nil {
		flows = []models.ExecutionFlow{}
	}

	// Collect entry entity IDs for batch fetch (deduplicated)
	var entryIDs []uuid.UUID
	seen := make(map[uuid.UUID]struct{})
	for _, f := range flows {
		if _, ok := seen[f.EntryEntityID]; !ok {
			seen[f.EntryEntityID] = struct{}{}
			entryIDs = append(entryIDs, f.EntryEntityID)
		}
	}

	entityStore := &models.EntityStore{Pool: s.pool}
	entryMap := make(map[uuid.UUID]*models.Entity)
	if len(entryIDs) > 0 {
		entities, err := entityStore.GetByIDs(r.Context(), entryIDs)
		if err != nil {
			writeError(w, NewInternal("fetching entry entities: "+err.Error()))
			return
		}
		for i := range entities {
			entryMap[entities[i].ID] = &entities[i]
		}
	}

	result := make([]flowResponse, 0, len(flows))
	for _, f := range flows {
		resp := flowResponse{ExecutionFlow: f}
		if e, ok := entryMap[f.EntryEntityID]; ok {
			resp.EntryEntity = e
		}
		result = append(result, resp)
	}

	writeJSON(w, http.StatusOK, result)
}
