package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
	"golang.org/x/sync/errgroup"
)

const systemPromptBackfill = `You are a code analysis expert. You generate facts and relationships for specific entities extracted from source code.

CRITICAL RULES:
- You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.
- Every string value must contain actual content extracted from the code.
- Use ONLY the entity qualified_names provided. Do not rename them.`

func backfillPrompt(filePath, language string, content string, orphanNames []string) string {
	nameList := strings.Join(orphanNames, "\n- ")
	return fmt.Sprintf(`The following entities were extracted from this file but have NO facts or relationships yet.
Generate at least 2 facts and 1 relationship for EACH entity listed below.

File: %s
Language: %s

<file_content>
%s
</file_content>

ENTITIES NEEDING FACTS (use these exact qualified_names):
- %s

Respond with JSON:
{
  "facts": [
    {
      "entity_name": "exact qualified_name from list above",
      "claim": "a specific, grounded claim about this entity",
      "dimension": "what|how|why",
      "category": "behavior|constraint|pattern|convention|debt|risk",
      "confidence": "high|medium|low"
    }
  ],
  "relationships": [
    {
      "from": "qualified_name",
      "to": "qualified_name",
      "kind": "depends_on|calls|implements|extends|produces|consumes|tested_by|configured_by|owns",
      "description": "brief description",
      "strength": "strong|moderate|weak"
    }
  ]
}

RULES:
- Generate 4-10 facts per entity:
  - At least 1 "what" (behavior/purpose)
  - At least 2 "how" (implementation details, patterns, numeric constants)
  - At least 1 "when" if any timing/scheduling is visible (intervals, timeouts, TTLs)
- Extract specific numeric values: timeouts, pool sizes, retry counts, buffer sizes, intervals.
- Extract lifecycle patterns: init → run → cleanup sequences, state transitions.
- Extract facts from comments — comments often explain "why" and operational constraints.
- Generate at least 1 relationship per entity (e.g. "owns" from parent type to method).
- Use ONLY the exact qualified_names listed above for entity_name.
- For relationship targets, you may reference any entity in the codebase.`, filePath, language, content, nameList)
}

type BackfillConfig struct {
	RepoID      uuid.UUID
	RepoName    string
	RepoPath    string
	Model       string
	Concurrency int
	Pool        *pgxpool.Pool
	LLM         llm.Client
}

type BackfillStats struct {
	OrphanEntities int
	FilesProcessed int
	FactsCreated   int
	RelsCreated    int
}

