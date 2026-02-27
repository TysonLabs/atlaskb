package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type Phase4Config struct {
	RepoID   uuid.UUID
	RepoName string
	Model    string
	Pool     *pgxpool.Pool
	LLM      llm.Client
}

func RunPhase4(ctx context.Context, cfg Phase4Config) error {
	jobStore := &models.JobStore{Pool: cfg.Pool}
	entityStore := &models.EntityStore{Pool: cfg.Pool}
	factStore := &models.FactStore{Pool: cfg.Pool}
	relStore := &models.RelationshipStore{Pool: cfg.Pool}

	// Check if already done
	existing, _ := jobStore.GetByTarget(ctx, cfg.RepoID, models.PhasePhase4, "synthesis")
	if existing != nil && existing.Status == models.JobCompleted {
		fmt.Println("  Already completed, skipping.")
		return nil
	}

	// Reset failed job or create new one
	if existing != nil && existing.Status == models.JobFailed {
		jobStore.ResetFailed(ctx, cfg.RepoID, models.PhasePhase4)
	} else if existing == nil {
		job := &models.ExtractionJob{
			RepoID: cfg.RepoID,
			Phase:  models.PhasePhase4,
			Target: "synthesis",
			Status: models.JobPending,
		}
		jobStore.Create(ctx, job)
	}

	claimed, err := jobStore.ClaimNext(ctx, cfg.RepoID, models.PhasePhase4)
	if err != nil {
		return fmt.Errorf("claiming phase 4 job: %w", err)
	}
	if claimed == nil {
		fmt.Println("  No claimable job found, skipping.")
		return nil
	}

	// Gather entities and facts for context
	entities, err := entityStore.ListByRepo(ctx, cfg.RepoID)
	if err != nil {
		jobStore.Fail(ctx, claimed.ID, err.Error())
		return err
	}

	var sb strings.Builder
	for _, e := range entities {
		fmt.Fprintf(&sb, "## Entity: %s (kind: %s)\n", e.QualifiedName, e.Kind)
		summary := ""
		if e.Summary != nil {
			summary = *e.Summary
		}
		fmt.Fprintf(&sb, "Summary: %s\n", summary)
		if len(e.Capabilities) > 0 {
			fmt.Fprintf(&sb, "Capabilities: %s\n", strings.Join(e.Capabilities, ", "))
		}

		facts, _ := factStore.ListByEntity(ctx, e.ID)
		for _, f := range facts {
			fmt.Fprintf(&sb, "- [%s/%s] %s\n", f.Dimension, f.Category, f.Claim)
		}
		sb.WriteString("\n")
	}

	prompt := Phase4Prompt(cfg.RepoName, sb.String())

	var result *Phase4Result
	var lastResp *llm.Response
	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := cfg.LLM.Complete(ctx, cfg.Model, systemPromptPhase4, []llm.Message{
			{Role: "user", Content: prompt},
		}, 8192)
		if err != nil {
			if attempt == maxRetries {
				jobStore.Fail(ctx, claimed.ID, err.Error())
				return fmt.Errorf("LLM call (attempt %d/%d): %w", attempt, maxRetries, err)
			}
			fmt.Printf("  Phase 4 LLM attempt %d/%d failed: %v, retrying...\n", attempt, maxRetries, err)
			continue
		}

		result, err = ParsePhase4(resp.Content)
		if err != nil {
			if attempt == maxRetries {
				jobStore.Fail(ctx, claimed.ID, err.Error())
				return fmt.Errorf("parsing phase 4 result (attempt %d/%d): %w", attempt, maxRetries, err)
			}
			fmt.Printf("  Phase 4 parse attempt %d/%d failed: %v, retrying...\n", attempt, maxRetries, err)
			continue
		}
		lastResp = resp
		break
	}

	// Store architectural pattern facts
	for _, p := range result.ArchitecturalPatterns {
		// Find or create a repo-level entity
		entity, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, cfg.RepoName)
		if entity == nil {
			entity = &models.Entity{
				RepoID:        cfg.RepoID,
				Kind:          models.EntityConcept,
				Name:          cfg.RepoName,
				QualifiedName: cfg.RepoName,
				Summary:       models.Ptr("Repository-level entity"),
			}
			entityStore.Create(ctx, entity)
		}

		fact := &models.Fact{
			EntityID:   entity.ID,
			RepoID:     cfg.RepoID,
			Claim:      fmt.Sprintf("Architectural pattern: %s — %s", p.Pattern, p.Description),
			Dimension:  models.DimensionHow,
			Category:   models.CategoryPattern,
			Confidence: p.Confidence,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        "phase4-synthesis",
			}},
		}
		factStore.Create(ctx, fact)
	}

	// Store extracted facts
	for _, ef := range result.Facts {
		entity, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, ef.EntityName)
		if entity == nil {
			continue
		}
		fact := &models.Fact{
			EntityID:   entity.ID,
			RepoID:     cfg.RepoID,
			Claim:      ef.Claim,
			Dimension:  ef.Dimension,
			Category:   ef.Category,
			Confidence: ef.Confidence,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        "phase4-synthesis",
			}},
		}
		factStore.Create(ctx, fact)
	}

	// Store relationships
	for _, er := range result.Relationships {
		fromEntity, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, er.From)
		toEntity, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, er.To)
		if fromEntity == nil || toEntity == nil {
			continue
		}
		rel := &models.Relationship{
			RepoID:       cfg.RepoID,
			FromEntityID: fromEntity.ID,
			ToEntityID:   toEntity.ID,
			Kind:         er.Kind,
			Description:  models.Ptr(er.Description),
			Strength:     er.Strength,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        "phase4-synthesis",
			}},
		}
		relStore.Create(ctx, rel)
	}

	tokens := lastResp.InputTokens + lastResp.OutputTokens
	costUSD := float64(lastResp.InputTokens)/1_000_000*OpusInputPer1M + float64(lastResp.OutputTokens)/1_000_000*OpusOutputPer1M
	return jobStore.Complete(ctx, claimed.ID, tokens, costUSD)
}
