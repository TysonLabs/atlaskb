package pipeline

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
	"golang.org/x/sync/errgroup"
)

type Phase2Config struct {
	RepoID      uuid.UUID
	RepoName    string
	RepoPath    string
	Manifest    *Manifest
	Model       string
	Concurrency int
	Pool        *pgxpool.Pool
	LLM         llm.Client
	Roster      []EntityEntry // Ctags-derived entity roster for grounding names
}

type Phase2Stats struct {
	FilesProcessed  int
	FilesSkipped    int
	EntitiesCreated int
	FactsCreated    int
	RelsCreated     int
	RelsDeferred    int
	RelsResolved    int
	TotalTokens     int
}

// DeferredRelationship is a relationship that couldn't be resolved during initial
// processing because the target entity hadn't been created yet (concurrent processing).
type DeferredRelationship struct {
	From        string
	To          string
	Kind        string
	Description string
	Strength    string
	SourceFile  string
	RepoName    string
	AnalyzedAt  string
}

func RunPhase2(ctx context.Context, cfg Phase2Config) (*Phase2Stats, error) {
	stats := &Phase2Stats{}
	jobStore := &models.JobStore{Pool: cfg.Pool}
	entityStore := &models.EntityStore{Pool: cfg.Pool}
	factStore := &models.FactStore{Pool: cfg.Pool}
	relStore := &models.RelationshipStore{Pool: cfg.Pool}

	// Deferred relationships: collected during concurrent processing, resolved after
	var deferredMu sync.Mutex
	var deferred []DeferredRelationship

	// Create jobs for all analyzable files
	for _, fi := range cfg.Manifest.Files {
		if !ShouldAnalyze(fi) || IsManifestFile(fi.Path) || fi.Class == ClassDoc {
			continue
		}

		content, err := os.ReadFile(filepath.Join(cfg.RepoPath, fi.Path))
		if err != nil {
			continue
		}

		hash := fmt.Sprintf("%x", sha256.Sum256(content))

		// Check if file has changed
		existing, _ := jobStore.GetByTarget(ctx, cfg.RepoID, models.PhasePhase2, fi.Path)
		if existing != nil && existing.Status == models.JobCompleted && existing.ContentHash != nil && *existing.ContentHash == hash {
			stats.FilesSkipped++
			continue
		}

		job := &models.ExtractionJob{
			RepoID:      cfg.RepoID,
			Phase:       models.PhasePhase2,
			Target:      fi.Path,
			ContentHash: &hash,
			Status:      models.JobPending,
		}
		if err := jobStore.Create(ctx, job); err != nil {
			logVerboseF("warn: creating job for %s: %v", fi.Path, err)
		}
	}

	// Count total pending jobs for progress
	counts, _ := jobStore.CountByStatus(ctx, cfg.RepoID, models.PhasePhase2)
	totalJobs := counts["pending"]
	processed := 0

	// Process jobs with bounded concurrency
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(cfg.Concurrency)

	for {
		job, err := jobStore.ClaimNext(gctx, cfg.RepoID, models.PhasePhase2)
		if err != nil {
			return stats, fmt.Errorf("claiming job: %w", err)
		}
		if job == nil {
			break // no more pending jobs
		}

		g.Go(func() error {
			fmt.Printf("  [%d/%d] Analyzing %s...\n", processed+1, totalJobs, job.Target)
			fileDeferred, err := processFile(gctx, cfg, job, entityStore, factStore, relStore, stats)
			processed++
			if err != nil {
				jobStore.Fail(gctx, job.ID, err.Error())
				fmt.Printf("  [%d/%d] FAILED %s: %v\n", processed, totalJobs, job.Target, err)
				return nil // don't cancel other workers
			}
			if len(fileDeferred) > 0 {
				deferredMu.Lock()
				deferred = append(deferred, fileDeferred...)
				deferredMu.Unlock()
			}
			fmt.Printf("  [%d/%d] Done %s\n", processed, totalJobs, job.Target)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return stats, err
	}

	// Second pass: resolve deferred relationships now that all entities are in the DB
	if len(deferred) > 0 {
		resolved := 0
		for _, d := range deferred {
			fromID, fromOK := resolveEntity(ctx, entityStore, cfg.RepoID, d.From)
			if !fromOK {
				logVerboseF("[phase2-deferred] %s: still unresolved (from): %s", d.SourceFile, d.From)
				continue
			}
			toID, toOK := resolveEntity(ctx, entityStore, cfg.RepoID, d.To)
			if !toOK {
				logVerboseF("[phase2-deferred] %s: still unresolved (to): %s", d.SourceFile, d.To)
				continue
			}
			rel := &models.Relationship{
				RepoID:       cfg.RepoID,
				FromEntityID: fromID,
				ToEntityID:   toID,
				Kind:         d.Kind,
				Description:  models.Ptr(d.Description),
				Strength:     d.Strength,
				Provenance: []models.Provenance{{
					SourceType: "file",
					Repo:       d.RepoName,
					Ref:        d.SourceFile,
					AnalyzedAt: d.AnalyzedAt,
				}},
			}
			if err := relStore.Upsert(ctx, rel); err != nil {
				logVerboseF("[phase2-deferred] warn: upserting relationship: %v", err)
			} else {
				resolved++
			}
		}
		stats.RelsDeferred = len(deferred)
		stats.RelsResolved = resolved
		if resolved > 0 || len(deferred) > 0 {
			fmt.Printf("  Deferred relationships: %d total, %d resolved, %d still unresolved\n",
				len(deferred), resolved, len(deferred)-resolved)
		}
	}

	return stats, nil
}

func processFile(ctx context.Context, cfg Phase2Config, job *models.ExtractionJob,
	entityStore *models.EntityStore, factStore *models.FactStore, relStore *models.RelationshipStore,
	stats *Phase2Stats) ([]DeferredRelationship, error) {

	jobStore := &models.JobStore{Pool: cfg.Pool}

	content, err := os.ReadFile(filepath.Join(cfg.RepoPath, job.Target))
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	fi := ClassifyFile(job.Target, int64(len(content)))

	prompt := Phase2Prompt(job.Target, fi.Language, cfg.RepoName, cfg.Manifest.Stack, string(content), cfg.Roster)

	resp, attempts, err := callLLMWithRetry(ctx, cfg.LLM, cfg.Model, systemPromptPhase2, []llm.Message{
		{Role: "user", Content: prompt},
	}, 4096, SchemaPhase2, DefaultRetryConfig)
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	result, err := ParsePhase2(resp.Content)
	if err != nil {
		cleaned := CleanJSON(resp.Content)
		preview := cleaned
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("parsing response: %w\n  raw preview: %s", err, preview)
	}
	_ = attempts

	log.Printf("[phase2] %s: extracted %d entities, %d facts, %d relationships",
		job.Target, len(result.Entities), len(result.Facts), len(result.Relationships))

	// Store entities with dedup
	entityMap := make(map[string]uuid.UUID) // qualified_name -> entity ID
	for _, ext := range result.Entities {
		// Check for existing entity by qualified_name
		existing, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, ext.QualifiedName)
		if existing != nil {
			// Exact match found — use the existing entity
			entityMap[ext.QualifiedName] = existing.ID
			logVerboseF("[phase2] %s: entity %q → SKIP (already exists)", job.Target, ext.QualifiedName)
			continue
		}

		// No exact match — check for fuzzy matches by name+kind
		fuzzy, _ := entityStore.FindByNameAndKind(ctx, cfg.RepoID, ext.Name, ext.Kind)
		if len(fuzzy) > 0 {
			// Only consider dedup if the candidate has the same owner as the fuzzy match.
			// "Owner" = the type prefix for methods (e.g. "storage::MemoryStorage" for
			// "storage::MemoryStorage.Save") or the package for top-level symbols.
			// This prevents deduping same-name methods on different types within the
			// same package (e.g. MemoryStorage.Save vs FileStorage.Save).
			sameOwner := false
			candidateOwner := qualifiedNameOwner(ext.QualifiedName)
			var matchedFuzzy *models.Entity
			for i, f := range fuzzy {
				if qualifiedNameOwner(f.QualifiedName) == candidateOwner {
					sameOwner = true
					matchedFuzzy = &fuzzy[i]
					break
				}
			}

			if sameOwner && matchedFuzzy != nil {
				// Use LLM dedup to decide
				decision, err := DedupEntity(ctx, cfg.LLM, cfg.Model, matchedFuzzy, ext)
				if err != nil {
					logVerboseF("[phase2] %s: dedup error for %q, inserting: %v", job.Target, ext.QualifiedName, err)
				} else {
					switch decision.Action {
					case "skip":
						entityMap[ext.QualifiedName] = matchedFuzzy.ID
						logVerboseF("[phase2] %s: entity %q → SKIP (%s)", job.Target, ext.QualifiedName, decision.Reason)
						continue
					case "update":
						matchedFuzzy.Summary = models.Ptr(ext.Summary)
						matchedFuzzy.Capabilities = ext.Capabilities
						matchedFuzzy.Assumptions = ext.Assumptions
						entityStore.Update(ctx, matchedFuzzy)
						entityMap[ext.QualifiedName] = matchedFuzzy.ID
						logVerboseF("[phase2] %s: entity %q → UPDATE (%s)", job.Target, ext.QualifiedName, decision.Reason)
						continue
					default:
						logVerboseF("[phase2] %s: entity %q → INSERT (%s)", job.Target, ext.QualifiedName, decision.Reason)
					}
				}
			} else {
				logVerboseF("[phase2] %s: entity %q → INSERT (different owner from fuzzy match %q)", job.Target, ext.QualifiedName, fuzzy[0].QualifiedName)
			}
		}

		// Insert new entity using Upsert for safety
		entity := &models.Entity{
			RepoID:        cfg.RepoID,
			Kind:          ext.Kind,
			Name:          ext.Name,
			QualifiedName: ext.QualifiedName,
			Path:          models.Ptr(job.Target),
			Summary:       models.Ptr(ext.Summary),
			Capabilities:  ext.Capabilities,
			Assumptions:   ext.Assumptions,
		}
		if err := entityStore.Upsert(ctx, entity); err != nil {
			logVerboseF("[phase2] warn: upserting entity %s: %v", ext.QualifiedName, err)
			continue
		}
		entityMap[ext.QualifiedName] = entity.ID
		stats.EntitiesCreated++
		logVerboseF("[phase2] %s: entity %q → INSERT (no existing match)", job.Target, ext.QualifiedName)
	}

	// Store facts
	for _, ext := range result.Facts {
		entityID, ok := resolveEntityWithMap(ctx, entityStore, cfg.RepoID, ext.EntityName, entityMap)
		if !ok {
			logVerboseF("[phase2] %s: fact skipped (entity not found): %s", job.Target, ext.EntityName)
			continue
		}

		fact := &models.Fact{
			EntityID:   entityID,
			RepoID:     cfg.RepoID,
			Claim:      ext.Claim,
			Dimension:  ext.Dimension,
			Category:   ext.Category,
			Confidence: ext.Confidence,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        job.Target,
				AnalyzedAt: job.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}},
		}
		if err := factStore.Create(ctx, fact); err != nil {
			logVerboseF("[phase2] warn: creating fact: %v", err)
			continue
		}
		stats.FactsCreated++
	}

	// Store relationships using Upsert, defer unresolvable ones
	var fileDeferred []DeferredRelationship
	relsCreated := 0
	for _, ext := range result.Relationships {
		fromID, fromOK := resolveEntityWithMap(ctx, entityStore, cfg.RepoID, ext.From, entityMap)
		toID, toOK := resolveEntityWithMap(ctx, entityStore, cfg.RepoID, ext.To, entityMap)

		if !fromOK || !toOK {
			// Defer: the target entity may not exist yet due to concurrent processing
			fileDeferred = append(fileDeferred, DeferredRelationship{
				From:        ext.From,
				To:          ext.To,
				Kind:        ext.Kind,
				Description: ext.Description,
				Strength:    ext.Strength,
				SourceFile:  job.Target,
				RepoName:    cfg.RepoName,
				AnalyzedAt:  job.CreatedAt.Format("2006-01-02T15:04:05Z"),
			})
			logVerboseF("[phase2] %s: relationship deferred (entity not yet available): %s → %s", job.Target, ext.From, ext.To)
			continue
		}
		rel := &models.Relationship{
			RepoID:       cfg.RepoID,
			FromEntityID: fromID,
			ToEntityID:   toID,
			Kind:         ext.Kind,
			Description:  models.Ptr(ext.Description),
			Strength:     ext.Strength,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        job.Target,
				AnalyzedAt: job.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}},
		}
		if err := relStore.Upsert(ctx, rel); err != nil {
			logVerboseF("[phase2] warn: upserting relationship: %v", err)
		} else {
			relsCreated++
		}
	}
	stats.RelsCreated += relsCreated

	// Mark job complete
	stats.TotalTokens += resp.InputTokens + resp.OutputTokens
	stats.FilesProcessed++
	costUSD := float64(resp.InputTokens)/1_000_000*SonnetInputPer1M + float64(resp.OutputTokens)/1_000_000*SonnetOutputPer1M
	err = jobStore.CompleteWithDetails(ctx, job.ID, resp.InputTokens+resp.OutputTokens, costUSD, resp.Model, attempts)
	return fileDeferred, err
}

func logVerboseF(format string, args ...any) {
	// This will be wired to the CLI verbose flag via the orchestrator
	fmt.Printf(format+"\n", args...)
}

// qualifiedNamePackage extracts the package/module prefix from a qualified name.
// e.g. "store::TaskStore.Create" → "store", "models::Task" → "models"
func qualifiedNamePackage(qn string) string {
	if idx := strings.Index(qn, "::"); idx >= 0 {
		return qn[:idx]
	}
	if idx := strings.Index(qn, "."); idx >= 0 {
		return qn[:idx]
	}
	return qn
}

// qualifiedNameOwner extracts the owning scope from a qualified name.
// For methods it returns the type prefix; for top-level symbols it returns the package.
// e.g. "storage::MemoryStorage.Save" → "storage::MemoryStorage"
//
//	"storage::FileStorage.Save"   → "storage::FileStorage"
//	"storage::NewMemoryStorage"   → "storage"
//	"bus::Bus.Publish"            → "bus::Bus"
func qualifiedNameOwner(qn string) string {
	if idx := strings.LastIndex(qn, "."); idx >= 0 {
		return qn[:idx]
	}
	return qualifiedNamePackage(qn)
}