func RunBackfill(ctx context.Context, cfg BackfillConfig) (*BackfillStats, error) {
	stats := &BackfillStats{}
	entityStore := &models.EntityStore{Pool: cfg.Pool}
	factStore := &models.FactStore{Pool: cfg.Pool}
	relStore := &models.RelationshipStore{Pool: cfg.Pool}

	orphans, err := entityStore.ListOrphans(ctx, cfg.RepoID)
	if err != nil {
		return stats, fmt.Errorf("listing orphans: %w", err)
	}

	if len(orphans) == 0 {
		return stats, nil
	}

	stats.OrphanEntities = len(orphans)

	// Group orphans by file path
	byFile := make(map[string][]models.Entity)
	for _, e := range orphans {
		if e.Path == nil {
			continue
		}
		byFile[*e.Path] = append(byFile[*e.Path], e)
	}

	fmt.Printf("  Backfill: %d orphan entities across %d files\n", len(orphans), len(byFile))

	type fileWork struct {
		path     string
		entities []models.Entity
	}
	var work []fileWork
	for path, entities := range byFile {
		work = append(work, fileWork{path: path, entities: entities})
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(cfg.Concurrency)

	for _, w := range work {
		w := w
		g.Go(func() error {
			content, err := os.ReadFile(filepath.Join(cfg.RepoPath, w.path))
			if err != nil {
				return nil // skip unreadable files
			}

			fi := ClassifyFile(w.path, int64(len(content)))
			var names []string
			entityIDMap := make(map[string]uuid.UUID)
			for _, e := range w.entities {
				names = append(names, e.QualifiedName)
				entityIDMap[e.QualifiedName] = e.ID
			}

			prompt := backfillPrompt(w.path, fi.Language, string(content), names)

			resp, _, err := callLLMWithRetry(gctx, cfg.LLM, cfg.Model, systemPromptBackfill, []llm.Message{
				{Role: "user", Content: prompt},
			}, 4096, SchemaPhase2, DefaultRetryConfig)
			if err != nil {
				logVerboseF("[backfill] %s: LLM error: %v", w.path, err)
				return nil
			}

			// Parse as a minimal structure with just facts and relationships
			type BackfillResult struct {
				Facts         []ExtractedFact     `json:"facts"`
				Relationships []ExtractedRelation `json:"relationships"`
			}

			cleaned := CleanJSON(resp.Content)
			var result BackfillResult
			if err := parseJSON(cleaned, &result); err != nil {
				logVerboseF("[backfill] %s: parse error: %v", w.path, err)
				return nil
			}

			result.Facts = sanitizeFacts(result.Facts)
			result.Relationships = sanitizeRelationships(result.Relationships)

			// Store facts
			factsCreated := 0
			for _, ef := range result.Facts {
				// First check exact match in our orphan set
				entityID, ok := entityIDMap[ef.EntityName]
				if !ok {
					// Try resolve
					entityID, ok = resolveEntity(gctx, entityStore, cfg.RepoID, ef.EntityName)
					if !ok {
						continue
					}
				}
				fact := &models.Fact{
					EntityID:   entityID,
					RepoID:     cfg.RepoID,
					Claim:      ef.Claim,
					Dimension:  ef.Dimension,
					Category:   ef.Category,
					Confidence: ef.Confidence,
					Provenance: []models.Provenance{{
						SourceType: "file",
						Repo:       cfg.RepoName,
						Ref:        w.path + " (backfill)",
					}},
				}
				if err := factStore.Create(gctx, fact); err == nil {
					factsCreated++
				}
			}

			// Store relationships
			relsCreated := 0
			for _, er := range result.Relationships {
				fromID, fromOK := resolveEntity(gctx, entityStore, cfg.RepoID, er.From)
				if !fromOK {
					if id, ok := entityIDMap[er.From]; ok {
						fromID = id
						fromOK = true
					}
				}
				toID, toOK := resolveEntity(gctx, entityStore, cfg.RepoID, er.To)
				if !toOK {
					if id, ok := entityIDMap[er.To]; ok {
						toID = id
						toOK = true
					}
				}
				if !fromOK || !toOK {
					continue
				}
				rel := &models.Relationship{
					RepoID:       cfg.RepoID,
					FromEntityID: fromID,
					ToEntityID:   toID,
					Kind:         er.Kind,
					Description:  models.Ptr(er.Description),
					Strength:     er.Strength,
					Provenance: []models.Provenance{{
						SourceType: "file",
						Repo:       cfg.RepoName,
						Ref:        w.path + " (backfill)",
					}},
				}
				if err := relStore.Upsert(gctx, rel); err == nil {
					relsCreated++
				}
			}

			stats.FilesProcessed++
			stats.FactsCreated += factsCreated
			stats.RelsCreated += relsCreated
			logVerboseF("[backfill] %s: %d facts, %d relationships for %d entities",
				w.path, factsCreated, relsCreated, len(w.entities))

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return stats, err
	}

	log.Printf("[backfill] fact pass: %d orphans, %d files, %d facts, %d relationships",
		stats.OrphanEntities, stats.FilesProcessed, stats.FactsCreated, stats.RelsCreated)

	// Round 2: Fix isolated entities (no relationships) with deterministic "owns" relationships
	isolated, err := entityStore.ListWithoutRelationships(ctx, cfg.RepoID)
	if err != nil {
		return stats, fmt.Errorf("listing isolated entities: %w", err)
	}

	autoRels := 0
	for _, e := range isolated {
		// If this is a method (name contains "."), auto-create "owns" from parent
		owner := qualifiedNameOwner(e.QualifiedName)
		if owner == e.QualifiedName || owner == qualifiedNamePackage(e.QualifiedName) {
			continue // not a method, skip
		}

		ownerEntity, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, owner)
		if ownerEntity == nil {
			continue
		}

		rel := &models.Relationship{
			RepoID:       cfg.RepoID,
			FromEntityID: ownerEntity.ID,
			ToEntityID:   e.ID,
			Kind:         models.RelOwns,
			Description:  models.Ptr(fmt.Sprintf("%s is a method on %s", e.Name, ownerEntity.Name)),
			Strength:     models.StrengthStrong,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        "auto-owns",
			}},
		}
		if err := relStore.Upsert(ctx, rel); err == nil {
			autoRels++
		}
	}

	if autoRels > 0 {
		fmt.Printf("  Auto-created %d 'owns' relationships for isolated methods\n", autoRels)
		stats.RelsCreated += autoRels
	}

	// Round 3: For remaining isolated entities, try LLM-based relationship generation
	isolated, _ = entityStore.ListWithoutRelationships(ctx, cfg.RepoID)
	if len(isolated) > 0 {
		isolatedByFile := make(map[string][]models.Entity)
		for _, e := range isolated {
			if e.Path == nil {
				continue
			}
			isolatedByFile[*e.Path] = append(isolatedByFile[*e.Path], e)
		}

		var isoWork []fileWork
		for path, entities := range isolatedByFile {
			isoWork = append(isoWork, fileWork{path: path, entities: entities})
		}

		g2, gctx2 := errgroup.WithContext(ctx)
		g2.SetLimit(cfg.Concurrency)

		for _, w := range isoWork {
			w := w
			g2.Go(func() error {
				content, err := os.ReadFile(filepath.Join(cfg.RepoPath, w.path))
				if err != nil {
					return nil
				}

				fi := ClassifyFile(w.path, int64(len(content)))
				var names []string
				entityIDMap := make(map[string]uuid.UUID)
				for _, e := range w.entities {
					names = append(names, e.QualifiedName)
					entityIDMap[e.QualifiedName] = e.ID
				}

				prompt := backfillPrompt(w.path, fi.Language, string(content), names)

				resp, _, err := callLLMWithRetry(gctx2, cfg.LLM, cfg.Model, systemPromptBackfill, []llm.Message{
					{Role: "user", Content: prompt},
				}, 4096, SchemaPhase2, DefaultRetryConfig)
				if err != nil {
					return nil
				}

				type BackfillResult struct {
					Facts         []ExtractedFact     `json:"facts"`
					Relationships []ExtractedRelation `json:"relationships"`
				}

				cleaned := CleanJSON(resp.Content)
				var result BackfillResult
				if err := parseJSON(cleaned, &result); err != nil {
					return nil
				}
				result.Relationships = sanitizeRelationships(result.Relationships)

				relsCreated := 0
				for _, er := range result.Relationships {
					fromID, fromOK := resolveEntity(gctx2, entityStore, cfg.RepoID, er.From)
					if !fromOK {
						if id, ok := entityIDMap[er.From]; ok {
							fromID = id
							fromOK = true
						}
					}
					toID, toOK := resolveEntity(gctx2, entityStore, cfg.RepoID, er.To)
					if !toOK {
						if id, ok := entityIDMap[er.To]; ok {
							toID = id
							toOK = true
						}
					}
					if !fromOK || !toOK {
						continue
					}
					rel := &models.Relationship{
						RepoID:       cfg.RepoID,
						FromEntityID: fromID,
						ToEntityID:   toID,
						Kind:         er.Kind,
						Description:  models.Ptr(er.Description),
						Strength:     er.Strength,
						Provenance: []models.Provenance{{
							SourceType: "file",
							Repo:       cfg.RepoName,
							Ref:        w.path + " (rel-backfill)",
						}},
					}
					if err := relStore.Upsert(gctx2, rel); err == nil {
						relsCreated++
					}
				}

				stats.RelsCreated += relsCreated
				if relsCreated > 0 {
					logVerboseF("[backfill-rel] %s: %d relationships for %d entities", w.path, relsCreated, len(w.entities))
				}

				return nil
			})
		}

		g2.Wait()
	}

	log.Printf("[backfill] completed: %d orphans, %d files, %d facts, %d total relationships",
		stats.OrphanEntities, stats.FilesProcessed, stats.FactsCreated, stats.RelsCreated)

	return stats, nil
}

func parseJSON(cleaned string, result interface{}) error {
	return json.Unmarshal([]byte(cleaned), result)
}
