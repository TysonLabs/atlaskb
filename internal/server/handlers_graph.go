package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type graphNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
}

type graphEdge struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Target      string `json:"target"`
	Kind        string `json:"kind"`
	Strength    string `json:"strength"`
	Description string `json:"description,omitempty"`
}

type graphResponse struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

func (s *Server) handleRepoGraph(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid repo id"))
		return
	}

	entityKindsParam := r.URL.Query().Get("entity_kinds")
	relKindsParam := r.URL.Query().Get("rel_kinds")

	entityStore := &models.EntityStore{Pool: s.pool}
	entities, err := entityStore.ListByRepo(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("listing entities: "+err.Error()))
		return
	}

	relStore := &models.RelationshipStore{Pool: s.pool}
	rels, err := relStore.ListByRepo(r.Context(), id)
	if err != nil {
		writeError(w, NewInternal("listing relationships: "+err.Error()))
		return
	}

	entityKinds := parseCommaSeparated(entityKindsParam)
	relKinds := parseCommaSeparated(relKindsParam)

	entityMap := make(map[uuid.UUID]models.Entity)
	for _, e := range entities {
		if len(entityKinds) > 0 && !contains(entityKinds, e.Kind) {
			continue
		}
		entityMap[e.ID] = e
	}

	var filteredRels []models.Relationship
	for _, rel := range rels {
		if len(relKinds) > 0 && !contains(relKinds, rel.Kind) {
			continue
		}
		_, fromOk := entityMap[rel.FromEntityID]
		_, toOk := entityMap[rel.ToEntityID]
		if fromOk && toOk {
			filteredRels = append(filteredRels, rel)
		}
	}

	// Build connected node set — only include entities that have relationships
	connectedIDs := make(map[uuid.UUID]bool)
	for _, rel := range filteredRels {
		connectedIDs[rel.FromEntityID] = true
		connectedIDs[rel.ToEntityID] = true
	}

	resp := graphResponse{
		Nodes: make([]graphNode, 0),
		Edges: make([]graphEdge, 0),
	}

	for eid := range connectedIDs {
		e := entityMap[eid]
		node := graphNode{
			ID:   e.ID.String(),
			Name: e.Name,
			Kind: e.Kind,
		}
		if e.Path != nil {
			node.Path = *e.Path
		}
		resp.Nodes = append(resp.Nodes, node)
	}

	for _, rel := range filteredRels {
		edge := graphEdge{
			ID:       rel.ID.String(),
			Source:   rel.FromEntityID.String(),
			Target:   rel.ToEntityID.String(),
			Kind:     rel.Kind,
			Strength: rel.Strength,
		}
		if rel.Description != nil {
			edge.Description = *rel.Description
		}
		resp.Edges = append(resp.Edges, edge)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleEntityGraph(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, NewBadRequest("invalid entity id"))
		return
	}

	depth := 1
	if d, err := strconv.Atoi(r.URL.Query().Get("depth")); err == nil && d > 0 && d <= 3 {
		depth = d
	}

	entityStore := &models.EntityStore{Pool: s.pool}
	relStore := &models.RelationshipStore{Pool: s.pool}

	visited := make(map[uuid.UUID]models.Entity)
	var allRels []models.Relationship
	frontier := []uuid.UUID{id}

	for d := 0; d < depth && len(frontier) > 0; d++ {
		var nextFrontier []uuid.UUID
		for _, eid := range frontier {
			if _, ok := visited[eid]; ok {
				continue
			}
			entity, err := entityStore.GetByID(r.Context(), eid)
			if err != nil || entity == nil {
				continue
			}
			visited[eid] = *entity

			rels, err := relStore.ListByEntity(r.Context(), eid)
			if err != nil {
				continue
			}
			for _, rel := range rels {
				allRels = append(allRels, rel)
				other := rel.ToEntityID
				if rel.ToEntityID == eid {
					other = rel.FromEntityID
				}
				if _, ok := visited[other]; !ok {
					nextFrontier = append(nextFrontier, other)
				}
			}
		}
		frontier = nextFrontier
	}

	// Add frontier entities (not yet visited)
	for _, eid := range frontier {
		if _, ok := visited[eid]; ok {
			continue
		}
		entity, err := entityStore.GetByID(r.Context(), eid)
		if err != nil || entity == nil {
			continue
		}
		visited[eid] = *entity
	}

	resp := graphResponse{
		Nodes: make([]graphNode, 0, len(visited)),
		Edges: make([]graphEdge, 0),
	}

	for _, e := range visited {
		node := graphNode{
			ID:   e.ID.String(),
			Name: e.Name,
			Kind: e.Kind,
		}
		if e.Path != nil {
			node.Path = *e.Path
		}
		resp.Nodes = append(resp.Nodes, node)
	}

	seenEdges := make(map[uuid.UUID]bool)
	for _, rel := range allRels {
		if seenEdges[rel.ID] {
			continue
		}
		if _, ok := visited[rel.FromEntityID]; !ok {
			continue
		}
		if _, ok := visited[rel.ToEntityID]; !ok {
			continue
		}
		seenEdges[rel.ID] = true
		edge := graphEdge{
			ID:       rel.ID.String(),
			Source:   rel.FromEntityID.String(),
			Target:   rel.ToEntityID.String(),
			Kind:     rel.Kind,
			Strength: rel.Strength,
		}
		if rel.Description != nil {
			edge.Description = *rel.Description
		}
		resp.Edges = append(resp.Edges, edge)
	}

	writeJSON(w, http.StatusOK, resp)
}

func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
