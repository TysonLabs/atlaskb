package pipeline

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// ExclusionSet is the effective exclusion configuration for a single indexing run.
// Source precedence is CLI > repo settings > global settings, plus .atlaskbignore.
type ExclusionSet struct {
	GlobalPatterns []string `json:"global_patterns"`
	RepoPatterns   []string `json:"repo_patterns"`
	CLIPatterns    []string `json:"cli_patterns"`
	IgnorePatterns []string `json:"ignore_patterns"`
	Effective      []string `json:"effective"`

	directoryPrefixes []string
	matcher           *ignore.GitIgnore
}

// BuildExclusionSet composes exclusion inputs and loads .atlaskbignore from repoPath.
func BuildExclusionSet(repoPath string, global, perRepo, cli []string) (*ExclusionSet, error) {
	global = normalizeMatcherPatterns(global)
	perRepo = normalizeMatcherPatterns(perRepo)
	cli = normalizeMatcherPatterns(cli)

	ignorePatterns, err := loadAtlasIgnorePatterns(repoPath)
	if err != nil {
		return nil, err
	}

	effective := append([]string{}, global...)
	effective = append(effective, perRepo...)
	effective = append(effective, cli...)
	effective = append(effective, ignorePatterns...)
	effective = dedupeNonEmpty(effective)

	dirs := collectDirectoryPrefixes(global, perRepo, cli)
	allPatterns := append([]string{}, global...)
	allPatterns = append(allPatterns, perRepo...)
	allPatterns = append(allPatterns, cli...)
	allPatterns = append(allPatterns, ignorePatterns...)

	return &ExclusionSet{
		GlobalPatterns:    global,
		RepoPatterns:      perRepo,
		CLIPatterns:       cli,
		IgnorePatterns:    ignorePatterns,
		Effective:         effective,
		directoryPrefixes: dirs,
		matcher:           ignore.CompileIgnoreLines(allPatterns...),
	}, nil
}

func loadAtlasIgnorePatterns(repoPath string) ([]string, error) {
	ignorePath := filepath.Join(repoPath, ".atlaskbignore")
	f, err := os.Open(ignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening .atlaskbignore: %w", err)
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading .atlaskbignore: %w", err)
	}
	return dedupeNonEmpty(normalizeMatcherPatterns(patterns)), nil
}

func normalizeMatcherPatterns(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	for _, raw := range patterns {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		p = filepath.ToSlash(p)
		p = strings.TrimPrefix(p, "./")
		if p == "." {
			continue
		}
		out = append(out, p)
	}
	return dedupeNonEmpty(out)
}

func dedupeNonEmpty(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func collectDirectoryPrefixes(groups ...[]string) []string {
	var dirs []string
	for _, group := range groups {
		for _, p := range group {
			// Treat plain paths as directory prefixes; patterns with glob chars stay matcher-only.
			if strings.ContainsAny(p, "*?[]!") || strings.HasPrefix(p, "!") {
				continue
			}
			cp := filepath.ToSlash(strings.TrimSpace(p))
			cp = strings.TrimPrefix(cp, "./")
			cp = strings.TrimPrefix(cp, "/")
			cp = strings.TrimSuffix(cp, "/")
			if cp == "" || cp == "." {
				continue
			}
			dirs = append(dirs, cp)
		}
	}
	dirs = dedupeNonEmpty(dirs)
	slices.Sort(dirs)
	return dirs
}

func normalizeRelPath(relPath string) string {
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	relPath = strings.TrimPrefix(relPath, "./")
	relPath = strings.TrimPrefix(relPath, "/")
	return relPath
}

// ShouldExclude returns true when relPath should be skipped.
func (e *ExclusionSet) ShouldExclude(relPath string, isDir bool) bool {
	if e == nil {
		return false
	}
	relPath = normalizeRelPath(relPath)
	if relPath == "" || relPath == "." {
		return false
	}

	for _, dir := range e.directoryPrefixes {
		if relPath == dir || strings.HasPrefix(relPath, dir+"/") {
			return true
		}
	}

	if e.matcher != nil {
		if e.matcher.MatchesPath(relPath) {
			return true
		}
		if isDir && e.matcher.MatchesPath(relPath+"/") {
			return true
		}
	}
	return false
}

func (e *ExclusionSet) DirectoryExcludes() []string {
	if e == nil {
		return nil
	}
	out := make([]string, len(e.directoryPrefixes))
	copy(out, e.directoryPrefixes)
	return out
}
