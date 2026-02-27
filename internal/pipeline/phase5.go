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

type Phase5Config struct {
	RepoID   uuid.UUID
	RepoName string
	Model    string
	Pool     *pgxpool.Pool
	LLM      llm.Client
}

func RunPhase5(ctx context.Context, cfg Phase5Config) error {
	jobStore := &models.JobStore{Pool: cfg.Pool}
	entityStore := &models.EntityStore{Pool: cfg.Pool}
	factStore := &models.FactStore{Pool: cfg.Pool}
	decisionStore := &models.DecisionStore{Pool: cfg.Pool}

	// Check if already done
	existing, _ := jobStore.GetByTarget(ctx, cfg.RepoID, models.PhasePhase5, "summary")
	if existing != nil && existing.Status == models.JobCompleted {
		fmt.Println("  Already completed, skipping.")
		return nil
	}

	// Reset failed job or create new one
	if existing != nil && existing.Status == models.JobFailed {
		jobStore.ResetFailed(ctx, cfg.RepoID, models.PhasePhase5)
	} else if existing == nil {
		job := &models.ExtractionJob{
			RepoID: cfg.RepoID,
			Phase:  models.PhasePhase5,
			Target: "summary",
			Status: models.JobPending,
		}
		jobStore.Create(ctx, job)
	}

	claimed, err := jobStore.ClaimNext(ctx, cfg.RepoID, models.PhasePhase5)
	if err != nil {
		return fmt.Errorf("claiming phase 5 job: %w", err)
	}
	if claimed == nil {
		fmt.Println("  No claimable job found, skipping.")
		return nil
	}

	// Gather entity summaries
	entities, err := entityStore.ListByRepo(ctx, cfg.RepoID)
	if err != nil {
		jobStore.Fail(ctx, claimed.ID, err.Error())
		return err
	}

	var entitySB strings.Builder
	for _, e := range entities {
		summary := ""
		if e.Summary != nil {
			summary = *e.Summary
		}
		fmt.Fprintf(&entitySB, "- %s (%s): %s\n", e.QualifiedName, e.Kind, summary)
	}

	// Gather architectural facts
	var archSB strings.Builder
	for _, e := range entities {
		facts, _ := factStore.ListByEntity(ctx, e.ID)
		for _, f := range facts {
			if f.Category == models.CategoryPattern || f.Dimension == models.DimensionHow {
				fmt.Fprintf(&archSB, "- %s\n", f.Claim)
			}
		}
	}

	// Gather decisions
	decisions, _ := decisionStore.ListByRepo(ctx, cfg.RepoID)
	var decSB strings.Builder
	for _, d := range decisions {
		fmt.Fprintf(&decSB, "- %s: %s\n", d.Summary, d.Rationale)
	}

	prompt := Phase5Prompt(cfg.RepoName, entitySB.String(), archSB.String(), decSB.String())

	var result *Phase5Result
	var lastResp *llm.Response
	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := cfg.LLM.Complete(ctx, cfg.Model, systemPromptPhase5, []llm.Message{
			{Role: "user", Content: prompt},
		}, 8192)
		if err != nil {
			if attempt == maxRetries {
				jobStore.Fail(ctx, claimed.ID, err.Error())
				return fmt.Errorf("LLM call (attempt %d/%d): %w", attempt, maxRetries, err)
			}
			fmt.Printf("  Phase 5 LLM attempt %d/%d failed: %v, retrying...\n", attempt, maxRetries, err)
			continue
		}

		result, err = ParsePhase5(resp.Content)
		if err != nil {
			if attempt == maxRetries {
				jobStore.Fail(ctx, claimed.ID, err.Error())
				return fmt.Errorf("parsing phase 5 result (attempt %d/%d): %w", attempt, maxRetries, err)
			}
			fmt.Printf("  Phase 5 parse attempt %d/%d failed: %v, retrying...\n", attempt, maxRetries, err)
			continue
		}
		lastResp = resp
		break
	}

	// Find or create repo-level entity and update with summary
	repoEntity, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, cfg.RepoName)
	if repoEntity == nil {
		repoEntity = &models.Entity{
			RepoID:        cfg.RepoID,
			Kind:          models.EntityService,
			Name:          cfg.RepoName,
			QualifiedName: cfg.RepoName,
			Summary:       models.Ptr(result.Summary),
			Capabilities:  result.Capabilities,
		}
		entityStore.Create(ctx, repoEntity)
	}

	// Store convention facts
	for _, conv := range result.Conventions {
		fact := &models.Fact{
			EntityID:   repoEntity.ID,
			RepoID:     cfg.RepoID,
			Claim:      fmt.Sprintf("[%s] %s", conv.Category, conv.Description),
			Dimension:  models.DimensionHow,
			Category:   models.CategoryConvention,
			Confidence: models.ConfidenceMedium,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        "phase5-summary",
			}},
		}
		factStore.Create(ctx, fact)
	}

	// Store risk/debt facts
	for _, risk := range result.RisksAndDebt {
		fact := &models.Fact{
			EntityID:   repoEntity.ID,
			RepoID:     cfg.RepoID,
			Claim:      risk,
			Dimension:  models.DimensionWhat,
			Category:   models.CategoryRisk,
			Confidence: models.ConfidenceMedium,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        "phase5-summary",
			}},
		}
		factStore.Create(ctx, fact)
	}

	tokens := lastResp.InputTokens + lastResp.OutputTokens
	costUSD := float64(lastResp.InputTokens)/1_000_000*OpusInputPer1M + float64(lastResp.OutputTokens)/1_000_000*OpusOutputPer1M
	return jobStore.Complete(ctx, claimed.ID, tokens, costUSD)
}
