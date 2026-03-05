package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/version"
)

func TestSkipDBConnection(t *testing.T) {
	if !skipDBConnection(&cobra.Command{Use: "setup"}) {
		t.Fatal("setup should skip DB connection")
	}
	if !skipDBConnection(&cobra.Command{Use: "version"}) {
		t.Fatal("version should skip DB connection")
	}
	if !skipDBConnection(&cobra.Command{Use: "help"}) {
		t.Fatal("help should skip DB connection")
	}

	parent := &cobra.Command{Use: "config"}
	child := &cobra.Command{Use: "show"}
	parent.AddCommand(child)
	if !skipDBConnection(child) {
		t.Fatal("config subcommands should skip DB connection")
	}

	if skipDBConnection(&cobra.Command{Use: "index"}) {
		t.Fatal("index should not skip DB connection")
	}
}

func TestIsServeCommand(t *testing.T) {
	if isServeCommand(nil) {
		t.Fatal("nil command should not be serve command")
	}
	if !isServeCommand(&cobra.Command{Use: "serve"}) {
		t.Fatal("serve command should be treated as serve command")
	}
	standalone := &cobra.Command{Use: "anything"}
	if !isServeCommand(standalone) {
		t.Fatal("top-level command is treated as runtime entry")
	}
}

func TestConfigFileExists(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(filePath, []byte("k=v"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ok, err := configFileExists(filePath)
	if err != nil {
		t.Fatalf("configFileExists(file) error: %v", err)
	}
	if !ok {
		t.Fatal("expected existing file to return true")
	}

	ok, err = configFileExists(filepath.Join(dir, "missing.toml"))
	if err != nil {
		t.Fatalf("configFileExists(missing) error: %v", err)
	}
	if ok {
		t.Fatal("missing file should return false")
	}

	ok, err = configFileExists(dir)
	if err != nil {
		t.Fatalf("configFileExists(dir) error: %v", err)
	}
	if ok {
		t.Fatal("directory path should return false")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	ok, err = configFileExists("")
	if err != nil {
		t.Fatalf("configFileExists(default path) error: %v", err)
	}
	if ok {
		t.Fatal("default path should be missing in temp HOME")
	}
}

func TestVersionOutput(t *testing.T) {
	old := version.Version
	t.Cleanup(func() { version.Version = old })

	version.Version = ""
	if got := versionOutput(); got != "dev" {
		t.Fatalf("versionOutput empty = %q, want dev", got)
	}

	version.Version = "1.2.3"
	if got := versionOutput(); got != "1.2.3" {
		t.Fatalf("versionOutput = %q, want 1.2.3", got)
	}
}

func TestWriteVersionInfo(t *testing.T) {
	oldJSON := jsonOut
	oldVersion := version.Version
	oldCommit := version.Commit
	oldDate := version.Date
	t.Cleanup(func() {
		jsonOut = oldJSON
		version.Version = oldVersion
		version.Commit = oldCommit
		version.Date = oldDate
	})

	version.Version = "9.9.9"
	version.Commit = "abcdef0"
	version.Date = "2026-03-05T00:00:00Z"

	jsonOut = false
	textOut, err := captureStdout(writeVersionInfo)
	if err != nil {
		t.Fatalf("writeVersionInfo text error: %v", err)
	}
	if strings.TrimSpace(textOut) != "9.9.9" {
		t.Fatalf("text output = %q, want 9.9.9", textOut)
	}

	jsonOut = true
	jsonText, err := captureStdout(writeVersionInfo)
	if err != nil {
		t.Fatalf("writeVersionInfo json error: %v", err)
	}
	if !strings.Contains(jsonText, `"version": "9.9.9"`) || !strings.Contains(jsonText, `"commit": "abcdef0"`) {
		t.Fatalf("json output missing expected fields: %q", jsonText)
	}
}

func TestExecute(t *testing.T) {
	oldJSON := jsonOut
	oldArgs := rootCmd.Flags().Args()
	t.Cleanup(func() {
		jsonOut = oldJSON
		rootCmd.SetArgs(nil)
		_ = oldArgs
	})

	jsonOut = false
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	rootCmd.SetArgs([]string{"version"})
	execErr := Execute()
	_ = w.Close()
	os.Stdout = origStdout
	_, _ = bytes.NewBuffer(nil).ReadFrom(r)
	_ = r.Close()

	if execErr != nil {
		t.Fatalf("Execute(version) error: %v", execErr)
	}
}

func TestLogVerbose(t *testing.T) {
	oldVerbose := verbose
	t.Cleanup(func() { verbose = oldVerbose })

	verbose = false
	stderr, err := captureStderr(func() error {
		logVerbose("hidden %d", 1)
		return nil
	})
	if err != nil {
		t.Fatalf("capture stderr: %v", err)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr when verbose=false, got %q", stderr)
	}

	verbose = true
	stderr, err = captureStderr(func() error {
		logVerbose("shown %d", 2)
		return nil
	})
	if err != nil {
		t.Fatalf("capture stderr: %v", err)
	}
	if !strings.Contains(stderr, "shown 2") {
		t.Fatalf("expected verbose stderr output, got %q", stderr)
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "(not set)"},
		{"short", "****"},
		{"ghp_1234567890", "ghp_...7890"},
	}
	for _, tc := range tests {
		if got := maskToken(tc.in); got != tc.want {
			t.Fatalf("maskToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRunConfigShow(t *testing.T) {
	oldCfg := cfg
	t.Cleanup(func() { cfg = oldCfg })

	cfg = config.DefaultConfig()
	cfg.GitHub.Token = "ghp_1234567890"
	cfg.Server.Port = 8080
	out, err := captureStdout(func() error { return runConfigShow(nil, nil) })
	if err != nil {
		t.Fatalf("runConfigShow error: %v", err)
	}
	for _, snippet := range []string{"Database:", "LLM:", "Embeddings:", "Pipeline:", "GitHub:", "Server:"} {
		if !strings.Contains(out, snippet) {
			t.Fatalf("output missing %q", snippet)
		}
	}
	if !strings.Contains(out, "ghp_...7890") {
		t.Fatalf("masked token not shown in output: %q", out)
	}
}

func TestRunConfigSetGitHubTokenAndURL(t *testing.T) {
	oldCfg := cfg
	oldCfgPath := cfgPath
	t.Cleanup(func() {
		cfg = oldCfg
		cfgPath = oldCfgPath
	})

	cfg = config.DefaultConfig()
	cfgPath = filepath.Join(t.TempDir(), "config.toml")

	warnOut, err := captureStdout(func() error {
		return runConfigSetGitHubToken(nil, []string{"token_without_prefix"})
	})
	if err != nil {
		t.Fatalf("runConfigSetGitHubToken(warn) error: %v", err)
	}
	if !strings.Contains(warnOut, "doesn't look like a GitHub PAT") {
		t.Fatalf("expected warning output, got %q", warnOut)
	}

	okOut, err := captureStdout(func() error {
		return runConfigSetGitHubToken(nil, []string{"ghp_1234567890"})
	})
	if err != nil {
		t.Fatalf("runConfigSetGitHubToken(ok) error: %v", err)
	}
	if cfg.GitHub.Token != "ghp_1234567890" {
		t.Fatalf("cfg token = %q, want ghp_1234567890", cfg.GitHub.Token)
	}
	if !strings.Contains(okOut, "GitHub token saved") {
		t.Fatalf("success output missing expected text: %q", okOut)
	}

	if err := runConfigSetGitHubURL(nil, []string{"https://ghe.example.com/api/graphql"}); err != nil {
		t.Fatalf("runConfigSetGitHubURL error: %v", err)
	}
	if cfg.GitHub.APIURL != "https://ghe.example.com/api/graphql" {
		t.Fatalf("cfg GitHub API URL = %q", cfg.GitHub.APIURL)
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if loaded.GitHub.Token != "ghp_1234567890" || loaded.GitHub.APIURL != "https://ghe.example.com/api/graphql" {
		t.Fatalf("saved config mismatch: %+v", loaded.GitHub)
	}
}

func TestRunServeInvalidPort(t *testing.T) {
	oldCfg := cfg
	oldServePort := servePort
	t.Cleanup(func() {
		cfg = oldCfg
		servePort = oldServePort
	})

	cfg = config.DefaultConfig()
	servePort = -1

	cmd := &cobra.Command{Use: "serve"}
	cmd.Flags().Int("port", 3000, "")
	if err := cmd.Flags().Set("port", "-1"); err != nil {
		t.Fatalf("setting port flag: %v", err)
	}

	err := runServe(cmd, nil)
	if err == nil {
		t.Fatal("runServe() expected invalid port error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "port") {
		t.Fatalf("runServe() error = %v, expected port-related error", err)
	}
}

func TestJoinArgs(t *testing.T) {
	if got := joinArgs([]string{"a", "b", "c"}); got != "a b c" {
		t.Fatalf("joinArgs = %q, want %q", got, "a b c")
	}
	if got := joinArgs(nil); got != "" {
		t.Fatalf("joinArgs(nil) = %q, want empty", got)
	}
}

func TestCtagsInstallCommandDarwinPathLookup(t *testing.T) {
	tmp := t.TempDir()
	brewPath := filepath.Join(tmp, "brew")
	if err := os.WriteFile(brewPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake brew: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", tmp)

	cmd, args := ctagsInstallCommand()
	if runtime.GOOS == "darwin" {
		if cmd != "brew" {
			t.Fatalf("cmd = %q, want brew", cmd)
		}
		if strings.Join(args, " ") != "install universal-ctags" {
			t.Fatalf("args = %v, want install universal-ctags", args)
		}
	} else {
		// On non-darwin platforms this helper may return distro-specific choices.
		// We only assert no panic and valid tuple shape.
		if (cmd == "") != (len(args) == 0) {
			t.Fatalf("inconsistent return values cmd=%q args=%v", cmd, args)
		}
	}
	t.Setenv("PATH", oldPath)
}

func captureStdout(fn func() error) (string, error) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w

	runErr := fn()
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()
	return buf.String(), runErr
}

func captureStderr(fn func() error) (string, error) {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stderr = w

	runErr := fn()
	_ = w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()
	return buf.String(), runErr
}
