package pipeline

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	gitpkg "github.com/tgeorge06/atlaskb/internal/git"
	ghpkg "github.com/tgeorge06/atlaskb/internal/github"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type OrchestratorConfig struct {
	RepoPath          string
	Force             bool
	DryRun            bool
	Concurrency       int
	ExtractionModel   string
	SynthesisModel    string
	Pool              *pgxpool.Pool
	LLM               llm.Client
	Embedder          embeddings.Client
	Verbose           bool
	GitLogLimit       int
	Phases            []string // If non-empty, only run these phases (e.g. ["phase4"])
	ProgressFunc      func(msg string) // Optional callback for progress updates
	GlobalExcludeDirs []string // Global dirs to exclude (from config)
	GitHubClient      *ghpkg.Client    // nil if no GitHub token configured
	GitHubMaxPRs      int
	GitHubPRBatchSize int
}

func (cfg *OrchestratorConfig) progress(msg string) {
	if cfg.ProgressFunc != nil {
		cfg.ProgressFunc(msg)
	}
}

type OrchestratorResult struct {
	RepoID          uuid.UUID
	RepoName        string
	Phase2Stats     *Phase2Stats
	QualityScore    *QualityScore
	CostEstimate    CostEstimate
	TotalTokens     int
	TotalCostUSD    float64
	Duration        time.Duration
}

// shouldRunPhase checks if a phase should be run given the Phases filter.
// If Phases is empty, all phases run.
func shouldRunPhase(phases []string, phase string) bool {
	if len(phases) == 0 {
		return true
	}
	for _, p := range phases {
		if p == phase {
			return true
		}
	}
	return false
}

