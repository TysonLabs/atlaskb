package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/models"
)

var statusCmd = &cobra.Command{
	Use:   "status [repo-name]",
	Short: "Show indexing status and system health",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

type repoStatus struct {
	Name        string              `json:"name"`
	Path        string              `json:"path"`
	LastIndexed string              `json:"last_indexed,omitempty"`
	JobCounts   map[string]int      `json:"job_counts"`
	LastRun     *models.IndexingRun `json:"last_run,omitempty"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	repoStore := &models.RepoStore{Pool: pool}
	jobStore := &models.JobStore{Pool: pool}
	runStore := &models.IndexingRunStore{Pool: pool}

	repos, err := repoStore.List(cmd.Context())
	if err != nil {
		return fmt.Errorf("listing repos: %w", err)
	}

	if len(repos) == 0 {
		fmt.Println("No repositories indexed yet. Run `atlaskb index /path/to/repo` to get started.")
		return nil
	}

	// Filter by repo name if provided
	if len(args) > 0 {
		filterName := args[0]
		var filtered []models.Repo
		for _, r := range repos {
			if r.Name == filterName {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("repository %q not found", filterName)
		}
		repos = filtered
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

		// Fetch last indexing run
		lastRun, _ := runStore.GetLatest(cmd.Context(), r.ID)
		s.LastRun = lastRun

		statuses = append(statuses, s)
	}

	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(statuses)
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	goodStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

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

		// Show last run metrics
		if s.LastRun != nil {
			r := s.LastRun
			fmt.Println()
			fmt.Println("  Last indexing run:")
			fmt.Printf("    Mode: %s", r.Mode)
			if r.CommitSHA != nil {
				sha := *r.CommitSHA
				if len(sha) > 7 {
					sha = sha[:7]
				}
				fmt.Printf("  Commit: %s", sha)
			}
			fmt.Println()

			if r.QualityOverall != nil {
				score := *r.QualityOverall
				scoreStr := fmt.Sprintf("%.0f/100", score)
				if score >= 80 {
					fmt.Printf("    Quality: %s\n", goodStyle.Render(scoreStr))
				} else if score >= 50 {
					fmt.Printf("    Quality: %s\n", warnStyle.Render(scoreStr))
				} else {
					fmt.Printf("    Quality: %s\n", scoreStr)
				}
			}

			if r.FilesAnalyzed != nil {
				fmt.Printf("    Files: %d analyzed", *r.FilesAnalyzed)
				if r.FilesSkipped != nil && *r.FilesSkipped > 0 {
					fmt.Printf(", %d skipped", *r.FilesSkipped)
				}
				fmt.Println()
			}

			if r.EntitiesCreated != nil || r.FactsCreated != nil || r.RelsCreated != nil {
				fmt.Printf("    Created:")
				if r.EntitiesCreated != nil {
					fmt.Printf(" %d entities", *r.EntitiesCreated)
				}
				if r.FactsCreated != nil {
					fmt.Printf(", %d facts", *r.FactsCreated)
				}
				if r.RelsCreated != nil {
					fmt.Printf(", %d rels", *r.RelsCreated)
				}
				fmt.Println()
			}

			if r.DurationMS != nil {
				dur := time.Duration(*r.DurationMS) * time.Millisecond
				fmt.Printf("    Duration: %s\n", dur.Round(time.Second))
			}

			if r.TotalTokens != nil {
				fmt.Printf("    Tokens: %d", *r.TotalTokens)
				if r.TotalCostUSD != nil {
					fmt.Printf("  (~$%.2f)", *r.TotalCostUSD)
				}
				fmt.Println()
			}

			// Quality trend: show comparison if >=2 runs exist
			runs, _ := runStore.ListByRepo(cmd.Context(), r.RepoID)
			if len(runs) >= 2 {
				prev := runs[1] // second most recent
				if prev.QualityOverall != nil && r.QualityOverall != nil {
					diff := *r.QualityOverall - *prev.QualityOverall
					if diff > 0 {
						fmt.Printf("    Trend: %s (%.0f → %.0f)\n",
							goodStyle.Render(fmt.Sprintf("+%.1f", diff)),
							*prev.QualityOverall, *r.QualityOverall)
					} else if diff < 0 {
						fmt.Printf("    Trend: %s (%.0f → %.0f)\n",
							warnStyle.Render(fmt.Sprintf("%.1f", diff)),
							*prev.QualityOverall, *r.QualityOverall)
					} else {
						fmt.Printf("    Trend: unchanged (%.0f)\n", *r.QualityOverall)
					}
				}
			}
		}

		fmt.Println()
	}

	return nil
}
