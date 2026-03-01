package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	ghpkg "github.com/tgeorge06/atlaskb/internal/github"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/pipeline"
)

var (
	indexDryRun      bool
	indexForce       bool
	indexYes         bool
	indexConcurrency int
	indexPhases      []string
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a repository",
	Long:  "Analyze a local repository and extract knowledge into the graph.",
	Args:  cobra.ExactArgs(1),
	RunE:  runIndex,
}

func init() {
	indexCmd.Flags().BoolVar(&indexDryRun, "dry-run", false, "estimate cost without making LLM calls")
	indexCmd.Flags().BoolVar(&indexForce, "force", false, "re-analyze all files even if unchanged")
	indexCmd.Flags().BoolVarP(&indexYes, "yes", "y", false, "skip confirmation prompts")
	indexCmd.Flags().IntVar(&indexConcurrency, "concurrency", 0, "number of parallel LLM calls (default from config)")
	indexCmd.Flags().StringSliceVar(&indexPhases, "phase", nil, "run only specific phases (phase1, phase2, backfill, gitlog, phase3, phase4, phase5, embedding)")
	rootCmd.AddCommand(indexCmd)
}

func runIndex(cmd *cobra.Command, args []string) error {
	repoPath := args[0]

	concurrency := cfg.Pipeline.Concurrency
	if indexConcurrency > 0 {
		concurrency = indexConcurrency
	}

	llmClient := llm.NewOpenAIClient(cfg.LLM.BaseURL, cfg.LLM.APIKey)
	embedClient := embeddings.NewOpenAIClient(cfg.Embeddings.BaseURL, cfg.Embeddings.APIKey)

	var ghClient *ghpkg.Client
	if cfg.GitHub.Token != "" {
		ghClient = ghpkg.NewClient(cfg.GitHub)
	}

	result, err := pipeline.Orchestrate(cmd.Context(), pipeline.OrchestratorConfig{
		RepoPath:          repoPath,
		Force:             indexForce,
		DryRun:            indexDryRun,
		Concurrency:       concurrency,
		ExtractionModel:   cfg.Pipeline.ExtractionModel,
		SynthesisModel:    cfg.Pipeline.SynthesisModel,
		Pool:              pool,
		LLM:               llmClient,
		Embedder:          embedClient,
		Verbose:           verbose,
		Phases:            indexPhases,
		GlobalExcludeDirs: cfg.Pipeline.GlobalExcludeDirs,
		GitHubClient:      ghClient,
		GitHubMaxPRs:      cfg.GitHub.MaxPRs,
		GitHubPRBatchSize: cfg.GitHub.PRBatchSize,
	})
	if err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	if result.Phase2Stats != nil {
		fmt.Printf("\nSummary:\n")
		fmt.Printf("  Repository: %s\n", result.RepoName)
		fmt.Printf("  Files analyzed: %d\n", result.Phase2Stats.FilesProcessed)
		fmt.Printf("  Entities created: %d\n", result.Phase2Stats.EntitiesCreated)
		fmt.Printf("  Facts created: %d\n", result.Phase2Stats.FactsCreated)
		fmt.Printf("  Duration: %s\n", result.Duration)
	}

	return nil
}
