package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	ghpkg "github.com/tgeorge06/atlaskb/internal/github"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func initGitRepoForIntegration(t *testing.T, repoPath string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", repoPath, "init"},
		{"git", "-C", repoPath, "config", "user.email", "test@example.com"},
		{"git", "-C", repoPath, "config", "user.name", "atlas-test"},
		{"git", "-C", repoPath, "add", "."},
		{"git", "-C", repoPath, "commit", "-m", "initial"},
	}
	for _, args := range cmds {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			t.Fatalf("%s: %v", strings.Join(args, " "), err)
		}
	}
}

func writeOrchestratorRepo(t *testing.T) string {
	t.Helper()
	repoPath := t.TempDir()

	goMod := `module github.com/example/atlas-orchestrator

go 1.25

require github.com/acme/librepo v1.2.3
`
	if err := os.WriteFile(filepath.Join(repoPath, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	svc := `package svc

type Service struct{}

type Helper struct{}

func Run() {
	validate()
	helper()
}

func validate() {}

func helper() {}
`
	if err := os.MkdirAll(filepath.Join(repoPath, "internal", "svc"), 0o755); err != nil {
		t.Fatalf("mkdir internal/svc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "internal", "svc", "service.go"), []byte(svc), 0o644); err != nil {
		t.Fatalf("write service.go: %v", err)
	}

	initGitRepoForIntegration(t, repoPath)
	return repoPath
}

func orchestratorMockLLM() *llm.MockClient {
	return &llm.MockClient{CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
		if schema == nil {
			return &llm.Response{Content: `{}`, Model: model, InputTokens: 20, OutputTokens: 20}, nil
		}
		switch schema.Name {
		case "phase2_extraction":
			return &llm.Response{Model: model, InputTokens: 180, OutputTokens: 260, Content: `{
  "file_summary": "Service package orchestrates validation and helper logic.",
  "entities": [
    {"kind":"type","name":"Service","qualified_name":"svc::Service","summary":"Main service type"},
    {"kind":"type","name":"Helper","qualified_name":"svc::Helper","summary":"Helper type"},
    {"kind":"function","name":"Run","qualified_name":"svc::Run","summary":"Pipeline entry function"},
    {"kind":"function","name":"validate","qualified_name":"svc::validate","summary":"Validation step"},
    {"kind":"function","name":"helper","qualified_name":"svc::helper","summary":"Helper step"}
  ],
  "facts": [
    {"entity_name":"svc::Run","claim":"Run orchestrates service execution","dimension":"what","category":"behavior","confidence":"high"},
    {"entity_name":"svc::validate","claim":"validate enforces preconditions","dimension":"why","category":"constraint","confidence":"high"},
    {"entity_name":"svc::helper","claim":"helper provides reusable logic","dimension":"how","category":"pattern","confidence":"medium"}
  ],
  "relationships": [
    {"from":"svc::Run","to":"svc::validate","kind":"calls","description":"Run validates first","strength":"strong"},
    {"from":"svc::Run","to":"svc::helper","kind":"calls","description":"Run calls helper","strength":"moderate"}
  ]
}`}, nil
		case "phase4_synthesis":
			return &llm.Response{Model: model, InputTokens: 140, OutputTokens: 220, Content: `{
  "architectural_patterns": [{"pattern":"service orchestration","description":"Run coordinates smaller steps","confidence":"high"}],
  "data_flows": [{"description":"request validation flow","from_module":"svc::Run","to_module":"svc::validate","mechanism":"direct call"}],
  "contracts": [{"between":["svc::Run","svc::validate"],"description":"validation before helper","explicit":true}],
  "facts": [{"entity_name":"svc::Service","claim":"Service uses orchestrated flow","dimension":"what","category":"behavior","confidence":"high"}],
  "relationships": [{"from":"svc::Service","to":"svc::Helper","kind":"depends_on","description":"Service depends on helper","strength":"moderate"}]
}`}, nil
		case "phase5_summary":
			return &llm.Response{Model: model, InputTokens: 120, OutputTokens: 180, Content: `{
  "summary":"Repository demonstrates a simple orchestrated service flow.",
  "capabilities":["flow orchestration","validation","helper composition"],
  "architecture":"Single service package with explicit execution sequencing.",
  "conventions":[{"category":"style","description":"Small focused functions","examples":["Run","validate"]}],
  "risks_and_debt":["Limited error handling"],
  "key_integration_points":["Run -> validate","Run -> helper"]
}`}, nil
		case "gitlog_analysis":
			return &llm.Response{Model: model, InputTokens: 90, OutputTokens: 120, Content: `{
  "facts": [{"entity_name":"svc::Run","claim":"Git history shows Run as the main orchestration path","dimension":"when","category":"timeline","confidence":"medium"}],
  "decisions": [{"summary":"Keep orchestration in Run","description":"Centralized control flow in a single entrypoint","rationale":"Improves discoverability","made_at":"2026-01-01"}]
}`}, nil
		case "cluster_label":
			return &llm.Response{Model: model, InputTokens: 40, OutputTokens: 24, Content: `{"label":"service orchestration"}`}, nil
		default:
			return &llm.Response{Content: `{}`, Model: model, InputTokens: 20, OutputTokens: 20}, nil
		}
	}}
}

func TestIntegrationOrchestrateAndIncrementalSkip(t *testing.T) {
	pool, ctx := newIntegrationPipelinePool(t)
	repoPath := writeOrchestratorRepo(t)

	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	runStore := &models.IndexingRunStore{Pool: pool}

	depRepo := &models.Repo{Name: "librepo", LocalPath: t.TempDir(), DefaultBranch: "main", RemoteURL: models.Ptr("https://github.com/acme/librepo.git")}
	if err := repoStore.Create(ctx, depRepo); err != nil {
		t.Fatalf("create dep repo: %v", err)
	}
	depRootEntity := &models.Entity{RepoID: depRepo.ID, Kind: models.EntityService, Name: depRepo.Name, QualifiedName: depRepo.Name, Summary: models.Ptr("Dependency repo root")}
	if err := entityStore.Create(ctx, depRootEntity); err != nil {
		t.Fatalf("create dep repo root entity: %v", err)
	}

	progressCalls := 0
	result, err := Orchestrate(ctx, OrchestratorConfig{
		RepoPath:        repoPath,
		Force:           true,
		Concurrency:     1,
		ExtractionModel: "mock-model",
		SynthesisModel:  "mock-model",
		Pool:            pool,
		LLM:             orchestratorMockLLM(),
		Embedder: &embeddings.MockClient{EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
			out := make([][]float32, len(texts))
			for i := range out {
				v := make([]float32, 1024)
				for j := range v {
					v[j] = 0.07 + float32((i+j)%5)*0.0001
				}
				out[i] = v
			}
			return out, nil
		}},
		GitLogLimit: 15,
		Phases:      []string{"phase1", "phase2", "phase4", "phase5", "phase6", "embedding", "gitlog"},
		ProgressFunc: func(msg string) {
			if strings.TrimSpace(msg) != "" {
				progressCalls++
			}
		},
	})
	if err != nil {
		t.Fatalf("Orchestrate(full partial-phase run): %v", err)
	}
	if result == nil || result.RepoID == depRepo.ID {
		t.Fatalf("unexpected orchestrator result: %+v", result)
	}
	if result.Phase2Stats == nil || result.Phase2Stats.FilesProcessed == 0 {
		t.Fatalf("phase2 stats missing from orchestrator result: %+v", result.Phase2Stats)
	}
	if progressCalls == 0 {
		t.Fatalf("expected progress callbacks during orchestration")
	}

	cross, err := relStore.ListAllCrossRepo(ctx)
	if err != nil {
		t.Fatalf("ListAllCrossRepo: %v", err)
	}
	if len(cross) == 0 {
		t.Fatalf("expected at least one discovered cross-repo relationship")
	}
	pendingEmbeddings, err := factStore.ListByRepoWithoutEmbedding(ctx, result.RepoID)
	if err != nil {
		t.Fatalf("ListByRepoWithoutEmbedding: %v", err)
	}
	if len(pendingEmbeddings) != 0 {
		t.Fatalf("expected embeddings generated for all facts, pending=%d", len(pendingEmbeddings))
	}
	runs, err := runStore.ListByRepo(ctx, result.RepoID)
	if err != nil {
		t.Fatalf("ListByRepo(indexing runs): %v", err)
	}
	if len(runs) == 0 {
		t.Fatalf("expected indexing run to be persisted")
	}

	result2, err := Orchestrate(ctx, OrchestratorConfig{
		RepoPath:        repoPath,
		Force:           false,
		Concurrency:     1,
		ExtractionModel: "mock-model",
		SynthesisModel:  "mock-model",
		Pool:            pool,
		LLM:             orchestratorMockLLM(),
		Embedder:        &embeddings.MockClient{},
		GitLogLimit:     15,
	})
	if err != nil {
		t.Fatalf("Orchestrate(incremental): %v", err)
	}
	if result2 == nil || result2.RepoID != result.RepoID {
		t.Fatalf("unexpected incremental result: %+v", result2)
	}

	runsAfter, err := runStore.ListByRepo(ctx, result.RepoID)
	if err != nil {
		t.Fatalf("ListByRepo after incremental run: %v", err)
	}
	if len(runsAfter) < 2 {
		t.Fatalf("expected two indexing runs after incremental rerun, got %d", len(runsAfter))
	}
}

func TestIntegrationImportCrossRepoAndQualitySuccessPaths(t *testing.T) {
	pool, ctx := newIntegrationPipelinePool(t)
	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}

	repoA := &models.Repo{Name: "service-a", LocalPath: t.TempDir(), DefaultBranch: "main"}
	repoB := &models.Repo{Name: "librepo", LocalPath: t.TempDir(), DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repoA); err != nil {
		t.Fatalf("create repoA: %v", err)
	}
	if err := repoStore.Create(ctx, repoB); err != nil {
		t.Fatalf("create repoB: %v", err)
	}

	sourceEntity := &models.Entity{RepoID: repoA.ID, Kind: models.EntityFunction, Name: "Run", QualifiedName: "svc::Run", Path: models.Ptr("internal/svc/service.go"), Summary: models.Ptr("Entry")}
	repoRootA := &models.Entity{RepoID: repoA.ID, Kind: models.EntityService, Name: repoA.Name, QualifiedName: repoA.Name, Summary: models.Ptr("Repo A root")}
	repoRootB := &models.Entity{RepoID: repoB.ID, Kind: models.EntityService, Name: repoB.Name, QualifiedName: repoB.Name, Summary: models.Ptr("Repo B root")}
	for _, e := range []*models.Entity{sourceEntity, repoRootA, repoRootB} {
		if err := entityStore.Create(ctx, e); err != nil {
			t.Fatalf("create entity %s: %v", e.QualifiedName, err)
		}
	}

	created := StoreImportRelationships(ctx, pool, repoA.ID, repoA.Name, []ImportEntry{{
		FilePath:   "internal/svc/service.go",
		ImportPath: "github.com/acme/librepo",
	}}, []EntityEntry{{
		Path:          "internal/svc/service.go",
		QualifiedName: "svc::Run",
	}})
	if created != 0 {
		t.Fatalf("expected zero created imports relationships with current schema enum, got %d", created)
	}

	crossCreated, _ := DiscoverCrossRepoLinks(ctx, pool, repoA.ID, repoA.Name, []Dependency{{
		Name:   "github.com/acme/librepo",
		Source: "go.mod",
	}})
	if crossCreated == 0 {
		t.Fatalf("expected cross-repo discovery to create links")
	}

	fact := &models.Fact{EntityID: sourceEntity.ID, RepoID: repoA.ID, Claim: "Run calls into librepo adapter", Dimension: models.DimensionWhat, Category: models.CategoryBehavior, Confidence: models.ConfidenceHigh}
	if err := factStore.Create(ctx, fact); err != nil {
		t.Fatalf("create fact: %v", err)
	}
	rel := &models.Relationship{RepoID: repoA.ID, FromEntityID: sourceEntity.ID, ToEntityID: repoRootA.ID, Kind: models.RelCalls, Strength: models.StrengthStrong, Confidence: 0.91}
	if err := relStore.Create(ctx, rel); err != nil {
		t.Fatalf("create relationship: %v", err)
	}

	qs, err := ComputeQuality(ctx, pool, repoA.ID)
	if err != nil {
		t.Fatalf("ComputeQuality success path: %v", err)
	}
	if qs == nil || qs.Overall <= 0 {
		t.Fatalf("expected non-zero quality score, got %+v", qs)
	}
}

