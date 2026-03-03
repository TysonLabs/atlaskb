package pipeline

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// resolveEntity attempts to find an entity by qualified_name using a fallback chain:
//  1. Exact match via FindByQualifiedName
//  2. Owner entity (e.g. "bus::Bus" for "bus::Bus.dispatch")
//  3. Fuzzy name suffix match within the same package
//
// Returns the entity ID and true if found, or uuid.Nil and false if not.
func resolveEntity(ctx context.Context, entityStore *models.EntityStore, repoID uuid.UUID, qualifiedName string) (uuid.UUID, bool) {
	// 1. Exact match
	entity, _ := entityStore.FindByQualifiedName(ctx, repoID, qualifiedName)
	if entity != nil {
		return entity.ID, true
	}

	// 2. Owner entity fallback (e.g. "bus::Bus" for "bus::Bus.dispatch")
	owner := qualifiedNameOwner(qualifiedName)
	if owner != qualifiedName {
		ownerEntity, _ := entityStore.FindByQualifiedName(ctx, repoID, owner)
		if ownerEntity != nil {
			logVerboseF("[resolve] %s → reparented to owner %s", qualifiedName, owner)
			return ownerEntity.ID, true
		}
	}

	// 3. Fuzzy suffix match: find entities in the same package whose name ends with the short name
	shortName := qualifiedName
	if idx := strings.LastIndex(shortName, "::"); idx >= 0 {
		shortName = shortName[idx+2:]
	}
	// Also strip type prefix for method names (e.g. "Handler.Publish" -> "Publish")
	bareMethod := shortName
	if idx := strings.LastIndex(bareMethod, "."); idx >= 0 {
		bareMethod = bareMethod[idx+1:]
	}
	pkg := qualifiedNamePackage(qualifiedName)

	// Search by the short name across all entity kinds
	candidates, _ := entityStore.FindByName(ctx, repoID, shortName)
	for _, c := range candidates {
		if qualifiedNamePackage(c.QualifiedName) == pkg {
			logVerboseF("[resolve] %s → fuzzy matched to %s", qualifiedName, c.QualifiedName)
			return c.ID, true
		}
	}

	// Also try matching as a method suffix (e.g. "api::Publish" matches "api::Handler.Publish")
	if bareMethod != shortName {
		candidates, _ = entityStore.FindByName(ctx, repoID, bareMethod)
		for _, c := range candidates {
			if qualifiedNamePackage(c.QualifiedName) == pkg && strings.HasSuffix(c.QualifiedName, "."+bareMethod) {
				logVerboseF("[resolve] %s → suffix matched to %s", qualifiedName, c.QualifiedName)
				return c.ID, true
			}
		}
	} else if !strings.Contains(shortName, ".") {
		// shortName has no dot — try finding entities where it's a method name
		candidates, _ = entityStore.FindByName(ctx, repoID, shortName)
		for _, c := range candidates {
			if qualifiedNamePackage(c.QualifiedName) == pkg && strings.HasSuffix(c.QualifiedName, "."+shortName) {
				logVerboseF("[resolve] %s → suffix matched to %s", qualifiedName, c.QualifiedName)
				return c.ID, true
			}
		}
	}

	// 4. Unqualified name fallback: if the name has no "::" separator, search across all packages
	// This handles cases like "HeaderFilter" (from tests) matching "filters::HeaderFilter"
	if !strings.Contains(qualifiedName, "::") {
		candidates, _ := entityStore.FindByName(ctx, repoID, qualifiedName)
		if len(candidates) == 1 {
			// Unambiguous match — use it
			logVerboseF("[resolve] %s → unqualified match to %s", qualifiedName, candidates[0].QualifiedName)
			return candidates[0].ID, true
		}
	}

	return uuid.Nil, false
}

// resolveEntityWithMap is like resolveEntity but also checks a local entity map first.
// This is used in phase2 where entities from the current file are tracked in-memory.
func resolveEntityWithMap(ctx context.Context, entityStore *models.EntityStore, repoID uuid.UUID, qualifiedName string, entityMap map[string]uuid.UUID) (uuid.UUID, bool) {
	// Check local map first
	if id, ok := entityMap[qualifiedName]; ok {
		return id, true
	}

	// Check owner in local map
	owner := qualifiedNameOwner(qualifiedName)
	if owner != qualifiedName {
		if id, ok := entityMap[owner]; ok {
			logVerboseF("[resolve] %s → reparented to owner %s (from map)", qualifiedName, owner)
			return id, true
		}
	}

	// Check suffix match in local map
	shortName := qualifiedName
	if idx := strings.LastIndex(shortName, "::"); idx >= 0 {
		shortName = shortName[idx+2:]
	}
	pkg := qualifiedNamePackage(qualifiedName)
	for qn, eid := range entityMap {
		if qualifiedNamePackage(qn) == pkg && strings.HasSuffix(qn, "."+shortName) {
			logVerboseF("[resolve] %s → suffix matched to %s (from map)", qualifiedName, qn)
			return eid, true
		}
	}

	// Fall back to DB resolution
	return resolveEntity(ctx, entityStore, repoID, qualifiedName)
}

// sortEntitiesByRichness orders entities by fact count + relationship count (descending)
// so that capping keeps the most information-rich entities.
func sortEntitiesByRichness(ctx context.Context, entities []models.Entity, factStore *models.FactStore, relStore *models.RelationshipStore) []models.Entity {
	type scored struct {
		entity models.Entity
		weight int
	}
	scored_ := make([]scored, len(entities))
	for i, e := range entities {
		facts, _ := factStore.ListByEntity(ctx, e.ID)
		rels, _ := relStore.ListByEntity(ctx, e.ID)
		scored_[i] = scored{entity: e, weight: len(facts) + len(rels)}
	}
	sort.Slice(scored_, func(i, j int) bool {
		return scored_[i].weight > scored_[j].weight
	})
	result := make([]models.Entity, len(entities))
	for i, s := range scored_ {
		result[i] = s.entity
	}
	return result
}
