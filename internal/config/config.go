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
	Database  DatabaseConfig  `toml:"database"`
	Anthropic AnthropicConfig `toml:"anthropic"`
	Voyage    VoyageConfig    `toml:"voyage"`
	Pipeline  PipelineConfig  `toml:"pipeline"`
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

type AnthropicConfig struct {
	APIKey string `toml:"api_key"`
}

type VoyageConfig struct {
	APIKey string `toml:"api_key"`
}

type PipelineConfig struct {
	Concurrency    int    `toml:"concurrency"`
	ExtractionModel string `toml:"extraction_model"`
	SynthesisModel  string `toml:"synthesis_model"`
}

func DefaultConfig() Config {
	return Config{
		Database: DatabaseConfig{
			Host:    "localhost",
			Port:    5432,
			User:    "postgres",
			Password: "",
			DBName:  "atlaskb",
			SSLMode: "disable",
		},
		Pipeline: PipelineConfig{
			Concurrency:     4,
			ExtractionModel: "claude-sonnet-4-20250514",
			SynthesisModel:  "claude-opus-4-20250514",
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
	if cfg.Anthropic.APIKey == "" {
		return fmt.Errorf("anthropic API key is required")
	}
	if cfg.Voyage.APIKey == "" {
		return fmt.Errorf("voyage API key is required")
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
	if v := os.Getenv("ATLASKB_ANTHROPIC_API_KEY"); v != "" {
		cfg.Anthropic.APIKey = v
	}
	if v := os.Getenv("ATLASKB_VOYAGE_API_KEY"); v != "" {
		cfg.Voyage.APIKey = v
	}
}
