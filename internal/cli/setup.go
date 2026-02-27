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

	// --- Step 1: Database configuration + connection test + migrations ---
	dbHost := existing.Database.Host
	dbPort := strconv.Itoa(existing.Database.Port)
	dbUser := existing.Database.User
	dbPass := existing.Database.Password
	dbName := existing.Database.DBName
	dbSSL := existing.Database.SSLMode
	if dbSSL == "" {
		dbSSL = "disable"
	}

	var dbCfg config.DatabaseConfig
	for {
		fmt.Println(titleStyle.Render("Database Configuration"))

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
			fmt.Println(errorStyle.Render("  Invalid port number: " + dbPort))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}

		dbCfg = config.DatabaseConfig{
			Host:     dbHost,
			Port:     port,
			User:     dbUser,
			Password: dbPass,
			DBName:   dbName,
			SSLMode:  dbSSL,
		}

		// Test database connection
		fmt.Print(dimStyle.Render("  Testing database connection... "))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		testPool, err := db.Connect(ctx, dbCfg)
		if err != nil {
			cancel()
			fmt.Println(errorStyle.Render("FAILED"))
			fmt.Println(errorStyle.Render("  " + err.Error()))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}
		fmt.Println(successStyle.Render("OK"))

		// Check for existing schema
		fmt.Print(dimStyle.Render("  Checking for existing database... "))
		hasSchema, schemaErr := db.HasExistingSchema(ctx, testPool)
		if schemaErr != nil {
			cancel()
			testPool.Close()
			fmt.Println(errorStyle.Render("FAILED"))
			fmt.Println(errorStyle.Render("  " + schemaErr.Error()))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}

		if hasSchema {
			fmt.Println(dimStyle.Render("found existing tables"))
			var dbAction string
			actionForm := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Existing AtlasKB database detected").
						Description("What would you like to do?").
						Options(
							huh.NewOption("Keep existing data and run pending migrations", "keep"),
							huh.NewOption("Delete everything and start fresh", "reset"),
						).
						Value(&dbAction),
				),
			)
			if err := actionForm.Run(); err != nil {
				cancel()
				testPool.Close()
				return fmt.Errorf("database action form: %w", err)
			}

			if dbAction == "reset" {
				fmt.Print(dimStyle.Render("  Resetting database... "))
				if err := db.ResetSchema(ctx, testPool); err != nil {
					cancel()
					testPool.Close()
					fmt.Println(errorStyle.Render("FAILED"))
					fmt.Println(errorStyle.Render("  " + err.Error()))
					if !confirmRetry() {
						return fmt.Errorf("setup cancelled")
					}
					continue
				}
				fmt.Println(successStyle.Render("OK"))
			}
		} else {
			fmt.Println(successStyle.Render("OK (fresh database)"))
		}

		cancel()
		testPool.Close()

		// Run migrations
		fmt.Print(dimStyle.Render("  Running migrations... "))
		if err := db.RunMigrations(dbCfg.DSN()); err != nil {
			fmt.Println(errorStyle.Render("FAILED"))
			fmt.Println(errorStyle.Render("  " + err.Error()))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}
		fmt.Println(successStyle.Render("OK"))
		fmt.Println()
		break
	}

	// --- Step 2: LLM configuration ---
	llmURL := existing.LLM.BaseURL
	if llmURL == "" {
		llmURL = "http://localhost:1234"
	}
	llmKey := existing.LLM.APIKey

	for {
		fmt.Println(titleStyle.Render("LLM Configuration"))

		llmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("LLM Server URL").
					Description("OpenAI-compatible chat completions endpoint (e.g. LM Studio)").
					Value(&llmURL),
				huh.NewInput().
					Title("LLM API Key (optional)").
					EchoMode(huh.EchoModePassword).
					Value(&llmKey),
			),
		)

		if err := llmForm.Run(); err != nil {
			return fmt.Errorf("LLM form: %w", err)
		}

		if llmURL == "" {
			fmt.Println(errorStyle.Render("  LLM server URL is required"))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}
		fmt.Println()
		break
	}

	// --- Step 3: Embeddings configuration ---
	embURL := existing.Embeddings.BaseURL
	if embURL == "" {
		embURL = "http://localhost:1234"
	}
	embModel := existing.Embeddings.Model
	if embModel == "" {
		embModel = "mxbai-embed-large-v1"
	}
	embKey := existing.Embeddings.APIKey

	for {
		fmt.Println(titleStyle.Render("Embeddings Configuration"))

		embForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Embeddings Server URL").
					Description("OpenAI-compatible embeddings endpoint (e.g. LM Studio)").
					Value(&embURL),
				huh.NewInput().
					Title("Embeddings Model").
					Value(&embModel),
				huh.NewInput().
					Title("Embeddings API Key (optional)").
					EchoMode(huh.EchoModePassword).
					Value(&embKey),
			),
		)

		if err := embForm.Run(); err != nil {
			return fmt.Errorf("embeddings form: %w", err)
		}

		if embURL == "" {
			fmt.Println(errorStyle.Render("  Embeddings server URL is required"))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}
		if embModel == "" {
			fmt.Println(errorStyle.Render("  Embeddings model is required"))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}
		fmt.Println()
		break
	}

	// --- Step 4: Pipeline settings ---
	concurrency := strconv.Itoa(existing.Pipeline.Concurrency)
	if concurrency == "0" {
		concurrency = "2"
	}

	var conc int
	for {
		fmt.Println(titleStyle.Render("Pipeline Settings"))

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

		var err error
		conc, err = strconv.Atoi(concurrency)
		if err != nil || conc < 1 {
			fmt.Println(errorStyle.Render("  Concurrency must be a number >= 1"))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}
		fmt.Println()
		break
	}

	// Build final config
	finalCfg := config.Config{
		Database:   dbCfg,
		LLM:        config.LLMConfig{BaseURL: llmURL, APIKey: llmKey},
		Embeddings: config.EmbeddingsConfig{BaseURL: embURL, Model: embModel, APIKey: embKey},
		Pipeline: config.PipelineConfig{
			Concurrency:     conc,
			ExtractionModel: existing.Pipeline.ExtractionModel,
			SynthesisModel:  existing.Pipeline.SynthesisModel,
		},
	}

	if finalCfg.Pipeline.ExtractionModel == "" {
		finalCfg.Pipeline.ExtractionModel = "qwen/qwen3.5-35b-a3b"
	}
	if finalCfg.Pipeline.SynthesisModel == "" {
		finalCfg.Pipeline.SynthesisModel = "qwen/qwen3.5-35b-a3b"
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

// confirmRetry asks the user whether to retry the current step or quit.
func confirmRetry() bool {
	var retry bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Would you like to try again?").
				Affirmative("Yes, retry").
				Negative("No, exit setup").
				Value(&retry),
		),
	)
	if err := form.Run(); err != nil {
		return false
	}
	fmt.Println()
	return retry
}
