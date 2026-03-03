package pipeline

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// resolveNameAlternatives generates alternative qualified names to try when exact
// matching fails. Fixes common LLM naming mistakes:
//   - "pkg::Owner::Method" → "pkg::Owner.Method" (Rust uses :: for methods, we use .)
//   - "src::Name" → "Name" (LLM uses "src" as module prefix)
//   - "tests::Name" → "Name" (LLM uses test module prefix)
//
// Returns a list of alternative names to try (may be empty).
func resolveNameAlternatives(qualifiedName string) []string {
	var alts []string

	// Fix Rust-style Owner::Method where LLM used :: instead of .
	// Pattern: "Owner::Method" where Owner is CamelCase (likely a type name)
	// e.g. "KeyBindings::defaults" → look for "keybinds::KeyBindings.defaults"
	if idx := strings.Index(qualifiedName, "::"); idx >= 0 {
		afterSep := qualifiedName[idx+2:]
		// Check if what's after :: contains another :: (i.e. "pkg::Owner::Method")
		if innerIdx := strings.Index(afterSep, "::"); innerIdx >= 0 {
			// "pkg::Owner::Method" → try "pkg::Owner.Method"
			pkg := qualifiedName[:idx]
			owner := afterSep[:innerIdx]
			method := afterSep[innerIdx+2:]
			alts = append(alts, pkg+"::"+owner+"."+method)
		} else if len(afterSep) > 0 && afterSep[0] >= 'A' && afterSep[0] <= 'Z' {
			// "Owner::Method" with no pkg prefix — could be missing the package
			// Don't add alt here, the fuzzy match below will handle it
		}
	}

	// Strip "src::" prefix — LLM sometimes uses "src" as the module name
	if strings.HasPrefix(qualifiedName, "src::") {
		stripped := qualifiedName[5:]
		alts = append(alts, stripped)
	}

	// Strip "tests::" prefix — test module names
	if strings.HasPrefix(qualifiedName, "tests::") {
		stripped := qualifiedName[7:]
		alts = append(alts, stripped)
	}

	return alts
}

// resolveEntity attempts to find an entity by qualified_name using a fallback chain:
//  1. Exact match via FindByQualifiedName
//  1b. Normalized name alternatives (Owner::Method → Owner.Method, strip "src::")
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

	// 1b. Try normalized alternatives
	for _, alt := range resolveNameAlternatives(qualifiedName) {
		entity, _ = entityStore.FindByQualifiedName(ctx, repoID, alt)
		if entity != nil {
			logVerboseF("[resolve] %s → normalized to %s", qualifiedName, alt)
			return entity.ID, true
		}
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

	// 4. Cross-package method match: "Owner::Method" → search for any "*.Owner.Method"
	// Handles LLM using wrong package or Rust-style :: for methods
	if strings.Contains(shortName, "::") {
		parts := strings.SplitN(shortName, "::", 2)
		if len(parts) == 2 {
			methodName := parts[1]
			candidates, _ = entityStore.FindByName(ctx, repoID, methodName)
			dotForm := parts[0] + "." + parts[1]
			for _, c := range candidates {
				if strings.HasSuffix(c.QualifiedName, dotForm) {
					logVerboseF("[resolve] %s → cross-pkg method match to %s", qualifiedName, c.QualifiedName)
					return c.ID, true
				}
			}
		}
	}

	// 5. Unqualified name fallback: if the name has no "::" separator, search across all packages
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

	// Check normalized alternatives in local map
	for _, alt := range resolveNameAlternatives(qualifiedName) {
		if id, ok := entityMap[alt]; ok {
			logVerboseF("[resolve] %s → normalized to %s (from map)", qualifiedName, alt)
			return id, true
		}
	}

	// Check owner in local map
	owner := qualifiedNameOwner(qualifiedName)
	if owner != qualifiedName {
		if id, ok := entityMap[owner]; ok {
			logVerboseF("[resolve] %s → reparented to owner %s (from map)", qualifiedName, owner)
			return id, true
		}
	}

	// Check suffix match in local map — handles cross-package method references
	// e.g. "KeyBindings::defaults" matching "keybinds::KeyBindings.defaults"
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

	// Also try matching "Owner::Method" as any entity ending in "Owner.Method"
	// across all packages in the map (handles wrong package prefix from LLM)
	if strings.Contains(shortName, "::") {
		// shortName is "Owner::Method" — convert to "Owner.Method" for suffix matching
		dotForm := strings.Replace(shortName, "::", ".", 1)
		for qn, eid := range entityMap {
			if strings.HasSuffix(qn, dotForm) {
				logVerboseF("[resolve] %s → cross-pkg matched to %s (from map)", qualifiedName, qn)
				return eid, true
			}
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
