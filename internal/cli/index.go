package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
	indexExcludes    []string
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
	indexCmd.Flags().StringSliceVar(&indexExcludes, "exclude", nil, "exclude path/pattern (repeatable, highest precedence)")
	rootCmd.AddCommand(indexCmd)
}

// preflightCheck verifies that the LLM and embedding services are reachable
// before starting any indexing work.
func preflightCheck(ctx context.Context, llmURL, embedURL, embedModel string, phases []string) error {
	needsLLM := len(phases) == 0
	needsEmbed := len(phases) == 0
	for _, p := range phases {
		switch p {
		case "phase2", "phase3", "phase4", "phase5":
			needsLLM = true
		case "embedding":
			needsEmbed = true
		}
	}

	client := &http.Client{Timeout: 5 * time.Second}

	if needsLLM {
		fmt.Printf("  Checking LLM service (%s)... ", llmURL)
		req, err := http.NewRequestWithContext(ctx, "GET", llmURL+"/v1/models", nil)
		if err != nil {
			return fmt.Errorf("llm: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("FAIL")
			return fmt.Errorf("LLM service unreachable at %s: %w", llmURL, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			fmt.Println("FAIL")
			return fmt.Errorf("LLM service returned %s at %s/v1/models", resp.Status, llmURL)
		}
		fmt.Println("OK")
	}

	if needsEmbed {
		fmt.Printf("  Checking embedding service (%s)... ", embedURL)
		payload := fmt.Sprintf(`{"input":["preflight"],"model":%q}`, embedModel)
		req, err := http.NewRequestWithContext(ctx, "POST", embedURL+"/v1/embeddings",
			strings.NewReader(payload))
		if err != nil {
			return fmt.Errorf("embeddings: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("FAIL")
			return fmt.Errorf("embedding service unreachable at %s: %w", embedURL, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			fmt.Println("FAIL")
			return fmt.Errorf("embedding service returned %s at %s/v1/embeddings", resp.Status, embedURL)
		}
		fmt.Println("OK")
	}

	return nil
}

func runIndex(cmd *cobra.Command, args []string) error {
	repoPath := args[0]

	concurrency := cfg.Pipeline.Concurrency
	if indexConcurrency > 0 {
		concurrency = indexConcurrency
	}

	// Preflight: verify LLM and embedding services are reachable
	if !indexDryRun {
		fmt.Println("Preflight checks:")
		if err := preflightCheck(cmd.Context(), cfg.LLM.BaseURL, cfg.Embeddings.BaseURL, cfg.Embeddings.Model, indexPhases); err != nil {
			return fmt.Errorf("preflight failed: %w", err)
		}
		fmt.Println()
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
		GitLogLimit:       cfg.Pipeline.GitLogLimit,
		Phases:            indexPhases,
		GlobalExcludeDirs: cfg.Pipeline.GlobalExcludeDirs,
		CLIExcludes:       indexExcludes,
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
