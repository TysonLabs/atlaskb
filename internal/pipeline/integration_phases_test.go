package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/db"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func newIntegrationPipelinePool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ATLASKB_TEST_DSN"))
	if dsn == "" {
		t.Skip("integration DB not configured; set ATLASKB_TEST_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("integration DB unavailable: %v", err)
	}
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire lock connection: %v", err)
	}
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(88012233)); err != nil {
		lockConn.Release()
		t.Fatalf("pg_advisory_lock: %v", err)
	}
	t.Cleanup(func() {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, int64(88012233))
		lockConn.Release()
	})

	if err := db.ResetSchema(ctx, pool); err != nil {
		t.Fatalf("ResetSchema: %v", err)
	}
	if err := db.RunMigrations(dsn); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return pool, ctx
}

func writePipelineTestRepo(t *testing.T) (root string, relPath string) {
	t.Helper()
	root = t.TempDir()
	relPath = "internal/svc/service.go"
	abs := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	src := `package svc

type Validator struct{}
type Deep struct{}

func Helper() {}

func Validate() {}

func Service() {
	Helper()
	Validate()
}

func Deeper() {}
`
	if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root, relPath
}

func TestIntegrationRunPhase2To6AndFlows(t *testing.T) {
	pool, ctx := newIntegrationPipelinePool(t)

	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	flowStore := &models.FlowStore{Pool: pool}

	repoPath, relPath := writePipelineTestRepo(t)
	manifest, err := RunPhase1(repoPath, nil)
	if err != nil {
		t.Fatalf("RunPhase1: %v", err)
	}

	repo := &models.Repo{
		Name:          "pipeline-integration",
		LocalPath:     repoPath,
		DefaultBranch: "main",
	}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("Repo Create: %v", err)
	}

	// Seed a stale entity in the same path so processFile exercises stale cleanup.
	stale := &models.Entity{
		RepoID:        repo.ID,
		Kind:          models.EntityFunction,
		Name:          "OldEntity",
		QualifiedName: "svc::OldEntity",
		Path:          models.Ptr(relPath),
		Summary:       models.Ptr("stale"),
	}
	if err := entityStore.Create(ctx, stale); err != nil {
		t.Fatalf("seed stale entity: %v", err)
	}

	mockLLM := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			if schema == nil {
				return &llm.Response{Content: `{}`, Model: model, InputTokens: 50, OutputTokens: 50}, nil
			}
			switch schema.Name {
			case "phase2_extraction":
				return &llm.Response{
					Model: model, InputTokens: 220, OutputTokens: 380,
					Content: `{
  "file_summary": "Service orchestrates helper and validator behavior.",
  "entities": [
    {"kind":"function","name":"Service","qualified_name":"svc::Service","summary":"Orchestrates request processing","capabilities":["orchestrate"],"assumptions":["validator available"]},
    {"kind":"function","name":"Helper","qualified_name":"svc::Helper","summary":"Shared helper logic","capabilities":["utility"],"assumptions":[]},
    {"kind":"function","name":"Validate","qualified_name":"svc::Validate","summary":"Validation checks","capabilities":["validation"],"assumptions":[]},
    {"kind":"function","name":"Deeper","qualified_name":"svc::Deeper","summary":"Downstream deep step","capabilities":["deep-step"],"assumptions":[]}
  ],
  "facts": [
    {"entity_name":"svc::Service","claim":"Service coordinates helper and validation calls","dimension":"what","category":"behavior","confidence":"high"},
    {"entity_name":"svc::Helper","claim":"Helper centralizes reusable utility behavior","dimension":"how","category":"pattern","confidence":"medium"},
    {"entity_name":"svc::Validate","claim":"Validate enforces input constraints","dimension":"why","category":"constraint","confidence":"high"},
    {"entity_name":"svc::Deeper","claim":"Deeper handles post-validation enrichment","dimension":"how","category":"behavior","confidence":"medium"}
  ],
  "relationships": [
    {"from":"svc::Service","to":"svc::Helper","kind":"calls","description":"Service calls helper","strength":"strong"},
    {"from":"svc::Service","to":"svc::Validate","kind":"calls","description":"Service calls validator","strength":"strong"},
    {"from":"svc::Helper","to":"svc::Validate","kind":"calls","description":"Helper uses validator checks","strength":"moderate"},
    {"from":"svc::Helper","to":"svc::Deeper","kind":"calls","description":"Helper delegates deeper processing","strength":"moderate"},
    {"from":"svc::Service","to":"svc::Missing","kind":"depends_on","description":"intentionally unresolved to exercise defer path","strength":"weak"}
  ]
}`,
				}, nil
			case "phase4_synthesis":
				return &llm.Response{
					Model: model, InputTokens: 160, OutputTokens: 290,
					Content: `{
  "architectural_patterns": [{"pattern":"layered service","description":"Service delegates to helper and validator","confidence":"high"}],
  "data_flows": [{"description":"request validation path","from_module":"svc::Service","to_module":"svc::Validate","mechanism":"direct call"}],
  "contracts": [{"between":["svc::Service","svc::Validate"],"description":"Service expects validation before completion","explicit":true}],
  "facts": [{"entity_name":"svc::Service","claim":"Service is the entry orchestration point","dimension":"what","category":"behavior","confidence":"high"}],
  "relationships": [{"from":"svc::Service","to":"svc::Helper","kind":"depends_on","description":"Service depends on helper routines","strength":"moderate"}]
}`,
				}, nil
			case "phase5_summary":
				return &llm.Response{
					Model: model, InputTokens: 120, OutputTokens: 240,
					Content: `{
  "summary":"Pipeline integration repository demonstrating service orchestration.",
  "capabilities":["request orchestration","input validation","helper abstractions"],
  "architecture":"Layered orchestration with explicit validation and helper collaboration.",
  "conventions":[{"category":"naming","description":"Service functions use verb-first naming","examples":["Service","Validate"]}],
  "risks_and_debt":["Validation flow lacks explicit retry policy."],
  "key_integration_points":["Service -> Validate","Service -> Helper"]
}`,
				}, nil
			default:
				return &llm.Response{Content: `{}`, Model: model, InputTokens: 40, OutputTokens: 40}, nil
			}
		},
	}

	phase2Stats, err := RunPhase2(ctx, Phase2Config{
		RepoID:      repo.ID,
		RepoName:    repo.Name,
		RepoPath:    repoPath,
		Manifest:    manifest,
		Model:       "test-model",
		Concurrency: 1,
		Pool:        pool,
		LLM:         mockLLM,
		Roster: []EntityEntry{
			{Name: "Service", QualifiedName: "svc::Service", Kind: "function", Path: relPath, Line: 9},
			{Name: "Helper", QualifiedName: "svc::Helper", Kind: "function", Path: relPath, Line: 5},
			{Name: "Validate", QualifiedName: "svc::Validate", Kind: "function", Path: relPath, Line: 7},
			{Name: "Deeper", QualifiedName: "svc::Deeper", Kind: "function", Path: relPath, Line: 15},
		},
		ContextWindow: 8192,
	})
	if err != nil {
		t.Fatalf("RunPhase2: %v", err)
	}
	if phase2Stats.FilesProcessed != 1 || phase2Stats.EntitiesCreated < 4 || phase2Stats.FactsCreated < 4 {
		t.Fatalf("unexpected phase2 stats: %+v", phase2Stats)
	}

	if err := RunPhase4(ctx, Phase4Config{
		RepoID:        repo.ID,
		RepoName:      repo.Name,
		Model:         "test-model",
		Pool:          pool,
		LLM:           mockLLM,
		ContextWindow: 8192,
	}); err != nil {
		t.Fatalf("RunPhase4: %v", err)
	}

	if err := RunPhase5(ctx, Phase5Config{
		RepoID:        repo.ID,
		RepoName:      repo.Name,
		Model:         "test-model",
		Pool:          pool,
		LLM:           mockLLM,
		ContextWindow: 8192,
	}); err != nil {
		t.Fatalf("RunPhase5: %v", err)
	}

	phase6Stats, err := RunPhase6(ctx, Phase6Config{
		RepoID:         repo.ID,
		RepoName:       repo.Name,
		Model:          "test-model",
		Pool:           pool,
		LLM:            nil, // force keyword fallback labeling
		MinClusterSize: 2,
	})
	if err != nil {
		t.Fatalf("RunPhase6: %v", err)
	}
	if phase6Stats.EntitiesInGraph == 0 {
		t.Fatalf("phase6 should observe entities in graph")
	}

	flowStats, err := RunPhaseFlows(ctx, FlowsConfig{
		RepoID:   repo.ID,
		RepoName: repo.Name,
		Pool:     pool,
	})
	if err != nil {
		t.Fatalf("RunPhaseFlows: %v", err)
	}
	if flowStats.EntryPoints == 0 || flowStats.FlowsCreated == 0 {
		t.Fatalf("unexpected flow stats: %+v", flowStats)
	}
	flows, err := flowStore.ListByRepo(ctx, repo.ID, 20)
	if err != nil || len(flows) == 0 {
		t.Fatalf("flow store list failed: len=%d err=%v", len(flows), err)
	}

	overview, err := GenerateOverview(ctx, pool, repo.ID, repo.Name)
	if err != nil {
		t.Fatalf("GenerateOverview: %v", err)
	}
	if !strings.Contains(overview, "Capabilities") || !strings.Contains(overview, "Key Components") {
		t.Fatalf("overview missing expected sections:\n%s", overview)
	}
}

