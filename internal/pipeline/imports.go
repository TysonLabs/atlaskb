package pipeline

import (
	"context"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// ImportEntry represents a single import statement found in a source file.
type ImportEntry struct {
	FilePath   string
	ImportPath string
	Alias      string
}

// ExtractGoImports parses Go import blocks from the given files using go/parser.
// Only the import block is parsed (extremely fast).
func ExtractGoImports(repoPath string, files []FileInfo) []ImportEntry {
	var result []ImportEntry
	fset := token.NewFileSet()

	for _, fi := range files {
		absPath := filepath.Join(repoPath, fi.Path)
		f, err := parser.ParseFile(fset, absPath, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			alias := ""
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			result = append(result, ImportEntry{
				FilePath:   fi.Path,
				ImportPath: importPath,
				Alias:      alias,
			})
		}
	}

	return result
}

// filterGoFiles returns only .go files from the file list.
func filterGoFiles(files []FileInfo) []FileInfo {
	var goFiles []FileInfo
	for _, fi := range files {
		if strings.HasSuffix(fi.Path, ".go") {
			goFiles = append(goFiles, fi)
		}
	}
	return goFiles
}

// StoreImportRelationships creates "imports" relationships between file entities
// and imported package entities. Returns the number of relationships created.
func StoreImportRelationships(ctx context.Context, pool *pgxpool.Pool, repoID uuid.UUID, repoName string, imports []ImportEntry, roster []EntityEntry) int {
	if len(imports) == 0 {
		return 0
	}

	entityStore := &models.EntityStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}

	// Build a map from file path to first roster entry (module-level entity)
	fileToEntity := make(map[string]string) // filePath -> qualifiedName
	seen := make(map[string]bool)
	for _, re := range roster {
		if !seen[re.Path] {
			seen[re.Path] = true
			fileToEntity[re.Path] = re.QualifiedName
		}
	}

	created := 0
	for _, imp := range imports {
		// Find the source entity (first entity in the file)
		sourceQN, ok := fileToEntity[imp.FilePath]
		if !ok {
			continue
		}
		sourceEntity, err := entityStore.FindByQualifiedName(ctx, repoID, sourceQN)
		if err != nil || sourceEntity == nil {
			// Try finding by path
			sourceEntity, err = entityStore.FindByPath(ctx, repoID, imp.FilePath)
			if err != nil || sourceEntity == nil {
				continue
			}
		}

		// Find or create target entity for the imported package
		targetQN := imp.ImportPath
		targetEntity, _ := entityStore.FindByQualifiedName(ctx, repoID, targetQN)
		if targetEntity == nil {
			// Extract short name from import path (last segment)
			shortName := imp.ImportPath
			if idx := strings.LastIndex(imp.ImportPath, "/"); idx >= 0 {
				shortName = imp.ImportPath[idx+1:]
			}
			targetEntity = &models.Entity{
				RepoID:        repoID,
				Kind:          models.EntityModule,
				Name:          shortName,
				QualifiedName: targetQN,
			}
			if err := entityStore.Upsert(ctx, targetEntity); err != nil {
				continue
			}
		}

		// Don't create self-referencing relationships
		if sourceEntity.ID == targetEntity.ID {
			continue
		}

		rel := &models.Relationship{
			RepoID:       repoID,
			FromEntityID: sourceEntity.ID,
			ToEntityID:   targetEntity.ID,
			Kind:         models.RelImports,
			Strength:     models.StrengthStrong,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       repoName,
				Ref:        imp.FilePath,
			}},
		}
		if err := relStore.Upsert(ctx, rel); err != nil {
			continue
		}
		created++
	}

	return created
}
