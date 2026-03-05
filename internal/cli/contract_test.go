package cli

import (
	"bytes"
	"strings"
	"testing"
)

func findTopLevel(name string) bool {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == name {
			return true
		}
	}
	return false
}

func TestContractTopLevelCommandsAndFlags(t *testing.T) {
	requiredCommands := []string{
		"serve", "index", "ask", "status", "repos", "retry", "link", "setup", "mcp", "version", "config",
	}
	for _, name := range requiredCommands {
		if !findTopLevel(name) {
			t.Fatalf("missing required top-level command %q", name)
		}
	}

	requiredPersistentFlags := []string{"config", "verbose", "json"}
	for _, name := range requiredPersistentFlags {
		if rootCmd.PersistentFlags().Lookup(name) == nil {
			t.Fatalf("missing required persistent flag --%s", name)
		}
	}

	index, _, err := rootCmd.Find([]string{"index"})
	if err != nil {
		t.Fatalf("finding index command: %v", err)
	}
	requiredIndexFlags := []string{"dry-run", "force", "yes", "concurrency", "phase", "exclude"}
	for _, name := range requiredIndexFlags {
		if index.Flags().Lookup(name) == nil {
			t.Fatalf("missing required index flag --%s", name)
		}
	}

	ask, _, err := rootCmd.Find([]string{"ask"})
	if err != nil {
		t.Fatalf("finding ask command: %v", err)
	}
	requiredAskFlags := []string{"repo", "top-k"}
	for _, name := range requiredAskFlags {
		if ask.Flags().Lookup(name) == nil {
			t.Fatalf("missing required ask flag --%s", name)
		}
	}

	if cmd, _, err := rootCmd.Find([]string{"configure"}); err != nil || cmd == nil || cmd.Name() != "setup" {
		t.Fatalf("setup alias 'configure' did not resolve to setup command")
	}
	if cmd, _, err := rootCmd.Find([]string{"init"}); err != nil || cmd == nil || cmd.Name() != "setup" {
		t.Fatalf("setup alias 'init' did not resolve to setup command")
	}
}

func TestContractHelpSurface(t *testing.T) {
	buf := &bytes.Buffer{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("executing --help: %v", err)
	}

	helpText := buf.String()
	for _, snippet := range []string{"index", "ask", "serve", "setup", "mcp"} {
		if !strings.Contains(helpText, snippet) {
			t.Fatalf("help output missing expected snippet %q", snippet)
		}
	}
}
