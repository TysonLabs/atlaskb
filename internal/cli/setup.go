package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/db"
)

var setupCmd = &cobra.Command{
	Use: "setup",
	Aliases: []string{
		"configure",
		"init",
	},
	Short: "Interactive setup wizard for AtlasKB",
	Long:  "Configures AtlasKB database, runtime, model, and GitHub integration settings.",
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

	// --- Step 4: Runtime settings ---
	serverPort := strconv.Itoa(existing.Server.Port)
	if existing.Server.Port <= 0 {
		serverPort = "3000"
	}
	chatsDir := existing.Server.ChatsDir
	runtimePort := 3000

	for {
		fmt.Println(titleStyle.Render("Runtime Settings"))

		runtimeForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Dashboard/MCP HTTP Port").
					Description("Used by `atlaskb` and `atlaskb serve`").
					Value(&serverPort),
				huh.NewInput().
					Title("Chats Directory (optional)").
					Description("Leave empty for ~/.atlaskb/chats").
					Value(&chatsDir),
			),
		)

		if err := runtimeForm.Run(); err != nil {
			return fmt.Errorf("runtime form: %w", err)
		}

		port, err := strconv.Atoi(serverPort)
		if err != nil || port < 1 || port > 65535 {
			fmt.Println(errorStyle.Render("  Port must be a number between 1 and 65535"))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}

		runtimePort = port
		chatsDir = strings.TrimSpace(chatsDir)
		fmt.Println()
		break
	}

	// --- Step 5: Pipeline settings ---
	concurrency := strconv.Itoa(existing.Pipeline.Concurrency)
	if concurrency == "0" {
		concurrency = "2"
	}
	extractionModel := existing.Pipeline.ExtractionModel
	if extractionModel == "" {
		extractionModel = "qwen/qwen3.5-35b-a3b"
	}
	synthesisModel := existing.Pipeline.SynthesisModel
	if synthesisModel == "" {
		synthesisModel = "qwen/qwen3.5-35b-a3b"
	}

	var conc int
	for {
		fmt.Println(titleStyle.Render("Pipeline Settings"))

		pipeForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Concurrency (parallel LLM calls)").
					Value(&concurrency).
					Description("Higher values are faster but use more model capacity"),
				huh.NewInput().
					Title("Extraction Model").
					Value(&extractionModel),
				huh.NewInput().
					Title("Synthesis Model").
					Value(&synthesisModel),
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
		if strings.TrimSpace(extractionModel) == "" || strings.TrimSpace(synthesisModel) == "" {
			fmt.Println(errorStyle.Render("  Extraction and synthesis models are required"))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}
		extractionModel = strings.TrimSpace(extractionModel)
		synthesisModel = strings.TrimSpace(synthesisModel)
		fmt.Println()
		break
	}

	// --- Step 6: GitHub integration (optional but recommended) ---
	ghToken := existing.GitHub.Token
	ghAPIURL := existing.GitHub.APIURL
	if ghAPIURL == "" {
		ghAPIURL = "https://api.github.com/graphql"
	}
	ghEnterpriseHost := existing.GitHub.EnterpriseHost
	ghMaxPRs := strconv.Itoa(existing.GitHub.MaxPRs)
	if existing.GitHub.MaxPRs <= 0 {
		ghMaxPRs = "200"
	}
	ghBatchSize := strconv.Itoa(existing.GitHub.PRBatchSize)
	if existing.GitHub.PRBatchSize <= 0 {
		ghBatchSize = "10"
	}

	var maxPRs, batchSize int
	for {
		fmt.Println(titleStyle.Render("GitHub Integration"))

		ghForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("GitHub Token (optional)").
					Description("Needed for PR mining in Phase 3; leave blank to skip").
					EchoMode(huh.EchoModePassword).
					Value(&ghToken),
				huh.NewInput().
					Title("GitHub API URL").
					Description("Use default for github.com; set GHE GraphQL endpoint for enterprise").
					Value(&ghAPIURL),
				huh.NewInput().
					Title("GitHub Enterprise Host (optional)").
					Description("Host used to detect enterprise remotes, e.g. github.acme.com").
					Value(&ghEnterpriseHost),
				huh.NewInput().
					Title("Max PRs to Mine").
					Value(&ghMaxPRs),
				huh.NewInput().
					Title("PR Batch Size").
					Value(&ghBatchSize),
			),
		)
		if err := ghForm.Run(); err != nil {
			return fmt.Errorf("github form: %w", err)
		}

		if strings.TrimSpace(ghAPIURL) == "" {
			fmt.Println(errorStyle.Render("  GitHub API URL is required"))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}

		var err error
		maxPRs, err = strconv.Atoi(ghMaxPRs)
		if err != nil || maxPRs < 1 {
			fmt.Println(errorStyle.Render("  Max PRs must be a number >= 1"))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}
		batchSize, err = strconv.Atoi(ghBatchSize)
		if err != nil || batchSize < 1 {
			fmt.Println(errorStyle.Render("  PR batch size must be a number >= 1"))
			if !confirmRetry() {
				return fmt.Errorf("setup cancelled")
			}
			continue
		}

		ghToken = strings.TrimSpace(ghToken)
		ghAPIURL = strings.TrimSpace(ghAPIURL)
		ghEnterpriseHost = strings.TrimSpace(ghEnterpriseHost)
		fmt.Println()
		break
	}

	// Build final config by mutating existing so untouched defaults/settings remain intact.
	finalCfg := existing
	finalCfg.Database = dbCfg
	finalCfg.LLM = config.LLMConfig{BaseURL: llmURL, APIKey: llmKey}
	finalCfg.Embeddings = config.EmbeddingsConfig{BaseURL: embURL, Model: embModel, APIKey: embKey}
	finalCfg.Pipeline.Concurrency = conc
	finalCfg.Pipeline.ExtractionModel = extractionModel
	finalCfg.Pipeline.SynthesisModel = synthesisModel
	finalCfg.Server.Port = runtimePort
	finalCfg.Server.ChatsDir = chatsDir
	finalCfg.GitHub.Token = ghToken
	finalCfg.GitHub.APIURL = ghAPIURL
	finalCfg.GitHub.EnterpriseHost = ghEnterpriseHost
	finalCfg.GitHub.MaxPRs = maxPRs
	finalCfg.GitHub.PRBatchSize = batchSize

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

	// Check for optional ctags
	if _, err := exec.LookPath("ctags"); err != nil {
		fmt.Println()
		fmt.Println(dimStyle.Render("  Universal Ctags improves entity name accuracy during indexing."))

		var installCtags bool
		ctagsForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Install Universal Ctags?").
					Description("Recommended for better indexing quality").
					Affirmative("Yes, install").
					Negative("No, skip").
					Value(&installCtags),
			),
		)
		if err := ctagsForm.Run(); err == nil && installCtags {
			installCmd, installArgs := ctagsInstallCommand()
			if installCmd == "" {
				fmt.Println(errorStyle.Render("  Could not detect package manager. Install manually:"))
				fmt.Println(dimStyle.Render("    brew install universal-ctags    (macOS)"))
				fmt.Println(dimStyle.Render("    apt install universal-ctags     (Linux)"))
			} else {
				fmt.Printf(dimStyle.Render("  Running: %s %s\n"), installCmd, joinArgs(installArgs))
				cmd := exec.Command(installCmd, installArgs...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					fmt.Println(errorStyle.Render("  Install failed: " + err.Error()))
					fmt.Println(dimStyle.Render("  You can install manually later."))
				} else {
					fmt.Println(successStyle.Render("  ctags installed successfully"))
				}
			}
		} else if err == nil {
			fmt.Println(dimStyle.Render("  Skipped. You can install later with:"))
			installCmd, installArgs := ctagsInstallCommand()
			if installCmd != "" {
				fmt.Printf(dimStyle.Render("    %s %s\n"), installCmd, joinArgs(installArgs))
			} else {
				fmt.Println(dimStyle.Render("    brew install universal-ctags    (macOS)"))
				fmt.Println(dimStyle.Render("    apt install universal-ctags     (Linux)"))
			}
		}
	} else {
		fmt.Println(dimStyle.Render("  ctags detected — entity grounding enabled"))
	}

	fmt.Println()
	fmt.Println(dimStyle.Render("  Next steps:"))
	fmt.Println(dimStyle.Render("    atlaskb                         Start dashboard + MCP endpoint"))
	fmt.Println(dimStyle.Render("    atlaskb index /path/to/repo    Index a repository"))
	fmt.Println(dimStyle.Render("    atlaskb ask \"question\"         Ask about your code"))
	fmt.Println()

	return nil
}

// ctagsInstallCommand returns the package manager command and args for installing ctags.
func ctagsInstallCommand() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("brew"); err == nil {
			return "brew", []string{"install", "universal-ctags"}
		}
	case "linux":
		if _, err := exec.LookPath("apt-get"); err == nil {
			return "sudo", []string{"apt-get", "install", "-y", "universal-ctags"}
		}
		if _, err := exec.LookPath("dnf"); err == nil {
			return "sudo", []string{"dnf", "install", "-y", "ctags"}
		}
		if _, err := exec.LookPath("pacman"); err == nil {
			return "sudo", []string{"pacman", "-S", "--noconfirm", "ctags"}
		}
	}
	return "", nil
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
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
