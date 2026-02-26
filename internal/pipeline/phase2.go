package pipeline

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
	"golang.org/x/sync/errgroup"
)

type Phase2Config struct {
	RepoID      uuid.UUID
	RepoName    string
	RepoPath    string
	Manifest    *Manifest
	Model       string
	Concurrency int
	Pool        *pgxpool.Pool
	LLM         llm.Client
}

type Phase2Stats struct {
	FilesProcessed int
	FilesSkipped   int
	EntitiesCreated int
	FactsCreated   int
	TotalTokens    int
}

func RunPhase2(ctx context.Context, cfg Phase2Config) (*Phase2Stats, error) {
	stats := &Phase2Stats{}
	jobStore := &models.JobStore{Pool: cfg.Pool}
	entityStore := &models.EntityStore{Pool: cfg.Pool}
	factStore := &models.FactStore{Pool: cfg.Pool}
	relStore := &models.RelationshipStore{Pool: cfg.Pool}

	// Create jobs for all analyzable files
	for _, fi := range cfg.Manifest.Files {
		if !ShouldAnalyze(fi) {
			continue
		}

		content, err := os.ReadFile(filepath.Join(cfg.RepoPath, fi.Path))
		if err != nil {
			continue
		}

		hash := fmt.Sprintf("%x", sha256.Sum256(content))

		// Check if file has changed
		existing, _ := jobStore.GetByTarget(ctx, cfg.RepoID, models.PhasePhase2, fi.Path)
		if existing != nil && existing.Status == models.JobCompleted && existing.ContentHash == hash {
			stats.FilesSkipped++
			continue
		}

		job := &models.ExtractionJob{
			RepoID:      cfg.RepoID,
			Phase:       models.PhasePhase2,
			Target:      fi.Path,
			ContentHash: hash,
			Status:      models.JobPending,
		}
		if err := jobStore.Create(ctx, job); err != nil {
			logVerboseF("warn: creating job for %s: %v", fi.Path, err)
		}
	}

	// Process jobs with bounded concurrency
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(cfg.Concurrency)

	for {
		job, err := jobStore.ClaimNext(gctx, cfg.RepoID, models.PhasePhase2)
		if err != nil {
			return stats, fmt.Errorf("claiming job: %w", err)
		}
		if job == nil {
			break // no more pending jobs
		}

		g.Go(func() error {
			err := processFile(gctx, cfg, job, entityStore, factStore, relStore, stats)
			if err != nil {
				jobStore.Fail(gctx, job.ID, err.Error())
				logVerboseF("error processing %s: %v", job.Target, err)
				return nil // don't cancel other workers
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return stats, err
	}

	return stats, nil
}

func processFile(ctx context.Context, cfg Phase2Config, job *models.ExtractionJob,
	entityStore *models.EntityStore, factStore *models.FactStore, relStore *models.RelationshipStore,
	stats *Phase2Stats) error {

	jobStore := &models.JobStore{Pool: cfg.Pool}

	content, err := os.ReadFile(filepath.Join(cfg.RepoPath, job.Target))
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	fi := ClassifyFile(job.Target, int64(len(content)))

	prompt := Phase2Prompt(job.Target, fi.Language, cfg.RepoName, cfg.Manifest.Stack, string(content))

	resp, err := cfg.LLM.Complete(ctx, cfg.Model, systemPromptPhase2, []llm.Message{
		{Role: "user", Content: prompt},
	}, 4096)
	if err != nil {
		return fmt.Errorf("LLM call: %w", err)
	}

	result, err := ParsePhase2(resp.Content)
	if err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	// Store entities
	entityMap := make(map[string]uuid.UUID) // qualified_name -> entity ID
	for _, ext := range result.Entities {
		entity := &models.Entity{
			RepoID:        cfg.RepoID,
			Kind:          ext.Kind,
			Name:          ext.Name,
			QualifiedName: ext.QualifiedName,
			Path:          job.Target,
			Summary:       ext.Summary,
			Capabilities:  ext.Capabilities,
			Assumptions:   ext.Assumptions,
		}
		if err := entityStore.Create(ctx, entity); err != nil {
			logVerboseF("warn: creating entity %s: %v", ext.QualifiedName, err)
			continue
		}
		entityMap[ext.QualifiedName] = entity.ID
		stats.EntitiesCreated++
	}

	// Store facts
	for _, ext := range result.Facts {
		entityID, ok := entityMap[ext.EntityName]
		if !ok {
			// Try to find existing entity
			existing, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, ext.EntityName)
			if existing != nil {
				entityID = existing.ID
			} else {
				continue // skip fact for unknown entity
			}
		}

		fact := &models.Fact{
			EntityID:   entityID,
			RepoID:     cfg.RepoID,
			Claim:      ext.Claim,
			Dimension:  ext.Dimension,
			Category:   ext.Category,
			Confidence: ext.Confidence,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        job.Target,
				AnalyzedAt: job.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}},
		}
		if err := factStore.Create(ctx, fact); err != nil {
			logVerboseF("warn: creating fact: %v", err)
			continue
		}
		stats.FactsCreated++
	}

	// Store relationships
	for _, ext := range result.Relationships {
		fromID, ok := entityMap[ext.From]
		if !ok {
			existing, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, ext.From)
			if existing != nil {
				fromID = existing.ID
			} else {
				continue
			}
		}
		toID, ok := entityMap[ext.To]
		if !ok {
			existing, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, ext.To)
			if existing != nil {
				toID = existing.ID
			} else {
				continue
			}
		}

		rel := &models.Relationship{
			RepoID:       cfg.RepoID,
			FromEntityID: fromID,
			ToEntityID:   toID,
			Kind:         ext.Kind,
			Description:  ext.Description,
			Strength:     ext.Strength,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        job.Target,
				AnalyzedAt: job.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}},
		}
		if err := relStore.Create(ctx, rel); err != nil {
			logVerboseF("warn: creating relationship: %v", err)
		}
	}

	// Mark job complete
	stats.TotalTokens += resp.InputTokens + resp.OutputTokens
	stats.FilesProcessed++
	costUSD := float64(resp.InputTokens)/1_000_000*SonnetInputPer1M + float64(resp.OutputTokens)/1_000_000*SonnetOutputPer1M
	return jobStore.Complete(ctx, job.ID, resp.InputTokens+resp.OutputTokens, costUSD)
}

func logVerboseF(format string, args ...any) {
	// This will be wired to the CLI verbose flag via the orchestrator
	fmt.Printf(format+"\n", args...)
}
