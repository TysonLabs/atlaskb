package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	DefaultConfigDir  = ".atlaskb"
	DefaultConfigFile = "config.toml"
)

type Config struct {
	Database   DatabaseConfig   `toml:"database"`
	LLM        LLMConfig        `toml:"llm"`
	Embeddings EmbeddingsConfig `toml:"embeddings"`
	Pipeline   PipelineConfig   `toml:"pipeline"`
	Server     ServerConfig     `toml:"server"`
	GitHub     GitHubConfig     `toml:"github"`
}

type GitHubConfig struct {
	Token          string `toml:"token"`
	APIURL         string `toml:"api_url"`
	MaxPRs         int    `toml:"max_prs"`
	PRBatchSize    int    `toml:"pr_batch_size"`
	EnterpriseHost string `toml:"enterprise_host"`
}

type ServerConfig struct {
	Port     int    `toml:"port"`
	ChatsDir string `toml:"chats_dir"`
}

func (s ServerConfig) GetChatsDir() string {
	if s.ChatsDir != "" {
		return s.ChatsDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", DefaultConfigDir, "chats")
	}
	return filepath.Join(home, DefaultConfigDir, "chats")
}

type DatabaseConfig struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	DBName   string `toml:"dbname"`
	SSLMode  string `toml:"sslmode"`
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.DBName, d.SSLMode)
}

type LLMConfig struct {
	BaseURL string `toml:"base_url"`
	APIKey  string `toml:"api_key"`
}

type EmbeddingsConfig struct {
	BaseURL string `toml:"base_url"`
	Model   string `toml:"model"`
	APIKey  string `toml:"api_key"`
}

type PipelineConfig struct {
	Concurrency       int      `toml:"concurrency"`
	ExtractionModel   string   `toml:"extraction_model"`
	SynthesisModel    string   `toml:"synthesis_model"`
	ContextWindow     int      `toml:"context_window"`
	GlobalExcludeDirs []string `toml:"global_exclude_dirs"`
	GitLogLimit       int      `toml:"git_log_limit"`
}

func DefaultConfig() Config {
	return Config{
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			User:     "atlaskb",
			Password: "atlaskb",
			DBName:   "atlaskb",
			SSLMode:  "disable",
		},
		LLM: LLMConfig{
			BaseURL: "http://localhost:1234",
		},
		Embeddings: EmbeddingsConfig{
			BaseURL: "http://localhost:1234",
			Model:   "mxbai-embed-large-v1",
		},
		Pipeline: PipelineConfig{
			Concurrency:     2,
			ExtractionModel: "qwen/qwen3.5-35b-a3b",
			SynthesisModel:  "qwen/qwen3.5-35b-a3b",
			ContextWindow:   32768,
			GitLogLimit:     500,
			GlobalExcludeDirs: []string{
				"tests", "test", "__tests__", "spec",
				"testing", "testdata", "fixtures",
				"e2e", "cypress", "playwright",
				"migrations",
			},
		},
	}
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, DefaultConfigDir), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DefaultConfigFile), nil
}

func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		var err error
		path, err = ConfigPath()
		if err != nil {
			return cfg, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnvOverrides(&cfg)
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config: %w", err)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config: %w", err)
	}

	applyEnvOverrides(&cfg)
	return cfg, nil
}

func Save(cfg Config, path string) error {
	if path == "" {
		var err error
		path, err = ConfigPath()
		if err != nil {
			return err
		}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening config file: %w", err)
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}

func Validate(cfg Config) error {
	if cfg.Database.Host == "" {
		return fmt.Errorf("database host is required")
	}
	if cfg.Database.DBName == "" {
		return fmt.Errorf("database name is required")
	}
	if cfg.LLM.BaseURL == "" {
		return fmt.Errorf("LLM base URL is required")
	}
	if cfg.Embeddings.BaseURL == "" {
		return fmt.Errorf("embeddings base URL is required")
	}
	if cfg.Pipeline.Concurrency < 1 {
		return fmt.Errorf("pipeline concurrency must be at least 1")
	}
	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("ATLASKB_DB_HOST"); v != "" {
		cfg.Database.Host = v
	}
	if v := os.Getenv("ATLASKB_DB_USER"); v != "" {
		cfg.Database.User = v
	}
	if v := os.Getenv("ATLASKB_DB_PASSWORD"); v != "" {
		cfg.Database.Password = v
	}
	if v := os.Getenv("ATLASKB_DB_NAME"); v != "" {
		cfg.Database.DBName = v
	}
	if v := os.Getenv("ATLASKB_LLM_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("ATLASKB_LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("ATLASKB_EMBEDDINGS_URL"); v != "" {
		cfg.Embeddings.BaseURL = v
	}
	if v := os.Getenv("ATLASKB_EMBEDDINGS_MODEL"); v != "" {
		cfg.Embeddings.Model = v
	}
	if v := os.Getenv("ATLASKB_EMBEDDINGS_API_KEY"); v != "" {
		cfg.Embeddings.APIKey = v
	}

	// GitHub token: ATLASKB_GITHUB_TOKEN takes priority, then GITHUB_TOKEN
	if v := os.Getenv("ATLASKB_GITHUB_TOKEN"); v != "" {
		cfg.GitHub.Token = v
	} else if v := os.Getenv("GITHUB_TOKEN"); v != "" && cfg.GitHub.Token == "" {
		cfg.GitHub.Token = v
	}
	if v := os.Getenv("ATLASKB_GITHUB_API_URL"); v != "" {
		cfg.GitHub.APIURL = v
	}

	// Apply defaults for GitHub config
	if cfg.GitHub.APIURL == "" {
		cfg.GitHub.APIURL = "https://api.github.com/graphql"
	}
	if cfg.GitHub.MaxPRs == 0 {
		cfg.GitHub.MaxPRs = 200
	}
	if cfg.GitHub.PRBatchSize == 0 {
		cfg.GitHub.PRBatchSize = 10
	}
}
