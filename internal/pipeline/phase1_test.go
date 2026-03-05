package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunPhase1_MinimalGoProject(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal Go project
	os.MkdirAll(filepath.Join(dir, "cmd"), 0755)
	os.MkdirAll(filepath.Join(dir, "internal"), 0755)

	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "internal", "handler.go"), []byte("package internal\n\nfunc Handle() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "internal", "handler_test.go"), []byte("package internal\n\nfunc TestHandle(t *testing.T) {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644)

	manifest, err := RunPhase1(dir, nil)
	if err != nil {
		t.Fatalf("RunPhase1() error = %v", err)
	}

	if manifest.Stats.TotalFiles != 4 {
		t.Errorf("TotalFiles = %d, want 4", manifest.Stats.TotalFiles)
	}
	if manifest.Stats.AnalyzableFiles != 4 {
		t.Errorf("AnalyzableFiles = %d, want 4", manifest.Stats.AnalyzableFiles)
	}

	// Stack detection
	foundGo := false
	for _, lang := range manifest.Stack.Languages {
		if lang == "go" {
			foundGo = true
		}
	}
	if !foundGo {
		t.Errorf("Stack.Languages = %v, want to contain 'go'", manifest.Stack.Languages)
	}

	foundGoModules := false
	for _, bt := range manifest.Stack.BuildTools {
		if bt == "go modules" {
			foundGoModules = true
		}
	}
	if !foundGoModules {
		t.Errorf("Stack.BuildTools = %v, want to contain 'go modules'", manifest.Stack.BuildTools)
	}
}

func TestRunPhase1_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	manifest, err := RunPhase1(dir, nil)
	if err != nil {
		t.Fatalf("RunPhase1() error = %v", err)
	}

	if manifest.Stats.TotalFiles != 0 {
		t.Errorf("TotalFiles = %d, want 0", manifest.Stats.TotalFiles)
	}
	if len(manifest.Stack.Languages) != 0 {
		t.Errorf("Languages = %v, want empty", manifest.Stack.Languages)
	}
}

func TestRunPhase1_VendorSkipped(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "vendor", "lib"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0644)
	os.WriteFile(filepath.Join(dir, "vendor", "lib", "foo.go"), []byte("package lib\n"), 0644)

	manifest, err := RunPhase1(dir, nil)
	if err != nil {
		t.Fatalf("RunPhase1() error = %v", err)
	}

	// Only main.go should be counted
	if manifest.Stats.TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1 (vendor should be skipped)", manifest.Stats.TotalFiles)
	}
}

func TestRunPhase1_NodeProject(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "src"), 0755)

	pkgJSON := `{"name":"test","dependencies":{"react":"^18.0.0","express":"^4.18.0"}}`
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0644)
	os.WriteFile(filepath.Join(dir, "src", "app.tsx"), []byte("export default function App() {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "server.js"), []byte("const express = require('express');\n"), 0644)

	manifest, err := RunPhase1(dir, nil)
	if err != nil {
		t.Fatalf("RunPhase1() error = %v", err)
	}

	// Check framework detection
	foundReact := false
	foundExpress := false
	for _, fw := range manifest.Stack.Frameworks {
		if fw == "React" {
			foundReact = true
		}
		if fw == "Express" {
			foundExpress = true
		}
	}
	if !foundReact {
		t.Errorf("Frameworks = %v, want to contain 'React'", manifest.Stack.Frameworks)
	}
	if !foundExpress {
		t.Errorf("Frameworks = %v, want to contain 'Express'", manifest.Stack.Frameworks)
	}
}

func TestRunPhase1_ExcludePatterns(t *testing.T) {
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "internal", "generated"), 0o755)
	os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "internal", "generated", "gen.go"), []byte("package generated\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "fixtures", "seed.json"), []byte("{}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.tmp"), []byte("tmp\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".atlaskbignore"), []byte("*.tmp\nfixtures/\n"), 0o644)

	excludes, err := BuildExclusionSet(dir, []string{"vendor"}, []string{"internal/generated"}, nil)
	if err != nil {
		t.Fatalf("BuildExclusionSet() error = %v", err)
	}

	manifest, err := RunPhase1(dir, excludes)
	if err != nil {
		t.Fatalf("RunPhase1() error = %v", err)
	}

	seen := map[string]bool{}
	for _, fi := range manifest.Files {
		seen[fi.Path] = true
	}
	if !seen["main.go"] {
		t.Fatalf("main.go should be included")
	}
	for _, excluded := range []string{"internal/generated/gen.go", "fixtures/seed.json", "notes.tmp"} {
		if seen[excluded] {
			t.Fatalf("excluded file %s was still included in manifest", excluded)
		}
	}
}
