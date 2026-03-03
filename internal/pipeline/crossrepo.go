package pipeline

import (
	"context"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type repoMatch struct {
	repo models.Repo
}

// DiscoverCrossRepoLinks matches dependency names from ExtractDependencies against
// the repos table to auto-create cross-repo relationships.
func DiscoverCrossRepoLinks(ctx context.Context, pool *pgxpool.Pool, repoID uuid.UUID, repoName string, deps []Dependency) (created, skipped int) {
	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}

	allRepos, err := repoStore.List(ctx)
	if err != nil {
		log.Printf("[cross-repo] failed to list repos: %v", err)
		return 0, 0
	}

	if len(allRepos) < 2 {
		return 0, 0
	}

	// Build lookup: normalized name/URL fragment -> repo
	repoIndex := make(map[string]*repoMatch)
	for _, r := range allRepos {
		if r.ID == repoID {
			continue // skip self
		}
		// Index by lowercase repo name
		normalized := strings.ToLower(r.Name)
		repoIndex[normalized] = &repoMatch{repo: r}

		// Also index by last path segment of remote URL
		if r.RemoteURL != nil {
			urlName := extractURLRepoName(*r.RemoteURL)
			if urlName != "" {
				repoIndex[strings.ToLower(urlName)] = &repoMatch{repo: r}
			}
		}
	}

	// Find or create repo-root entity for the source repo
	fromEntity, _ := entityStore.FindByQualifiedName(ctx, repoID, repoName)
	if fromEntity == nil {
		return 0, 0
	}

	for _, dep := range deps {
		normalized := normalizeDependencyName(dep.Name)

		match, ok := repoIndex[normalized]
		if !ok {
			// Fuzzy fallback: substring/contains match
			match = fuzzyMatchRepo(normalized, repoIndex)
			if match == nil {
				continue
			}
		}

		// Find repo-root entity for the target repo
		toEntity, _ := entityStore.FindByQualifiedName(ctx, match.repo.ID, match.repo.Name)
		if toEntity == nil {
			skipped++
			continue
		}

		cr := &models.CrossRepoRelationship{
			FromEntityID: fromEntity.ID,
			ToEntityID:   toEntity.ID,
			FromRepoID:   repoID,
			ToRepoID:     match.repo.ID,
			Kind:         models.RelDependsOn,
			Strength:     models.StrengthStrong,
			Provenance: []models.Provenance{{
				SourceType: "auto-discovery",
				Repo:       repoName,
				Ref:        dep.Source,
			}},
		}

		if err := relStore.UpsertCrossRepo(ctx, cr); err != nil {
			log.Printf("[cross-repo] failed to upsert link %s -> %s: %v", repoName, match.repo.Name, err)
			skipped++
			continue
		}
		created++
	}

	return created, skipped
}

// normalizeDependencyName strips common prefixes (org/scope) and lowercases.
func normalizeDependencyName(name string) string {
	name = strings.ToLower(name)

	// Strip Go module prefix (e.g., "github.com/org/repo" -> "repo")
	if strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		name = parts[len(parts)-1]
	}

	// Strip npm scope (e.g., "@org/pkg" -> "pkg")
	if strings.HasPrefix(name, "@") {
		parts := strings.SplitN(name, "/", 2)
		if len(parts) == 2 {
			name = parts[1]
		}
	}

	return name
}

// fuzzyMatchRepo tries substring and contains matching against the repo index.
// Returns the best match or nil if none found.
func fuzzyMatchRepo(normalized string, repoIndex map[string]*repoMatch) *repoMatch {
	// 1. Check if the dependency name contains a repo name (e.g., "vector-ivr-core-lib" contains "vector-ivr-core")
	var bestMatch *repoMatch
	bestLen := 0
	for name, m := range repoIndex {
		// Repo name is a substring of dependency name
		if strings.Contains(normalized, name) && len(name) > bestLen {
			bestMatch = m
			bestLen = len(name)
		}
		// Dependency name is a substring of repo name
		if len(normalized) >= 3 && strings.Contains(name, normalized) && len(name) > bestLen {
			bestMatch = m
			bestLen = len(name)
		}
	}
	return bestMatch
}

// extractURLRepoName gets the repo name from a remote URL.
func extractURLRepoName(url string) string {
	// Strip .git suffix
	url = strings.TrimSuffix(url, ".git")
	// Get last path component
	if idx := strings.LastIndex(url, "/"); idx >= 0 {
		return url[idx+1:]
	}
	if idx := strings.LastIndex(url, ":"); idx >= 0 {
		return url[idx+1:]
	}
	return ""
}
