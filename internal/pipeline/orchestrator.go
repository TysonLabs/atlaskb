package pipeline

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	gitpkg "github.com/tgeorge06/atlaskb/internal/git"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type OrchestratorConfig struct {
	RepoPath        string
	Force           bool
	DryRun          bool
	Concurrency     int
	ExtractionModel string
	SynthesisModel  string
	Pool            *pgxpool.Pool
	LLM             llm.Client
	Embedder        embeddings.Client
	Verbose         bool
}

type OrchestratorResult struct {
	RepoID          uuid.UUID
	RepoName        string
	Phase2Stats     *Phase2Stats
	CostEstimate    CostEstimate
	TotalTokens     int
	TotalCostUSD    float64
	Duration        time.Duration
}

func Orchestrate(ctx context.Context, cfg OrchestratorConfig) (*OrchestratorResult, error) {
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

	// Phase 1: Structural inventory
	fmt.Println("Phase 1: Structural inventory...")
	manifest, err := RunPhase1(repoInfo.RootPath)
	if err != nil {
		return nil, fmt.Errorf("phase 1: %w", err)
	}

	fmt.Printf("  Files: %d total, %d analyzable\n", manifest.Stats.TotalFiles, manifest.Stats.AnalyzableFiles)
	fmt.Printf("  Stack: %v\n", manifest.Stack.Languages)

	// Cost estimate
	costEst := EstimateCost(manifest)
	result.CostEstimate = costEst
	fmt.Println()
	fmt.Println(FormatCost(costEst))
	fmt.Println()

	if cfg.DryRun {
		fmt.Println("Dry run — stopping before LLM calls.")
		return result, nil
	}

	// Check context before each phase
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("cancelled: %w", err)
	}

	// Phase 2: File analysis
	fmt.Println("Phase 2: File analysis...")
	phase2Stats, err := RunPhase2(ctx, Phase2Config{
		RepoID:      repo.ID,
		RepoName:    repoName,
		RepoPath:    repoInfo.RootPath,
		Manifest:    manifest,
		Model:       cfg.ExtractionModel,
		Concurrency: cfg.Concurrency,
		Pool:        cfg.Pool,
		LLM:         cfg.LLM,
	})
	if err != nil {
		return result, fmt.Errorf("phase 2: %w", err)
	}
	result.Phase2Stats = phase2Stats
	fmt.Printf("  Processed: %d files, Skipped: %d, Entities: %d, Facts: %d\n",
		phase2Stats.FilesProcessed, phase2Stats.FilesSkipped, phase2Stats.EntitiesCreated, phase2Stats.FactsCreated)

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Git log analysis (runs alongside phase 2 conceptually, but after for simplicity)
	fmt.Println("Git log analysis...")
	if err := RunGitLogAnalysis(ctx, GitLogConfig{
		RepoID:   repo.ID,
		RepoName: repoName,
		RepoPath: repoInfo.RootPath,
		Model:    cfg.ExtractionModel,
		Pool:     cfg.Pool,
		LLM:      cfg.LLM,
	}); err != nil {
		fmt.Printf("  Warning: git log analysis failed: %v\n", err)
	}

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Phase 4: Cross-module synthesis
	fmt.Println("Phase 4: Cross-module synthesis...")
	if err := RunPhase4(ctx, Phase4Config{
		RepoID:   repo.ID,
		RepoName: repoName,
		Model:    cfg.SynthesisModel,
		Pool:     cfg.Pool,
		LLM:      cfg.LLM,
	}); err != nil {
		return result, fmt.Errorf("phase 4: %w", err)
	}

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Phase 5: Repo summary (non-fatal — data is still queryable without it)
	fmt.Println("Phase 5: Repository summary...")
	if err := RunPhase5(ctx, Phase5Config{
		RepoID:   repo.ID,
		RepoName: repoName,
		Model:    cfg.SynthesisModel,
		Pool:     cfg.Pool,
		LLM:      cfg.LLM,
	}); err != nil {
		fmt.Printf("  Warning: phase 5 summary failed: %v\n", err)
	}

	if err := ctx.Err(); err != nil {
		fmt.Println("\nInterrupted — progress saved. Re-run to continue.")
		return result, nil
	}

	// Generate embeddings for all facts
	fmt.Println("Generating embeddings...")
	if err := generateEmbeddings(ctx, cfg.Pool, cfg.Embedder, repo.ID); err != nil {
		fmt.Printf("  Warning: embedding generation failed: %v\n", err)
	}

	// Update repo record
	repoStore.UpdateLastIndexed(ctx, repo.ID, repoInfo.HeadCommitSHA)

	result.Duration = time.Since(start)
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