func Orchestrate(ctx context.Context, cfg OrchestratorConfig) (*OrchestratorResult, error) {
	pipelineVerbose = cfg.Verbose
	start := time.Now()
	result := &OrchestratorResult{}

	// Set up signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Detect repo
	repoInfo, err := gitpkg.DetectRepo(cfg.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("detecting repo: %w", err)
	}

	repoName := extractRepoName(repoInfo)
	result.RepoName = repoName

	if cfg.Verbose {
		fmt.Printf("Repository: %s\n", repoName)
		fmt.Printf("Path: %s\n", repoInfo.RootPath)
		fmt.Printf("Branch: %s\n", repoInfo.DefaultBranch)
		fmt.Printf("HEAD: %s\n", repoInfo.HeadCommitSHA)
		fmt.Println()
	}

	// Get or create repo record
	repoStore := &models.RepoStore{Pool: cfg.Pool}
	repo, err := repoStore.GetByPath(ctx, repoInfo.RootPath)
	if err != nil {
		return nil, fmt.Errorf("querying repo: %w", err)
	}
	if repo == nil {
		repo = &models.Repo{
			Name:          repoName,
			RemoteURL:     models.Ptr(repoInfo.RemoteURL),
			LocalPath:     repoInfo.RootPath,
			DefaultBranch: repoInfo.DefaultBranch,
		}
		if err := repoStore.Create(ctx, repo); err != nil {
			return nil, fmt.Errorf("creating repo: %w", err)
		}
	}
	result.RepoID = repo.ID

	// Determine run mode
	runMode := "incremental"
	if cfg.Force {
		runMode = "full"
	}
	if len(cfg.Phases) > 0 {
		runMode = "partial"
	}

	// Create indexing run record
	runStore := &models.IndexingRunStore{Pool: cfg.Pool}
	indexingRun := &models.IndexingRun{
		RepoID:          repo.ID,
		CommitSHA:       models.Ptr(repoInfo.HeadCommitSHA),
		Mode:            runMode,
		ModelExtraction: models.Ptr(cfg.ExtractionModel),
		ModelSynthesis:  models.Ptr(cfg.SynthesisModel),
		Concurrency:     models.Ptr(cfg.Concurrency),
	}
	if err := runStore.Create(ctx, indexingRun); err != nil {
		log.Printf("[warn] failed to create indexing run record: %v", err)
	}

	// --force: clear all existing data for this repo
	if cfg.Force {
		log.Println("[force] Clearing existing data...")
		factStore := &models.FactStore{Pool: cfg.Pool}
		relStore := &models.RelationshipStore{Pool: cfg.Pool}
		decisionStore := &models.DecisionStore{Pool: cfg.Pool}
		entityStore := &models.EntityStore{Pool: cfg.Pool}
		jobStore := &models.JobStore{Pool: cfg.Pool}

		// Order matters for FK constraints
		factStore.DeleteByRepo(ctx, repo.ID)
		relStore.DeleteByRepo(ctx, repo.ID)
		decisionStore.DeleteByRepo(ctx, repo.ID)
		entityStore.DeleteByRepo(ctx, repo.ID)
		jobStore.DeleteByRepo(ctx, repo.ID)
		log.Println("[force] Done, re-extracting from scratch")
	}

	// Merge global + per-repo exclude dirs
	excludeDirs := mergeExcludeDirs(cfg.GlobalExcludeDirs, repo.ExcludeDirs)

	// Phase 1: Structural inventory (always runs unless phases filter excludes it)
	var manifest *Manifest
	if shouldRunPhase(cfg.Phases, "phase1") || len(cfg.Phases) == 0 {
		fmt.Println("Phase 1: Structural inventory...")
		cfg.progress("Phase 1: Structural inventory...")
		manifest, err = RunPhase1(repoInfo.RootPath, excludeDirs)
		if err != nil {
			return nil, fmt.Errorf("phase 1: %w", err)
		}

		fmt.Printf("  Files: %d total, %d analyzable\n", manifest.Stats.TotalFiles, manifest.Stats.AnalyzableFiles)
		fmt.Printf("  Stack: %v\n", manifest.Stack.Languages)
		cfg.progress(fmt.Sprintf("Phase 1 complete: %d files (%d analyzable), stack: %v", manifest.Stats.TotalFiles, manifest.Stats.AnalyzableFiles, manifest.Stack.Languages))

		indexingRun.FilesTotal = models.Ptr(manifest.Stats.TotalFiles)

		// Cost estimate
		costEst := EstimateCost(manifest)
		result.CostEstimate = costEst
		fmt.Println()
		fmt.Println(FormatCost(costEst))
		fmt.Println()

		// Stale entity cleanup: remove entities whose paths no longer exist in the repo
		if !cfg.Force {
			entityStore := &models.EntityStore{Pool: cfg.Pool}
			stalePaths, _ := entityStore.ListDistinctPaths(ctx, repo.ID)
			if len(stalePaths) > 0 {
				// Build a set of current file paths
				currentFiles := make(map[string]bool)
				for _, fi := range manifest.Files {
					currentFiles[fi.Path] = true
				}

				staleCount := 0
				for _, p := range stalePaths {
					if !currentFiles[p] {
						if err := entityStore.DeleteByPath(ctx, repo.ID, p); err != nil {
							log.Printf("[stale] warn: cleaning up %s: %v", p, err)
						} else {
							staleCount++
							logVerboseF("[stale] removed entities for deleted file: %s", p)
						}
					}
				}
				if staleCount > 0 {
					fmt.Printf("  Cleaned up entities from %d deleted/renamed files\n", staleCount)
				}
			}
		}

		// Structured dependency parsing (deterministic, no LLM)
		deps := ExtractDependencies(repoInfo.RootPath, manifest)
		if len(deps) > 0 {
			entityStore := &models.EntityStore{Pool: cfg.Pool}
			relStore := &models.RelationshipStore{Pool: cfg.Pool}
			factStore := &models.FactStore{Pool: cfg.Pool}

			// Find or create repo entity
			repoEntity, _ := entityStore.FindByQualifiedName(ctx, repo.ID, repoName)
			if repoEntity == nil {
				repoEntity = &models.Entity{
					RepoID:        repo.ID,
					Kind:          models.EntityService,
					Name:          repoName,
					QualifiedName: repoName,
					Summary:       models.Ptr("Repository root entity"),
				}
				entityStore.Upsert(ctx, repoEntity)
			}

			directCount, indirectCount := 0, 0
			for _, dep := range deps {
				if dep.Dev {
					indirectCount++
				} else {
					directCount++
				}

				// Create entity for the dependency
				depEntity := &models.Entity{
					RepoID:        repo.ID,
					Kind:          models.EntityModule,
					Name:          dep.Name,
					QualifiedName: dep.Name,
					Summary:       models.Ptr(fmt.Sprintf("External dependency from %s", dep.Source)),
				}
				if err := entityStore.Upsert(ctx, depEntity); err != nil {
					logVerboseF("[deps] warn: upserting dep entity %s: %v", dep.Name, err)
					continue
				}

				// Create depends_on relationship
				rel := &models.Relationship{
					RepoID:       repo.ID,
					FromEntityID: repoEntity.ID,
					ToEntityID:   depEntity.ID,
					Kind:         models.RelDependsOn,
					Description:  models.Ptr(fmt.Sprintf("Dependency from %s", dep.Source)),
					Strength:     models.StrengthStrong,
					Provenance: []models.Provenance{{
						SourceType: "file",
						Repo:       repoName,
						Ref:        dep.Source,
					}},
				}
				relStore.Upsert(ctx, rel)

				// Create version fact if available
				if dep.Version != "" {
					devStr := ""
					if dep.Dev {
						devStr = " (dev/indirect)"
					}
					fact := &models.Fact{
						EntityID:   depEntity.ID,
						RepoID:     repo.ID,
						Claim:      fmt.Sprintf("Required at version %s%s", dep.Version, devStr),
						Dimension:  models.DimensionWhat,
						Category:   models.CategoryConstraint,
						Confidence: models.ConfidenceHigh,
						Provenance: []models.Provenance{{
							SourceType: "file",
							Repo:       repoName,
							Ref:        dep.Source,
						}},
					}
					factStore.Create(ctx, fact)
				}

				logVerboseF("[deps] %s: %s %s", dep.Source, dep.Name, dep.Version)
			}

			log.Printf("[deps] parsed %d dependencies (%d direct, %d dev/indirect) from manifest files",
				len(deps), directCount, indirectCount)

			// Auto-discover cross-repo links by matching deps against indexed repos
			crossCreated, crossSkipped := DiscoverCrossRepoLinks(ctx, cfg.Pool, repo.ID, repoName, deps)
			if crossCreated > 0 || crossSkipped > 0 {
				log.Printf("[cross-repo] discovered %d cross-repo links (%d skipped)", crossCreated, crossSkipped)
				cfg.progress(fmt.Sprintf("Cross-repo: discovered %d links", crossCreated))
			}
		}
	}

	// Phase 1.5: Ctags structural extraction (provides ground-truth entity names)
	var entityRoster []EntityEntry
	if shouldRunPhase(cfg.Phases, "phase1.5") || shouldRunPhase(cfg.Phases, "phase2") || len(cfg.Phases) == 0 {
		fmt.Println("Phase 1.5: Extracting structural symbols...")
		cfg.progress("Phase 1.5: Extracting structural symbols...")
		symbols, ctagsErr := RunCtags(cfg.RepoPath)
		if ctagsErr != nil {
			fmt.Printf("  Warning: ctags extraction failed: %v\n", ctagsErr)
		} else if symbols == nil {
			fmt.Println("  ctags not installed — skipping (install with: brew install universal-ctags)")
		} else {
			entityRoster = BuildEntityRoster(symbols)
			fmt.Printf("  Discovered %d symbols across %d files\n", len(entityRoster), len(symbols))
		}
	}

	if cfg.DryRun {
		fmt.Println("Dry run — stopping before LLM calls.")
		return result, nil
	}

	// Check context before each phase
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("cancelled: %w", err)
	}

	// Track whether Phase 2 processed any files (for smart update logic)
	phase2Processed := false

	// Fetch model context window for dynamic token budgeting (used by Phase 2 + Backfill)
	contextWindow := 32768
	if shouldRunPhase(cfg.Phases, "phase2") || shouldRunPhase(cfg.Phases, "backfill") {
		if cw, err := cfg.LLM.GetContextWindow(ctx, cfg.ExtractionModel); err == nil && cw > 0 {
			contextWindow = cw
			log.Printf("[config] model context window: %d tokens", contextWindow)
		}
	}

	// Phase 2: File analysis
	if shouldRunPhase(cfg.Phases, "phase2") {
		if manifest == nil {
			// Need manifest for phase2 even if phase1 was skipped
			manifest, err = RunPhase1(repoInfo.RootPath, excludeDirs)
			if err != nil {
				return nil, fmt.Errorf("phase 1 (for phase 2): %w", err)
			}
		}

		fmt.Println("Phase 2: File analysis...")
		cfg.progress("Phase 2: LLM file analysis...")
		phase2Stats, err := RunPhase2(ctx, Phase2Config{
			RepoID:        repo.ID,
			RepoName:      repoName,
			RepoPath:      repoInfo.RootPath,
			Manifest:      manifest,
			Model:         cfg.ExtractionModel,
			Concurrency:   cfg.Concurrency,
			Pool:          cfg.Pool,
			LLM:           cfg.LLM,
			Roster:        entityRoster,
			ProgressFunc:  cfg.ProgressFunc,
			ContextWindow: contextWindow,
		})
		if err != nil {
			return result, fmt.Errorf("phase 2: %w", err)
		}
		result.Phase2Stats = phase2Stats
		fmt.Printf("  Processed: %d files, Skipped: %d, Entities: %d, Facts: %d\n",
			phase2Stats.FilesProcessed, phase2Stats.FilesSkipped, phase2Stats.EntitiesCreated, phase2Stats.FactsCreated)
		cfg.progress(fmt.Sprintf("Phase 2 complete: %d files analyzed, %d skipped, %d entities, %d facts",
			phase2Stats.FilesProcessed, phase2Stats.FilesSkipped, phase2Stats.EntitiesCreated, phase2Stats.FactsCreated))

		indexingRun.FilesAnalyzed = models.Ptr(phase2Stats.FilesProcessed)
		indexingRun.FilesSkipped = models.Ptr(phase2Stats.FilesSkipped)
		indexingRun.EntitiesCreated = models.Ptr(phase2Stats.EntitiesCreated)
		indexingRun.FactsCreated = models.Ptr(phase2Stats.FactsCreated)

		if phase2Stats.FilesProcessed > 0 {
			phase2Processed = true
		}
	}

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Smart update: if no files changed in Phase 2 and this is an incremental run (not --force, no --phase),
	// skip later phases since the data is already up to date.
	if !phase2Processed && runMode == "incremental" && len(cfg.Phases) == 0 {
		fmt.Println("\nNo files changed — knowledge base is up to date.")
		cfg.progress("No files changed — knowledge base is up to date.")
		indexingRun.DurationMS = models.Ptr(time.Since(start).Milliseconds())
		runStore.Complete(ctx, indexingRun)
		result.Duration = time.Since(start)
		return result, nil
	}

	// Phase 2.5: Backfill orphan entities (entities with no facts)
	if shouldRunPhase(cfg.Phases, "backfill") || shouldRunPhase(cfg.Phases, "phase2") {
		fmt.Println("Phase 2.5: Backfill orphan entities...")
		cfg.progress("Phase 2.5: Backfilling orphan entities...")
		backfillStats, err := RunBackfill(ctx, BackfillConfig{
			RepoID:        repo.ID,
			RepoName:      repoName,
			RepoPath:      repoInfo.RootPath,
			Model:         cfg.ExtractionModel,
			Concurrency:   cfg.Concurrency,
			Pool:          cfg.Pool,
			LLM:           cfg.LLM,
			ContextWindow: contextWindow,
		})
		if err != nil {
			fmt.Printf("  Warning: backfill failed: %v\n", err)
		} else if backfillStats.OrphanEntities > 0 {
			fmt.Printf("  Backfilled: %d facts, %d relationships for %d orphan entities\n",
				backfillStats.FactsCreated, backfillStats.RelsCreated, backfillStats.OrphanEntities)
			indexingRun.OrphanEntities = models.Ptr(backfillStats.OrphanEntities)
			indexingRun.BackfillFacts = models.Ptr(backfillStats.FactsCreated)
			indexingRun.BackfillRels = models.Ptr(backfillStats.RelsCreated)
		} else {
			fmt.Println("  No orphan entities found.")
		}
	}

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Git log analysis
	if shouldRunPhase(cfg.Phases, "gitlog") || len(cfg.Phases) == 0 {
		fmt.Println("Git log analysis...")
		cfg.progress("Analyzing git history...")
		if err := RunGitLogAnalysis(ctx, GitLogConfig{
			RepoID:      repo.ID,
			RepoName:    repoName,
			RepoPath:    repoInfo.RootPath,
			Model:       cfg.ExtractionModel,
			Pool:        cfg.Pool,
			LLM:         cfg.LLM,
			GitLogLimit: cfg.GitLogLimit,
		}); err != nil {
			fmt.Printf("  Warning: git log analysis failed: %v\n", err)
		}
	}

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Phase 3: GitHub PR/issue mining
	if shouldRunPhase(cfg.Phases, "phase3") || len(cfg.Phases) == 0 {
		fmt.Println("Phase 3: GitHub PR/issue mining...")
		cfg.progress("Phase 3: GitHub PR/issue mining...")

		remoteURL := ""
		if repo.RemoteURL != nil {
			remoteURL = *repo.RemoteURL
		}

		if err := RunPhase3(ctx, Phase3Config{
			RepoID:       repo.ID,
			RepoName:     repoName,
			RemoteURL:    remoteURL,
			Model:        cfg.ExtractionModel,
			Pool:         cfg.Pool,
			LLM:          cfg.LLM,
			GitHub:       cfg.GitHubClient,
			MaxPRs:       cfg.GitHubMaxPRs,
			PRBatchSize:  cfg.GitHubPRBatchSize,
			ProgressFunc: cfg.ProgressFunc,
		}); err != nil {
			fmt.Printf("  Warning: Phase 3 GitHub PR mining failed: %v\n", err)
		}
	}

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Phase 4: Cross-module synthesis
	if shouldRunPhase(cfg.Phases, "phase4") {
		// If re-running phase4, reset the existing job so it can re-run
		if len(cfg.Phases) > 0 {
			jobStore := &models.JobStore{Pool: cfg.Pool}
			existing, _ := jobStore.GetByTarget(ctx, repo.ID, models.PhasePhase4, "synthesis")
			if existing != nil && existing.Status == models.JobCompleted {
				// Delete the old completed job so phase4 will run fresh
				cfg.Pool.Exec(ctx, `DELETE FROM extraction_jobs WHERE id = $1`, existing.ID)
			}
		}

		fmt.Println("Phase 4: Cross-module synthesis...")
		cfg.progress("Phase 4: Cross-module LLM synthesis...")
		if err := RunPhase4(ctx, Phase4Config{
			RepoID:   repo.ID,
			RepoName: repoName,
			Model:    cfg.SynthesisModel,
			Pool:     cfg.Pool,
			LLM:      cfg.LLM,
		}); err != nil {
			fmt.Printf("  Warning: phase 4 synthesis failed: %v\n", err)
		}
	}

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Phase 5: Repo summary
	if shouldRunPhase(cfg.Phases, "phase5") {
		// If re-running phase5, reset the existing job
		if len(cfg.Phases) > 0 {
			jobStore := &models.JobStore{Pool: cfg.Pool}
			existing, _ := jobStore.GetByTarget(ctx, repo.ID, models.PhasePhase5, "summary")
			if existing != nil && existing.Status == models.JobCompleted {
				cfg.Pool.Exec(ctx, `DELETE FROM extraction_jobs WHERE id = $1`, existing.ID)
			}
		}

		fmt.Println("Phase 5: Repository summary...")
		cfg.progress("Phase 5: Generating repository summary...")
		if err := RunPhase5(ctx, Phase5Config{
			RepoID:   repo.ID,
			RepoName: repoName,
			Model:    cfg.SynthesisModel,
			Pool:     cfg.Pool,
			LLM:      cfg.LLM,
		}); err != nil {
			fmt.Printf("  Warning: phase 5 summary failed: %v\n", err)
		}
	}

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Generate embeddings for all facts
	if shouldRunPhase(cfg.Phases, "embedding") || len(cfg.Phases) == 0 {
		fmt.Println("Generating embeddings...")
		cfg.progress("Generating embeddings...")
		if err := generateEmbeddings(ctx, cfg.Pool, cfg.Embedder, repo.ID); err != nil {
			fmt.Printf("  Warning: embedding generation failed: %v\n", err)
		}
	}

	// Compute quality score
	fmt.Println("Computing quality score...")
	cfg.progress("Computing quality score...")
	qs, err := ComputeQuality(ctx, cfg.Pool, repo.ID)
	if err != nil {
		fmt.Printf("  Warning: quality score computation failed: %v\n", err)
	} else {
		result.QualityScore = qs
		fmt.Printf("  %s\n", FormatQualityScore(qs))
		fmt.Println()
		fmt.Print(FormatQualityDetails(qs))
		cfg.progress(fmt.Sprintf("Quality: %.0f%%", qs.Overall*100))

		indexingRun.QualityOverall = models.Ptr(qs.Overall)
		indexingRun.QualityEntityCov = models.Ptr(qs.EntityCoverage)
		indexingRun.QualityFactDensity = models.Ptr(qs.FactDensity)
		indexingRun.QualityRelConnect = models.Ptr(qs.RelConnectivity)
		indexingRun.QualityDimCoverage = models.Ptr(qs.DimensionCoverage)
		indexingRun.QualityParseRate = models.Ptr(qs.ParseSuccessRate)
	}

	// Update repo record
	repoStore.UpdateLastIndexed(ctx, repo.ID, repoInfo.HeadCommitSHA)

	result.Duration = time.Since(start)
	indexingRun.DurationMS = models.Ptr(result.Duration.Milliseconds())

	// Persist indexing run metrics
	if err := runStore.Complete(ctx, indexingRun); err != nil {
		log.Printf("[warn] failed to complete indexing run record: %v", err)
	}

	fmt.Printf("\nIndexing complete in %s\n", result.Duration.Round(time.Second))

	return result, nil
}

