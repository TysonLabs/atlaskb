package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
	"github.com/tgeorge06/atlaskb/internal/query"
)

var askRepo string
var askTopK int

var askCmd = &cobra.Command{
	Use:   "ask [question]",
	Short: "Ask a question about your indexed codebase",
	Long:  "Search the knowledge graph and synthesize an answer using AI.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runAsk,
}

func init() {
	askCmd.Flags().StringVar(&askRepo, "repo", "", "filter by repository name")
	askCmd.Flags().IntVar(&askTopK, "top-k", 40, "number of facts to retrieve for answer synthesis")
	rootCmd.AddCommand(askCmd)
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")

	// Resolve repo filter
	var repoIDs []uuid.UUID
	if askRepo != "" {
		repoStore := &models.RepoStore{Pool: pool}
		repos, err := repoStore.List(cmd.Context())
		if err != nil {
			return fmt.Errorf("listing repos: %w", err)
		}
		for _, r := range repos {
			if r.Name == askRepo {
				repoIDs = append(repoIDs, r.ID)
			}
		}
		if len(repoIDs) == 0 {
			return fmt.Errorf("repository %q not found", askRepo)
		}
	}

	embedClient := embeddings.NewOpenAIClient(cfg.Embeddings.BaseURL, cfg.Embeddings.APIKey)
	llmClient := llm.NewOpenAIClient(cfg.LLM.BaseURL, cfg.LLM.APIKey)
	engine := query.NewEngine(pool, embedClient)
	engine.SetLLM(llmClient, cfg.Pipeline.ExtractionModel)

	logVerbose("Searching knowledge graph...")
	results, err := engine.Search(cmd.Context(), question, repoIDs, askTopK)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No relevant knowledge found. Try indexing a repository first with `atlaskb index`.")
		return nil
	}

	logVerbose("Found %d relevant facts, synthesizing answer...", len(results))

	synth := query.NewSynthesizer(llmClient, cfg.Pipeline.ExtractionModel)

	stream, err := synth.Synthesize(cmd.Context(), question, results)
	if err != nil {
		return fmt.Errorf("synthesis failed: %w", err)
	}

	fmt.Println()
	for chunk := range stream {
		if chunk.Error != nil {
			return fmt.Errorf("stream error: %w", chunk.Error)
		}
		fmt.Fprint(os.Stdout, chunk.Text)
	}
	fmt.Println()

	return nil
}
