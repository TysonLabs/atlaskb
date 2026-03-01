//go:build ignore

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/db"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
	"github.com/tgeorge06/atlaskb/internal/query"
)

type testCase struct {
	Question   string
	RepoName   string // empty = unscoped
	ExpectIn3  string // entity name or substring expected in top 3 results
	CheckLabel string // short label for output
}

func main() {
	verbose := flag.Bool("v", false, "enable verbose score breakdowns")
	flag.Parse()

	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	pool, err := db.Connect(context.Background(), cfg.Database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	embedClient := embeddings.NewOpenAIClient(cfg.Embeddings.BaseURL, cfg.Embeddings.APIKey)
	llmClient := llm.NewOpenAIClient(cfg.LLM.BaseURL, cfg.LLM.APIKey)

	repoStore := &models.RepoStore{Pool: pool}

	tests := []testCase{
		// Go EventBus test repo
		{Question: "What design pattern does ExponentialBackoff use?", RepoName: "atlaskb-test-repo", ExpectIn3: "backoff", CheckLabel: "ExponentialBackoff pattern"},
		{Question: "What are BusConfig defaults?", RepoName: "atlaskb-test-repo", ExpectIn3: "BusConfig", CheckLabel: "BusConfig defaults"},
		{Question: "How does the EventBus handle subscriber registration?", RepoName: "atlaskb-test-repo", ExpectIn3: "EventBus", CheckLabel: "EventBus subscriber"},
		{Question: "What retry strategies does the event bus support?", RepoName: "atlaskb-test-repo", ExpectIn3: "retry", CheckLabel: "Retry strategies"},

		// Python TaskFlow test repo
		{Question: "How does TaskFlow handle task dependencies?", RepoName: "atlaskb-python-test-repo", ExpectIn3: "Task", CheckLabel: "TaskFlow dependencies"},
		{Question: "What is the Orchestrator's role in TaskFlow?", RepoName: "atlaskb-python-test-repo", ExpectIn3: "Orchestrator", CheckLabel: "Orchestrator role"},
		{Question: "How does TaskFlow handle error recovery?", RepoName: "atlaskb-python-test-repo", ExpectIn3: "error", CheckLabel: "TaskFlow errors"},
		{Question: "What execution modes does the Scheduler support?", RepoName: "atlaskb-python-test-repo", ExpectIn3: "Scheduler", CheckLabel: "Scheduler modes"},

		// TypeScript webhookrelay test repo
		{Question: "How does the webhook relay handle retries?", RepoName: "atlaskb-typescript-test-repo", ExpectIn3: "retry", CheckLabel: "Webhook retry"},
		{Question: "What delivery guarantees does webhookrelay provide?", RepoName: "atlaskb-typescript-test-repo", ExpectIn3: "deliver", CheckLabel: "Delivery guarantees"},
		{Question: "How is webhook routing configured?", RepoName: "atlaskb-typescript-test-repo", ExpectIn3: "rout", CheckLabel: "Webhook routing"},
		{Question: "What authentication does the webhook relay use?", RepoName: "atlaskb-typescript-test-repo", ExpectIn3: "auth", CheckLabel: "Webhook auth"},

		// Cross-repo / unscoped tests
		{Question: "What design patterns are used across all test repos?", RepoName: "", ExpectIn3: "", CheckLabel: "Cross-repo patterns (diversity)"},
		{Question: "How do different repos handle error recovery?", RepoName: "", ExpectIn3: "", CheckLabel: "Cross-repo errors (diversity)"},
	}

	fmt.Println("# AtlasKB Search Ranking Validation")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()

	passed := 0
	total := 0

	for i, tc := range tests {
		ctx := context.Background()

		var repoIDs []uuid.UUID
		if tc.RepoName != "" {
			repos, _ := repoStore.List(ctx)
			for _, r := range repos {
				if r.Name == tc.RepoName {
					repoIDs = append(repoIDs, r.ID)
				}
			}
		}

		engine := query.NewEngine(pool, embedClient)
		engine.Verbose = *verbose
		engine.SetLLM(llmClient, cfg.Pipeline.ExtractionModel)

		results, err := engine.Search(ctx, tc.Question, repoIDs, 20)
		if err != nil {
			fmt.Printf("Q%d [%s]: ERROR - %v\n\n", i+1, tc.CheckLabel, err)
			continue
		}

		fmt.Printf("## Q%d: %s\n", i+1, tc.Question)
		if tc.RepoName != "" {
			fmt.Printf("   Repo: %s\n", tc.RepoName)
		} else {
			fmt.Printf("   Repo: (unscoped)\n")
		}
		fmt.Printf("   Results: %d\n", len(results))

		// Show top 5 results
		top := 5
		if len(results) < top {
			top = len(results)
		}
		for j := 0; j < top; j++ {
			r := results[j]
			claim := r.Fact.Claim
			if len(claim) > 80 {
				claim = claim[:80] + "..."
			}
			fmt.Printf("   #%d [%.3f] %-12s | %-20s | %s\n",
				j+1, r.Score, r.Source, r.Entity.Name, claim)
		}

		// Check expectations
		if tc.ExpectIn3 != "" {
			total++
			found := false
			for j := 0; j < 3 && j < len(results); j++ {
				if containsCI(results[j].Entity.Name, tc.ExpectIn3) ||
					containsCI(results[j].Fact.Claim, tc.ExpectIn3) {
					found = true
					break
				}
			}
			if found {
				fmt.Printf("   PASS: Found %q in top 3\n", tc.ExpectIn3)
				passed++
			} else {
				fmt.Printf("   FAIL: %q NOT in top 3\n", tc.ExpectIn3)
			}
		}

		// For unscoped queries, check repo diversity
		if tc.RepoName == "" && len(results) > 0 {
			repoDistrib := make(map[string]int)
			for _, r := range results {
				repoDistrib[r.RepoName]++
			}
			fmt.Printf("   Repo distribution: ")
			for name, count := range repoDistrib {
				fmt.Printf("%s=%d ", name, count)
			}
			fmt.Println()
			maxCount := 0
			for _, c := range repoDistrib {
				if c > maxCount {
					maxCount = c
				}
			}
			if maxCount > len(results)/2 && len(repoDistrib) > 1 {
				fmt.Printf("   WARNING: Single repo has %d/%d results (>50%%)\n", maxCount, len(results))
			}
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("Results: %d/%d targeted checks passed\n", passed, total)
}

func containsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
