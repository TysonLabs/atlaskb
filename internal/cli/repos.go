package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/models"
	"github.com/tgeorge06/atlaskb/internal/pipeline"
)

var reposCmd = &cobra.Command{
	Use:   "repos",
	Short: "List indexed repositories",
	RunE:  runRepos,
}

func init() {
	rootCmd.AddCommand(reposCmd)
}

func runRepos(cmd *cobra.Command, args []string) error {
	repoStore := &models.RepoStore{Pool: pool}

	repos, err := repoStore.List(cmd.Context())
	if err != nil {
		return fmt.Errorf("listing repos: %w", err)
	}

	if len(repos) == 0 {
		fmt.Println("No repositories indexed yet.")
		return nil
	}

	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(repos)
	}

	for _, r := range repos {
		indexed := "never"
		if r.LastIndexedAt != nil {
			indexed = r.LastIndexedAt.Format("2006-01-02")
		}

		// Compute quality score for indexed repos
		scoreStr := "-"
		countsStr := ""
		if r.LastIndexedAt != nil {
			qs, err := pipeline.ComputeQuality(cmd.Context(), pool, r.ID)
			if err == nil {
				scoreStr = fmt.Sprintf("%.0f", qs.Overall)
				countsStr = fmt.Sprintf("  entities: %d  facts: %d  rels: %d", qs.EntityCount, qs.FactCount, qs.RelationshipCount)
			}
		}

		fmt.Printf("%-30s  %-50s  indexed: %s  score: %s%s\n", r.Name, r.LocalPath, indexed, scoreStr, countsStr)
	}

	return nil
}
