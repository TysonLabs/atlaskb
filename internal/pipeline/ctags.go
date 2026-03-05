package pipeline

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// CtagsSymbol represents a single symbol extracted by Universal Ctags.
type CtagsSymbol struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Kind      string `json:"kind"`
	Scope     string `json:"scope"`
	ScopeKind string `json:"scopeKind"`
	Signature string `json:"signature"`
	TypeRef   string `json:"typeref"`
}

// EntityEntry is a canonical entity name derived from ctags output.
type EntityEntry struct {
	Name          string // Short name, e.g. "ChannelRegistry"
	QualifiedName string // Canonical name, e.g. "channels::ChannelRegistry"
	Kind          string // ctags kind: "class", "function", "interface", etc.
	Path          string // Relative file path
	Line          int
	Signature     string // e.g. "(ctx context.Context, cfg Config)"
	TypeRef       string // e.g. "typename:(*Stats, error)"
}

// ctagsRawEntry matches the JSON output of `ctags --output-format=json`.
type ctagsRawEntry struct {
	Type      string `json:"_type"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Kind      string `json:"kind"`
	Scope     string `json:"scope"`
	ScopeKind string `json:"scopeKind"`
	Signature string `json:"signature"`
	TypeRef   string `json:"typeref"`
}

// topLevelKinds are the ctags kinds we consider top-level entities.
var topLevelKinds = map[string]bool{
	"class":     true,
	"interface": true,
	"function":  true,
	"type":      true,
	"alias":     true, // TypeScript type aliases (type Foo = ...)
	"struct":    true,
	"trait":     true,
	"module":    true,
	"enum":      true,
	"constant":  true,
	"variable":  true, // top-level exported vars
}

// methodKinds are ctags kinds that represent methods/members on a type.
// These are included in the roster with their owner as a prefix.
var methodKinds = map[string]bool{
	"function": true, // Rust impl methods, Go methods
	"method":   true, // Python/JS class methods
	"member":   true, // C++ class members
}

// methodScopeKinds are the scope kinds that indicate a method belongs to a type.
var methodScopeKinds = map[string]bool{
	"struct":    true, // Rust impl, Go receiver
	"type":      true, // Go type methods
	"class":     true, // Python/JS/TS class methods
	"interface": true, // Go interface methods, TS interface methods
	"trait":     true, // Rust trait methods
	"enum":      true, // Rust enum methods
	"impl":      true, // Rust impl blocks
}

// RunCtags executes Universal Ctags on the given repository path and returns
// symbols grouped by relative file path. Returns nil, nil if ctags is not installed.
func RunCtags(repoPath string, excludes *ExclusionSet) (map[string][]CtagsSymbol, error) {
	// Check if ctags is available
	ctagsBin, err := exec.LookPath("ctags")
	if err != nil {
		return nil, nil // ctags not installed — graceful degradation
	}

	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	args := []string{
		"--output-format=json",
		"--fields=+nKS+l+S+t", // line number, Kind (full), Scope, language, Signature, typeref
		"--extras=-F",         // no file-scope tags (reduces noise)
		"--recurse",
		"--exclude=.git",
		"--exclude=node_modules",
		"--exclude=vendor",
		"--exclude=dist",
		"--exclude=build",
		"--exclude=__pycache__",
		"--exclude=.mypy_cache",
		"--exclude=target",
		"--exclude=.myrmex",
	}
	if excludes != nil {
		for _, dir := range excludes.DirectoryExcludes() {
			args = append(args, "--exclude="+dir)
		}
	}
	args = append(args, absPath)

	// Run ctags with JSON output, all fields including scope
	cmd := exec.Command(ctagsBin, args...)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running ctags: %w", err)
	}

	// Parse JSON lines output
	result := make(map[string][]CtagsSymbol)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry ctagsRawEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // skip malformed lines
		}

		// Skip non-tag entries (ctags outputs program info as type "program")
		if entry.Type != "tag" {
			continue
		}

		// Include top-level symbols AND methods on types
		isTopLevel := topLevelKinds[entry.Kind] && (entry.Scope == "" || entry.ScopeKind == "module" || entry.ScopeKind == "package")
		isMethod := methodKinds[entry.Kind] && entry.Scope != "" && methodScopeKinds[entry.ScopeKind]

		if !isTopLevel && !isMethod {
			continue
		}

		// Make path relative to repo root
		relPath, err := filepath.Rel(absPath, entry.Path)
		if err != nil {
			relPath = entry.Path
		}
		if excludes != nil && excludes.ShouldExclude(relPath, false) {
			continue
		}

		// Skip constants/variables in test files — these are almost always local
		// test variables (e.g. `const result = ...` inside describe/it blocks)
		// that ctags can't distinguish from real top-level declarations
		if (entry.Kind == "constant" || entry.Kind == "variable") && isTestFile(relPath) {
			continue
		}

		sym := CtagsSymbol{
			Name:      entry.Name,
			Path:      relPath,
			Line:      entry.Line,
			Kind:      entry.Kind,
			Scope:     entry.Scope,
			ScopeKind: entry.ScopeKind,
			Signature: entry.Signature,
			TypeRef:   entry.TypeRef,
		}
		result[relPath] = append(result[relPath], sym)
	}

	return result, nil
}

// BuildEntityRoster converts grouped ctags symbols into a flat list of EntityEntry
// with canonical qualified names following the <module>::<Name> convention.
func BuildEntityRoster(symbols map[string][]CtagsSymbol) []EntityEntry {
	var roster []EntityEntry

	for _, syms := range symbols {
		for _, sym := range syms {
			qn := buildQualifiedName(sym)
			roster = append(roster, EntityEntry{
				Name:          sym.Name,
				QualifiedName: qn,
				Kind:          sym.Kind,
				Path:          sym.Path,
				Line:          sym.Line,
				Signature:     sym.Signature,
				TypeRef:       sym.TypeRef,
			})
		}
	}

	// Sort by path then line for deterministic output
	sort.Slice(roster, func(i, j int) bool {
		if roster[i].Path != roster[j].Path {
			return roster[i].Path < roster[j].Path
		}
		return roster[i].Line < roster[j].Line
	})

	return roster
}

// ComputeEndLines estimates the end line for each entity in the roster by using
// the next entity's start line in the same file. The roster must be sorted by (Path, Line).
func ComputeEndLines(roster []EntityEntry) map[string]int {
	endLines := make(map[string]int)
	for i := 0; i < len(roster); i++ {
		if i+1 < len(roster) && roster[i].Path == roster[i+1].Path {
			endLines[roster[i].QualifiedName] = roster[i+1].Line - 1
		}
	}
	return endLines
}

// buildQualifiedName creates a canonical qualified name from a ctags symbol.
// Convention: <module>::<Name> for top-level symbols, <module>::<Owner>.<Method> for methods.
//
// Examples:
//
//	src/channels/registry.ts → channels::ChannelRegistry
//	internal/storage/memory.go → storage::MemoryStorage
//	validators/email.py → validators::EmailValidator
//	impl MemoryStorage::Save → storage::MemoryStorage.Save
func buildQualifiedName(sym CtagsSymbol) string {
	module := deriveModuleName(sym.Path)
	// Methods: include owner as prefix with "." separator
	if sym.Scope != "" && methodScopeKinds[sym.ScopeKind] {
		return module + "::" + sym.Scope + "." + sym.Name
	}
	return module + "::" + sym.Name
}

// deriveModuleName extracts a module/package name from a file path.
// Uses the parent directory name, stripping common prefixes like "src", "lib",
// "internal", "pkg".
//
// Examples:
//
//	src/channels/registry.ts → channels
//	internal/storage/memory.go → storage
//	validators/email.py → validators
//	cmd/server/main.go → server
//	main.go → main
func deriveModuleName(path string) string {
	// Strip extension
	dir := filepath.Dir(path)

	if dir == "." {
		// Top-level file: use filename without extension as module
		base := filepath.Base(path)
		ext := filepath.Ext(base)
		return strings.TrimSuffix(base, ext)
	}

	// Split path into parts
	parts := strings.Split(filepath.ToSlash(dir), "/")

	// Strip common prefixes
	stripPrefixes := map[string]bool{
		"src": true, "lib": true, "internal": true, "pkg": true,
		"app": true, "source": true,
	}

	// Find the first meaningful part (skip prefixes)
	for i, p := range parts {
		if !stripPrefixes[strings.ToLower(p)] {
			// Use this part as the module name
			// If there are more parts after, join with "." for nested modules
			remaining := parts[i:]
			if len(remaining) == 1 {
				return remaining[0]
			}
			// For deeply nested paths, use last 2 meaningful segments joined with "."
			if len(remaining) > 2 {
				remaining = remaining[len(remaining)-2:]
			}
			return strings.Join(remaining, ".")
		}
	}

	// All parts were prefixes — use the last one
	return parts[len(parts)-1]
}

// FormatRosterForPrompt formats the entity roster for inclusion in an LLM prompt.
// It produces two sections: repo-wide roster and file-specific roster.
func FormatRosterForPrompt(roster []EntityEntry, filePath string) string {
	if len(roster) == 0 {
		return ""
	}

	var sb strings.Builder

	// File-specific entities
	var fileEntities []EntityEntry
	for _, e := range roster {
		if e.Path == filePath {
			fileEntities = append(fileEntities, e)
		}
	}

	// Repo-wide roster (limit to keep prompt manageable)
	maxRosterLines := 80
	sb.WriteString("\n## Known Entities (from static analysis)\n\n")
	sb.WriteString("The following entities were detected via static analysis (ctags). ")
	sb.WriteString("When you encounter these in the code, use EXACTLY the qualified_name shown. ")
	sb.WriteString("Do NOT invent alternative names.\n\n")

	count := 0
	for _, e := range roster {
		if count >= maxRosterLines {
			remaining := len(roster) - count
			sb.WriteString(fmt.Sprintf("... and %d more entities\n", remaining))
			break
		}
		if e.Signature != "" {
			returns := ""
			if e.TypeRef != "" {
				returns = " returns " + e.TypeRef
			}
			sb.WriteString(fmt.Sprintf("- %s (%s, %s:%d) → %s%s\n", e.QualifiedName, e.Kind, e.Path, e.Line, e.Signature, returns))
		} else {
			sb.WriteString(fmt.Sprintf("- %s (%s, %s:%d)\n", e.QualifiedName, e.Kind, e.Path, e.Line))
		}
		count++
	}

	// File-specific section
	if len(fileEntities) > 0 {
		sb.WriteString(fmt.Sprintf("\n## Entities in THIS File (%s):\n\n", filePath))
		for _, e := range fileEntities {
			if e.Signature != "" {
				returns := ""
				if e.TypeRef != "" {
					returns = " returns " + e.TypeRef
				}
				sb.WriteString(fmt.Sprintf("- %s (%s, line %d) → %s%s\n", e.QualifiedName, e.Kind, e.Line, e.Signature, returns))
			} else {
				sb.WriteString(fmt.Sprintf("- %s (%s, line %d)\n", e.QualifiedName, e.Kind, e.Line))
			}
		}
		sb.WriteString("\nUse these exact qualified_names for entities in this file.\n")
	}

	return sb.String()
}