func TestIntegrationRunBackfillAndGitLog(t *testing.T) {
	pool, ctx := newIntegrationPipelinePool(t)
	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	decisionStore := &models.DecisionStore{Pool: pool}
	jobStore := &models.JobStore{Pool: pool}

	repoPath, relPath := writePipelineTestRepo(t)
	repo := &models.Repo{Name: "pipeline-backfill", LocalPath: repoPath, DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("Repo Create: %v", err)
	}

	parent := &models.Entity{
		RepoID:        repo.ID,
		Kind:          models.EntityType,
		Name:          "PipelineRunner",
		QualifiedName: "svc::PipelineRunner",
		Path:          models.Ptr(relPath),
		Summary:       models.Ptr("Coordinates pipeline phases"),
	}
	method := &models.Entity{
		RepoID:        repo.ID,
		Kind:          models.EntityFunction,
		Name:          "Run",
		QualifiedName: "svc::PipelineRunner.Run",
		Path:          models.Ptr(relPath),
		Summary:       models.Ptr("Executes full indexing pipeline"),
	}
	for _, e := range []*models.Entity{parent, method} {
		if err := entityStore.Create(ctx, e); err != nil {
			t.Fatalf("seed entity %s: %v", e.Name, err)
		}
	}

	mockLLM := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			if schema == nil {
				return &llm.Response{Content: `{}`, Model: model, InputTokens: 20, OutputTokens: 20}, nil
			}
			switch schema.Name {
			case "phase2_extraction":
				// Backfill phase consumes only facts/relationships from this payload.
				return &llm.Response{
					Model: model, InputTokens: 180, OutputTokens: 260,
					Content: `{
  "facts": [
    {"entity_name":"svc::PipelineRunner","claim":"PipelineRunner coordinates multi-phase indexing work","dimension":"what","category":"behavior","confidence":"high"},
    {"entity_name":"svc::PipelineRunner.Run","claim":"Run drives deterministic phase ordering","dimension":"how","category":"pattern","confidence":"medium"}
  ],
  "relationships": []
}`,
				}, nil
			case "gitlog_analysis":
				return &llm.Response{
					Model: model, InputTokens: 210, OutputTokens: 300,
					Content: `{
  "facts": [
    {"entity_name":"svc::PipelineRunner","claim":"Recent commits optimized phase ordering behavior","dimension":"when","category":"behavior","confidence":"medium"}
  ],
  "decisions": [
    {"summary":"Consolidate phase ordering","description":"Adopt consistent phase progression across runs","rationale":"Reduces operator confusion","made_at":"2026-03-01"}
  ]
}`,
				}, nil
			default:
				return &llm.Response{Content: `{}`, Model: model, InputTokens: 20, OutputTokens: 20}, nil
			}
		},
	}

	backfillStats, err := RunBackfill(ctx, BackfillConfig{
		RepoID:        repo.ID,
		RepoName:      repo.Name,
		RepoPath:      repoPath,
		Model:         "test-model",
		Concurrency:   1,
		Pool:          pool,
		LLM:           mockLLM,
		ContextWindow: 8192,
	})
	if err != nil {
		t.Fatalf("RunBackfill: %v", err)
	}
	if backfillStats.OrphanEntities == 0 || backfillStats.FactsCreated.Load() == 0 {
		t.Fatalf("unexpected backfill stats: %+v", backfillStats)
	}

	rels, err := relStore.ListByRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("ListByRepo relationships: %v", err)
	}
	if len(rels) == 0 {
		t.Fatalf("expected at least one relationship after backfill auto-owns")
	}

	facts, err := factStore.ListByEntity(ctx, parent.ID)
	if err != nil || len(facts) == 0 {
		t.Fatalf("expected facts on parent after backfill; len=%d err=%v", len(facts), err)
	}

	if err := exec.CommandContext(ctx, "git", "-C", repoPath, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoPath, "config", "user.email", "test@example.com").Run(); err != nil {
		t.Fatalf("git config user.email: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoPath, "config", "user.name", "AtlasKB Test").Run(); err != nil {
		t.Fatalf("git config user.name: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoPath, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoPath, "commit", "-m", "initial pipeline file").Run(); err != nil {
		t.Fatalf("git commit #1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, relPath), []byte("package svc\n\nfunc Changed() {}\n"), 0o644); err != nil {
		t.Fatalf("write second revision: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoPath, "add", ".").Run(); err != nil {
		t.Fatalf("git add #2: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoPath, "commit", "-m", "add changed function").Run(); err != nil {
		t.Fatalf("git commit #2: %v", err)
	}

	if err := RunGitLogAnalysis(ctx, GitLogConfig{
		RepoID:      repo.ID,
		RepoName:    repo.Name,
		RepoPath:    repoPath,
		Model:       "test-model",
		Pool:        pool,
		LLM:         mockLLM,
		GitLogLimit: 20,
	}); err != nil {
		t.Fatalf("RunGitLogAnalysis: %v", err)
	}

	decisions, err := decisionStore.ListByRepo(ctx, repo.ID)
	if err != nil || len(decisions) == 0 {
		t.Fatalf("expected gitlog decisions; len=%d err=%v", len(decisions), err)
	}
	counts, err := jobStore.CountByStatus(ctx, repo.ID, models.PhaseGitLog)
	if err != nil {
		t.Fatalf("CountByStatus gitlog: %v", err)
	}
	if counts[models.JobCompleted] == 0 {
		t.Fatalf("expected completed gitlog job, counts=%v", counts)
	}
}

func TestIntegrationRunPhase17WithRoster(t *testing.T) {
	pool, ctx := newIntegrationPipelinePool(t)
	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}

	repoPath := t.TempDir()
	relPath := "internal/svc/tree.go"
	absPath := filepath.Join(repoPath, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	code := `package svc

type Base struct{}

type Child struct{
	Base
}

func Helper() {}

func (c *Child) Run() {
	Helper()
}
`
	if err := os.WriteFile(absPath, []byte(code), 0o644); err != nil {
		t.Fatalf("write tree.go: %v", err)
	}

	repo := &models.Repo{Name: "pipeline-ts", LocalPath: repoPath, DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("Repo Create: %v", err)
	}

	entities := []*models.Entity{
		{RepoID: repo.ID, Kind: models.EntityType, Name: "Base", QualifiedName: "svc::Base", Path: models.Ptr(relPath)},
		{RepoID: repo.ID, Kind: models.EntityType, Name: "Child", QualifiedName: "svc::Child", Path: models.Ptr(relPath)},
		{RepoID: repo.ID, Kind: models.EntityFunction, Name: "Helper", QualifiedName: "svc::Helper", Path: models.Ptr(relPath)},
		{RepoID: repo.ID, Kind: models.EntityFunction, Name: "Run", QualifiedName: "svc::Child.Run", Path: models.Ptr(relPath)},
	}
	for _, e := range entities {
		if err := entityStore.Create(ctx, e); err != nil {
			t.Fatalf("Create entity %s: %v", e.QualifiedName, err)
		}
	}

	manifest, err := RunPhase1(repoPath, nil)
	if err != nil {
		t.Fatalf("RunPhase1: %v", err)
	}
	roster := []EntityEntry{
		{Name: "Base", QualifiedName: "svc::Base", Kind: "type", Path: relPath, Line: 3},
		{Name: "Child", QualifiedName: "svc::Child", Kind: "type", Path: relPath, Line: 5},
		{Name: "Helper", QualifiedName: "svc::Helper", Kind: "function", Path: relPath, Line: 9},
		{Name: "Run", QualifiedName: "svc::Child.Run", Kind: "function", Path: relPath, Line: 11},
	}

	stats, err := RunPhase17(ctx, Phase17Config{
		RepoID:   repo.ID,
		RepoName: repo.Name,
		RepoPath: repoPath,
		Manifest: manifest,
		Roster:   roster,
		Pool:     pool,
	})
	if err != nil {
		t.Fatalf("RunPhase17: %v", err)
	}
	if stats.FilesProcessed == 0 {
		t.Fatalf("phase1.7 should process at least one file")
	}
	rels, err := relStore.ListByRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("ListByRepo rels: %v", err)
	}
	if len(rels) == 0 {
		t.Fatalf("phase1.7 should produce at least one relationship")
	}
}

func TestIntegrationCoverageHelpers(t *testing.T) {
	if got := buildFlowLabel([]string{"A", "B", "C"}); !strings.Contains(got, "A") {
		t.Fatalf("buildFlowLabel unexpected: %q", got)
	}
	if confidenceToStrength("high") != models.StrengthStrong {
		t.Fatalf("confidenceToStrength(high) mismatch")
	}
	if confidenceToStrength("unknown") != models.StrengthModerate {
		t.Fatalf("confidenceToStrength(default) mismatch")
	}
	if got := fmt.Sprintf("%v", shouldRunPhase([]string{"phase2", "phase4"}, "phase4")); got != "true" {
		t.Fatalf("shouldRunPhase expected true")
	}
}