func TestIntegrationRunPhase3WithGraphQLServer(t *testing.T) {
	pool, ctx := newIntegrationPipelinePool(t)

	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	decisionStore := &models.DecisionStore{Pool: pool}
	jobStore := &models.JobStore{Pool: pool}

	repo := &models.Repo{
		Name:          "phase3-repo",
		LocalPath:     t.TempDir(),
		DefaultBranch: "main",
		RemoteURL:     models.Ptr("https://github.com/acme/phase3-repo.git"),
	}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := entityStore.Create(ctx, &models.Entity{
		RepoID:        repo.ID,
		Kind:          models.EntityFunction,
		Name:          "Run",
		QualifiedName: "svc::Run",
		Summary:       models.Ptr("Main orchestration entrypoint"),
	}); err != nil {
		t.Fatalf("create seed entity: %v", err)
	}

	graphQL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequests": map[string]any{
						"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
						"nodes": []map[string]any{
							{
								"number":   42,
								"title":    "Improve service orchestration and validation",
								"url":      "https://github.com/acme/phase3-repo/pull/42",
								"body":     strings.Repeat("This PR adds significant behavior and design decisions. ", 4),
								"mergedAt": "2026-01-10T00:00:00Z",
								"author":   map[string]any{"login": "dev"},
								"labels":   map[string]any{"nodes": []map[string]any{{"name": "feature"}}},
								"reviews": map[string]any{"nodes": []map[string]any{
									{"author": map[string]any{"login": "reviewer"}, "body": "Looks good, ship it.", "state": "APPROVED"},
								}},
								"closingIssuesReferences": map[string]any{"nodes": []map[string]any{
									{
										"number": 7,
										"title":  "Track orchestration behavior",
										"body":   "Need stronger orchestration guarantees and observability.",
										"labels": map[string]any{"nodes": []map[string]any{{"name": "enhancement"}}},
									},
								}},
							},
						},
					},
				},
				"rateLimit": map[string]any{"remaining": 5000, "resetAt": "2026-01-10T01:00:00Z"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer graphQL.Close()

	ghClient := ghpkg.NewClient(config.GitHubConfig{
		Token:       "test-token",
		APIURL:      graphQL.URL,
		MaxPRs:      20,
		PRBatchSize: 10,
	})

	mockLLM := &llm.MockClient{CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
		if schema != nil && schema.Name == "phase3_pr_analysis" {
			return &llm.Response{
				Model:        model,
				InputTokens:  180,
				OutputTokens: 250,
				Content: `{
  "facts": [
    {"entity_name":"svc::Run","claim":"Run now coordinates validation and helper orchestration from PR context","dimension":"what","category":"behavior","confidence":"high"}
  ],
  "decisions": [
    {
      "summary":"Adopt centralized orchestration in Run",
      "description":"PR shifted sequencing logic into a single execution entrypoint.",
      "rationale":"Improves maintainability and traceability.",
      "alternatives":[{"description":"distribute orchestration","rejected_because":"harder to reason about"}],
      "tradeoffs":["larger Run function"],
      "pr_number":42,
      "made_at":"2026-01-10"
    }
  ]
}`,
			}, nil
		}
		return &llm.Response{Content: `{}`, Model: model, InputTokens: 10, OutputTokens: 10}, nil
	}}

	if err := RunPhase3(ctx, Phase3Config{
		RepoID:      repo.ID,
		RepoName:    repo.Name,
		RemoteURL:   "https://github.com/acme/phase3-repo.git",
		Model:       "mock-model",
		Pool:        pool,
		LLM:         mockLLM,
		GitHub:      ghClient,
		MaxPRs:      10,
		PRBatchSize: 5,
	}); err != nil {
		t.Fatalf("RunPhase3: %v", err)
	}

	totalFacts, _, err := factStore.CountByRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("CountByRepo facts: %v", err)
	}
	if totalFacts == 0 {
		t.Fatalf("expected phase3 facts to be stored")
	}
	decisions, err := decisionStore.ListByRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("ListByRepo decisions: %v", err)
	}
	if len(decisions) == 0 {
		t.Fatalf("expected at least one decision from phase3")
	}

	job, err := jobStore.GetByTarget(ctx, repo.ID, models.PhasePhase3, "github-prs")
	if err != nil {
		t.Fatalf("GetByTarget phase3 job: %v", err)
	}
	if job == nil || job.Status != models.JobCompleted {
		t.Fatalf("expected completed phase3 job, got %+v", job)
	}
}

