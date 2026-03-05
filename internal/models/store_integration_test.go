package models

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/db"
)

func newIntegrationModelPool(t *testing.T) (*pgxpool.Pool, context.Context) {
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

func vector1024(seed float32) pgvector.Vector {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = seed + float32(i%7)*0.0001
	}
	return pgvector.NewVector(v)
}

func findEntityByName(t *testing.T, entities []Entity, name string) *Entity {
	t.Helper()
	for i := range entities {
		if entities[i].Name == name {
			return &entities[i]
		}
	}
	t.Fatalf("entity %q not found", name)
	return nil
}

func sortedQueuedTargets(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestIntegrationRepoEntityRelationshipAndFlowStores(t *testing.T) {
	pool, ctx := newIntegrationModelPool(t)

	repoStore := &RepoStore{Pool: pool}
	entityStore := &EntityStore{Pool: pool}
	relStore := &RelationshipStore{Pool: pool}
	flowStore := &FlowStore{Pool: pool}

	repo1 := &Repo{Name: "repo-one", LocalPath: "/tmp/repo-one", DefaultBranch: "main"}
	repo2 := &Repo{Name: "repo-two", LocalPath: "/tmp/repo-two", DefaultBranch: "main", ExcludeDirs: []string{"vendor"}}
	if err := repoStore.Create(ctx, repo1); err != nil {
		t.Fatalf("Create(repo1): %v", err)
	}
	if err := repoStore.Create(ctx, repo2); err != nil {
		t.Fatalf("Create(repo2): %v", err)
	}

	if got, err := repoStore.GetByID(ctx, repo1.ID); err != nil || got == nil || got.Name != repo1.Name {
		t.Fatalf("GetByID: got=%+v err=%v", got, err)
	}
	if got, err := repoStore.GetByName(ctx, repo1.Name); err != nil || got == nil || got.ID != repo1.ID {
		t.Fatalf("GetByName: got=%+v err=%v", got, err)
	}
	if got, err := repoStore.GetByPath(ctx, repo1.LocalPath); err != nil || got == nil || got.ID != repo1.ID {
		t.Fatalf("GetByPath: got=%+v err=%v", got, err)
	}
	repos, err := repoStore.List(ctx)
	if err != nil || len(repos) != 2 {
		t.Fatalf("List repos: len=%d err=%v", len(repos), err)
	}

	repo1.Name = "repo-one-renamed"
	repo1.ExcludeDirs = []string{"dist", "tmp"}
	if err := repoStore.Update(ctx, repo1); err != nil {
		t.Fatalf("Update repo: %v", err)
	}
	if err := repoStore.UpdateLastIndexed(ctx, repo1.ID, "abc123"); err != nil {
		t.Fatalf("UpdateLastIndexed: %v", err)
	}
	if err := repoStore.UpdateOverview(ctx, repo1.ID, "overview text"); err != nil {
		t.Fatalf("UpdateOverview: %v", err)
	}

	e1 := &Entity{
		RepoID:        repo1.ID,
		Kind:          EntityFunction,
		Name:          "UserService",
		QualifiedName: "svc::UserService",
		Path:          Ptr("internal/svc/user_service.go"),
		Summary:       Ptr("Handles user account lifecycle"),
		Capabilities:  []string{"create-user"},
		Assumptions:   []string{"db-connected"},
		StartLine:     Ptr(10),
		EndLine:       Ptr(80),
	}
	e2 := &Entity{
		RepoID:        repo1.ID,
		Kind:          EntityType,
		Name:          "UserRecord",
		QualifiedName: "svc::UserRecord",
		Path:          Ptr("internal/svc/user_types.go"),
		Summary:       Ptr("Data model for user accounts"),
		StartLine:     Ptr(1),
		EndLine:       Ptr(40),
	}
	e3 := &Entity{
		RepoID:        repo1.ID,
		Kind:          EntityFunction,
		Name:          "BillingWorker",
		QualifiedName: "billing::Worker",
		Path:          Ptr("internal/billing/worker.go"),
		Summary:       Ptr("Runs billing cycle"),
	}
	e4 := &Entity{
		RepoID:        repo2.ID,
		Kind:          EntityFunction,
		Name:          "EmailSender",
		QualifiedName: "notify::EmailSender",
		Path:          Ptr("internal/notify/email.go"),
		Summary:       Ptr("Sends notification emails"),
	}
	for _, e := range []*Entity{e1, e2, e3, e4} {
		if err := entityStore.Create(ctx, e); err != nil {
			t.Fatalf("Create entity %s: %v", e.Name, err)
		}
	}

	e1.Summary = Ptr("Updated summary for user lifecycle")
	e1.Capabilities = []string{"create-user", "delete-user"}
	if err := entityStore.Upsert(ctx, e1); err != nil {
		t.Fatalf("Upsert e1: %v", err)
	}
	e2.Signature = Ptr("type UserRecord struct{}")
	e2.TypeRef = Ptr("struct")
	if err := entityStore.Update(ctx, e2); err != nil {
		t.Fatalf("Update e2: %v", err)
	}

	if got, err := entityStore.GetByID(ctx, e1.ID); err != nil || got == nil || got.Summary == nil || !strings.Contains(*got.Summary, "Updated summary") {
		t.Fatalf("GetByID e1: got=%+v err=%v", got, err)
	}
	if got, err := entityStore.GetByIDs(ctx, []uuid.UUID{e1.ID, e2.ID, e3.ID}); err != nil || len(got) != 3 {
		t.Fatalf("GetByIDs: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.FindByNameAndKind(ctx, repo1.ID, "UserService", EntityFunction); err != nil || len(got) != 1 {
		t.Fatalf("FindByNameAndKind: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.FindByName(ctx, repo1.ID, "UserService"); err != nil || len(got) != 1 {
		t.Fatalf("FindByName: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.FindByQualifiedName(ctx, repo1.ID, e1.QualifiedName); err != nil || got == nil || got.ID != e1.ID {
		t.Fatalf("FindByQualifiedName: got=%+v err=%v", got, err)
	}
	if got, err := entityStore.ListByRepo(ctx, repo1.ID); err != nil || len(got) != 3 {
		t.Fatalf("ListByRepo: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.ListByRepoAndKind(ctx, repo1.ID, EntityFunction); err != nil || len(got) != 2 {
		t.Fatalf("ListByRepoAndKind: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.FindByPath(ctx, repo1.ID, "internal/billing/worker.go"); err != nil || got == nil || got.ID != e3.ID {
		t.Fatalf("FindByPath: got=%+v err=%v", got, err)
	}
	if got, err := entityStore.FindByPathSuffix(ctx, repo1.ID, "worker.go"); err != nil || got == nil || got.ID != e3.ID {
		t.Fatalf("FindByPathSuffix: got=%+v err=%v", got, err)
	}
	if got, err := entityStore.ListByPathSuffix(ctx, repo1.ID, "worker.go"); err != nil || len(got) != 1 {
		t.Fatalf("ListByPathSuffix: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.ListByPath(ctx, repo1.ID, "internal/svc/user_service.go"); err != nil || len(got) != 1 {
		t.Fatalf("ListByPath: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.ListDistinctPaths(ctx, repo1.ID); err != nil || len(got) != 3 {
		t.Fatalf("ListDistinctPaths: len=%d err=%v", len(got), err)
	}

	if total, byKind, err := entityStore.CountByRepo(ctx, repo1.ID); err != nil || total != 3 || byKind[EntityFunction] != 2 {
		t.Fatalf("CountByRepo: total=%d byKind=%v err=%v", total, byKind, err)
	}
	if got, err := entityStore.SearchByName(ctx, &repo1.ID, "User", "", 10, 0); err != nil || got.Total < 2 {
		t.Fatalf("SearchByName: got=%+v err=%v", got, err)
	}
	if got, err := entityStore.SearchFuzzy(ctx, "UserServce", &repo1.ID, 0.2, 10); err != nil || len(got) == 0 {
		t.Fatalf("SearchFuzzy: len=%d err=%v", len(got), err)
	}

	if got, err := entityStore.ListOrphans(ctx, repo1.ID); err != nil || len(got) != 3 {
		t.Fatalf("ListOrphans: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.ListWithoutRelationships(ctx, repo1.ID); err != nil || len(got) != 3 {
		t.Fatalf("ListWithoutRelationships: len=%d err=%v", len(got), err)
	}

	rel := &Relationship{
		RepoID:       repo1.ID,
		FromEntityID: e1.ID,
		ToEntityID:   e2.ID,
		Kind:         RelCalls,
		Description:  Ptr("UserService calls UserRecord mapper"),
		Strength:     StrengthStrong,
		Confidence:   0.9,
		Provenance: []Provenance{
			{SourceType: "file", Repo: repo1.Name, Ref: "internal/svc/user_service.go", AnalyzedAt: time.Now().Format(time.RFC3339)},
		},
	}
	if err := relStore.Create(ctx, rel); err != nil {
		t.Fatalf("Create relationship: %v", err)
	}
	rel.Description = Ptr("updated desc")
	rel.Confidence = 0.95
	if err := relStore.Upsert(ctx, rel); err != nil {
		t.Fatalf("Upsert relationship: %v", err)
	}
	if got, err := relStore.ListByEntity(ctx, e1.ID); err != nil || len(got) == 0 {
		t.Fatalf("ListByEntity rels: len=%d err=%v", len(got), err)
	}
	if got, err := relStore.ListByEntityLimited(ctx, e1.ID, 1); err != nil || len(got) != 1 {
		t.Fatalf("ListByEntityLimited rels: len=%d err=%v", len(got), err)
	}
	if got, err := relStore.ListByRepo(ctx, repo1.ID); err != nil || len(got) != 1 {
		t.Fatalf("ListByRepo rels: len=%d err=%v", len(got), err)
	}
	if got, err := relStore.ListDependentsOf(ctx, e2.ID, 10); err != nil || len(got) != 1 {
		t.Fatalf("ListDependentsOf: len=%d err=%v", len(got), err)
	}
	if got, err := relStore.CountByRepo(ctx, repo1.ID); err != nil || got != 1 {
		t.Fatalf("CountByRepo rels: got=%d err=%v", got, err)
	}

	subgraph, err := relStore.TraverseFromEntity(ctx, e1.ID, TraversalOptions{
		MaxHops:        3,
		MaxEntities:    50,
		IncludeFacts:   true,
		FactsPerEntity: 5,
	})
	if err != nil {
		t.Fatalf("TraverseFromEntity: %v", err)
	}
	if len(subgraph.Entities) < 2 || len(subgraph.Relationships) < 1 {
		t.Fatalf("TraverseFromEntity unexpected subgraph: entities=%d rels=%d", len(subgraph.Entities), len(subgraph.Relationships))
	}

	cr := &CrossRepoRelationship{
		FromEntityID: e1.ID,
		ToEntityID:   e4.ID,
		FromRepoID:   repo1.ID,
		ToRepoID:     repo2.ID,
		Kind:         RelDependsOn,
		Description:  Ptr("depends on notifier"),
		Strength:     StrengthModerate,
		Confidence:   0.8,
		Provenance:   []Provenance{{SourceType: "file", Repo: repo1.Name, Ref: "internal/svc/user_service.go", AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	if err := relStore.CreateCrossRepo(ctx, cr); err != nil {
		t.Fatalf("CreateCrossRepo: %v", err)
	}
	cr.Description = Ptr("upserted cross-repo relationship")
	cr.Confidence = 0.9
	if err := relStore.UpsertCrossRepo(ctx, cr); err != nil {
		t.Fatalf("UpsertCrossRepo: %v", err)
	}
	if got, err := relStore.ListAllCrossRepo(ctx); err != nil || len(got) != 1 {
		t.Fatalf("ListAllCrossRepo: len=%d err=%v", len(got), err)
	}
	if got, err := relStore.GetCrossRepoByID(ctx, cr.ID); err != nil || got == nil || got.ID != cr.ID {
		t.Fatalf("GetCrossRepoByID: got=%+v err=%v", got, err)
	}
	if got, err := relStore.ListCrossRepoByEntity(ctx, e1.ID); err != nil || len(got) != 1 {
		t.Fatalf("ListCrossRepoByEntity: len=%d err=%v", len(got), err)
	}
	if got, err := relStore.ListCrossRepoByRepo(ctx, repo2.ID); err != nil || len(got) != 1 {
		t.Fatalf("ListCrossRepoByRepo: len=%d err=%v", len(got), err)
	}

	flow := &ExecutionFlow{
		RepoID:        repo1.ID,
		EntryEntityID: e1.ID,
		Label:         "User Signup",
		StepEntityIDs: []uuid.UUID{e1.ID, e2.ID},
		StepNames:     []string{"UserService", "UserRecord"},
		Depth:         2,
	}
	if err := flowStore.Upsert(ctx, flow); err != nil {
		t.Fatalf("Flow Upsert: %v", err)
	}
	if got, err := flowStore.ListByRepo(ctx, repo1.ID, 10); err != nil || len(got) != 1 {
		t.Fatalf("Flow ListByRepo: len=%d err=%v", len(got), err)
	}
	if got, err := flowStore.FindByEntity(ctx, e2.ID, 10); err != nil || len(got) != 1 {
		t.Fatalf("Flow FindByEntity: len=%d err=%v", len(got), err)
	}
	if err := flowStore.DeleteByRepo(ctx, repo1.ID); err != nil {
		t.Fatalf("Flow DeleteByRepo: %v", err)
	}
	if got, err := flowStore.ListByRepo(ctx, repo1.ID, 10); err != nil || len(got) != 0 {
		t.Fatalf("Flow ListByRepo after delete: len=%d err=%v", len(got), err)
	}

	if err := entityStore.UpdateSummaryEmbedding(ctx, e1.ID, vector1024(0.10)); err != nil {
		t.Fatalf("UpdateSummaryEmbedding e1: %v", err)
	}
	if err := entityStore.UpdateSummaryEmbedding(ctx, e2.ID, vector1024(0.20)); err != nil {
		t.Fatalf("UpdateSummaryEmbedding e2: %v", err)
	}
	if got, err := entityStore.SearchBySummaryVector(ctx, vector1024(0.10), []uuid.UUID{repo1.ID}, 5); err != nil || len(got) == 0 {
		t.Fatalf("SearchBySummaryVector: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.MaxSummarySimilarity(ctx, vector1024(0.10), []uuid.UUID{e1.ID, e2.ID}); err != nil || len(got) == 0 {
		t.Fatalf("MaxSummarySimilarity: len=%d err=%v", len(got), err)
	}
	if got, err := entityStore.ListByRepoWithoutSummaryEmbedding(ctx, repo1.ID); err != nil || len(got) != 1 {
		t.Fatalf("ListByRepoWithoutSummaryEmbedding: len=%d err=%v", len(got), err)
	}

	if err := entityStore.DeleteByPath(ctx, repo1.ID, "internal/billing/worker.go"); err != nil {
		t.Fatalf("DeleteByPath: %v", err)
	}
	if got, err := entityStore.FindByPath(ctx, repo1.ID, "internal/billing/worker.go"); err != nil || got != nil {
		t.Fatalf("FindByPath deleted path: got=%+v err=%v", got, err)
	}
	if err := entityStore.Delete(ctx, e2.ID); err != nil {
		t.Fatalf("Delete entity: %v", err)
	}
	if got, err := entityStore.GetByID(ctx, e2.ID); err != nil || got != nil {
		t.Fatalf("GetByID after delete: got=%+v err=%v", got, err)
	}

	if err := relStore.DeleteCrossRepo(ctx, cr.ID); err != nil {
		t.Fatalf("DeleteCrossRepo: %v", err)
	}
	if got, err := relStore.ListAllCrossRepo(ctx); err != nil || len(got) != 0 {
		t.Fatalf("ListAllCrossRepo after delete: len=%d err=%v", len(got), err)
	}

	if err := relStore.DeleteByRepo(ctx, repo1.ID); err != nil {
		t.Fatalf("DeleteByRepo rels: %v", err)
	}
	if err := entityStore.DeleteByRepo(ctx, repo2.ID); err != nil {
		t.Fatalf("DeleteByRepo entities: %v", err)
	}
	if err := repoStore.Delete(ctx, repo2.ID); err != nil {
		t.Fatalf("Delete repo2: %v", err)
	}
}

func TestIntegrationFactDecisionAndFeedbackStores(t *testing.T) {
	pool, ctx := newIntegrationModelPool(t)

	repoStore := &RepoStore{Pool: pool}
	entityStore := &EntityStore{Pool: pool}
	factStore := &FactStore{Pool: pool}
	decisionStore := &DecisionStore{Pool: pool}
	feedbackStore := &FactFeedbackStore{Pool: pool}
	jobStore := &JobStore{Pool: pool}

	repo := &Repo{Name: "repo-facts", LocalPath: "/tmp/repo-facts", DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("Create repo: %v", err)
	}

	e1 := &Entity{
		RepoID:        repo.ID,
		Kind:          EntityFunction,
		Name:          "TokenIssuer",
		QualifiedName: "auth::TokenIssuer",
		Path:          Ptr("internal/auth/token.go"),
		Summary:       Ptr("Issues auth tokens"),
	}
	e2 := &Entity{
		RepoID:        repo.ID,
		Kind:          EntityFunction,
		Name:          "TokenValidator",
		QualifiedName: "auth::TokenValidator",
		Path:          Ptr("internal/auth/validator.go"),
		Summary:       Ptr("Validates auth tokens"),
	}
	for _, e := range []*Entity{e1, e2} {
		if err := entityStore.Create(ctx, e); err != nil {
			t.Fatalf("Create entity %s: %v", e.Name, err)
		}
	}

	f1 := &Fact{
		EntityID:   e1.ID,
		RepoID:     repo.ID,
		Claim:      "TokenIssuer creates JWT access tokens",
		Dimension:  DimensionWhat,
		Category:   CategoryBehavior,
		Confidence: ConfidenceHigh,
		Provenance: []Provenance{
			{SourceType: "file", Repo: repo.Name, Ref: "internal/auth/token.go", AnalyzedAt: time.Now().Format(time.RFC3339)},
			{SourceType: "file", Repo: repo.Name, Ref: "internal/auth/validator.go", AnalyzedAt: time.Now().Format(time.RFC3339)},
			{SourceType: "file", Repo: repo.Name, Ref: "internal/auth/policy.go", AnalyzedAt: time.Now().Format(time.RFC3339)},
			{SourceType: "file", Repo: repo.Name, Ref: "internal/auth/token.go", AnalyzedAt: time.Now().Format(time.RFC3339)},
		},
	}
	f2 := &Fact{
		EntityID:   e2.ID,
		RepoID:     repo.ID,
		Claim:      "TokenValidator rejects expired tokens",
		Dimension:  DimensionHow,
		Category:   CategoryPattern,
		Confidence: ConfidenceMedium,
		Provenance: []Provenance{
			{SourceType: "file", Repo: repo.Name, Ref: "internal/auth/validator.go", AnalyzedAt: time.Now().Format(time.RFC3339)},
		},
	}
	f3 := &Fact{
		EntityID:   e1.ID,
		RepoID:     repo.ID,
		Claim:      "TokenIssuer performs signature rotation checks",
		Dimension:  DimensionWhy,
		Category:   CategoryPattern,
		Confidence: ConfidenceHigh,
		Provenance: []Provenance{
			{SourceType: "file", Repo: repo.Name, Ref: "internal/auth/policy.go", AnalyzedAt: time.Now().Format(time.RFC3339)},
		},
	}
	for _, f := range []*Fact{f1, f2, f3} {
		if err := factStore.Create(ctx, f); err != nil {
			t.Fatalf("Create fact %q: %v", f.Claim, err)
		}
	}

	if err := factStore.UpdateEmbedding(ctx, f1.ID, vector1024(0.11)); err != nil {
		t.Fatalf("UpdateEmbedding f1: %v", err)
	}
	if err := factStore.UpdateEmbedding(ctx, f2.ID, vector1024(0.22)); err != nil {
		t.Fatalf("UpdateEmbedding f2: %v", err)
	}
	if err := factStore.UpdateConfidence(ctx, f3.ID, ConfidenceLow); err != nil {
		t.Fatalf("UpdateConfidence f3: %v", err)
	}

	if got, err := factStore.GetByID(ctx, f1.ID); err != nil || got == nil || got.ID != f1.ID {
		t.Fatalf("GetByID fact: got=%+v err=%v", got, err)
	}
	if got, err := factStore.ListByEntity(ctx, e1.ID); err != nil || len(got) != 2 {
		t.Fatalf("ListByEntity facts: len=%d err=%v", len(got), err)
	}
	if got, err := factStore.ListByEntityLimited(ctx, e1.ID, 1); err != nil || len(got) != 1 {
		t.Fatalf("ListByEntityLimited facts: len=%d err=%v", len(got), err)
	}
	if got, err := factStore.ListByRepoWithoutEmbedding(ctx, repo.ID); err != nil || len(got) != 1 {
		t.Fatalf("ListByRepoWithoutEmbedding: len=%d err=%v", len(got), err)
	}
	if got, err := factStore.SearchByVector(ctx, vector1024(0.11), []uuid.UUID{repo.ID}, 5); err != nil || len(got) == 0 {
		t.Fatalf("SearchByVector: len=%d err=%v", len(got), err)
	}
	if got, err := factStore.SearchByVectorForEntity(ctx, vector1024(0.11), e1.ID, 5); err != nil || len(got) == 0 {
		t.Fatalf("SearchByVectorForEntity: len=%d err=%v", len(got), err)
	}
	if got, err := factStore.SearchByKeyword(ctx, "tokens", []uuid.UUID{repo.ID}, 5); err != nil || len(got) == 0 {
		t.Fatalf("SearchByKeyword: len=%d err=%v", len(got), err)
	}
	if got, err := factStore.SearchByFTSRanked(ctx, "expired tokens", []uuid.UUID{repo.ID}, 5); err != nil || len(got) == 0 {
		t.Fatalf("SearchByFTSRanked: len=%d err=%v", len(got), err)
	}
	if got, err := factStore.MaxSimilarityByEntity(ctx, vector1024(0.11), []uuid.UUID{e1.ID, e2.ID}); err != nil || len(got) == 0 {
		t.Fatalf("MaxSimilarityByEntity: len=%d err=%v", len(got), err)
	}

	if err := factStore.SetSupersededBy(ctx, f2.ID, f1.ID); err != nil {
		t.Fatalf("SetSupersededBy: %v", err)
	}
	if total, byDim, err := factStore.CountByRepo(ctx, repo.ID); err != nil || total != 2 || byDim[DimensionWhat] != 1 {
		t.Fatalf("CountByRepo facts: total=%d byDim=%v err=%v", total, byDim, err)
	}
	if got, err := factStore.ListByRepoAndCategory(ctx, repo.ID, []string{CategoryBehavior, CategoryPattern}, 10); err != nil || len(got) != 1 {
		t.Fatalf("ListByRepoAndCategory: len=%d err=%v", len(got), err)
	}
	if got, err := factStore.ListByRepoAndCategoryAllRepos(ctx, []string{CategoryBehavior, CategoryPattern}, 10); err != nil || len(got) != 1 {
		t.Fatalf("ListByRepoAndCategoryAllRepos: len=%d err=%v", len(got), err)
	}

	madeAt := time.Now().UTC()
	d1 := &Decision{
		RepoID:       repo.ID,
		Summary:      "Use JWT tokens",
		Description:  "Adopt JWT token auth",
		Rationale:    "Stateless auth",
		Alternatives: []Alternative{{Description: "Opaque tokens", RejectedBecause: "needs session store"}},
		Tradeoffs:    []string{"revocation complexity"},
		Provenance:   []Provenance{{SourceType: "doc", Repo: repo.Name, Ref: "docs/auth.md", AnalyzedAt: time.Now().Format(time.RFC3339)}},
		MadeAt:       &madeAt,
		StillValid:   true,
	}
	if err := decisionStore.Create(ctx, d1); err != nil {
		t.Fatalf("Decision Create: %v", err)
	}
	if err := decisionStore.LinkEntities(ctx, d1.ID, []uuid.UUID{e1.ID, e2.ID}); err != nil {
		t.Fatalf("Decision LinkEntities: %v", err)
	}
	if got, err := decisionStore.GetByID(ctx, d1.ID); err != nil || got == nil || got.ID != d1.ID {
		t.Fatalf("Decision GetByID: got=%+v err=%v", got, err)
	}
	if got, err := decisionStore.ListByEntity(ctx, e1.ID, 10); err != nil || len(got) != 1 {
		t.Fatalf("Decision ListByEntity: len=%d err=%v", len(got), err)
	}
	if got, err := decisionStore.CountByRepo(ctx, repo.ID); err != nil || got != 1 {
		t.Fatalf("Decision CountByRepo: got=%d err=%v", got, err)
	}
	if got, err := decisionStore.ListByRepo(ctx, repo.ID); err != nil || len(got) != 1 {
		t.Fatalf("Decision ListByRepo: len=%d err=%v", len(got), err)
	}

	job1 := &ExtractionJob{RepoID: repo.ID, Phase: PhasePhase2, Target: "internal/auth/token.go", Status: JobPending}
	job2 := &ExtractionJob{RepoID: repo.ID, Phase: PhasePhase2, Target: "internal/auth/validator.go", Status: JobPending}
	job3 := &ExtractionJob{RepoID: repo.ID, Phase: PhasePhase2, Target: "internal/auth/policy.go", Status: JobPending}
	for _, j := range []*ExtractionJob{job1, job2, job3} {
		if err := jobStore.Create(ctx, j); err != nil {
			t.Fatalf("Job Create %s: %v", j.Target, err)
		}
	}
	claimed, err := jobStore.ClaimNext(ctx, repo.ID, PhasePhase2)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNext: claimed=%+v err=%v", claimed, err)
	}
	if err := jobStore.CompleteWithDetails(ctx, claimed.ID, 1200, 0.42, "test-model", 2); err != nil {
		t.Fatalf("CompleteWithDetails: %v", err)
	}
	if err := jobStore.Complete(ctx, job2.ID, 900, 0.10); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := jobStore.Fail(ctx, job3.ID, "parse failure"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if counts, err := jobStore.CountByStatus(ctx, repo.ID, PhasePhase2); err != nil || counts[JobCompleted] < 2 || counts[JobFailed] != 1 {
		t.Fatalf("CountByStatus: counts=%v err=%v", counts, err)
	}
	if failed, err := jobStore.ListFailed(ctx, repo.ID); err != nil || len(failed) != 1 {
		t.Fatalf("ListFailed: len=%d err=%v", len(failed), err)
	}
	if got, err := jobStore.GetByTarget(ctx, repo.ID, PhasePhase2, "internal/auth/policy.go"); err != nil || got == nil {
		t.Fatalf("GetByTarget: got=%+v err=%v", got, err)
	}

	outcome := "accepted"
	fb1 := &FactFeedback{FactID: f1.ID, RepoID: repo.ID, Reason: "needs stricter claim"}
	if err := feedbackStore.Create(ctx, fb1); err != nil {
		t.Fatalf("Feedback Create: %v", err)
	}
	if listed, err := feedbackStore.List(ctx, &repo.ID, FeedbackPending, 0); err != nil || len(listed) == 0 {
		t.Fatalf("Feedback List: len=%d err=%v", len(listed), err)
	}
	if pending, err := feedbackStore.ListPendingByRepo(ctx, repo.ID); err != nil || len(pending) == 0 {
		t.Fatalf("ListPendingByRepo: len=%d err=%v", len(pending), err)
	}
	if counts, err := feedbackStore.CountPendingByFactIDs(ctx, []uuid.UUID{f1.ID, f2.ID}); err != nil || counts[f1.ID] == 0 {
		t.Fatalf("CountPendingByFactIDs: counts=%v err=%v", counts, err)
	}
	if err := feedbackStore.Resolve(ctx, fb1.ID, &outcome); err != nil {
		t.Fatalf("Feedback Resolve: %v", err)
	}
	if got, err := feedbackStore.GetByID(ctx, fb1.ID); err != nil || got == nil || got.Status != FeedbackResolved {
		t.Fatalf("Feedback GetByID: got=%+v err=%v", got, err)
	}

	// Existing statuses cover in-progress, completed->pending reset, and already-pending.
	job4 := &ExtractionJob{RepoID: repo.ID, Phase: PhasePhase2, Target: "internal/auth/token.go", Status: JobPending}
	if err := jobStore.Create(ctx, job4); err != nil {
		t.Fatalf("Job Create(job4): %v", err)
	}
	if _, err := jobStore.ClaimNext(ctx, repo.ID, PhasePhase2); err != nil {
		t.Fatalf("ClaimNext(job4): %v", err)
	}
	if err := jobStore.Fail(ctx, job2.ID, "force failed before submit"); err != nil {
		t.Fatalf("Fail(job2): %v", err)
	}
	if resetCount, err := jobStore.ResetFailed(ctx, repo.ID, PhasePhase2); err != nil {
		t.Fatalf("ResetFailed: %v", err)
	} else if resetCount == 0 {
		t.Fatalf("ResetFailed expected rows affected")
	}

	result, err := SubmitFactFeedback(ctx, pool, f1, "incorrect behavior claim", Ptr("TokenIssuer signs only short-lived JWTs"))
	if err != nil {
		t.Fatalf("SubmitFactFeedback: %v", err)
	}
	if len(result.QueuedTargets) == 0 {
		t.Fatalf("SubmitFactFeedback expected queued targets")
	}
	gotQueued := sortedQueuedTargets(result.QueuedTargets)
	if len(gotQueued) != 3 {
		t.Fatalf("SubmitFactFeedback queued targets=%v, want 3 unique queued", gotQueued)
	}

	updatedFact, err := factStore.GetByID(ctx, f1.ID)
	if err != nil || updatedFact == nil || updatedFact.Confidence != ConfidenceLow {
		t.Fatalf("fact confidence not lowered: fact=%+v err=%v", updatedFact, err)
	}

	if err := decisionStore.DeleteByRepo(ctx, repo.ID); err != nil {
		t.Fatalf("Decision DeleteByRepo: %v", err)
	}
	if got, err := decisionStore.ListByRepo(ctx, repo.ID); err != nil || len(got) != 0 {
		t.Fatalf("Decision list after delete: len=%d err=%v", len(got), err)
	}
	if _, err := jobStore.ResetFailed(ctx, repo.ID, ""); err != nil {
		t.Fatalf("ResetFailed(all phases): %v", err)
	}
	if err := jobStore.DeleteByRepo(ctx, repo.ID); err != nil {
		t.Fatalf("Job DeleteByRepo: %v", err)
	}
	if err := factStore.DeleteByRepo(ctx, repo.ID); err != nil {
		t.Fatalf("Fact DeleteByRepo: %v", err)
	}
}

func TestIntegrationIndexingRunStore(t *testing.T) {
	pool, ctx := newIntegrationModelPool(t)

	repoStore := &RepoStore{Pool: pool}
	runStore := &IndexingRunStore{Pool: pool}

	repo := &Repo{Name: "repo-indexing", LocalPath: "/tmp/repo-indexing", DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("Create repo: %v", err)
	}

	run := &IndexingRun{
		RepoID:          repo.ID,
		CommitSHA:       Ptr("deadbeef"),
		Mode:            "full",
		ModelExtraction: Ptr("extract-model"),
		ModelSynthesis:  Ptr("synth-model"),
		Concurrency:     Ptr(4),
		ParseFallbacks:  Ptr(2),
		UnresolvedRefs:  Ptr(1),
	}
	if err := runStore.Create(ctx, run); err != nil {
		t.Fatalf("IndexingRun Create: %v", err)
	}

	run.FilesTotal = Ptr(120)
	run.FilesAnalyzed = Ptr(110)
	run.FilesSkipped = Ptr(10)
	run.EntitiesCreated = Ptr(550)
	run.FactsCreated = Ptr(1400)
	run.RelsCreated = Ptr(700)
	run.DecisionsCreated = Ptr(12)
	run.OrphanEntities = Ptr(5)
	run.BackfillFacts = Ptr(40)
	run.BackfillRels = Ptr(25)
	run.TotalTokens = Ptr(420000)
	run.TotalCostUSD = Ptr(6.75)
	run.QualityOverall = Ptr(86.4)
	run.QualityEntityCov = Ptr(88.0)
	run.QualityFactDensity = Ptr(83.5)
	run.QualityRelConnect = Ptr(84.2)
	run.QualityDimCoverage = Ptr(90.1)
	run.QualityParseRate = Ptr(92.0)
	run.DurationMS = Ptr(int64(32400))
	if err := runStore.Complete(ctx, run); err != nil {
		t.Fatalf("IndexingRun Complete: %v", err)
	}

	latest, err := runStore.GetLatest(ctx, repo.ID)
	if err != nil || latest == nil {
		t.Fatalf("GetLatest: latest=%+v err=%v", latest, err)
	}
	if latest.CompletedAt == nil || latest.FilesAnalyzed == nil || *latest.FilesAnalyzed != 110 {
		t.Fatalf("GetLatest returned incomplete stats: %+v", latest)
	}

	runs, err := runStore.ListByRepo(ctx, repo.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("ListByRepo runs: len=%d err=%v", len(runs), err)
	}
}
