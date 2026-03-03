package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// GenerateOverview composes a markdown overview of a repository from existing KB data.
// No LLM call — purely template-driven from Phase 5 output and other stored data.
func GenerateOverview(ctx context.Context, pool *pgxpool.Pool, repoID uuid.UUID, repoName string) (string, error) {
	entityStore := &models.EntityStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}
	decisionStore := &models.DecisionStore{Pool: pool}

	var sb strings.Builder

	// --- Repo entity: summary + capabilities ---
	repoEntity, _ := entityStore.FindByQualifiedName(ctx, repoID, repoName)

	sb.WriteString("# ")
	sb.WriteString(repoName)
	sb.WriteString("\n\n")

	if repoEntity != nil && repoEntity.Summary != nil {
		sb.WriteString(*repoEntity.Summary)
		sb.WriteString("\n\n")
	}

	// Capabilities
	if repoEntity != nil && len(repoEntity.Capabilities) > 0 {
		sb.WriteString("## Capabilities\n\n")
		for _, cap := range repoEntity.Capabilities {
			sb.WriteString("- ")
			sb.WriteString(cap)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// --- Architecture fact ---
	if repoEntity != nil {
		facts, _ := factStore.ListByEntity(ctx, repoEntity.ID)
		for _, f := range facts {
			if f.Category == models.CategoryPattern && strings.HasPrefix(f.Claim, "Architecture: ") {
				sb.WriteString("## Architecture\n\n")
				sb.WriteString(strings.TrimPrefix(f.Claim, "Architecture: "))
				sb.WriteString("\n\n")
				break
			}
		}
	}

	// --- Key Components (top ~15 entities by richness) ---
	allEntities, _ := entityStore.ListByRepo(ctx, repoID)
	var coreEntities []models.Entity
	for _, e := range allEntities {
		// Skip external deps (module with no path) and the repo entity itself
		if e.Kind == models.EntityModule && e.Path == nil {
			continue
		}
		if repoEntity != nil && e.ID == repoEntity.ID {
			continue
		}
		coreEntities = append(coreEntities, e)
	}

	if len(coreEntities) > 0 {
		sorted := sortEntitiesByRichness(ctx, coreEntities, factStore, relStore)
		limit := 15
		if len(sorted) < limit {
			limit = len(sorted)
		}
		top := sorted[:limit]

		sb.WriteString("## Key Components\n\n")
		sb.WriteString("| Component | Kind | Description |\n")
		sb.WriteString("|-----------|------|-------------|\n")
		for _, e := range top {
			summary := ""
			if e.Summary != nil {
				// Truncate long summaries for table readability
				s := *e.Summary
				if len(s) > 120 {
					s = s[:117] + "..."
				}
				// Escape pipes in summary for markdown table
				s = strings.ReplaceAll(s, "|", "\\|")
				summary = s
			}
			sb.WriteString("| ")
			sb.WriteString(e.QualifiedName)
			sb.WriteString(" | ")
			sb.WriteString(e.Kind)
			sb.WriteString(" | ")
			sb.WriteString(summary)
			sb.WriteString(" |\n")
		}
		sb.WriteString("\n")
	}

	// --- Conventions ---
	if repoEntity != nil {
		facts, _ := factStore.ListByEntity(ctx, repoEntity.ID)
		var conventions []models.Fact
		for _, f := range facts {
			if f.Category == models.CategoryConvention {
				conventions = append(conventions, f)
			}
		}
		if len(conventions) > 0 {
			sb.WriteString("## Conventions\n\n")
			for _, f := range conventions {
				// Convention claims are stored as "[category] description"
				claim := f.Claim
				if strings.HasPrefix(claim, "[") {
					end := strings.Index(claim, "] ")
					if end > 0 {
						category := claim[1:end]
						description := claim[end+2:]
						sb.WriteString("### ")
						sb.WriteString(category)
						sb.WriteString("\n\n")
						sb.WriteString(description)
						sb.WriteString("\n\n")
						continue
					}
				}
				sb.WriteString("- ")
				sb.WriteString(claim)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	// --- Dependencies (external modules with no path) ---
	var deps []models.Entity
	for _, e := range allEntities {
		if e.Kind == models.EntityModule && e.Path == nil {
			deps = append(deps, e)
		}
	}
	if len(deps) > 0 {
		sb.WriteString("## Dependencies\n\n")
		for _, dep := range deps {
			// Look for version fact
			depFacts, _ := factStore.ListByEntity(ctx, dep.ID)
			version := ""
			for _, f := range depFacts {
				if strings.HasPrefix(f.Claim, "Required at version ") {
					version = strings.TrimPrefix(f.Claim, "Required at version ")
					break
				}
			}
			sb.WriteString("- **")
			sb.WriteString(dep.Name)
			sb.WriteString("**")
			if version != "" {
				sb.WriteString(": ")
				sb.WriteString(version)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// --- Decisions ---
	decisions, _ := decisionStore.ListByRepo(ctx, repoID)
	var validDecisions []models.Decision
	for _, d := range decisions {
		if d.StillValid {
			validDecisions = append(validDecisions, d)
		}
	}
	if len(validDecisions) > 10 {
		validDecisions = validDecisions[:10]
	}
	if len(validDecisions) > 0 {
		sb.WriteString("## Decisions\n\n")
		for _, d := range validDecisions {
			sb.WriteString("- **")
			sb.WriteString(d.Summary)
			sb.WriteString("**: ")
			sb.WriteString(d.Rationale)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// --- Risks & Technical Debt ---
	if repoEntity != nil {
		facts, _ := factStore.ListByEntity(ctx, repoEntity.ID)
		var risks []models.Fact
		for _, f := range facts {
			if f.Category == models.CategoryRisk {
				risks = append(risks, f)
			}
		}
		if len(risks) > 0 {
			sb.WriteString("## Risks & Technical Debt\n\n")
			for _, f := range risks {
				sb.WriteString("- ")
				sb.WriteString(f.Claim)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}

	result := strings.TrimRight(sb.String(), "\n")
	if result == fmt.Sprintf("# %s", repoName) {
		return "", fmt.Errorf("no data available to generate overview")
	}

	return result, nil
}
