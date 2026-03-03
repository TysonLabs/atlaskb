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
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
	"golang.org/x/sync/errgroup"
)

type Phase2Config struct {
	RepoID       uuid.UUID
	RepoName     string
	RepoPath     string
	Manifest     *Manifest
	Model        string
	Concurrency  int
	Pool         *pgxpool.Pool
	LLM          llm.Client
	Roster       []EntityEntry // Ctags-derived entity roster for grounding names
	ProgressFunc func(msg string)
	ContextWindow int // Model context window in tokens (0 = use default 32768)
}

// computeMaxContentBytes calculates the maximum file content size (in bytes) that
// fits within the model's context window after reserving space for the output tokens
// and the static prompt overhead.
func computeMaxContentBytes(contextWindow, maxTokens, staticPromptBytes int) int {
	staticTokens := staticPromptBytes / bytesPerToken
	contentTokens := contextWindow - maxTokens - staticTokens
	if contentTokens < 512 {
		contentTokens = 512
	}
	return contentTokens * bytesPerToken
}

// computeMaxTokens calculates dynamic max_tokens for the LLM response based on
// the number of ctags symbols in the file. More symbols → more entities → more output.
// Each symbol can produce an entity (~50 tokens), 4-10 facts (~150 tokens each),
// and relationships (~50 tokens each), so ~500 tokens per symbol is a reasonable estimate.
func computeMaxTokens(symbolCount int) int {
	tokens := 4096 + symbolCount*512
	if tokens > 16384 {
		return 16384
	}
	return tokens
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
	var completed atomic.Int64
	phase2Start := time.Now()

	progress := func(file string, done bool, failed bool) {
		c := completed.Load()
		if done {
			c = completed.Add(1)
		}
		if cfg.ProgressFunc == nil {
			return
		}
		if failed {
			cfg.ProgressFunc(fmt.Sprintf("Phase 2: [%d/%d] FAILED %s", c, totalJobs, file))
			return
		}
		if !done {
			cfg.ProgressFunc(fmt.Sprintf("Phase 2: [%d/%d] Analyzing %s...", c+1, totalJobs, file))
			return
		}
		// Calculate ETA
		eta := ""
		if c > 0 {
			elapsed := time.Since(phase2Start)
			perFile := elapsed / time.Duration(c)
			remaining := perFile * time.Duration(int64(totalJobs)-c)
			if remaining > time.Second {
				eta = fmt.Sprintf(" — ETA %s", remaining.Round(time.Second))
			}
		}
		cfg.ProgressFunc(fmt.Sprintf("Phase 2: [%d/%d] Done %s%s", c, totalJobs, file, eta))
	}

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
			c := completed.Load()
			fmt.Printf("  [%d/%d] Analyzing %s...\n", c+1, totalJobs, job.Target)
			progress(job.Target, false, false)
			fileDeferred, err := processFile(gctx, cfg, job, entityStore, factStore, relStore, stats)
			if err != nil {
				jobStore.Fail(gctx, job.ID, err.Error())
				progress(job.Target, true, true)
				fmt.Printf("  [FAILED] %s: %v\n", job.Target, err)
				return nil // don't cancel other workers
			}
			if len(fileDeferred) > 0 {
				deferredMu.Lock()
				deferred = append(deferred, fileDeferred...)
				deferredMu.Unlock()
			}
			progress(job.Target, true, false)
			c = completed.Load()
			fmt.Printf("  [%d/%d] Done %s\n", c, totalJobs, job.Target)
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

	// Count ctags symbols for this file to size the output budget
	symbolCount := 0
	for _, e := range cfg.Roster {
		if e.Path == job.Target {
			symbolCount++
		}
	}
	maxTokens := computeMaxTokens(symbolCount)

	// Measure static prompt overhead (prompt with empty content)
	staticPrompt := Phase2Prompt(job.Target, fi.Language, cfg.RepoName, cfg.Manifest.Stack, "", cfg.Roster)
	contextWindow := cfg.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 32768
	}
	maxContentBytes := computeMaxContentBytes(contextWindow, maxTokens, len(systemPromptPhase2)+len(staticPrompt))

	// Truncate file content if it exceeds the budget
	fileContent := string(content)
	if len(fileContent) > maxContentBytes {
		log.Printf("[phase2] %s: truncating content from %d to %d bytes (context window: %d tokens)",
			job.Target, len(fileContent), maxContentBytes, contextWindow)
		fileContent = fileContent[:maxContentBytes]
	}

	prompt := Phase2Prompt(job.Target, fi.Language, cfg.RepoName, cfg.Manifest.Stack, fileContent, cfg.Roster)

	// Parse retry loop: retry on parse failures with increased max_tokens
	const maxParseAttempts = 3
	var result *Phase2Result
	var resp *llm.Response
	var attempts int
	currentMaxTokens := maxTokens
	tokensCap := contextWindow / 4

	for attempt := 1; attempt <= maxParseAttempts; attempt++ {
		resp, attempts, err = callLLMWithRetry(ctx, cfg.LLM, cfg.Model, systemPromptPhase2, []llm.Message{
			{Role: "user", Content: prompt},
		}, currentMaxTokens, SchemaPhase2, DefaultRetryConfig)
		if err != nil {
			return nil, fmt.Errorf("LLM call: %w", err)
		}

		result, err = ParsePhase2(resp.Content)
		if err == nil {
			break // success
		}

		if attempt < maxParseAttempts {
			log.Printf("[phase2] %s: parse attempt %d/%d failed, bumping max_tokens %d → %d",
				job.Target, attempt, maxParseAttempts, currentMaxTokens, currentMaxTokens*3/2)
			currentMaxTokens = currentMaxTokens * 3 / 2
			if currentMaxTokens > tokensCap {
				currentMaxTokens = tokensCap
			}
		}
	}
	if err != nil {
		cleaned := CleanJSON(resp.Content)
		preview := cleaned
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return nil, fmt.Errorf("parsing response after %d attempts: %w\n  raw preview: %s", maxParseAttempts, err, preview)
	}

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

		// Fast fuzzy dedup: normalized name + same kind + same owner → skip LLM call
		if matchID, matched := FastFuzzyMatch(ctx, entityStore, cfg.RepoID, ext); matched {
			entityMap[ext.QualifiedName] = matchID
			logVerboseF("[phase2] %s: entity %q → SKIP (fast fuzzy match)", job.Target, ext.QualifiedName)
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

	// Store file summary as a fact on the first entity in this file
	if result.FileSummary != "" && len(entityMap) > 0 {
		// Use the first entity from this file's extraction as the anchor
		var summaryEntityID uuid.UUID
		for _, ext := range result.Entities {
			if id, ok := entityMap[ext.QualifiedName]; ok {
				summaryEntityID = id
				break
			}
		}
		if summaryEntityID != uuid.Nil {
			summaryFact := &models.Fact{
				EntityID:   summaryEntityID,
				RepoID:     cfg.RepoID,
				Claim:      fmt.Sprintf("File %s: %s", job.Target, result.FileSummary),
				Dimension:  models.DimensionWhat,
				Category:   models.CategoryBehavior,
				Confidence: models.ConfidenceMedium,
				Provenance: []models.Provenance{{
					SourceType: "file",
					Repo:       cfg.RepoName,
					Ref:        job.Target,
					AnalyzedAt: job.CreatedAt.Format("2006-01-02T15:04:05Z"),
				}},
			}
			if err := factStore.Create(ctx, summaryFact); err != nil {
				logVerboseF("[phase2] warn: creating file summary fact: %v", err)
			} else {
				stats.FactsCreated++
			}
		}
	}

	// Store facts — track which entities receive facts for orphan diagnostics
	entitiesWithFacts := make(map[uuid.UUID]int)    // entity ID → fact count
	factsSkippedNoEntity := 0
	factsSkippedError := 0
	var unresolvedFactNames []string
	for _, ext := range result.Facts {
		entityID, ok := resolveEntityWithMap(ctx, entityStore, cfg.RepoID, ext.EntityName, entityMap)
		if !ok {
			factsSkippedNoEntity++
			unresolvedFactNames = append(unresolvedFactNames, ext.EntityName)
			logVerboseF("[phase2] %s: fact skipped (entity not found): %q → claim: %q", job.Target, ext.EntityName, truncStr(ext.Claim, 80))
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
			factsSkippedError++
			logVerboseF("[phase2] warn: creating fact: %v", err)
			continue
		}
		entitiesWithFacts[entityID]++
		stats.FactsCreated++
	}

	// Orphan diagnostics: log entities that got 0 facts from the LLM
	var orphanNames []string
	llmFactNames := make(map[string]bool)
	for _, f := range result.Facts {
		llmFactNames[f.EntityName] = true
	}
	for _, ext := range result.Entities {
		eid, ok := entityMap[ext.QualifiedName]
		if !ok {
			continue
		}
		if entitiesWithFacts[eid] == 0 {
			orphanNames = append(orphanNames, ext.QualifiedName)
			// Determine why: did the LLM emit any facts targeting this entity?
			if llmFactNames[ext.QualifiedName] {
				logVerboseF("[phase2-orphan] %s: %q — LLM emitted facts but name resolution failed", job.Target, ext.QualifiedName)
			} else {
				logVerboseF("[phase2-orphan] %s: %q — LLM emitted ZERO facts for this entity", job.Target, ext.QualifiedName)
			}
		}
	}
	if len(orphanNames) > 0 || factsSkippedNoEntity > 0 {
		log.Printf("[phase2-orphan] %s: %d/%d entities got 0 facts, %d facts skipped (unresolved name), %d facts skipped (db error)",
			job.Target, len(orphanNames), len(result.Entities), factsSkippedNoEntity, factsSkippedError)
		if len(unresolvedFactNames) > 0 {
			// Deduplicate for log readability
			seen := make(map[string]bool)
			var unique []string
			for _, n := range unresolvedFactNames {
				if !seen[n] {
					seen[n] = true
					unique = append(unique, n)
				}
			}
			log.Printf("[phase2-orphan] %s: unresolved fact entity_names: %v", job.Target, unique)
		}
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

// pipelineVerbose is set by the orchestrator to control verbose logging.
var pipelineVerbose bool

func logVerboseF(format string, args ...any) {
	if pipelineVerbose {
		fmt.Printf(format+"\n", args...)
	}
}

func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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
