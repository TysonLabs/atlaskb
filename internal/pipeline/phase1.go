package pipeline

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Manifest struct {
	RepoPath   string              `json:"repo_path"`
	Files      []FileInfo          `json:"files"`
	Stack      StackInfo           `json:"stack"`
	Stats      ManifestStats       `json:"stats"`
	FilesByClass map[FileClass][]FileInfo `json:"files_by_class"`
}

type StackInfo struct {
	Languages  []string `json:"languages"`
	Frameworks []string `json:"frameworks"`
	BuildTools []string `json:"build_tools"`
}

type ManifestStats struct {
	TotalFiles      int   `json:"total_files"`
	AnalyzableFiles int   `json:"analyzable_files"`
	TotalBytes      int64 `json:"total_bytes"`
	ByClass         map[FileClass]int `json:"by_class"`
	ByLanguage      map[string]int    `json:"by_language"`
}

func RunPhase1(repoPath string, excludeDirs ...[]string) (*Manifest, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	// Flatten the optional excludeDirs parameter
	var excluded []string
	if len(excludeDirs) > 0 && excludeDirs[0] != nil {
		excluded = excludeDirs[0]
	}

	manifest := &Manifest{
		RepoPath:     absPath,
		FilesByClass: make(map[FileClass][]FileInfo),
		Stats: ManifestStats{
			ByClass:    make(map[FileClass]int),
			ByLanguage: make(map[string]int),
		},
	}

	err = filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip files we can't read
		}

		// Skip .git directory
		if d.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == ".idea" || base == ".vscode" {
				return filepath.SkipDir
			}
			// Skip vendored directories early
			if vendorDirs[strings.ToLower(base)] {
				return filepath.SkipDir
			}
			// Skip user-configured exclude dirs
			if len(excluded) > 0 {
				relDir, relErr := filepath.Rel(absPath, path)
				if relErr == nil {
					for _, ex := range excluded {
						if relDir == ex || strings.HasPrefix(relDir, ex+"/") {
							return filepath.SkipDir
						}
					}
				}
			}
			return nil
		}

		relPath, err := filepath.Rel(absPath, path)
		if err != nil {
			return nil
		}

		// Skip test files by naming convention
		if isTestFile(relPath) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		fi := ClassifyFile(relPath, info.Size())
		manifest.Files = append(manifest.Files, fi)
		manifest.FilesByClass[fi.Class] = append(manifest.FilesByClass[fi.Class], fi)
		manifest.Stats.TotalFiles++
		manifest.Stats.TotalBytes += info.Size()
		manifest.Stats.ByClass[fi.Class]++
		if fi.Language != "" {
			manifest.Stats.ByLanguage[fi.Language]++
		}
		if ShouldAnalyze(fi) {
			manifest.Stats.AnalyzableFiles++
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	manifest.Stack = detectStack(manifest)

	return manifest, nil
}

func detectStack(m *Manifest) StackInfo {
	si := StackInfo{}

	// Detect languages (sorted by file count)
	type langCount struct {
		lang  string
		count int
	}
	var langs []langCount
	for lang, count := range m.Stats.ByLanguage {
		langs = append(langs, langCount{lang, count})
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i].count > langs[j].count })
	for _, lc := range langs {
		si.Languages = append(si.Languages, lc.lang)
	}

	// Detect frameworks and build tools from build/config files
	for _, fi := range m.FilesByClass[ClassBuild] {
		base := strings.ToLower(filepath.Base(fi.Path))
		switch base {
		case "go.mod":
			si.BuildTools = append(si.BuildTools, "go modules")
		case "package.json":
			si.BuildTools = append(si.BuildTools, "npm")
			detectNodeFrameworks(m.RepoPath, fi.Path, &si)
		case "cargo.toml":
			si.BuildTools = append(si.BuildTools, "cargo")
		case "pyproject.toml", "requirements.txt":
			si.BuildTools = append(si.BuildTools, "pip")
		case "makefile":
			si.BuildTools = append(si.BuildTools, "make")
		case "pom.xml":
			si.BuildTools = append(si.BuildTools, "maven")
		case "build.gradle":
			si.BuildTools = append(si.BuildTools, "gradle")
		}
	}

	// Detect frameworks from config files
	for _, fi := range m.FilesByClass[ClassConfig] {
		base := strings.ToLower(filepath.Base(fi.Path))
		switch {
		case strings.HasPrefix(base, "next.config"):
			si.Frameworks = appendUnique(si.Frameworks, "Next.js")
		case strings.HasPrefix(base, "vite.config"):
			si.Frameworks = appendUnique(si.Frameworks, "Vite")
		case base == "dockerfile" || base == "docker-compose.yml" || base == "docker-compose.yaml":
			si.Frameworks = appendUnique(si.Frameworks, "Docker")
		}
	}

	return si
}

func detectNodeFrameworks(repoPath, pkgPath string, si *StackInfo) {
	data, err := os.ReadFile(filepath.Join(repoPath, pkgPath))
	if err != nil {
		return
	}
	content := string(data)
	if strings.Contains(content, "\"react\"") {
		si.Frameworks = appendUnique(si.Frameworks, "React")
	}
	if strings.Contains(content, "\"express\"") {
		si.Frameworks = appendUnique(si.Frameworks, "Express")
	}
	if strings.Contains(content, "\"next\"") {
		si.Frameworks = appendUnique(si.Frameworks, "Next.js")
	}
	if strings.Contains(content, "\"vue\"") {
		si.Frameworks = appendUnique(si.Frameworks, "Vue")
	}
	if strings.Contains(content, "\"fastify\"") {
		si.Frameworks = appendUnique(si.Frameworks, "Fastify")
	}
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
