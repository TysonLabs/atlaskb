package pipeline

import (
	"context"
	"fmt"
	"strings"

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
		return nil
	}

	// Create job
	job := &models.ExtractionJob{
		RepoID: cfg.RepoID,
		Phase:  models.PhaseGitLog,
		Target: "git-history",
		Status: models.JobPending,
	}
	jobStore.Create(ctx, job)

	claimed, err := jobStore.ClaimNext(ctx, cfg.RepoID, models.PhaseGitLog)
	if err != nil || claimed == nil {
		return fmt.Errorf("claiming git log job: %w", err)
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

	// Format commits for LLM
	var sb strings.Builder
	for _, c := range commits {
		fmt.Fprintf(&sb, "Commit: %s\nAuthor: %s\nDate: %s\nMessage: %s\nFiles: %s\n\n",
			c.SHA[:8], c.Author, c.Date.Format("2006-01-02"), c.Message, strings.Join(c.FilesChanged, ", "))
	}

	prompt := GitLogPrompt(cfg.RepoName, sb.String())
	resp, err := cfg.LLM.Complete(ctx, cfg.Model, systemPromptGitLog, []llm.Message{
		{Role: "user", Content: prompt},
	}, 4096)
	if err != nil {
		jobStore.Fail(ctx, claimed.ID, err.Error())
		return fmt.Errorf("LLM call: %w", err)
	}

	result, err := ParseGitLog(resp.Content)
	if err != nil {
		jobStore.Fail(ctx, claimed.ID, err.Error())
		return fmt.Errorf("parsing git log result: %w", err)
	}

	// Store facts
	for _, ef := range result.Facts {
		entity, _ := entityStore.FindByQualifiedName(ctx, cfg.RepoID, ef.EntityName)
		var entityID uuid.UUID
		if entity != nil {
			entityID = entity.ID
		} else {
			// Create a repo-level entity for repo-level facts
			repoEntity := &models.Entity{
				RepoID:        cfg.RepoID,
				Kind:          models.EntityConcept,
				Name:          cfg.RepoName,
				QualifiedName: ef.EntityName,
				Summary:       "Repository-level entity",
			}
			entityStore.Create(ctx, repoEntity)
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
		factStore.Create(ctx, fact)
	}

	// Store decisions
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
		decisionStore.Create(ctx, decision)
	}

	tokens := resp.InputTokens + resp.OutputTokens
	costUSD := float64(resp.InputTokens)/1_000_000*SonnetInputPer1M + float64(resp.OutputTokens)/1_000_000*SonnetOutputPer1M
	return jobStore.Complete(ctx, claimed.ID, tokens, costUSD)
}
