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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Path     string `json:"path,omitempty"`
	RepoID   string `json:"repoId,omitempty"`
	RepoName string `json:"repoName,omitempty"`
}

type graphEdge struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Target      string `json:"target"`
	Kind        string `json:"kind"`
	Strength    string `json:"strength"`
	Description string `json:"description,omitempty"`
	CrossRepo   bool   `json:"crossRepo,omitempty"`
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

	// Optionally include cross-repo edges
	if r.URL.Query().Get("include_cross_repo") == "true" {
		crossRels, err := relStore.ListCrossRepoByRepo(r.Context(), id)
		if err == nil {
			repoStore := &models.RepoStore{Pool: s.pool}
			for _, cr := range crossRels {
				edge := graphEdge{
					ID:        cr.ID.String(),
					Source:    cr.FromEntityID.String(),
					Target:    cr.ToEntityID.String(),
					Kind:      cr.Kind,
					Strength:  cr.Strength,
					CrossRepo: true,
				}
				if cr.Description != nil {
					edge.Description = *cr.Description
				}
				resp.Edges = append(resp.Edges, edge)

				// Add external entity nodes if not already present
				for _, eid := range []uuid.UUID{cr.FromEntityID, cr.ToEntityID} {
					if connectedIDs[eid] {
						continue
					}
					connectedIDs[eid] = true
					ext, err := entityStore.GetByID(r.Context(), eid)
					if err != nil || ext == nil {
						continue
					}
					node := graphNode{
						ID:     ext.ID.String(),
						Name:   ext.Name,
						Kind:   ext.Kind,
						RepoID: ext.RepoID.String(),
					}
					if ext.Path != nil {
						node.Path = *ext.Path
					}
					// Resolve repo name
					repo, _ := repoStore.GetByID(r.Context(), ext.RepoID)
					if repo != nil {
						node.RepoName = repo.Name
					}
					resp.Nodes = append(resp.Nodes, node)
				}
			}
		}
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

func (s *Server) handleMultiRepoGraph(w http.ResponseWriter, r *http.Request) {
	repoIDsParam := r.URL.Query().Get("repo_ids")
	if repoIDsParam == "" {
		writeError(w, NewBadRequest("repo_ids is required"))
		return
	}

	idStrs := parseCommaSeparated(repoIDsParam)
	var repoIDs []uuid.UUID
	for _, idStr := range idStrs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			writeError(w, NewBadRequest("invalid repo_id: "+idStr))
			return
		}
		repoIDs = append(repoIDs, id)
	}

	entityStore := &models.EntityStore{Pool: s.pool}
	relStore := &models.RelationshipStore{Pool: s.pool}
	repoStore := &models.RepoStore{Pool: s.pool}

	// Build repo name lookup
	repoNames := make(map[uuid.UUID]string)
	repoIDSet := make(map[uuid.UUID]bool)
	for _, rid := range repoIDs {
		repoIDSet[rid] = true
		repo, err := repoStore.GetByID(r.Context(), rid)
		if err == nil && repo != nil {
			repoNames[rid] = repo.Name
		}
	}

	resp := graphResponse{
		Nodes: make([]graphNode, 0),
		Edges: make([]graphEdge, 0),
	}

	allEntityIDs := make(map[uuid.UUID]bool)
	entityMap := make(map[uuid.UUID]models.Entity)

	// Collect entities and intra-repo edges for each repo
	for _, rid := range repoIDs {
		entities, err := entityStore.ListByRepo(r.Context(), rid)
		if err != nil {
			continue
		}
		for _, e := range entities {
			entityMap[e.ID] = e
			allEntityIDs[e.ID] = true
		}

		rels, err := relStore.ListByRepo(r.Context(), rid)
		if err != nil {
			continue
		}
		for _, rel := range rels {
			if !allEntityIDs[rel.FromEntityID] || !allEntityIDs[rel.ToEntityID] {
				continue
			}
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
	}

	// Add cross-repo edges between selected repos
	for _, rid := range repoIDs {
		crossRels, err := relStore.ListCrossRepoByRepo(r.Context(), rid)
		if err != nil {
			continue
		}
		for _, cr := range crossRels {
			// Only include if both repos are in the selected set
			if !repoIDSet[cr.FromRepoID] || !repoIDSet[cr.ToRepoID] {
				continue
			}
			edge := graphEdge{
				ID:        cr.ID.String(),
				Source:    cr.FromEntityID.String(),
				Target:    cr.ToEntityID.String(),
				Kind:      cr.Kind,
				Strength:  cr.Strength,
				CrossRepo: true,
			}
			if cr.Description != nil {
				edge.Description = *cr.Description
			}
			resp.Edges = append(resp.Edges, edge)
			allEntityIDs[cr.FromEntityID] = true
			allEntityIDs[cr.ToEntityID] = true

			// Ensure external entities are in the map
			for _, eid := range []uuid.UUID{cr.FromEntityID, cr.ToEntityID} {
				if _, ok := entityMap[eid]; !ok {
					ext, err := entityStore.GetByID(r.Context(), eid)
					if err == nil && ext != nil {
						entityMap[eid] = *ext
					}
				}
			}
		}
	}

	// Build connected node set
	connectedIDs := make(map[uuid.UUID]bool)
	for _, edge := range resp.Edges {
		srcID, _ := uuid.Parse(edge.Source)
		tgtID, _ := uuid.Parse(edge.Target)
		connectedIDs[srcID] = true
		connectedIDs[tgtID] = true
	}

	for eid := range connectedIDs {
		e, ok := entityMap[eid]
		if !ok {
			continue
		}
		node := graphNode{
			ID:       e.ID.String(),
			Name:     e.Name,
			Kind:     e.Kind,
			RepoID:   e.RepoID.String(),
			RepoName: repoNames[e.RepoID],
		}
		if e.Path != nil {
			node.Path = *e.Path
		}
		resp.Nodes = append(resp.Nodes, node)
	}

	writeJSON(w, http.StatusOK, resp)
}
