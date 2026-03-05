package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/db"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type mcpSeed struct {
	repo1       *models.Repo
	repo2       *models.Repo
	service     *models.Entity
	helper      *models.Entity
	validator   *models.Entity
	cluster     *models.Entity
	notifier    *models.Entity
	fact1       *models.Fact
	fileRelPath string
}

func newIntegrationMCPPool(t *testing.T) (*pgxpool.Pool, context.Context) {
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

func mcpVec(seed float32) pgvector.Vector {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = seed + float32(i%4)*0.0001
	}
	return pgvector.NewVector(v)
}

func seedMCPData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *mcpSeed {
	t.Helper()
	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}
	decisionStore := &models.DecisionStore{Pool: pool}
	flowStore := &models.FlowStore{Pool: pool}

	repo1Path := t.TempDir()
	repo2Path := t.TempDir()
	fileRelPath := "internal/svc/service.go"
	fileAbs := filepath.Join(repo1Path, fileRelPath)
	if err := os.MkdirAll(filepath.Dir(fileAbs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(fileAbs, []byte("package svc\n\nfunc Service(){}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	repo1 := &models.Repo{Name: "mcp-repo-a", LocalPath: repo1Path, DefaultBranch: "main"}
	repo2 := &models.Repo{Name: "mcp-repo-b", LocalPath: repo2Path, DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repo1); err != nil {
		t.Fatalf("repo1 create: %v", err)
	}
	if err := repoStore.Create(ctx, repo2); err != nil {
		t.Fatalf("repo2 create: %v", err)
	}
	if err := repoStore.UpdateOverview(ctx, repo1.ID, "MCP integration overview"); err != nil {
		t.Fatalf("repo overview: %v", err)
	}

	service := &models.Entity{RepoID: repo1.ID, Kind: models.EntityFunction, Name: "Service", QualifiedName: "svc::Service", Path: models.Ptr(fileRelPath), Summary: models.Ptr("Service entrypoint")}
	helper := &models.Entity{RepoID: repo1.ID, Kind: models.EntityFunction, Name: "Helper", QualifiedName: "svc::Helper", Path: models.Ptr(fileRelPath), Summary: models.Ptr("Helper component")}
	validator := &models.Entity{RepoID: repo1.ID, Kind: models.EntityFunction, Name: "Validator", QualifiedName: "svc::Validator", Path: models.Ptr("internal/svc/validator.go"), Summary: models.Ptr("Validator component")}
	cluster := &models.Entity{RepoID: repo1.ID, Kind: models.EntityCluster, Name: "svc-core", QualifiedName: "cluster::svc-core", Summary: models.Ptr("Core service cluster")}
	notifier := &models.Entity{RepoID: repo2.ID, Kind: models.EntityFunction, Name: "Notifier", QualifiedName: "notify::Notifier", Path: models.Ptr("internal/notify/notifier.go"), Summary: models.Ptr("Notifier")}
	for _, e := range []*models.Entity{service, helper, validator, cluster, notifier} {
		if err := entityStore.Create(ctx, e); err != nil {
			t.Fatalf("entity create %s: %v", e.Name, err)
		}
	}

	f1 := &models.Fact{
		EntityID:   service.ID,
		RepoID:     repo1.ID,
		Claim:      "Service orchestrates helper and validator execution",
		Dimension:  models.DimensionWhat,
		Category:   models.CategoryBehavior,
		Confidence: models.ConfidenceHigh,
		Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: fileRelPath, AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	f2 := &models.Fact{
		EntityID:   helper.ID,
		RepoID:     repo1.ID,
		Claim:      "Helper conventions require validate-first flow",
		Dimension:  models.DimensionHow,
		Category:   models.CategoryConvention,
		Confidence: models.ConfidenceMedium,
		Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: fileRelPath, AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	f3 := &models.Fact{
		EntityID:   cluster.ID,
		RepoID:     repo1.ID,
		Claim:      "Architecture: service orchestrator with validator dependency",
		Dimension:  models.DimensionWhat,
		Category:   models.CategoryPattern,
		Confidence: models.ConfidenceHigh,
		Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: "phase5-summary", AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	for _, f := range []*models.Fact{f1, f2, f3} {
		if err := factStore.Create(ctx, f); err != nil {
			t.Fatalf("fact create: %v", err)
		}
	}
	if err := factStore.UpdateEmbedding(ctx, f1.ID, mcpVec(0.11)); err != nil {
		t.Fatalf("fact embedding f1: %v", err)
	}
	if err := factStore.UpdateEmbedding(ctx, f2.ID, mcpVec(0.12)); err != nil {
		t.Fatalf("fact embedding f2: %v", err)
	}
	if err := factStore.UpdateEmbedding(ctx, f3.ID, mcpVec(0.13)); err != nil {
		t.Fatalf("fact embedding f3: %v", err)
	}

	for _, rel := range []*models.Relationship{
		{RepoID: repo1.ID, FromEntityID: service.ID, ToEntityID: helper.ID, Kind: models.RelCalls, Strength: models.StrengthStrong, Confidence: 0.95, Description: models.Ptr("service calls helper"), Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: fileRelPath}}},
		{RepoID: repo1.ID, FromEntityID: helper.ID, ToEntityID: validator.ID, Kind: models.RelDependsOn, Strength: models.StrengthModerate, Confidence: 0.8, Description: models.Ptr("helper depends on validator"), Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: fileRelPath}}},
		{RepoID: repo1.ID, FromEntityID: helper.ID, ToEntityID: cluster.ID, Kind: models.RelMemberOf, Strength: models.StrengthStrong, Confidence: 0.9, Description: models.Ptr("helper member of cluster"), Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: "phase6-clustering"}}},
	} {
		if err := relStore.Create(ctx, rel); err != nil {
			t.Fatalf("relationship create: %v", err)
		}
	}

	cross := &models.CrossRepoRelationship{
		FromEntityID: service.ID,
		ToEntityID:   notifier.ID,
		FromRepoID:   repo1.ID,
		ToRepoID:     repo2.ID,
		Kind:         models.RelDependsOn,
		Strength:     models.StrengthModerate,
		Confidence:   0.75,
		Description:  models.Ptr("service uses notifier"),
		Provenance:   []models.Provenance{{SourceType: "manual", Repo: repo1.Name, Ref: "integration"}},
	}
	if err := relStore.CreateCrossRepo(ctx, cross); err != nil {
		t.Fatalf("cross repo create: %v", err)
	}

	decision := &models.Decision{
		RepoID:      repo1.ID,
		Summary:     "Split helper and validator",
		Description: "Maintain clear component boundaries",
		Rationale:   "Improves maintainability",
		StillValid:  true,
		Provenance:  []models.Provenance{{SourceType: "doc", Repo: repo1.Name, Ref: "docs/decisions.md", AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	if err := decisionStore.Create(ctx, decision); err != nil {
		t.Fatalf("decision create: %v", err)
	}
	if err := decisionStore.LinkEntities(ctx, decision.ID, []uuid.UUID{service.ID}); err != nil {
		t.Fatalf("decision link: %v", err)
	}

	flow := &models.ExecutionFlow{
		RepoID:        repo1.ID,
		EntryEntityID: service.ID,
		Label:         "Service -> Helper -> Validator",
		StepEntityIDs: []uuid.UUID{service.ID, helper.ID, validator.ID},
		StepNames:     []string{"Service", "Helper", "Validator"},
		Depth:         2,
	}
	if err := flowStore.Upsert(ctx, flow); err != nil {
		t.Fatalf("flow upsert: %v", err)
	}

	return &mcpSeed{
		repo1:       repo1,
		repo2:       repo2,
		service:     service,
		helper:      helper,
		validator:   validator,
		cluster:     cluster,
		notifier:    notifier,
		fact1:       f1,
		fileRelPath: fileRelPath,
	}
}

func assertMCPResultOK(t *testing.T, name string, res any, callErr error) *gomcp.CallToolResult {
	t.Helper()
	if callErr != nil {
		t.Fatalf("%s: unexpected call error: %v", name, callErr)
	}
	cr, ok := res.(*gomcp.CallToolResult)
	if !ok {
		t.Fatalf("%s: unexpected result type %T", name, res)
	}
	if cr == nil {
		t.Fatalf("%s: nil result", name)
	}
	if cr.IsError {
		txt := ""
		if len(cr.Content) > 0 {
			if tc, ok := cr.Content[0].(*gomcp.TextContent); ok {
				txt = tc.Text
			}
		}
		t.Fatalf("%s: unexpected error result: %s", name, txt)
	}
	return cr
}

func TestIntegrationMCPHandlers(t *testing.T) {
	pool, ctx := newIntegrationMCPPool(t)
	seed := seedMCPData(t, ctx, pool)

	s := &Server{
		pool: pool,
		embedder: &embeddings.MockClient{
			EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
				v := make([]float32, 1024)
				for i := range v {
					v[i] = 0.11 + float32(i%4)*0.0001
				}
				return [][]float32{v}, nil
			},
		},
	}

	if res, _, err := s.handleListRepos(ctx, nil, listReposInput{}); assertMCPResultOK(t, "handleListRepos", res, err) == nil {
		t.Fatal("handleListRepos returned nil")
	}
	if res, _, err := s.handleSearch(ctx, nil, searchInput{Query: "service orchestration", Repo: seed.repo1.Name, Limit: 10}); assertMCPResultOK(t, "handleSearch facts", res, err) == nil {
		t.Fatal("handleSearch facts returned nil")
	}
	if res, _, err := s.handleSearch(ctx, nil, searchInput{Query: "service dependencies", Repo: seed.repo1.Name, Mode: "graph", Limit: 10}); assertMCPResultOK(t, "handleSearch graph", res, err) == nil {
		t.Fatal("handleSearch graph returned nil")
	}
	if res, _, err := s.handleGetConventions(ctx, nil, getConventionsInput{Repo: seed.repo1.Name, MaxResults: 20}); assertMCPResultOK(t, "handleGetConventions repo", res, err) == nil {
		t.Fatal("handleGetConventions repo returned nil")
	}
	if res, _, err := s.handleGetConventions(ctx, nil, getConventionsInput{MaxResults: 20}); assertMCPResultOK(t, "handleGetConventions org", res, err) == nil {
		t.Fatal("handleGetConventions org returned nil")
	}
	if res, _, err := s.handleGetModuleContext(ctx, nil, getModuleContextInput{Repo: seed.repo1.Name, Path: seed.fileRelPath, Depth: "deep", MaxResults: 20}); assertMCPResultOK(t, "handleGetModuleContext", res, err) == nil {
		t.Fatal("handleGetModuleContext returned nil")
	}
	if res, _, err := s.handleGetServiceContract(ctx, nil, getServiceContractInput{Repo: seed.repo1.Name, Path: seed.fileRelPath, MaxResults: 20}); assertMCPResultOK(t, "handleGetServiceContract", res, err) == nil {
		t.Fatal("handleGetServiceContract returned nil")
	}
	if res, _, err := s.handleGetImpactAnalysis(ctx, nil, getImpactAnalysisInput{Repo: seed.repo1.Name, Path: seed.fileRelPath, MaxHops: 2, MaxResults: 30}); assertMCPResultOK(t, "handleGetImpactAnalysis", res, err) == nil {
		t.Fatal("handleGetImpactAnalysis returned nil")
	}
	if res, _, err := s.handleGetDecisionContext(ctx, nil, getDecisionContextInput{Repo: seed.repo1.Name, Path: seed.fileRelPath, MaxResults: 20}); assertMCPResultOK(t, "handleGetDecisionContext", res, err) == nil {
		t.Fatal("handleGetDecisionContext returned nil")
	}
	if res, _, err := s.handleGetTaskContext(ctx, nil, getTaskContextInput{Repo: seed.repo1.Name, Files: []string{seed.fileRelPath}, Depth: "deep", MaxResults: 20}); assertMCPResultOK(t, "handleGetTaskContext", res, err) == nil {
		t.Fatal("handleGetTaskContext returned nil")
	}
	if res, _, err := s.handleGetExecutionFlows(ctx, nil, getExecutionFlowsInput{Repo: seed.repo1.Name, Through: "Service", MaxResults: 20}); assertMCPResultOK(t, "handleGetExecutionFlows through", res, err) == nil {
		t.Fatal("handleGetExecutionFlows through returned nil")
	}
	if res, _, err := s.handleGetExecutionFlows(ctx, nil, getExecutionFlowsInput{Repo: seed.repo1.Name, MaxResults: 20}); assertMCPResultOK(t, "handleGetExecutionFlows", res, err) == nil {
		t.Fatal("handleGetExecutionFlows returned nil")
	}
	if res, _, err := s.handleGetFunctionalClusters(ctx, nil, getFunctionalClustersInput{Repo: seed.repo1.Name, MaxResults: 20}); assertMCPResultOK(t, "handleGetFunctionalClusters", res, err) == nil {
		t.Fatal("handleGetFunctionalClusters returned nil")
	}
	if res, _, err := s.handleGetRepoOverview(ctx, nil, getRepoOverviewInput{Repo: seed.repo1.Name}); assertMCPResultOK(t, "handleGetRepoOverview", res, err) == nil {
		t.Fatal("handleGetRepoOverview returned nil")
	}
	if res, _, err := s.handleSearchEntities(ctx, nil, searchEntitiesInput{Repo: seed.repo1.Name, Query: "Service", Limit: 20}); assertMCPResultOK(t, "handleSearchEntities", res, err) == nil {
		t.Fatal("handleSearchEntities returned nil")
	}
	if res, _, err := s.handleGetEntitySource(ctx, nil, getEntitySourceInput{Repo: seed.repo1.Name, Path: seed.fileRelPath}); assertMCPResultOK(t, "handleGetEntitySource", res, err) == nil {
		t.Fatal("handleGetEntitySource returned nil")
	}
	if res, _, err := s.handleSubmitFactFeedback(ctx, nil, submitFactFeedbackInput{
		FactID:     seed.fact1.ID.String(),
		Reason:     "outdated behavior fact",
		Correction: "Service now delegates validation to middleware",
	}); assertMCPResultOK(t, "handleSubmitFactFeedback", res, err) == nil {
		t.Fatal("handleSubmitFactFeedback returned nil")
	}
}
