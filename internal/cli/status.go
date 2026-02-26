package cli

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/models"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show indexing status and system health",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

type repoStatus struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	LastIndexed string         `json:"last_indexed,omitempty"`
	JobCounts   map[string]int `json:"job_counts"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	repoStore := &models.RepoStore{Pool: pool}
	jobStore := &models.JobStore{Pool: pool}

	repos, err := repoStore.List(cmd.Context())
	if err != nil {
		return fmt.Errorf("listing repos: %w", err)
	}

	if len(repos) == 0 {
		fmt.Println("No repositories indexed yet. Run `atlaskb index /path/to/repo` to get started.")
		return nil
	}

	var statuses []repoStatus
	for _, r := range repos {
		counts, err := jobStore.CountByStatus(cmd.Context(), r.ID, "")
		if err != nil {
			return fmt.Errorf("counting jobs: %w", err)
		}

		s := repoStatus{
			Name:      r.Name,
			Path:      r.LocalPath,
			JobCounts: counts,
		}
		if r.LastIndexedAt != nil {
			s.LastIndexed = r.LastIndexedAt.Format("2006-01-02 15:04:05")
		}
		statuses = append(statuses, s)
	}

	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(statuses)
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	for _, s := range statuses {
		fmt.Println(headerStyle.Render(s.Name))
		fmt.Println(dimStyle.Render("  Path: " + s.Path))
		if s.LastIndexed != "" {
			fmt.Println(dimStyle.Render("  Last indexed: " + s.LastIndexed))
		}

		total := 0
		for _, c := range s.JobCounts {
			total += c
		}
		fmt.Printf("  Jobs: %d total", total)
		if c, ok := s.JobCounts["completed"]; ok && c > 0 {
			fmt.Printf(", %d completed", c)
		}
		if c, ok := s.JobCounts["failed"]; ok && c > 0 {
			fmt.Printf(", %d failed", c)
		}
		if c, ok := s.JobCounts["pending"]; ok && c > 0 {
			fmt.Printf(", %d pending", c)
		}
		fmt.Println()
		fmt.Println()
	}

	return nil
}
