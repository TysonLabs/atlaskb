package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/db"
)

var (
	cfgPath string
	verbose bool
	jsonOut bool

	cfg  config.Config
	pool *pgxpool.Pool
)

var rootCmd = &cobra.Command{
	Use:   "atlaskb",
	Short: "AtlasKB — a knowledge base built from your codebase",
	Long:  "AtlasKB indexes repositories via multi-phase LLM extraction and stores knowledge in a Postgres+pgvector graph for natural language querying.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip DB connection for setup command
		if cmd.Name() == "setup" {
			return nil
		}

		var err error
		cfg, err = config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		pool, err = db.Connect(context.Background(), cfg.Database)
		if err != nil {
			return fmt.Errorf("connecting to database: %w", err)
		}

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if pool != nil {
			pool.Close()
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path (default ~/.atlaskb/config.toml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output as JSON")
}

func Execute() error {
	return rootCmd.Execute()
}

func logVerbose(format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}
