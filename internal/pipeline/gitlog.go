package pipeline

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/git"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type GitLogConfig struct {
	RepoID   uuid.UUID
	RepoName string
	RepoPath string
	Model    string
	Pool     *pgxpool.Pool
	LLM      llm.Client
}

func RunGitLogAnalysis(ctx context.Context, cfg GitLogConfig) error {
	jobStore := &models.JobStore{Pool: cfg.Pool}
	factStore := &models.FactStore{Pool: cfg.Pool}
	entityStore := &models.EntityStore{Pool: cfg.Pool}
	decisionStore := &models.DecisionStore{Pool: cfg.Pool}

	// Check if already done
	existing, _ := jobStore.GetByTarget(ctx, cfg.RepoID, models.PhaseGitLog, "git-history")
	if existing != nil && existing.Status == models.JobCompleted {
		fmt.Println("  Already completed, skipping.")
		return nil
	}

	// Reset failed job or create new one
	if existing != nil && existing.Status == models.JobFailed {
		jobStore.ResetFailed(ctx, cfg.RepoID, models.PhaseGitLog)
	} else if existing == nil {
		job := &models.ExtractionJob{
			RepoID: cfg.RepoID,
			Phase:  models.PhaseGitLog,
			Target: "git-history",
			Status: models.JobPending,
		}
		jobStore.Create(ctx, job)
	}

	claimed, err := jobStore.ClaimNext(ctx, cfg.RepoID, models.PhaseGitLog)
	if err != nil {
		return fmt.Errorf("claiming git log job: %w", err)
	}
	if claimed == nil {
		fmt.Println("  No claimable job found, skipping.")
		return nil
	}

	// Parse git log
	commits, err := git.ParseLog(cfg.RepoPath, 100)
	if err != nil {
		jobStore.Fail(ctx, claimed.ID, err.Error())
		return fmt.Errorf("parsing git log: %w", err)
	}

	if len(commits) == 0 {
		return jobStore.Complete(ctx, claimed.ID, 0, 0)
	}

	// Format commits for LLM — truncate message body to keep context manageable
	var sb strings.Builder
	for _, c := range commits {
		msg := c.Message
		// Truncate long commit bodies to first 500 chars
		if len(msg) > 500 {
			msg = msg[:500] + "..."
		}
		files := c.FilesChanged
		if len(files) > 10 {
			files = files[:10]
		}
		fmt.Fprintf(&sb, "Commit: %s\nAuthor: %s\nDate: %s\nMessage: %s\nFiles: %s\n\n",
			c.SHA[:8], c.Author, c.Date.Format("2006-01-02"), msg, strings.Join(files, ", "))
	}

	prompt := GitLogPrompt(cfg.RepoName, sb.String())

	var result *GitLogResult
	var lastResp *llm.Response
	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := cfg.LLM.Complete(ctx, cfg.Model, systemPromptGitLog, []llm.Message{
			{Role: "user", Content: prompt},
		}, 4096, SchemaGitLog)
		if err != nil {
			if attempt == maxRetries {
				jobStore.Fail(ctx, claimed.ID, err.Error())
				return fmt.Errorf("LLM call (attempt %d/%d): %w", attempt, maxRetries, err)
			}
			fmt.Printf("  Git log LLM attempt %d/%d failed: %v, retrying...\n", attempt, maxRetries, err)
			continue
		}

		result, err = ParseGitLog(resp.Content)
		if err != nil {
			if attempt == maxRetries {
				jobStore.Fail(ctx, claimed.ID, err.Error())
				return fmt.Errorf("parsing git log result (attempt %d/%d): %w", attempt, maxRetries, err)
			}
			fmt.Printf("  Git log parse attempt %d/%d failed: %v, retrying...\n", attempt, maxRetries, err)
			continue
		}
		lastResp = resp
		break
	}

	// Store facts
	factsCreated := 0
	for _, ef := range result.Facts {
		entity, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, ef.EntityName)
		var entityID uuid.UUID
		if entity != nil {
			entityID = entity.ID
		} else {
			// Create a repo-level entity for repo-level facts — use entity name from fact, not repo name
			repoEntity := &models.Entity{
				RepoID:        cfg.RepoID,
				Kind:          models.EntityConcept,
				Name:          ef.EntityName,
				QualifiedName: ef.EntityName,
				Summary:       models.Ptr("Repository-level entity"),
			}
			if err := entityStore.Upsert(ctx, repoEntity); err != nil {
				logVerboseF("[gitlog] warn: upserting entity %s: %v", ef.EntityName, err)
				continue
			}
			entityID = repoEntity.ID
		}

		fact := &models.Fact{
			EntityID:   entityID,
			RepoID:     cfg.RepoID,
			Claim:      ef.Claim,
			Dimension:  ef.Dimension,
			Category:   ef.Category,
			Confidence: ef.Confidence,
			Provenance: []models.Provenance{{
				SourceType: "commit",
				Repo:       cfg.RepoName,
				Ref:        "git-history",
			}},
		}
		if err := factStore.Create(ctx, fact); err != nil {
			logVerboseF("[gitlog] warn: creating fact: %v", err)
			continue
		}
		factsCreated++
	}

	// Store decisions
	decisionsCreated := 0
	for _, d := range result.Decisions {
		decision := &models.Decision{
			RepoID:      cfg.RepoID,
			Summary:     d.Summary,
			Description: d.Description,
			Rationale:   d.Rationale,
			StillValid:  true,
			Provenance: []models.Provenance{{
				SourceType: "commit",
				Repo:       cfg.RepoName,
				Ref:        "git-history",
			}},
		}

		// Parse MadeAt timestamp
		if d.MadeAt != "" {
			if t, err := time.Parse(time.RFC3339, d.MadeAt); err == nil {
				decision.MadeAt = &t
			} else if t, err := time.Parse("2006-01-02", d.MadeAt); err == nil {
				decision.MadeAt = &t
			}
		}

		if err := decisionStore.Create(ctx, decision); err != nil {
			logVerboseF("[gitlog] warn: creating decision: %v", err)
			continue
		}
		decisionsCreated++

		// Link decision to related entities
		var linkedEntityIDs []uuid.UUID
		// Try to find entities mentioned in summary/description
		entities, _ := entityStore.ListByRepo(ctx, cfg.RepoID)
		descLower := strings.ToLower(d.Summary + " " + d.Description)
		for _, e := range entities {
			if strings.Contains(descLower, strings.ToLower(e.Name)) {
				linkedEntityIDs = append(linkedEntityIDs, e.ID)
			}
		}
		if len(linkedEntityIDs) > 0 {
			if err := decisionStore.LinkEntities(ctx, decision.ID, linkedEntityIDs); err != nil {
				logVerboseF("[gitlog] warn: linking decision entities: %v", err)
			}
		}
	}

	log.Printf("[gitlog] parsed %d commits (full body), extracted %d facts, %d decisions", len(commits), factsCreated, decisionsCreated)

	tokens := lastResp.InputTokens + lastResp.OutputTokens
	costUSD := float64(lastResp.InputTokens)/1_000_000*SonnetInputPer1M + float64(lastResp.OutputTokens)/1_000_000*SonnetOutputPer1M
	return jobStore.Complete(ctx, claimed.ID, tokens, costUSD)
}
