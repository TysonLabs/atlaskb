package pipeline

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	ghpkg "github.com/tgeorge06/atlaskb/internal/github"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type Phase3Config struct {
	RepoID       uuid.UUID
	RepoName     string
	RemoteURL    string
	Model        string
	Pool         *pgxpool.Pool
	LLM          llm.Client
	GitHub       *ghpkg.Client
	MaxPRs       int
	PRBatchSize  int
	ProgressFunc func(msg string)
}

func (cfg *Phase3Config) progress(msg string) {
	if cfg.ProgressFunc != nil {
		cfg.ProgressFunc(msg)
	}
}

var trivialPRTitle = regexp.MustCompile(`(?i)^(chore|ci|docs|bump|update dependencies|dependabot)`)

func RunPhase3(ctx context.Context, cfg Phase3Config) error {
	if cfg.GitHub == nil {
		fmt.Println("  Skipped (no GitHub token configured)")
		return nil
	}

	// Parse remote URL to get owner/repo
	owner, repo, ok := ghpkg.ParseRemoteURL(cfg.RemoteURL)
	if !ok {
		fmt.Println("  Skipped (not a GitHub remote)")
		return nil
	}

	jobStore := &models.JobStore{Pool: cfg.Pool}
	factStore := &models.FactStore{Pool: cfg.Pool}
	entityStore := &models.EntityStore{Pool: cfg.Pool}
	decisionStore := &models.DecisionStore{Pool: cfg.Pool}

	// Check if already done
	existing, _ := jobStore.GetByTarget(ctx, cfg.RepoID, models.PhasePhase3, "github-prs")
	if existing != nil && existing.Status == models.JobCompleted {
		fmt.Println("  Already completed, skipping.")
		return nil
	}

	// Reset failed job or create new one
	if existing != nil && existing.Status == models.JobFailed {
		jobStore.ResetFailed(ctx, cfg.RepoID, models.PhasePhase3)
	} else if existing == nil {
		job := &models.ExtractionJob{
			RepoID: cfg.RepoID,
			Phase:  models.PhasePhase3,
			Target: "github-prs",
			Status: models.JobPending,
		}
		jobStore.Create(ctx, job)
	}

	claimed, err := jobStore.ClaimNext(ctx, cfg.RepoID, models.PhasePhase3)
	if err != nil {
		return fmt.Errorf("claiming phase3 job: %w", err)
	}
	if claimed == nil {
		fmt.Println("  No claimable job found, skipping.")
		return nil
	}

	maxPRs := cfg.MaxPRs
	if maxPRs == 0 {
		maxPRs = 200
	}
	batchSize := cfg.PRBatchSize
	if batchSize == 0 {
		batchSize = 10
	}

	// Fetch merged PRs
	fmt.Printf("  Fetching merged PRs from %s/%s...\n", owner, repo)
	cfg.progress(fmt.Sprintf("Phase 3: Fetching PRs from %s/%s...", owner, repo))

	prs, err := cfg.GitHub.FetchMergedPRs(ctx, owner, repo, maxPRs)
	if err != nil {
		jobStore.Fail(ctx, claimed.ID, err.Error())
		return fmt.Errorf("fetching PRs: %w", err)
	}

	fmt.Printf("  Fetched %d merged PRs\n", len(prs))

	// Filter out trivial PRs
	var signalPRs []ghpkg.PR
	for _, pr := range prs {
		if len(pr.Body) < 50 {
			continue
		}
		if trivialPRTitle.MatchString(pr.Title) {
			continue
		}
		if len(pr.ReviewComments) == 0 && len(pr.LinkedIssues) == 0 && len(pr.Body) < 150 {
			continue
		}
		signalPRs = append(signalPRs, pr)
	}

	fmt.Printf("  %d PRs with signal (filtered %d trivial)\n", len(signalPRs), len(prs)-len(signalPRs))

	if len(signalPRs) == 0 {
		fmt.Println("  No PRs with sufficient signal to analyze.")
		return jobStore.Complete(ctx, claimed.ID, 0, 0)
	}

	// Build entity roster from existing entities in DB
	entities, _ := entityStore.ListByRepo(ctx, cfg.RepoID)
	var rosterSB strings.Builder
	if len(entities) > 0 {
		rosterSB.WriteString("Known entities (use these exact names when referencing):\n")
		limit := 100
		if len(entities) < limit {
			limit = len(entities)
		}
		for _, e := range entities[:limit] {
			fmt.Fprintf(&rosterSB, "- %s (%s)\n", e.QualifiedName, e.Kind)
		}
	} else {
		rosterSB.WriteString("No entities extracted yet — use the repository name for repo-level facts.\n")
	}
	entityRoster := rosterSB.String()

	// Process PRs in batches
	totalTokens := 0
	totalCostUSD := 0.0
	totalFacts := 0
	totalDecisions := 0

	for i := 0; i < len(signalPRs); i += batchSize {
		if err := ctx.Err(); err != nil {
			jobStore.Fail(ctx, claimed.ID, "cancelled")
			return fmt.Errorf("cancelled: %w", err)
		}

		end := i + batchSize
		if end > len(signalPRs) {
			end = len(signalPRs)
		}
		batch := signalPRs[i:end]

		batchNum := (i / batchSize) + 1
		totalBatches := (len(signalPRs) + batchSize - 1) / batchSize
		fmt.Printf("  Batch %d/%d (%d PRs)...\n", batchNum, totalBatches, len(batch))
		cfg.progress(fmt.Sprintf("Phase 3: Analyzing PR batch %d/%d", batchNum, totalBatches))

		// Format PR batch for LLM
		prsText := formatPRBatch(batch)

		prompt := Phase3Prompt(cfg.RepoName, prsText, entityRoster)

		var result *Phase3Result
		var lastResp *llm.Response
		const maxRetries = 3
		for attempt := 1; attempt <= maxRetries; attempt++ {
			resp, err := cfg.LLM.Complete(ctx, cfg.Model, systemPromptPhase3, []llm.Message{
				{Role: "user", Content: prompt},
			}, 4096, SchemaPhase3)
			if err != nil {
				if attempt == maxRetries {
					log.Printf("[phase3] LLM call failed after %d attempts: %v", maxRetries, err)
					break
				}
				fmt.Printf("    LLM attempt %d/%d failed: %v, retrying...\n", attempt, maxRetries, err)
				continue
			}

			result, err = ParsePhase3(resp.Content)
			if err != nil {
				if attempt == maxRetries {
					log.Printf("[phase3] parse failed after %d attempts: %v", maxRetries, err)
					break
				}
				fmt.Printf("    Parse attempt %d/%d failed: %v, retrying...\n", attempt, maxRetries, err)
				continue
			}
			lastResp = resp
			break
		}

		if result == nil || lastResp == nil {
			continue
		}

		totalTokens += lastResp.InputTokens + lastResp.OutputTokens
		totalCostUSD += float64(lastResp.InputTokens)/1_000_000*SonnetInputPer1M + float64(lastResp.OutputTokens)/1_000_000*SonnetOutputPer1M

		// Build batch PR ref string (e.g., "PR #12, #15, #23")
		var prNums []string
		for _, pr := range batch {
			prNums = append(prNums, fmt.Sprintf("#%d", pr.Number))
		}
		batchRef := "PR " + strings.Join(prNums, ", ")

		// Store facts with PR provenance
		for _, ef := range result.Facts {
			entityID, found := resolveEntity(ctx, entityStore, cfg.RepoID, ef.EntityName)
			if !found {
				// Create a concept entity for unresolved names
				newEntity := &models.Entity{
					RepoID:        cfg.RepoID,
					Kind:          models.EntityConcept,
					Name:          ef.EntityName,
					QualifiedName: ef.EntityName,
					Summary:       models.Ptr("Entity referenced in PR discussions"),
				}
				if err := entityStore.Upsert(ctx, newEntity); err != nil {
					logVerboseF("[phase3] warn: upserting entity %s: %v", ef.EntityName, err)
					continue
				}
				entityID = newEntity.ID
			}

			fact := &models.Fact{
				EntityID:   entityID,
				RepoID:     cfg.RepoID,
				Claim:      ef.Claim,
				Dimension:  ef.Dimension,
				Category:   ef.Category,
				Confidence: ef.Confidence,
				Provenance: []models.Provenance{{
					SourceType: "pr",
					Repo:       cfg.RepoName,
					Ref:        batchRef,
				}},
			}
			if err := factStore.Create(ctx, fact); err != nil {
				logVerboseF("[phase3] warn: creating fact: %v", err)
				continue
			}
			totalFacts++
		}

		// Store decisions with PR provenance
		for _, d := range result.Decisions {
			// Build provenance with PR-specific details
			ref := "github-prs"
			url := ""
			excerpt := d.Summary
			if d.PRNumber > 0 {
				ref = fmt.Sprintf("PR #%d", d.PRNumber)
				// Find the PR URL from the batch
				for _, pr := range batch {
					if pr.Number == d.PRNumber {
						url = pr.URL
						excerpt = pr.Title
						break
					}
				}
			}

			// Convert alternatives
			var alts []models.Alternative
			for _, a := range d.Alternatives {
				alts = append(alts, models.Alternative{
					Description:     a.Description,
					RejectedBecause: a.RejectedBecause,
				})
			}

			decision := &models.Decision{
				RepoID:       cfg.RepoID,
				Summary:      d.Summary,
				Description:  d.Description,
				Rationale:    d.Rationale,
				Alternatives: alts,
				Tradeoffs:    d.Tradeoffs,
				StillValid:   true,
				Provenance: []models.Provenance{{
					SourceType: "pr",
					Repo:       cfg.RepoName,
					Ref:        ref,
					URL:        url,
					Excerpt:    excerpt,
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
				logVerboseF("[phase3] warn: creating decision: %v", err)
				continue
			}
			totalDecisions++

			// Link decision to related entities
			var linkedEntityIDs []uuid.UUID
			descLower := strings.ToLower(d.Summary + " " + d.Description)
			for _, e := range entities {
				if strings.Contains(descLower, strings.ToLower(e.Name)) {
					linkedEntityIDs = append(linkedEntityIDs, e.ID)
				}
			}
			if len(linkedEntityIDs) > 0 {
				if err := decisionStore.LinkEntities(ctx, decision.ID, linkedEntityIDs); err != nil {
					logVerboseF("[phase3] warn: linking decision entities: %v", err)
				}
			}
		}
	}

	log.Printf("[phase3] analyzed %d PRs (%d with signal), extracted %d facts, %d decisions",
		len(prs), len(signalPRs), totalFacts, totalDecisions)
	fmt.Printf("  Extracted: %d facts, %d decisions\n", totalFacts, totalDecisions)

	return jobStore.Complete(ctx, claimed.ID, totalTokens, totalCostUSD)
}

func formatPRBatch(prs []ghpkg.PR) string {
	var sb strings.Builder
	for _, pr := range prs {
		fmt.Fprintf(&sb, "### PR #%d: %s\n", pr.Number, pr.Title)
		fmt.Fprintf(&sb, "Author: %s | Merged: %s\n", pr.Author, pr.MergedAt.Format("2006-01-02"))
		if len(pr.Labels) > 0 {
			fmt.Fprintf(&sb, "Labels: %s\n", strings.Join(pr.Labels, ", "))
		}

		body := pr.Body
		if len(body) > 2000 {
			body = body[:2000] + "..."
		}
		fmt.Fprintf(&sb, "\n%s\n", body)

		if len(pr.ReviewComments) > 0 {
			sb.WriteString("\nReview comments:\n")
			for _, rc := range pr.ReviewComments {
				comment := rc.Body
				if len(comment) > 500 {
					comment = comment[:500] + "..."
				}
				fmt.Fprintf(&sb, "- %s (%s): %s\n", rc.Author, rc.State, comment)
			}
		}

		if len(pr.LinkedIssues) > 0 {
			sb.WriteString("\nLinked issues:\n")
			for _, issue := range pr.LinkedIssues {
				issueBody := issue.Body
				if len(issueBody) > 500 {
					issueBody = issueBody[:500] + "..."
				}
				fmt.Fprintf(&sb, "- #%d: %s — %s\n", issue.Number, issue.Title, issueBody)
			}
		}

		sb.WriteString("\n---\n\n")
	}
	return sb.String()
}
