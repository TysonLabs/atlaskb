package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildExclusionSet_PrecedenceAndIgnoreFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".atlaskbignore"), []byte("tmp/\n*.log\n"), 0o644); err != nil {
		t.Fatalf("write .atlaskbignore: %v", err)
	}

	set, err := BuildExclusionSet(dir, []string{"global", "tmp"}, []string{"repo"}, []string{"cli", "*.tmp"})
	if err != nil {
		t.Fatalf("BuildExclusionSet() error = %v", err)
	}

	// Effective order is global -> repo -> cli -> .atlaskbignore.
	wantPrefix := []string{"global", "tmp", "repo", "cli", "*.tmp"}
	for i := range wantPrefix {
		if len(set.Effective) <= i || set.Effective[i] != wantPrefix[i] {
			t.Fatalf("effective[%d] = %q, want %q", i, set.Effective[i], wantPrefix[i])
		}
	}

	if !set.ShouldExclude("repo/file.go", false) {
		t.Fatalf("repo settings pattern should exclude path")
	}
	if !set.ShouldExclude("tmp/out.txt", false) {
		t.Fatalf(".atlaskbignore directory pattern should exclude path")
	}
	if !set.ShouldExclude("build/app.log", false) {
		t.Fatalf(".atlaskbignore glob pattern should exclude path")
	}
	if !set.ShouldExclude("scratch/foo.tmp", false) {
		t.Fatalf("CLI glob pattern should exclude path")
	}
	if set.ShouldExclude("cmd/main.go", false) {
		t.Fatalf("non-matching path should not be excluded")
	}
}

func TestBuildExclusionSet_AnchoredDirectoryPattern(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".atlaskbignore"), []byte("/build/\n"), 0o644); err != nil {
		t.Fatalf("write .atlaskbignore: %v", err)
	}

	set, err := BuildExclusionSet(dir, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildExclusionSet() error = %v", err)
	}

	if !set.ShouldExclude("build/out.txt", false) {
		t.Fatalf("expected root build/ to be excluded")
	}
	if set.ShouldExclude("src/build/out.txt", false) {
		t.Fatalf("expected anchored /build/ pattern not to exclude src/build")
	}
}
