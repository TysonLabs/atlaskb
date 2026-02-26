package pipeline

import (
	"path/filepath"
	"strings"
)

type FileClass string

const (
	ClassSource     FileClass = "source"
	ClassTest       FileClass = "test"
	ClassConfig     FileClass = "config"
	ClassDoc        FileClass = "doc"
	ClassBuild      FileClass = "build"
	ClassGenerated  FileClass = "generated"
	ClassVendored   FileClass = "vendored"
	ClassData       FileClass = "data"
	ClassIgnored    FileClass = "ignored"
)

type FileInfo struct {
	Path     string    `json:"path"`
	Class    FileClass `json:"class"`
	Language string    `json:"language"`
	Size     int64     `json:"size"`
}

var languageExtensions = map[string]string{
	".go":    "go",
	".py":    "python",
	".js":    "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".jsx":   "javascript",
	".rs":    "rust",
	".java":  "java",
	".rb":    "ruby",
	".c":     "c",
	".cpp":   "cpp",
	".h":     "c",
	".hpp":   "cpp",
	".cs":    "csharp",
	".swift": "swift",
	".kt":    "kotlin",
	".scala": "scala",
	".php":   "php",
	".sh":    "shell",
	".bash":  "shell",
	".zsh":   "shell",
	".lua":   "lua",
	".r":     "r",
	".sql":   "sql",
	".proto": "protobuf",
}

var configFiles = map[string]bool{
	"dockerfile":       true,
	"docker-compose.yml": true,
	"docker-compose.yaml": true,
	".env":             true,
	".env.example":     true,
	".eslintrc":        true,
	".eslintrc.js":     true,
	".eslintrc.json":   true,
	".prettierrc":      true,
	".prettierrc.js":   true,
	".prettierrc.json": true,
	"tsconfig.json":    true,
	".babelrc":         true,
	"webpack.config.js": true,
	"vite.config.ts":   true,
	"vite.config.js":   true,
	"next.config.js":   true,
	"next.config.mjs":  true,
	".goreleaser.yml":  true,
	".goreleaser.yaml": true,
	"nginx.conf":       true,
}

var buildFiles = map[string]bool{
	"go.mod":         true,
	"go.sum":         true,
	"package.json":   true,
	"package-lock.json": true,
	"yarn.lock":      true,
	"pnpm-lock.yaml": true,
	"cargo.toml":     true,
	"cargo.lock":     true,
	"gemfile":        true,
	"gemfile.lock":   true,
	"requirements.txt": true,
	"pyproject.toml": true,
	"poetry.lock":    true,
	"pom.xml":        true,
	"build.gradle":   true,
	"makefile":       true,
	"cmakelists.txt": true,
}

var vendorDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	"third_party":  true,
	".next":        true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
	".tox":         true,
	".venv":        true,
	"venv":         true,
}

var ignoredExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
	".ico": true, ".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".bz2": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".min.js": true, ".min.css": true,
	".lock": true,
}

func ClassifyFile(path string, size int64) FileInfo {
	info := FileInfo{Path: path, Size: size}
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	dir := filepath.Dir(path)

	// Check vendored directories
	for _, part := range strings.Split(dir, string(filepath.Separator)) {
		if vendorDirs[strings.ToLower(part)] {
			info.Class = ClassVendored
			return info
		}
	}

	// Check ignored extensions
	if ignoredExts[ext] {
		info.Class = ClassIgnored
		return info
	}

	// Check for generated files
	if strings.Contains(base, ".gen.") || strings.Contains(base, "_gen.") ||
		strings.Contains(base, ".generated.") || strings.Contains(base, "_generated.") ||
		strings.Contains(base, ".pb.") || strings.HasSuffix(base, "_pb2.py") {
		info.Class = ClassGenerated
		return info
	}

	// Check build files
	if buildFiles[base] {
		info.Class = ClassBuild
		return info
	}

	// Check config files
	if configFiles[base] {
		info.Class = ClassConfig
		return info
	}
	if ext == ".yml" || ext == ".yaml" || ext == ".toml" || ext == ".ini" || ext == ".cfg" {
		// CI/CD configs
		if strings.Contains(dir, ".github") || strings.Contains(dir, ".gitlab") ||
			strings.Contains(dir, ".circleci") {
			info.Class = ClassConfig
			return info
		}
	}

	// Check documentation
	if ext == ".md" || ext == ".rst" || ext == ".txt" || ext == ".adoc" {
		info.Class = ClassDoc
		info.Language = "markdown"
		return info
	}
	if strings.Contains(strings.ToLower(dir), "docs") || strings.Contains(strings.ToLower(dir), "doc") {
		if ext == ".md" || ext == ".rst" || ext == ".txt" {
			info.Class = ClassDoc
			return info
		}
	}

	// Check if it's a test file
	if isTestFile(path) {
		info.Class = ClassTest
		if lang, ok := languageExtensions[ext]; ok {
			info.Language = lang
		}
		return info
	}

	// Check source code
	if lang, ok := languageExtensions[ext]; ok {
		info.Class = ClassSource
		info.Language = lang
		return info
	}

	// Data files
	if ext == ".json" || ext == ".csv" || ext == ".xml" {
		info.Class = ClassData
		return info
	}

	info.Class = ClassIgnored
	return info
}

func isTestFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	dir := strings.ToLower(filepath.Dir(path))

	// Go test files
	if strings.HasSuffix(base, "_test.go") {
		return true
	}

	// Python test files
	if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
		return true
	}

	// JS/TS test files
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}

	// Test directories
	if strings.Contains(dir, "test") || strings.Contains(dir, "__tests__") ||
		strings.Contains(dir, "spec") {
		return true
	}

	return false
}

func ShouldAnalyze(fi FileInfo) bool {
	switch fi.Class {
	case ClassSource, ClassConfig, ClassDoc, ClassBuild:
		return true
	case ClassTest:
		return true
	default:
		return false
	}
}
