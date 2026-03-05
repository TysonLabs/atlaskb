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

func TestServerConfigGetChatsDir(t *testing.T) {
	custom := ServerConfig{ChatsDir: "/tmp/custom-chats"}
	if got := custom.GetChatsDir(); got != "/tmp/custom-chats" {
		t.Fatalf("GetChatsDir(custom) = %q, want /tmp/custom-chats", got)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	def := ServerConfig{}
	want := filepath.Join(home, DefaultConfigDir, "chats")
	if got := def.GetChatsDir(); got != want {
		t.Fatalf("GetChatsDir(default) = %q, want %q", got, want)
	}
}

func TestConfigDirAndPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error = %v", err)
	}
	if want := filepath.Join(home, DefaultConfigDir); dir != want {
		t.Fatalf("ConfigDir() = %q, want %q", dir, want)
	}

	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath() error = %v", err)
	}
	if want := filepath.Join(home, DefaultConfigDir, DefaultConfigFile); path != want {
		t.Fatalf("ConfigPath() = %q, want %q", path, want)
	}
}

func TestGitHubTokenOverridePriority(t *testing.T) {
	cfg := DefaultConfig()

	t.Setenv("GITHUB_TOKEN", "github-token")
	t.Setenv("ATLASKB_GITHUB_TOKEN", "atlaskb-token")
	applyEnvOverrides(&cfg)
	if cfg.GitHub.Token != "atlaskb-token" {
		t.Fatalf("token = %q, want atlaskb-token", cfg.GitHub.Token)
	}

	cfg = DefaultConfig()
	t.Setenv("ATLASKB_GITHUB_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "github-token-only")
	applyEnvOverrides(&cfg)
	if cfg.GitHub.Token != "github-token-only" {
		t.Fatalf("fallback token = %q, want github-token-only", cfg.GitHub.Token)
	}
}

func TestValidateAdditionalFailures(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Database.DBName = ""
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for missing database name")
	}

	cfg = DefaultConfig()
	cfg.LLM.BaseURL = ""
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for missing llm url")
	}

	cfg = DefaultConfig()
	cfg.Embeddings.BaseURL = ""
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for missing embeddings url")
	}

	cfg = DefaultConfig()
	cfg.Pipeline.Concurrency = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("expected error for invalid pipeline concurrency")
	}
}

func TestApplyEnvOverridesAdditionalFields(t *testing.T) {
	cfg := DefaultConfig()

	t.Setenv("ATLASKB_DB_USER", "env-user")
	t.Setenv("ATLASKB_DB_PASSWORD", "env-pass")
	t.Setenv("ATLASKB_DB_NAME", "env-db")
	t.Setenv("ATLASKB_LLM_URL", "http://llm.env")
	t.Setenv("ATLASKB_LLM_API_KEY", "llm-key")
	t.Setenv("ATLASKB_EMBEDDINGS_URL", "http://embed.env")
	t.Setenv("ATLASKB_EMBEDDINGS_MODEL", "embed-model")
	t.Setenv("ATLASKB_EMBEDDINGS_API_KEY", "embed-key")
	t.Setenv("ATLASKB_GITHUB_API_URL", "https://ghe.example/graphql")

	applyEnvOverrides(&cfg)

	if cfg.Database.User != "env-user" || cfg.Database.Password != "env-pass" || cfg.Database.DBName != "env-db" {
		t.Fatalf("database env overrides not applied: %+v", cfg.Database)
	}
	if cfg.LLM.BaseURL != "http://llm.env" || cfg.LLM.APIKey != "llm-key" {
		t.Fatalf("llm env overrides not applied: %+v", cfg.LLM)
	}
	if cfg.Embeddings.BaseURL != "http://embed.env" || cfg.Embeddings.Model != "embed-model" || cfg.Embeddings.APIKey != "embed-key" {
		t.Fatalf("embeddings env overrides not applied: %+v", cfg.Embeddings)
	}
	if cfg.GitHub.APIURL != "https://ghe.example/graphql" {
		t.Fatalf("github api url env override not applied: %+v", cfg.GitHub)
	}
}

func TestLoadAndSaveErrorPaths(t *testing.T) {
	// Load parse error branch.
	badTOML := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(badTOML, []byte("database = ["), 0o600); err != nil {
		t.Fatalf("write bad TOML: %v", err)
	}
	if _, err := Load(badTOML); err == nil {
		t.Fatalf("Load should fail on malformed TOML")
	}

	// Load read error branch (permission denied).
	unreadable := filepath.Join(t.TempDir(), "unreadable.toml")
	if err := os.WriteFile(unreadable, []byte("x=1"), 0o000); err != nil {
		t.Fatalf("write unreadable file: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })
	if _, err := Load(unreadable); err == nil {
		t.Fatalf("Load should fail on unreadable file")
	}

	// Save mkdir error branch: parent component is a file.
	blockedParent := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blockedParent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocked parent: %v", err)
	}
	if err := Save(DefaultConfig(), filepath.Join(blockedParent, "config.toml")); err == nil {
		t.Fatalf("Save should fail when parent path is a file")
	}

	// Save open error branch: target path is a directory.
	asDir := filepath.Join(t.TempDir(), "cfgdir")
	if err := os.MkdirAll(asDir, 0o755); err != nil {
		t.Fatalf("mkdir asDir: %v", err)
	}
	if err := Save(DefaultConfig(), asDir); err == nil {
		t.Fatalf("Save should fail when target path is a directory")
	}
}
