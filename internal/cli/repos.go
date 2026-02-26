package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/models"
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
			indexed = r.LastIndexedAt.Format("2006-01-02 15:04:05")
		}
		fmt.Printf("%-30s  %-50s  indexed: %s\n", r.Name, r.LocalPath, indexed)
	}

	return nil
}