func TestIntegrationRunPhase3NoSignalAndAlreadyCompleted(t *testing.T) {
	pool, ctx := newIntegrationPipelinePool(t)
	repoStore := &models.RepoStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	decisionStore := &models.DecisionStore{Pool: pool}
	jobStore := &models.JobStore{Pool: pool}

	repo := &models.Repo{
		Name:          "phase3-nosignal",
		LocalPath:     t.TempDir(),
		DefaultBranch: "main",
		RemoteURL:     models.Ptr("https://github.com/acme/phase3-nosignal.git"),
	}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	graphQL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequests": map[string]any{
						"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
						"nodes": []map[string]any{
							{
								"number":                  9,
								"title":                   "chore: update docs",
								"url":                     "https://github.com/acme/phase3-nosignal/pull/9",
								"body":                    "tiny",
								"mergedAt":                "2026-01-12T00:00:00Z",
								"author":                  map[string]any{"login": "dev"},
								"labels":                  map[string]any{"nodes": []map[string]any{}},
								"reviews":                 map[string]any{"nodes": []map[string]any{}},
								"closingIssuesReferences": map[string]any{"nodes": []map[string]any{}},
							},
						},
					},
				},
				"rateLimit": map[string]any{"remaining": 5000, "resetAt": "2026-01-12T01:00:00Z"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer graphQL.Close()

	ghClient := ghpkg.NewClient(config.GitHubConfig{
		Token:       "test-token",
		APIURL:      graphQL.URL,
		MaxPRs:      5,
		PRBatchSize: 5,
	})

	cfg := Phase3Config{
		RepoID:      repo.ID,
		RepoName:    repo.Name,
		RemoteURL:   "https://github.com/acme/phase3-nosignal.git",
		Model:       "mock-model",
		Pool:        pool,
		LLM:         &llm.MockClient{},
		GitHub:      ghClient,
		MaxPRs:      5,
		PRBatchSize: 5,
	}
	if err := RunPhase3(ctx, cfg); err != nil {
		t.Fatalf("RunPhase3 no-signal: %v", err)
	}
	// Second run should exercise the already-completed fast path.
	if err := RunPhase3(ctx, cfg); err != nil {
		t.Fatalf("RunPhase3 already-completed: %v", err)
	}

	totalFacts, _, err := factStore.CountByRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("CountByRepo facts: %v", err)
	}
	if totalFacts != 0 {
		t.Fatalf("expected no facts for no-signal PRs, got %d", totalFacts)
	}
	decisions, err := decisionStore.ListByRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("ListByRepo decisions: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected no decisions for no-signal PRs, got %d", len(decisions))
	}
	job, err := jobStore.GetByTarget(ctx, repo.ID, models.PhasePhase3, "github-prs")
	if err != nil {
		t.Fatalf("GetByTarget phase3 job: %v", err)
	}
	if job == nil || job.Status != models.JobCompleted {
		t.Fatalf("expected completed no-signal job, got %+v", job)
	}
}

