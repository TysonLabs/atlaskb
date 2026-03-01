package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and manage configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	RunE:  runConfigShow,
}

var configSetGitHubTokenCmd = &cobra.Command{
	Use:   "set-github-token [token]",
	Short: "Set the GitHub personal access token for PR mining",
	Long: `Set the GitHub PAT used by Phase 3 to mine PR descriptions and review comments.

The token needs the 'repo' scope (or 'public_repo' for public repos only).
Create one at: https://github.com/settings/tokens

You can also set this via environment variables:
  GITHUB_TOKEN=ghp_...
  ATLASKB_GITHUB_TOKEN=ghp_...`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigSetGitHubToken,
}

var configSetGitHubURLCmd = &cobra.Command{
	Use:   "set-github-api-url [url]",
	Short: "Set the GitHub API URL (for GitHub Enterprise)",
	Long:  "Set a custom GraphQL API URL for GitHub Enterprise. Default: https://api.github.com/graphql",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigSetGitHubURL,
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetGitHubTokenCmd)
	configCmd.AddCommand(configSetGitHubURLCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	fmt.Println("Database:")
	fmt.Printf("  Host:     %s:%d\n", cfg.Database.Host, cfg.Database.Port)
	fmt.Printf("  Database: %s\n", cfg.Database.DBName)
	fmt.Printf("  User:     %s\n", cfg.Database.User)
	fmt.Printf("  SSL:      %s\n", cfg.Database.SSLMode)
	fmt.Println()

	fmt.Println("LLM:")
	fmt.Printf("  URL:   %s\n", cfg.LLM.BaseURL)
	fmt.Printf("  Key:   %s\n", maskToken(cfg.LLM.APIKey))
	fmt.Println()

	fmt.Println("Embeddings:")
	fmt.Printf("  URL:   %s\n", cfg.Embeddings.BaseURL)
	fmt.Printf("  Model: %s\n", cfg.Embeddings.Model)
	fmt.Printf("  Key:   %s\n", maskToken(cfg.Embeddings.APIKey))
	fmt.Println()

	fmt.Println("Pipeline:")
	fmt.Printf("  Concurrency:       %d\n", cfg.Pipeline.Concurrency)
	fmt.Printf("  Extraction Model:  %s\n", cfg.Pipeline.ExtractionModel)
	fmt.Printf("  Synthesis Model:   %s\n", cfg.Pipeline.SynthesisModel)
	if len(cfg.Pipeline.GlobalExcludeDirs) > 0 {
		fmt.Printf("  Global Exclude:    %s\n", strings.Join(cfg.Pipeline.GlobalExcludeDirs, ", "))
	}
	fmt.Println()

	fmt.Println("GitHub:")
	fmt.Printf("  Token:      %s\n", maskToken(cfg.GitHub.Token))
	fmt.Printf("  API URL:    %s\n", cfg.GitHub.APIURL)
	fmt.Printf("  Max PRs:    %d\n", cfg.GitHub.MaxPRs)
	fmt.Printf("  Batch Size: %d\n", cfg.GitHub.PRBatchSize)
	if cfg.GitHub.EnterpriseHost != "" {
		fmt.Printf("  GHE Host:   %s\n", cfg.GitHub.EnterpriseHost)
	}
	fmt.Println()

	if cfg.Server.Port > 0 {
		fmt.Println("Server:")
		fmt.Printf("  Port: %d\n", cfg.Server.Port)
		fmt.Println()
	}

	return nil
}

func runConfigSetGitHubToken(cmd *cobra.Command, args []string) error {
	token := args[0]

	if !strings.HasPrefix(token, "ghp_") && !strings.HasPrefix(token, "github_pat_") && !strings.HasPrefix(token, "gho_") {
		fmt.Println("Warning: token doesn't look like a GitHub PAT (expected ghp_/github_pat_/gho_ prefix)")
	}

	cfg.GitHub.Token = token
	if err := config.Save(cfg, cfgPath); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("GitHub token saved (%s)\n", maskToken(token))
	fmt.Println("Phase 3 (PR mining) will now run during indexing.")
	return nil
}

func runConfigSetGitHubURL(cmd *cobra.Command, args []string) error {
	cfg.GitHub.APIURL = args[0]
	if err := config.Save(cfg, cfgPath); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("GitHub API URL set to: %s\n", args[0])
	return nil
}

func maskToken(token string) string {
	if token == "" {
		return "(not set)"
	}
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
