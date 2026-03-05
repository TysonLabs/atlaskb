package pipeline

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Dependency represents a parsed dependency from a manifest file.
type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Dev     bool   `json:"dev"`
	Source  string `json:"source"` // which file it was parsed from
}

// ExtractDependencies deterministically parses dependencies from known manifest files.
func ExtractDependencies(repoPath string, manifest *Manifest) []Dependency {
	var deps []Dependency

	for _, fi := range manifest.Files {
		base := strings.ToLower(filepath.Base(fi.Path))
		fullPath := filepath.Join(repoPath, fi.Path)

		var parsed []Dependency
		switch base {
		case "go.mod":
			parsed = parseGoMod(fullPath)
		case "package.json":
			parsed = parsePackageJSON(fullPath)
		case "requirements.txt":
			parsed = parseRequirementsTxt(fullPath)
		case "pyproject.toml":
			parsed = parsePyprojectToml(fullPath)
		case "cargo.toml":
			parsed = parseCargoToml(fullPath)
		case "gemfile":
			parsed = parseGemfile(fullPath)
		case "pom.xml":
			parsed = parsePomXML(fullPath)
		case "build.gradle", "build.gradle.kts":
			parsed = parseBuildGradle(fullPath)
		case "composer.json":
			parsed = parseComposerJSON(fullPath)
		case "pubspec.yaml":
			parsed = parsePubspecYAML(fullPath)
		case "mix.exs":
			parsed = parseMixExs(fullPath)
		case "package.swift":
			parsed = parsePackageSwift(fullPath)
		}

		// Also handle *.csproj
		if strings.HasSuffix(strings.ToLower(fi.Path), ".csproj") {
			parsed = parseCsproj(fullPath)
		}

		for i := range parsed {
			parsed[i].Source = fi.Path
		}
		deps = append(deps, parsed...)
	}

	return deps
}

// parseGoMod parses go.mod using simple line parsing (no external dependency needed).
func parseGoMod(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var deps []Dependency
	lines := strings.Split(string(data), "\n")
	inRequire := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "require (") || strings.HasPrefix(line, "require(") {
			inRequire = true
			continue
		}
		if line == ")" {
			inRequire = false
			continue
		}

		// Single-line require
		if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				indirect := strings.Contains(line, "// indirect")
				deps = append(deps, Dependency{
					Name:    parts[1],
					Version: parts[2],
					Dev:     indirect,
				})
			}
			continue
		}

		if inRequire {
			// Skip comments and empty lines
			if line == "" || strings.HasPrefix(line, "//") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				indirect := strings.Contains(line, "// indirect")
				deps = append(deps, Dependency{
					Name:    parts[0],
					Version: parts[1],
					Dev:     indirect,
				})
			}
		}
	}

	return deps
}

func parsePackageJSON(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}

	var deps []Dependency
	for name, ver := range pkg.Dependencies {
		deps = append(deps, Dependency{Name: name, Version: ver, Dev: false})
	}
	for name, ver := range pkg.DevDependencies {
		deps = append(deps, Dependency{Name: name, Version: ver, Dev: true})
	}
	return deps
}

var reqLineRe = regexp.MustCompile(`^([a-zA-Z0-9_-][a-zA-Z0-9._-]*)\s*(?:[=!<>~]+\s*(.+))?`)
var pep621DepRe = regexp.MustCompile(`^([A-Za-z0-9._-]+)(?:\[[^\]]+\])?\s*(.*)$`)

func parseRequirementsTxt(path string) []Dependency {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var deps []Dependency
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		if m := reqLineRe.FindStringSubmatch(line); len(m) >= 2 {
			deps = append(deps, Dependency{Name: m[1], Version: m[2]})
		}
	}
	return deps
}

func parsePyprojectToml(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var pyproject struct {
		Project struct {
			Dependencies []string `toml:"dependencies"`
		} `toml:"project"`
		Tool struct {
			Poetry struct {
				Dependencies    map[string]interface{} `toml:"dependencies"`
				DevDependencies map[string]interface{} `toml:"dev-dependencies"`
			} `toml:"poetry"`
		} `toml:"tool"`
	}
	if err := toml.Unmarshal(data, &pyproject); err != nil {
		return nil
	}

	var deps []Dependency

	// PEP 621 style
	for _, dep := range pyproject.Project.Dependencies {
		spec := strings.TrimSpace(dep)
		if spec == "" {
			continue
		}
		// Strip markers (e.g. '; python_version >= "3.10"').
		if idx := strings.Index(spec, ";"); idx >= 0 {
			spec = strings.TrimSpace(spec[:idx])
		}
		m := pep621DepRe.FindStringSubmatch(spec)
		if len(m) != 3 {
			continue
		}
		name := strings.TrimSpace(m[1])
		constraint := strings.TrimSpace(m[2])
		version := ""
		if constraint != "" {
			token := strings.FieldsFunc(constraint, func(r rune) bool {
				return r == ',' || r == ' '
			})
			if len(token) > 0 {
				version = strings.TrimLeft(token[0], "<>=!~")
			}
		}
		if name != "" {
			deps = append(deps, Dependency{Name: name, Version: version})
		}
	}

	// Poetry style
	for name, val := range pyproject.Tool.Poetry.Dependencies {
		if name == "python" {
			continue
		}
		deps = append(deps, Dependency{Name: name, Version: extractVersion(val)})
	}
	for name, val := range pyproject.Tool.Poetry.DevDependencies {
		deps = append(deps, Dependency{Name: name, Version: extractVersion(val), Dev: true})
	}

	return deps
}

