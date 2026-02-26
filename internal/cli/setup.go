package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/db"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup wizard for AtlasKB",
	Long:  "Configures database connection, API keys, and runs migrations.",
	RunE:  runSetup,
}

func init() {
	rootCmd.AddCommand(setupCmd)
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99"))
	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))
)

func runSetup(cmd *cobra.Command, args []string) error {
	fmt.Println(titleStyle.Render("\n  AtlasKB Setup Wizard\n"))

	// Load existing config as defaults
	existing, _ := config.Load(cfgPath)

	// Database configuration
	fmt.Println(titleStyle.Render("Database Configuration"))

	dbHost := existing.Database.Host
	dbPort := strconv.Itoa(existing.Database.Port)
	dbUser := existing.Database.User
	dbPass := existing.Database.Password
	dbName := existing.Database.DBName
	dbSSL := existing.Database.SSLMode
	if dbSSL == "" {
		dbSSL = "disable"
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("PostgreSQL Host").
				Value(&dbHost),
			huh.NewInput().
				Title("PostgreSQL Port").
				Value(&dbPort),
			huh.NewInput().
				Title("Database User").
				Value(&dbUser),
			huh.NewInput().
				Title("Database Password").
				EchoMode(huh.EchoModePassword).
				Value(&dbPass),
			huh.NewInput().
				Title("Database Name").
				Value(&dbName),
			huh.NewSelect[string]().
				Title("SSL Mode").
				Options(
					huh.NewOption("disable", "disable"),
					huh.NewOption("require", "require"),
					huh.NewOption("verify-full", "verify-full"),
				).
				Value(&dbSSL),
		),
	)

	if err := form.Run(); err != nil {
		return fmt.Errorf("database form: %w", err)
	}

	port, err := strconv.Atoi(dbPort)
	if err != nil {
		return fmt.Errorf("invalid port: %w", err)
	}

	dbCfg := config.DatabaseConfig{
		Host:    dbHost,
		Port:    port,
		User:    dbUser,
		Password: dbPass,
		DBName:  dbName,
		SSLMode: dbSSL,
	}

	// Test database connection
	fmt.Print(dimStyle.Render("  Testing database connection... "))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testPool, err := db.Connect(ctx, dbCfg)
	if err != nil {
		fmt.Println(errorStyle.Render("FAILED"))
		return fmt.Errorf("database connection failed: %w", err)
	}
	testPool.Close()
	fmt.Println(successStyle.Render("OK"))

	// Run migrations
	fmt.Print(dimStyle.Render("  Running migrations... "))
	if err := db.RunMigrations(dbCfg.DSN()); err != nil {
		fmt.Println(errorStyle.Render("FAILED"))
		return fmt.Errorf("migrations failed: %w", err)
	}
	fmt.Println(successStyle.Render("OK"))

	// API Keys
	fmt.Println()
	fmt.Println(titleStyle.Render("API Keys"))

	anthropicKey := existing.Anthropic.APIKey
	voyageKey := existing.Voyage.APIKey

	apiForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Anthropic API Key").
				EchoMode(huh.EchoModePassword).
				Value(&anthropicKey),
			huh.NewInput().
				Title("Voyage AI API Key").
				EchoMode(huh.EchoModePassword).
				Value(&voyageKey),
		),
	)

	if err := apiForm.Run(); err != nil {
		return fmt.Errorf("API keys form: %w", err)
	}

	// Pipeline settings
	fmt.Println()
	fmt.Println(titleStyle.Render("Pipeline Settings"))

	concurrency := strconv.Itoa(existing.Pipeline.Concurrency)
	if concurrency == "0" {
		concurrency = "4"
	}

	pipeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Concurrency (parallel LLM calls)").
				Value(&concurrency),
		),
	)

	if err := pipeForm.Run(); err != nil {
		return fmt.Errorf("pipeline form: %w", err)
	}

	conc, err := strconv.Atoi(concurrency)
	if err != nil || conc < 1 {
		conc = 4
	}

	// Build final config
	finalCfg := config.Config{
		Database:  dbCfg,
		Anthropic: config.AnthropicConfig{APIKey: anthropicKey},
		Voyage:    config.VoyageConfig{APIKey: voyageKey},
		Pipeline: config.PipelineConfig{
			Concurrency:     conc,
			ExtractionModel: existing.Pipeline.ExtractionModel,
			SynthesisModel:  existing.Pipeline.SynthesisModel,
		},
	}

	if finalCfg.Pipeline.ExtractionModel == "" {
		finalCfg.Pipeline.ExtractionModel = "claude-sonnet-4-20250514"
	}
	if finalCfg.Pipeline.SynthesisModel == "" {
		finalCfg.Pipeline.SynthesisModel = "claude-opus-4-20250514"
	}

	// Validate
	if err := config.Validate(finalCfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Save
	if err := config.Save(finalCfg, cfgPath); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	path, _ := config.ConfigPath()
	if cfgPath != "" {
		path = cfgPath
	}

	fmt.Println()
	fmt.Println(successStyle.Render("  Setup complete!"))
	fmt.Println(dimStyle.Render("  Config saved to: " + path))
	fmt.Println()
	fmt.Println(dimStyle.Render("  Next steps:"))
	fmt.Println(dimStyle.Render("    atlaskb index /path/to/repo    Index a repository"))
	fmt.Println(dimStyle.Render("    atlaskb ask \"question\"         Ask about your code"))
	fmt.Println()

	return nil
}
