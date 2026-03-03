package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// Phase17Config configures the Tree-sitter structural extraction phase.
type Phase17Config struct {
	RepoID   uuid.UUID
	RepoName string
	RepoPath string
	Manifest *Manifest
	Roster   []EntityEntry
	Pool     *pgxpool.Pool
}

// Phase17Stats reports what Phase 1.7 extracted.
type Phase17Stats struct {
	FilesProcessed   int
	CallsExtracted   int
	CallsResolved    int
	InheritanceFound int
}

// RunPhase17 performs deterministic call graph and inheritance extraction
// using Tree-sitter AST parsing. This complements the ctags symbol extraction
// (Phase 1.5) by discovering function call relationships and struct embeddings.
//
// All failures are non-fatal: if Tree-sitter cannot be initialized or a file
// fails to parse, execution continues with the remaining files.
func RunPhase17(ctx context.Context, cfg Phase17Config) (*Phase17Stats, error) {
	stats := &Phase17Stats{}

	// Create Tree-sitter engine (graceful degradation if it fails)
	engine, err := NewTreeSitterEngine()
	if err != nil {
		return stats, fmt.Errorf("initializing tree-sitter: %w", err)
	}
	defer engine.Close()

	// Build suffix index from roster for fast symbol resolution
	idx := BuildSuffixIndex(cfg.Roster)

	// Build set of paths that have roster entries (skip files with no ctags symbols)
	pathsWithSymbols := make(map[string]bool)
	for _, e := range cfg.Roster {
		pathsWithSymbols[e.Path] = true
	}

	entityStore := &models.EntityStore{Pool: cfg.Pool}
	relStore := &models.RelationshipStore{Pool: cfg.Pool}

	// Process each Go file in the manifest that has roster entries
	for _, fi := range cfg.Manifest.Files {
		// Check context for cancellation
		if err := ctx.Err(); err != nil {
			return stats, nil
		}

		// Only process Go files
		if !strings.HasSuffix(fi.Path, ".go") {
			continue
		}

		// Skip files with no ctags symbols
		if !pathsWithSymbols[fi.Path] {
			continue
		}

		// Skip test files — they produce noisy call relationships
		if isTestFile(fi.Path) {
			continue
		}

		// Read file content
		absPath := filepath.Join(cfg.RepoPath, fi.Path)
		content, err := os.ReadFile(absPath)
		if err != nil {
			logVerboseF("[treesitter] warn: reading %s: %v", fi.Path, err)
			continue
		}

		// Parse with Tree-sitter
		root, err := engine.ParseGo(ctx, content)
		if err != nil {
			logVerboseF("[treesitter] warn: parsing %s: %v", fi.Path, err)
			continue
		}

		stats.FilesProcessed++

		// Extract function calls
		calls := ExtractGoCalls(root, content, fi.Path, cfg.RepoPath, idx)
		stats.CallsExtracted += len(calls)

		// Store resolved call relationships
		for _, call := range calls {
			strength := confidenceToStrength(call.Confidence)

			fromEntity, err := entityStore.FindByQualifiedName(ctx, cfg.RepoID, call.CallerQN)
			if err != nil || fromEntity == nil {
				continue
			}

			toEntity, err := entityStore.FindByQualifiedName(ctx, cfg.RepoID, call.CalleeQN)
			if err != nil || toEntity == nil {
				continue
			}

			// Don't create self-referencing relationships
			if fromEntity.ID == toEntity.ID {
				continue
			}

			desc := fmt.Sprintf("%s calls %s", call.CallerQN, call.CalleeQN)
			rel := &models.Relationship{
				RepoID:       cfg.RepoID,
				FromEntityID: fromEntity.ID,
				ToEntityID:   toEntity.ID,
				Kind:         models.RelCalls,
				Description:  &desc,
				Strength:     strength,
				Provenance: []models.Provenance{{
					SourceType: "treesitter",
					Repo:       cfg.RepoName,
					Ref:        fi.Path,
				}},
			}
			if err := relStore.Upsert(ctx, rel); err != nil {
				logVerboseF("[treesitter] warn: upserting call rel %s -> %s: %v", call.CallerQN, call.CalleeQN, err)
				continue
			}
			stats.CallsResolved++
		}

		// Extract struct embeddings
		embeddings := ExtractGoEmbeddings(root, content, fi.Path, cfg.RepoPath, idx)
		for _, emb := range embeddings {
			fromEntity, err := entityStore.FindByQualifiedName(ctx, cfg.RepoID, emb.ChildQN)
			if err != nil || fromEntity == nil {
				continue
			}

			toEntity, err := entityStore.FindByQualifiedName(ctx, cfg.RepoID, emb.ParentQN)
			if err != nil || toEntity == nil {
				continue
			}

			if fromEntity.ID == toEntity.ID {
				continue
			}

			desc := fmt.Sprintf("%s embeds %s", emb.ChildQN, emb.ParentQN)
			rel := &models.Relationship{
				RepoID:       cfg.RepoID,
				FromEntityID: fromEntity.ID,
				ToEntityID:   toEntity.ID,
				Kind:         models.RelExtends,
				Description:  &desc,
				Strength:     models.StrengthStrong,
				Provenance: []models.Provenance{{
					SourceType: "treesitter",
					Repo:       cfg.RepoName,
					Ref:        fi.Path,
				}},
			}
			if err := relStore.Upsert(ctx, rel); err != nil {
				logVerboseF("[treesitter] warn: upserting embedding rel %s -> %s: %v", emb.ChildQN, emb.ParentQN, err)
				continue
			}
			stats.InheritanceFound++
		}
	}

	return stats, nil
}

// confidenceToStrength maps extraction confidence to relationship strength.
func confidenceToStrength(confidence string) string {
	switch confidence {
	case "high":
		return models.StrengthStrong
	case "moderate":
		return models.StrengthModerate
	case "low":
		return models.StrengthWeak
	default:
		return models.StrengthModerate
	}
}
