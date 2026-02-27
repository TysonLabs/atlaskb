package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/models"
)

var retryPhase string

var retryCmd = &cobra.Command{
	Use:   "retry [repo-name]",
	Short: "Retry failed extraction jobs",
	Long:  "Reset failed jobs to pending status so they will be retried on the next index run.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRetry,
}

func init() {
	retryCmd.Flags().StringVar(&retryPhase, "phase", "", "only retry jobs for a specific phase")
	rootCmd.AddCommand(retryCmd)
}

func runRetry(cmd *cobra.Command, args []string) error {
	repoName := args[0]

	repoStore := &models.RepoStore{Pool: pool}
	repos, err := repoStore.List(cmd.Context())
	if err != nil {
		return fmt.Errorf("listing repos: %w", err)
	}

	var repo *models.Repo
	for _, r := range repos {
		if r.Name == repoName {
			repo = &r
			break
		}
	}
	if repo == nil {
		return fmt.Errorf("repository %q not found", repoName)
	}

	jobStore := &models.JobStore{Pool: pool}

	// Show failed jobs first
	failed, err := jobStore.ListFailed(cmd.Context(), repo.ID)
	if err != nil {
		return fmt.Errorf("listing failed jobs: %w", err)
	}

	if len(failed) == 0 {
		fmt.Println("No failed jobs found.")
		return nil
	}

	fmt.Printf("Found %d failed jobs:\n", len(failed))
	for _, j := range failed {
		errMsg := ""
		if j.ErrorMessage != nil {
			errMsg = *j.ErrorMessage
		}
		if len(errMsg) > 80 {
			errMsg = errMsg[:80] + "..."
		}
		fmt.Printf("  [%s] %s: %s\n", j.Phase, j.Target, errMsg)
	}

	// Reset them
	count, err := jobStore.ResetFailed(cmd.Context(), repo.ID, retryPhase)
	if err != nil {
		return fmt.Errorf("resetting jobs: %w", err)
	}

	fmt.Printf("\nReset %d jobs to pending. Run `atlaskb index %s` to retry.\n", count, repo.LocalPath)
	return nil
}
