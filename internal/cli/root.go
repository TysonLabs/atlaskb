package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/db"
	"github.com/tgeorge06/atlaskb/internal/version"
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
	Short: "AtlasKB runtime and CLI for code knowledge",
	Long:  "By default, runs the AtlasKB runtime (web dashboard + MCP over HTTP). Also provides indexing and management subcommands.",
	RunE:  runServe,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip DB connection for commands that don't need runtime services.
		if skipDBConnection(cmd) {
			// Still load config for config subcommands
			if cmd.Parent() != nil && cmd.Parent().Name() == "config" {
				var err error
				cfg, err = config.Load(cfgPath)
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
			}
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

		if err := db.RunMigrations(cfg.Database.DSN()); err != nil {
			return fmt.Errorf("running migrations: %w", err)
		}

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if pool != nil {
			pool.Close()
		}
	},
}

func skipDBConnection(cmd *cobra.Command) bool {
	if cmd.Name() == "setup" || cmd.Name() == "version" || cmd.Name() == "help" {
		return true
	}
	return cmd.Parent() != nil && cmd.Parent().Name() == "config"
}

func versionOutput() string {
	if version.Version == "" {
		return "dev"
	}
	return version.Version
}

func writeVersionInfo() error {
	if jsonOut {
		payload := map[string]string{
			"version": versionOutput(),
			"commit":  version.Commit,
			"date":    version.Date,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	fmt.Println(versionOutput())
	return nil
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