func parseCargoToml(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var cargo struct {
		Dependencies    map[string]interface{} `toml:"dependencies"`
		DevDependencies map[string]interface{} `toml:"dev-dependencies"`
	}
	if err := toml.Unmarshal(data, &cargo); err != nil {
		return nil
	}

	var deps []Dependency
	for name, val := range cargo.Dependencies {
		ver := extractVersion(val)
		deps = append(deps, Dependency{Name: name, Version: ver})
	}
	for name, val := range cargo.DevDependencies {
		ver := extractVersion(val)
		deps = append(deps, Dependency{Name: name, Version: ver, Dev: true})
	}
	return deps
}

func extractVersion(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case map[string]interface{}:
		if ver, ok := v["version"]; ok {
			return fmt.Sprint(ver)
		}
	}
	return ""
}

var gemRe = regexp.MustCompile(`gem\s+["']([^"']+)["'](?:\s*,\s*["']([^"']+)["'])?`)

func parseGemfile(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var deps []Dependency
	for _, m := range gemRe.FindAllStringSubmatch(string(data), -1) {
		dep := Dependency{Name: m[1]}
		if len(m) > 2 {
			dep.Version = m[2]
		}
		deps = append(deps, dep)
	}
	return deps
}

func parsePomXML(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var pom struct {
		Dependencies struct {
			Dependency []struct {
				GroupID    string `xml:"groupId"`
				ArtifactID string `xml:"artifactId"`
				Version    string `xml:"version"`
				Scope      string `xml:"scope"`
			} `xml:"dependency"`
		} `xml:"dependencies"`
	}
	if err := xml.Unmarshal(data, &pom); err != nil {
		return nil
	}

	var deps []Dependency
	for _, d := range pom.Dependencies.Dependency {
		deps = append(deps, Dependency{
			Name:    d.GroupID + ":" + d.ArtifactID,
			Version: d.Version,
			Dev:     d.Scope == "test",
		})
	}
	return deps
}

var gradleDepRe = regexp.MustCompile(`(?:implementation|api|compileOnly|runtimeOnly|testImplementation|testRuntimeOnly)\s*[("']([^)"']+)[)"']`)

func parseBuildGradle(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var deps []Dependency
	for _, m := range gradleDepRe.FindAllStringSubmatch(string(data), -1) {
		dep := Dependency{Name: m[1]}
		if strings.Contains(m[0], "test") {
			dep.Dev = true
		}
		// Try to split group:artifact:version
		parts := strings.SplitN(m[1], ":", 3)
		if len(parts) == 3 {
			dep.Name = parts[0] + ":" + parts[1]
			dep.Version = parts[2]
		}
		deps = append(deps, dep)
	}
	return deps
}

func parseComposerJSON(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var composer struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if err := json.Unmarshal(data, &composer); err != nil {
		return nil
	}

	var deps []Dependency
	for name, ver := range composer.Require {
		if name == "php" {
			continue
		}
		deps = append(deps, Dependency{Name: name, Version: ver})
	}
	for name, ver := range composer.RequireDev {
		deps = append(deps, Dependency{Name: name, Version: ver, Dev: true})
	}
	return deps
}

func parsePubspecYAML(path string) []Dependency {
	// Simple line-based YAML parsing for dependencies section
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var deps []Dependency
	lines := strings.Split(string(data), "\n")
	inDeps := false
	isDev := false
	sectionIndent := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if trimmed == "dependencies:" {
			inDeps = true
			isDev = false
			sectionIndent = indent
			continue
		}
		if trimmed == "dev_dependencies:" {
			inDeps = true
			isDev = true
			sectionIndent = indent
			continue
		}
		// New top-level key
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			inDeps = false
			continue
		}
		// Only parse direct children in the dependencies block.
		if inDeps && indent != sectionIndent+2 {
			continue
		}
		if inDeps && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) >= 1 {
				name := strings.TrimSpace(parts[0])
				ver := ""
				if len(parts) > 1 {
					ver = strings.TrimSpace(parts[1])
				}
				if name != "" && name != "flutter" && name != "flutter_test" {
					deps = append(deps, Dependency{Name: name, Version: ver, Dev: isDev})
				}
			}
		}
	}
	return deps
}

var mixDepRe = regexp.MustCompile(`\{:(\w+),\s*"([^"]*)"`)

func parseMixExs(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var deps []Dependency
	for _, m := range mixDepRe.FindAllStringSubmatch(string(data), -1) {
		deps = append(deps, Dependency{Name: m[1], Version: m[2]})
	}
	return deps
}

var swiftPkgRe = regexp.MustCompile(`\.package\s*\(\s*url:\s*"([^"]+)"(?:\s*,\s*(?:from|exact|\.upToNextMajor|\.upToNextMinor).*?"([^"]*)")?`)

func parsePackageSwift(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var deps []Dependency
	for _, m := range swiftPkgRe.FindAllStringSubmatch(string(data), -1) {
		name := m[1]
		// Extract repo name from URL
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = strings.TrimSuffix(name[idx+1:], ".git")
		}
		ver := ""
		if len(m) > 2 {
			ver = m[2]
		}
		deps = append(deps, Dependency{Name: name, Version: ver})
	}
	return deps
}

func parseCsproj(path string) []Dependency {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var project struct {
		ItemGroups []struct {
			PackageReference []struct {
				Include string `xml:"Include,attr"`
				Version string `xml:"Version,attr"`
			} `xml:"PackageReference"`
		} `xml:"ItemGroup"`
	}
	if err := xml.Unmarshal(data, &project); err != nil {
		return nil
	}

	var deps []Dependency
	for _, ig := range project.ItemGroups {
		for _, pr := range ig.PackageReference {
			deps = append(deps, Dependency{Name: pr.Include, Version: pr.Version})
		}
	}
	return deps
}
