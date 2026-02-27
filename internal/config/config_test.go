package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Database.Host != "localhost" {
		t.Errorf("expected localhost, got %s", cfg.Database.Host)
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("expected 5432, got %d", cfg.Database.Port)
	}
	if cfg.Pipeline.Concurrency != 2 {
		t.Errorf("expected 2, got %d", cfg.Pipeline.Concurrency)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := DefaultConfig()
	cfg.Database.Host = "testhost"
	cfg.Database.User = "testuser"
	cfg.LLM.BaseURL = "http://myserver:1234"

	if err := Save(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Database.Host != "testhost" {
		t.Errorf("expected testhost, got %s", loaded.Database.Host)
	}
	if loaded.LLM.BaseURL != "http://myserver:1234" {
		t.Errorf("expected http://myserver:1234, got %s", loaded.LLM.BaseURL)
	}
}

func TestLoadNonExistent(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("expected defaults, got host=%s", cfg.Database.Host)
	}
}

func TestEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := DefaultConfig()
	if err := Save(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}

	os.Setenv("ATLASKB_DB_HOST", "envhost")
	defer os.Unsetenv("ATLASKB_DB_HOST")

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Database.Host != "envhost" {
		t.Errorf("expected envhost, got %s", loaded.Database.Host)
	}
}

func TestValidate(t *testing.T) {
	cfg := DefaultConfig()

	if err := Validate(cfg); err != nil {
		t.Errorf("expected valid config: %v", err)
	}

	cfg.Database.Host = ""
	if err := Validate(cfg); err == nil {
		t.Error("expected error for missing host")
	}
}

func TestDSN(t *testing.T) {
	d := DatabaseConfig{
		Host: "localhost", Port: 5432, User: "user", Password: "pass", DBName: "db", SSLMode: "disable",
	}
	expected := "postgres://user:pass@localhost:5432/db?sslmode=disable"
	if got := d.DSN(); got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}
