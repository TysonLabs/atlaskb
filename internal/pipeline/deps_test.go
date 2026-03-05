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

func TestParsePyprojectToml(t *testing.T) {
	path := writeTestFile(t, "pyproject.toml", `
[project]
dependencies = [
  "fastapi>=0.100.0",
  "uvicorn[standard]==0.30.0 ; python_version >= '3.10'"
]

[tool.poetry.dependencies]
python = "^3.11"
requests = "^2.32.0"
httpx = { version = "0.27.0", extras = ["http2"] }

[tool.poetry.dev-dependencies]
pytest = "^8.3.0"
ruff = { version = "0.5.0" }
`)

	deps := parsePyprojectToml(path)
	if len(deps) != 6 {
		t.Fatalf("parsePyprojectToml() returned %d deps, want 6", len(deps))
	}

	assertDep(t, deps, "fastapi", "0.100.0", false)
	assertDep(t, deps, "uvicorn", "0.30.0", false)
	assertDep(t, deps, "requests", "^2.32.0", false)
	assertDep(t, deps, "httpx", "0.27.0", false)
	assertDep(t, deps, "pytest", "^8.3.0", true)
	assertDep(t, deps, "ruff", "0.5.0", true)
}

func TestParseCargoToml(t *testing.T) {
	path := writeTestFile(t, "Cargo.toml", `
[package]
name = "demo"
version = "0.1.0"

[dependencies]
serde = "1.0"
tokio = { version = "1.40", features = ["rt"] }

[dev-dependencies]
criterion = { version = "0.5" }
`)

	deps := parseCargoToml(path)
	if len(deps) != 3 {
		t.Fatalf("parseCargoToml() returned %d deps, want 3", len(deps))
	}

	assertDep(t, deps, "serde", "1.0", false)
	assertDep(t, deps, "tokio", "1.40", false)
	assertDep(t, deps, "criterion", "0.5", true)
}

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want string
	}{
		{name: "string", in: "1.2.3", want: "1.2.3"},
		{name: "map version", in: map[string]interface{}{"version": "4.5.6"}, want: "4.5.6"},
		{name: "map no version", in: map[string]interface{}{"path": "../x"}, want: ""},
		{name: "unknown type", in: 123, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractVersion(tc.in); got != tc.want {
				t.Fatalf("extractVersion() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseGemfile(t *testing.T) {
	path := writeTestFile(t, "Gemfile", `
source "https://rubygems.org"
gem "rails", "7.1.0"
gem "pg"
`)
	deps := parseGemfile(path)
	if len(deps) != 2 {
		t.Fatalf("parseGemfile() returned %d deps, want 2", len(deps))
	}
	assertDep(t, deps, "rails", "7.1.0", false)
	assertDep(t, deps, "pg", "", false)
}

func TestParsePomXML(t *testing.T) {
	path := writeTestFile(t, "pom.xml", `
<project>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>6.1.0</version>
    </dependency>
    <dependency>
      <groupId>org.junit.jupiter</groupId>
      <artifactId>junit-jupiter</artifactId>
      <version>5.10.2</version>
      <scope>test</scope>
    </dependency>
  </dependencies>
</project>
`)
	deps := parsePomXML(path)
	if len(deps) != 2 {
		t.Fatalf("parsePomXML() returned %d deps, want 2", len(deps))
	}
	assertDep(t, deps, "org.springframework:spring-core", "6.1.0", false)
	assertDep(t, deps, "org.junit.jupiter:junit-jupiter", "5.10.2", true)
}

func TestParseBuildGradle(t *testing.T) {
	path := writeTestFile(t, "build.gradle", `
dependencies {
  implementation "org.springframework:spring-core:6.1.0"
  api "com.google.guava:guava:33.1.0"
  testImplementation "org.junit.jupiter:junit-jupiter:5.10.2"
}
`)
	deps := parseBuildGradle(path)
	if len(deps) != 3 {
		t.Fatalf("parseBuildGradle() returned %d deps, want 3", len(deps))
	}
	assertDep(t, deps, "org.springframework:spring-core", "6.1.0", false)
	assertDep(t, deps, "com.google.guava:guava", "33.1.0", false)
	assertDep(t, deps, "org.junit.jupiter:junit-jupiter", "5.10.2", true)
}

func TestParseComposerJSON(t *testing.T) {
	path := writeTestFile(t, "composer.json", `
{
  "require": {
    "php": "^8.3",
    "laravel/framework": "^11.0"
  },
  "require-dev": {
    "pestphp/pest": "^2.0"
  }
}
`)
	deps := parseComposerJSON(path)
	if len(deps) != 2 {
		t.Fatalf("parseComposerJSON() returned %d deps, want 2", len(deps))
	}
	assertDep(t, deps, "laravel/framework", "^11.0", false)
	assertDep(t, deps, "pestphp/pest", "^2.0", true)
}

func TestParsePubspecYAML(t *testing.T) {
	path := writeTestFile(t, "pubspec.yaml", `
name: sample
dependencies:
  flutter:
    sdk: flutter
  http: ^1.2.2
  my_local_pkg:
    path: ../my_local_pkg
dev_dependencies:
  flutter_test:
    sdk: flutter
  test: ^1.25.8
`)
	deps := parsePubspecYAML(path)
	if len(deps) != 3 {
		t.Fatalf("parsePubspecYAML() returned %d deps, want 3", len(deps))
	}
	assertDep(t, deps, "http", "^1.2.2", false)
	assertDep(t, deps, "my_local_pkg", "", false)
	assertDep(t, deps, "test", "^1.25.8", true)
}

func TestParseMixExs(t *testing.T) {
	path := writeTestFile(t, "mix.exs", `
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:plug_cowboy, "~> 2.7"}
  ]
end
`)
	deps := parseMixExs(path)
	if len(deps) != 2 {
		t.Fatalf("parseMixExs() returned %d deps, want 2", len(deps))
	}
	assertDep(t, deps, "phoenix", "~> 1.7", false)
	assertDep(t, deps, "plug_cowboy", "~> 2.7", false)
}

func TestParsePackageSwift(t *testing.T) {
	path := writeTestFile(t, "Package.swift", `
let package = Package(
  dependencies: [
    .package(url: "https://github.com/Alamofire/Alamofire.git", from: "5.9.1"),
    .package(url: "https://github.com/apple/swift-collections.git", exact: "1.1.1")
  ]
)
`)
	deps := parsePackageSwift(path)
	if len(deps) != 2 {
		t.Fatalf("parsePackageSwift() returned %d deps, want 2", len(deps))
	}
	assertDep(t, deps, "Alamofire", "5.9.1", false)
	assertDep(t, deps, "swift-collections", "1.1.1", false)
}

func TestParseCsproj(t *testing.T) {
	path := writeTestFile(t, "demo.csproj", `
<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
    <PackageReference Include="xunit" Version="2.8.0" />
  </ItemGroup>
</Project>
`)
	deps := parseCsproj(path)
	if len(deps) != 2 {
		t.Fatalf("parseCsproj() returned %d deps, want 2", len(deps))
	}
	assertDep(t, deps, "Newtonsoft.Json", "13.0.3", false)
	assertDep(t, deps, "xunit", "2.8.0", false)
}

func TestExtractDependencies_SourceField(t *testing.T) {
	repoDir := t.TempDir()
	goModPath := filepath.Join(repoDir, "go.mod")
	packagePath := filepath.Join(repoDir, "web", "package.json")
	csprojPath := filepath.Join(repoDir, "src", "app.csproj")
	if err := os.MkdirAll(filepath.Dir(packagePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(csprojPath), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(goModPath, []byte("module example.com/x\n\nrequire github.com/google/uuid v1.6.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagePath, []byte(`{"dependencies":{"react":"^18.0.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(csprojPath, []byte(`<Project><ItemGroup><PackageReference Include="Dapper" Version="2.1.0" /></ItemGroup></Project>`), 0644); err != nil {
		t.Fatal(err)
	}

	manifest := &Manifest{
		Files: []FileInfo{
			{Path: "go.mod"},
			{Path: "web/package.json"},
			{Path: "src/app.csproj"},
		},
	}
	deps := ExtractDependencies(repoDir, manifest)
	if len(deps) != 3 {
		t.Fatalf("ExtractDependencies() returned %d deps, want 3", len(deps))
	}

	for _, dep := range deps {
		if dep.Source == "" {
			t.Fatalf("dependency %q missing Source", dep.Name)
		}
	}
}

func TestParsers_NonExistentFile(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) []Dependency
	}{
		{name: "pyproject", fn: parsePyprojectToml},
		{name: "cargo", fn: parseCargoToml},
		{name: "gemfile", fn: parseGemfile},
		{name: "pom", fn: parsePomXML},
		{name: "gradle", fn: parseBuildGradle},
		{name: "composer", fn: parseComposerJSON},
		{name: "pubspec", fn: parsePubspecYAML},
		{name: "mix", fn: parseMixExs},
		{name: "swift", fn: parsePackageSwift},
		{name: "csproj", fn: parseCsproj},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(filepath.Join(t.TempDir(), "missing")); got != nil {
				t.Fatalf("parser returned %v, want nil", got)
			}
		})
	}
}

func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertDep(t *testing.T, deps []Dependency, name, version string, dev bool) {
	t.Helper()
	for _, d := range deps {
		if d.Name == name {
			if d.Version != version {
				t.Fatalf("dep %q version = %q, want %q", name, d.Version, version)
			}
			if d.Dev != dev {
				t.Fatalf("dep %q dev = %v, want %v", name, d.Dev, dev)
			}
			return
		}
	}
	t.Fatalf("dep %q not found", name)
}
