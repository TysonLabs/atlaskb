package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGoMod(t *testing.T) {
	content := `module github.com/example/test

go 1.21

require (
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.10.9
	golang.org/x/sync v0.6.0 // indirect
)

require github.com/single/dep v0.1.0
`
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	deps := parseGoMod(path)

	if len(deps) != 4 {
		t.Fatalf("parseGoMod() returned %d deps, want 4", len(deps))
	}

	// Check a direct dep
	found := false
	for _, d := range deps {
		if d.Name == "github.com/google/uuid" {
			found = true
			if d.Version != "v1.6.0" {
				t.Errorf("uuid version = %q, want v1.6.0", d.Version)
			}
			if d.Dev {
				t.Error("uuid marked as dev, want false")
			}
		}
	}
	if !found {
		t.Error("did not find github.com/google/uuid in deps")
	}

	// Check indirect dep
	for _, d := range deps {
		if d.Name == "golang.org/x/sync" {
			if !d.Dev {
				t.Error("sync should be marked as dev/indirect")
			}
		}
	}

	// Check single-line require
	for _, d := range deps {
		if d.Name == "github.com/single/dep" {
			if d.Version != "v0.1.0" {
				t.Errorf("single dep version = %q, want v0.1.0", d.Version)
			}
		}
	}
}

func TestParseGoMod_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(path, []byte("module example.com/test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}
	deps := parseGoMod(path)
	if len(deps) != 0 {
		t.Errorf("parseGoMod() returned %d deps for empty go.mod, want 0", len(deps))
	}
}

func TestParseGoMod_Nonexistent(t *testing.T) {
	deps := parseGoMod("/nonexistent/go.mod")
	if deps != nil {
		t.Errorf("parseGoMod() returned %v for nonexistent file, want nil", deps)
	}
}

func TestParsePackageJSON(t *testing.T) {
	content := `{
  "name": "test-app",
  "dependencies": {
    "express": "^4.18.0",
    "react": "^18.2.0"
  },
  "devDependencies": {
    "jest": "^29.0.0",
    "typescript": "^5.0.0"
  }
}`
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	deps := parsePackageJSON(path)
	if len(deps) != 4 {
		t.Fatalf("parsePackageJSON() returned %d deps, want 4", len(deps))
	}

	devCount := 0
	for _, d := range deps {
		if d.Dev {
			devCount++
		}
	}
	if devCount != 2 {
		t.Errorf("dev deps = %d, want 2", devCount)
	}
}

func TestParseRequirementsTxt(t *testing.T) {
	content := `# Python dependencies
flask==2.3.0
requests>=2.28.0
numpy
# pinned
pandas==1.5.3
-r other-requirements.txt
`
	dir := t.TempDir()
	path := filepath.Join(dir, "requirements.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	deps := parseRequirementsTxt(path)
	if len(deps) != 4 {
		t.Fatalf("parseRequirementsTxt() returned %d deps, want 4", len(deps))
	}

	// Check versioned dep
	for _, d := range deps {
		if d.Name == "flask" && d.Version == "" {
			t.Error("flask should have a version")
		}
	}

	// Check unversioned dep
	for _, d := range deps {
		if d.Name == "numpy" && d.Version != "" {
			t.Errorf("numpy version = %q, want empty", d.Version)
		}
	}
}

func TestParsePackageJSON_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	if err := os.WriteFile(path, []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatal(err)
	}
	deps := parsePackageJSON(path)
	if len(deps) != 0 {
		t.Errorf("parsePackageJSON() returned %d deps for empty package.json, want 0", len(deps))
	}
}