func TestIntegrationRunPhase3ResetsFailedJob(t *testing.T) {
	pool, ctx := newIntegrationPipelinePool(t)
	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	jobStore := &models.JobStore{Pool: pool}

	repo := &models.Repo{
		Name:          "phase3-reset",
		LocalPath:     t.TempDir(),
		DefaultBranch: "main",
		RemoteURL:     models.Ptr("https://github.com/acme/phase3-reset.git"),
	}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := entityStore.Create(ctx, &models.Entity{
		RepoID:        repo.ID,
		Kind:          models.EntityFunction,
		Name:          "Run",
		QualifiedName: "svc::Run",
		Summary:       models.Ptr("Entry"),
	}); err != nil {
		t.Fatalf("create seed entity: %v", err)
	}

	seedJob := &models.ExtractionJob{RepoID: repo.ID, Phase: models.PhasePhase3, Target: "github-prs", Status: models.JobPending}
	if err := jobStore.Create(ctx, seedJob); err != nil {
		t.Fatalf("seed Create job: %v", err)
	}
	claimed, err := jobStore.ClaimNext(ctx, repo.ID, models.PhasePhase3)
	if err != nil {
		t.Fatalf("seed ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatalf("expected claimable seed job")
	}
	if err := jobStore.Fail(ctx, claimed.ID, "intentional test failure"); err != nil {
		t.Fatalf("seed Fail job: %v", err)
	}

	graphQL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequests": map[string]any{
						"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
						"nodes": []map[string]any{
							{
								"number":   11,
								"title":    "Add high-signal orchestrator change",
								"url":      "https://github.com/acme/phase3-reset/pull/11",
								"body":     strings.Repeat("This pull request changes behavior and architecture. ", 5),
								"mergedAt": "2026-01-20T00:00:00Z",
								"author":   map[string]any{"login": "dev"},
								"labels":   map[string]any{"nodes": []map[string]any{{"name": "feature"}}},
								"reviews": map[string]any{"nodes": []map[string]any{
									{"author": map[string]any{"login": "reviewer"}, "body": "approved with notes", "state": "APPROVED"},
								}},
								"closingIssuesReferences": map[string]any{"nodes": []map[string]any{}},
							},
						},
					},
				},
				"rateLimit": map[string]any{"remaining": 5000, "resetAt": "2026-01-20T01:00:00Z"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer graphQL.Close()

	ghClient := ghpkg.NewClient(config.GitHubConfig{
		Token:       "test-token",
		APIURL:      graphQL.URL,
		MaxPRs:      10,
		PRBatchSize: 5,
	})
	mockLLM := &llm.MockClient{CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
		return &llm.Response{
			Model:        model,
			InputTokens:  100,
			OutputTokens: 140,
			Content: `{
  "facts": [{"entity_name":"svc::Run","claim":"Run behavior changed based on PR evidence","dimension":"what","category":"behavior","confidence":"high"}],
  "decisions": []
}`,
		}, nil
	}}

	if err := RunPhase3(ctx, Phase3Config{
		RepoID:      repo.ID,
		RepoName:    repo.Name,
		RemoteURL:   "https://github.com/acme/phase3-reset.git",
		Model:       "mock-model",
		Pool:        pool,
		LLM:         mockLLM,
		GitHub:      ghClient,
		MaxPRs:      10,
		PRBatchSize: 5,
	}); err != nil {
		t.Fatalf("RunPhase3 reset-failed: %v", err)
	}

	job, err := jobStore.GetByTarget(ctx, repo.ID, models.PhasePhase3, "github-prs")
	if err != nil {
		t.Fatalf("GetByTarget phase3 job: %v", err)
	}
	if job == nil || job.Status != models.JobCompleted {
		t.Fatalf("expected completed job after reset path, got %+v", job)
	}
}