func generateEmbeddings(ctx context.Context, pool *pgxpool.Pool, embedder embeddings.Client, repoID uuid.UUID) error {
	factStore := &models.FactStore{Pool: pool}

	facts, err := factStore.ListByRepoWithoutEmbedding(ctx, repoID)
	if err != nil {
		return err
	}

	if len(facts) == 0 {
		fmt.Println("  No new facts to embed.")
		return nil
	}

	fmt.Printf("  Embedding %d facts...\n", len(facts))

	// Batch embed
	claims := make([]string, len(facts))
	for i, f := range facts {
		claims[i] = f.Claim
	}

	vectors, err := embedder.Embed(ctx, claims, embeddings.DefaultModel)
	if err != nil {
		return fmt.Errorf("embedding: %w", err)
	}

	for i, f := range facts {
		if i < len(vectors) && len(vectors[i]) > 0 {
			vec := pgvector.NewVector(vectors[i])
			if err := factStore.UpdateEmbedding(ctx, f.ID, vec); err != nil {
				logVerboseF("warn: updating embedding for fact %s: %v", f.ID, err)
			}
		}
	}

	return nil
}

func extractRepoName(info *gitpkg.RepoInfo) string {
	if info.RemoteURL != "" {
		// Parse from remote URL
		url := info.RemoteURL
		// Strip .git suffix
		if len(url) > 4 && url[len(url)-4:] == ".git" {
			url = url[:len(url)-4]
		}
		// Get last path component
		for i := len(url) - 1; i >= 0; i-- {
			if url[i] == '/' || url[i] == ':' {
				return url[i+1:]
			}
		}
	}
	// Fallback to directory name
	parts := splitPath(info.RootPath)
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

func mergeExcludeDirs(global, perRepo []string) []string {
	seen := make(map[string]bool)
	var merged []string
	for _, d := range global {
		if !seen[d] {
			seen[d] = true
			merged = append(merged, d)
		}
	}
	for _, d := range perRepo {
		if !seen[d] {
			seen[d] = true
			merged = append(merged, d)
		}
	}
	return merged
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range []byte(path) {
		if p == '/' {
			continue
		}
		if len(parts) == 0 || path[len(path)-1] == '/' {
			parts = append(parts, "")
		}
	}
	// Simple split
	result := []string{}
	current := ""
	for _, c := range path {
		if c == '/' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
