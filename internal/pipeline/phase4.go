package pipeline

import (
	"context"
	"fmt"
	"log"
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

	// Filter to only non-dependency entities (skip external deps for synthesis)
	var coreEntities []models.Entity
	for _, e := range entities {
		if e.Kind == models.EntityModule && e.Path == nil {
			continue // Skip external dependency entities
		}
		coreEntities = append(coreEntities, e)
	}
	// Cap entities to keep context manageable (model has 40k context)
	if len(coreEntities) > 60 {
		coreEntities = coreEntities[:60]
	}

	// Build a plain list of all entity qualified_names for the model to reference
	var sb strings.Builder
	sb.WriteString("## AVAILABLE ENTITIES (use ONLY these exact strings for entity_name, from, and to fields):\n")
	for _, e := range coreEntities {
		fmt.Fprintf(&sb, "- %s\n", e.QualifiedName)
	}
	sb.WriteString("\n")

	for _, e := range coreEntities {
		fmt.Fprintf(&sb, "## Entity: %s (kind: %s)\n", e.QualifiedName, e.Kind)
		summary := ""
		if e.Summary != nil {
			summary = *e.Summary
		}
		fmt.Fprintf(&sb, "Summary: %s\n", summary)
		if len(e.Capabilities) > 0 {
			caps := e.Capabilities
			if len(caps) > 5 {
				caps = caps[:5]
			}
			fmt.Fprintf(&sb, "Capabilities: %s\n", strings.Join(caps, ", "))
		}

		facts, _ := factStore.ListByEntity(ctx, e.ID)
		// Limit facts per entity to keep context manageable
		if len(facts) > 5 {
			facts = facts[:5]
		}
		for _, f := range facts {
			fmt.Fprintf(&sb, "- [%s/%s] %s\n", f.Dimension, f.Category, f.Claim)
		}
		sb.WriteString("\n")
	}

	// Truncate if context is too large (model has 40k context, use ~40%)
	context := sb.String()
	if len(context) > 16000 {
		context = context[:16000] + "\n\n... (truncated)"
	}

	prompt := Phase4Prompt(cfg.RepoName, context)

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}
	lastResp, attempts, err := callLLMWithRetry(ctx, cfg.LLM, cfg.Model, systemPromptPhase4, messages, 8192, SchemaPhase4, DefaultRetryConfig)
	if err != nil {
		jobStore.Fail(ctx, claimed.ID, err.Error())
		return fmt.Errorf("LLM call: %w", err)
	}

	result, err := ParsePhase4(lastResp.Content)
	if err != nil {
		jobStore.Fail(ctx, claimed.ID, err.Error())
		return fmt.Errorf("parsing phase 4 result: %w", err)
	}
	_ = attempts

	// Find or create repo-level entity
	repoEntity, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, cfg.RepoName)
	if repoEntity == nil {
		repoEntity = &models.Entity{
			RepoID:        cfg.RepoID,
			Kind:          models.EntityConcept,
			Name:          cfg.RepoName,
			QualifiedName: cfg.RepoName,
			Summary:       models.Ptr("Repository-level entity"),
		}
		entityStore.Upsert(ctx, repoEntity)
	}

	// Store architectural pattern facts
	patternsStored := 0
	for _, p := range result.ArchitecturalPatterns {
		fact := &models.Fact{
			EntityID:   repoEntity.ID,
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
		if err := factStore.Create(ctx, fact); err != nil {
			logVerboseF("[phase4] warn: creating pattern fact: %v", err)
		} else {
			patternsStored++
		}
	}

	// Store data_flows as relationships
	dataFlowsStored := 0
	for _, df := range result.DataFlows {
		fromID, fromOK := resolveEntity(ctx, entityStore, cfg.RepoID, df.FromModule)
		toID, toOK := resolveEntity(ctx, entityStore, cfg.RepoID, df.ToModule)
		if !fromOK || !toOK {
			logVerboseF("[phase4] data_flow skipped (entity not found): %s -> %s", df.FromModule, df.ToModule)
			continue
		}
		rel := &models.Relationship{
			RepoID:       cfg.RepoID,
			FromEntityID: fromID,
			ToEntityID:   toID,
			Kind:         models.RelProduces,
			Description:  models.Ptr(fmt.Sprintf("%s (mechanism: %s)", df.Description, df.Mechanism)),
			Strength:     models.StrengthModerate,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        "phase4-synthesis",
			}},
		}
		if err := relStore.Upsert(ctx, rel); err != nil {
			logVerboseF("[phase4] warn: upserting data_flow relationship: %v", err)
		} else {
			dataFlowsStored++
		}
	}

	// Store contracts as facts on repo entity
	contractsStored := 0
	for _, c := range result.Contracts {
		claim := fmt.Sprintf("Contract between %s: %s", strings.Join(c.Between, " and "), c.Description)
		if c.Explicit {
			claim += " (explicit)"
		} else {
			claim += " (implicit)"
		}
		fact := &models.Fact{
			EntityID:   repoEntity.ID,
			RepoID:     cfg.RepoID,
			Claim:      claim,
			Dimension:  models.DimensionWhat,
			Category:   models.CategoryConstraint,
			Confidence: models.ConfidenceMedium,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        "phase4-synthesis",
			}},
		}
		if err := factStore.Create(ctx, fact); err != nil {
			logVerboseF("[phase4] warn: creating contract fact: %v", err)
		} else {
			contractsStored++
		}
	}

	// Store extracted facts
	factsStored := 0
	for _, ef := range result.Facts {
		entityID, ok := resolveEntity(ctx, entityStore, cfg.RepoID, ef.EntityName)
		if !ok {
			logVerboseF("[phase4] fact skipped (entity not found): %s", ef.EntityName)
			continue
		}
		fact := &models.Fact{
			EntityID:   entityID,
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
		if err := factStore.Create(ctx, fact); err != nil {
			logVerboseF("[phase4] warn: creating fact: %v", err)
		} else {
			factsStored++
		}
	}

	// Store relationships
	relsStored := 0
	for _, er := range result.Relationships {
		fromID, fromOK := resolveEntity(ctx, entityStore, cfg.RepoID, er.From)
		toID, toOK := resolveEntity(ctx, entityStore, cfg.RepoID, er.To)
		if !fromOK || !toOK {
			logVerboseF("[phase4] relationship skipped (entity not found): %s -> %s", er.From, er.To)
			continue
		}
		rel := &models.Relationship{
			RepoID:       cfg.RepoID,
			FromEntityID: fromID,
			ToEntityID:   toID,
			Kind:         er.Kind,
			Description:  models.Ptr(er.Description),
			Strength:     er.Strength,
			Provenance: []models.Provenance{{
				SourceType: "file",
				Repo:       cfg.RepoName,
				Ref:        "phase4-synthesis",
			}},
		}
		if err := relStore.Upsert(ctx, rel); err != nil {
			logVerboseF("[phase4] warn: upserting relationship: %v", err)
		} else {
			relsStored++
		}
	}

	log.Printf("[phase4] stored %d patterns, %d data_flows as relationships, %d contracts as facts, %d facts, %d relationships",
		patternsStored, dataFlowsStored, contractsStored, factsStored, relsStored)

	tokens := lastResp.InputTokens + lastResp.OutputTokens
	costUSD := float64(lastResp.InputTokens)/1_000_000*OpusInputPer1M + float64(lastResp.OutputTokens)/1_000_000*OpusOutputPer1M
	return jobStore.CompleteWithDetails(ctx, claimed.ID, tokens, costUSD, lastResp.Model, attempts)
}
