package models

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

func newUnreachableModelPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	cfg, err := pgxpool.ParseConfig("postgres://atlaskb:atlaskb@127.0.0.1:1/atlaskb?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.MaxConns = 1

	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func expectErr(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error, got nil", name)
	}
}

func TestRepoStoreErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := &RepoStore{Pool: newUnreachableModelPool(t)}
	repo := &Repo{Name: "repo-a", LocalPath: "/tmp/repo-a", DefaultBranch: "main"}
	id := uuid.New()

	expectErr(t, "Create", store.Create(ctx, repo))
	_, err := store.GetByID(ctx, id)
	expectErr(t, "GetByID", err)
	_, err = store.GetByName(ctx, "repo-a")
	expectErr(t, "GetByName", err)
	_, err = store.GetByPath(ctx, "/tmp/repo-a")
	expectErr(t, "GetByPath", err)
	_, err = store.List(ctx)
	expectErr(t, "List", err)
	expectErr(t, "Update", store.Update(ctx, &Repo{ID: id, Name: "repo-b"}))
	expectErr(t, "UpdateLastIndexed", store.UpdateLastIndexed(ctx, id, "abc123"))
	expectErr(t, "UpdateOverview", store.UpdateOverview(ctx, id, "overview"))
	expectErr(t, "Delete", store.Delete(ctx, id))
}

func TestDecisionStoreErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := &DecisionStore{Pool: newUnreachableModelPool(t)}
	now := time.Now()
	d := &Decision{
		RepoID:       uuid.New(),
		Summary:      "summary",
		Description:  "description",
		Rationale:    "rationale",
		Alternatives: []Alternative{{Description: "alt"}},
		Tradeoffs:    []string{"tradeoff"},
		Provenance:   []Provenance{{SourceType: "file", Repo: "r", Ref: "f.go", AnalyzedAt: now.Format(time.RFC3339)}},
		MadeAt:       &now,
		StillValid:   true,
	}

	expectErr(t, "Create", store.Create(ctx, d))
	expectErr(t, "LinkEntities", store.LinkEntities(ctx, uuid.New(), []uuid.UUID{uuid.New()}))
	_, err := store.GetByID(ctx, uuid.New())
	expectErr(t, "GetByID", err)
	expectErr(t, "DeleteByRepo", store.DeleteByRepo(ctx, uuid.New()))
	_, err = store.ListByEntity(ctx, uuid.New(), 5)
	expectErr(t, "ListByEntity", err)
	_, err = store.CountByRepo(ctx, uuid.New())
	expectErr(t, "CountByRepo", err)
	_, err = store.ListByRepo(ctx, uuid.New())
	expectErr(t, "ListByRepo", err)
}

func TestEntityStoreErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := &EntityStore{Pool: newUnreachableModelPool(t)}

	path := "internal/a.go"
	summary := "entity summary"
	sig := "func DoThing() error"
	typ := "error"
	start, end := 10, 20

	e := &Entity{
		RepoID:        uuid.New(),
		Kind:          EntityFunction,
		Name:          "DoThing",
		QualifiedName: "pkg.DoThing",
		Path:          &path,
		Summary:       &summary,
		Capabilities:  []string{"read"},
		Assumptions:   []string{"db up"},
		Signature:     &sig,
		TypeRef:       &typ,
		StartLine:     &start,
		EndLine:       &end,
	}
	repoID := uuid.New()
	id := uuid.New()
	vec := pgvector.NewVector([]float32{0.1, 0.2, 0.3})

	expectErr(t, "Create", store.Create(ctx, e))
	expectErr(t, "Upsert", store.Upsert(ctx, e))
	_, err := store.FindByNameAndKind(ctx, repoID, "DoThing", EntityFunction)
	expectErr(t, "FindByNameAndKind", err)
	_, err = store.FindByName(ctx, repoID, "DoThing")
	expectErr(t, "FindByName", err)
	expectErr(t, "Update", store.Update(ctx, &Entity{ID: id}))
	_, err = store.GetByID(ctx, id)
	expectErr(t, "GetByID", err)

	got, err := store.GetByIDs(ctx, nil)
	if err != nil {
		t.Fatalf("GetByIDs(empty): %v", err)
	}
	if got != nil {
		t.Fatalf("GetByIDs(empty) expected nil slice, got len=%d", len(got))
	}
	_, err = store.GetByIDs(ctx, []uuid.UUID{id})
	expectErr(t, "GetByIDs(non-empty)", err)

	_, err = store.ListByRepo(ctx, repoID)
	expectErr(t, "ListByRepo", err)
	_, err = store.ListByRepoAndKind(ctx, repoID, EntityFunction)
	expectErr(t, "ListByRepoAndKind", err)
	_, err = store.FindByQualifiedName(ctx, repoID, "pkg.DoThing")
	expectErr(t, "FindByQualifiedName", err)
	_, err = store.ListOrphans(ctx, repoID)
	expectErr(t, "ListOrphans", err)
	_, err = store.ListWithoutRelationships(ctx, repoID)
	expectErr(t, "ListWithoutRelationships", err)
	_, _, err = store.CountByRepo(ctx, repoID)
	expectErr(t, "CountByRepo", err)
	_, err = store.CountWithFacts(ctx, repoID)
	expectErr(t, "CountWithFacts", err)
	_, err = store.CountWithRelationships(ctx, repoID)
	expectErr(t, "CountWithRelationships", err)
	_, err = store.FindByPath(ctx, repoID, path)
	expectErr(t, "FindByPath", err)
	_, err = store.FindByPathSuffix(ctx, repoID, "a.go")
	expectErr(t, "FindByPathSuffix", err)
	_, err = store.ListByPathSuffix(ctx, repoID, "a.go")
	expectErr(t, "ListByPathSuffix", err)
	expectErr(t, "DeleteByRepo", store.DeleteByRepo(ctx, repoID))
	_, err = store.ListDistinctPaths(ctx, repoID)
	expectErr(t, "ListDistinctPaths", err)
	_, err = store.SearchByName(ctx, &repoID, "do", "", 10, 0)
	expectErr(t, "SearchByName", err)
	_, err = store.SearchFuzzy(ctx, "DoThing", &repoID, 0, 0)
	expectErr(t, "SearchFuzzy", err)

	if NormalizeName("Do_Thing-Test") != "dothingtest" {
		t.Fatalf("NormalizeName() unexpected result")
	}

	_, err = store.ListByPath(ctx, repoID, path)
	expectErr(t, "ListByPath", err)
	expectErr(t, "Delete", store.Delete(ctx, id))
	expectErr(t, "UpdateSummaryEmbedding", store.UpdateSummaryEmbedding(ctx, id, vec))
	_, err = store.ListByRepoWithoutSummaryEmbedding(ctx, repoID)
	expectErr(t, "ListByRepoWithoutSummaryEmbedding", err)
	_, err = store.SearchBySummaryVector(ctx, vec, []uuid.UUID{repoID}, 5)
	expectErr(t, "SearchBySummaryVector", err)

	sim, err := store.MaxSummarySimilarity(ctx, vec, nil)
	if err != nil {
		t.Fatalf("MaxSummarySimilarity(empty): %v", err)
	}
	if len(sim) != 0 {
		t.Fatalf("MaxSummarySimilarity(empty) expected empty map, got %d", len(sim))
	}
	_, err = store.MaxSummarySimilarity(ctx, vec, []uuid.UUID{id})
	expectErr(t, "MaxSummarySimilarity(non-empty)", err)

	expectErr(t, "DeleteByPath", store.DeleteByPath(ctx, repoID, path))
}

func TestFactStoreErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := &FactStore{Pool: newUnreachableModelPool(t)}
	repoID := uuid.New()
	entityID := uuid.New()
	factID := uuid.New()
	vec := pgvector.NewVector([]float32{0.1, 0.2, 0.3})

	f := &Fact{
		EntityID:   entityID,
		RepoID:     repoID,
		Claim:      "claim",
		Dimension:  DimensionWhat,
		Category:   CategoryBehavior,
		Confidence: ConfidenceMedium,
		Provenance: []Provenance{{SourceType: "file", Repo: "r", Ref: "a.go", AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}

	expectErr(t, "Create", store.Create(ctx, f))
	expectErr(t, "UpdateEmbedding", store.UpdateEmbedding(ctx, factID, vec))
	expectErr(t, "UpdateConfidence", store.UpdateConfidence(ctx, factID, ConfidenceLow))
	_, err := store.GetByID(ctx, factID)
	expectErr(t, "GetByID", err)
	_, err = store.ListByEntity(ctx, entityID)
	expectErr(t, "ListByEntity", err)
	_, err = store.ListByEntityLimited(ctx, entityID, 5)
	expectErr(t, "ListByEntityLimited", err)
	_, err = store.ListByRepoWithoutEmbedding(ctx, repoID)
	expectErr(t, "ListByRepoWithoutEmbedding", err)
	_, err = store.SearchByVector(ctx, vec, []uuid.UUID{repoID}, 5)
	expectErr(t, "SearchByVector", err)
	_, err = store.SearchByVectorForEntity(ctx, vec, entityID, 5)
	expectErr(t, "SearchByVectorForEntity", err)
	_, err = store.SearchByKeyword(ctx, "claim", []uuid.UUID{repoID}, 5)
	expectErr(t, "SearchByKeyword", err)
	_, err = store.SearchByFTSRanked(ctx, "claim", []uuid.UUID{repoID}, 5)
	expectErr(t, "SearchByFTSRanked", err)
	expectErr(t, "SetSupersededBy", store.SetSupersededBy(ctx, factID, uuid.New()))
	_, _, err = store.CountByRepo(ctx, repoID)
	expectErr(t, "CountByRepo", err)
	_, err = store.ListByRepoAndCategory(ctx, repoID, []string{CategoryPattern}, 5)
	expectErr(t, "ListByRepoAndCategory", err)

	sim, err := store.MaxSimilarityByEntity(ctx, vec, nil)
	if err != nil {
		t.Fatalf("MaxSimilarityByEntity(empty): %v", err)
	}
	if len(sim) != 0 {
		t.Fatalf("MaxSimilarityByEntity(empty) expected empty map, got %d", len(sim))
	}
	_, err = store.MaxSimilarityByEntity(ctx, vec, []uuid.UUID{entityID})
	expectErr(t, "MaxSimilarityByEntity(non-empty)", err)

	_, err = store.ListByRepoAndCategoryAllRepos(ctx, []string{CategoryPattern}, 5)
	expectErr(t, "ListByRepoAndCategoryAllRepos", err)
	expectErr(t, "DeleteByRepo", store.DeleteByRepo(ctx, repoID))
}

func TestFactFeedbackStoreErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool := newUnreachableModelPool(t)
	store := &FactFeedbackStore{Pool: pool}
	repoID := uuid.New()
	factID := uuid.New()
	outcome := "resolved"

	fb := &FactFeedback{
		FactID:     factID,
		RepoID:     repoID,
		Reason:     "incorrect",
		Correction: Ptr("fixed"),
	}

	expectErr(t, "Create", store.Create(ctx, fb))
	expectErr(t, "Resolve", store.Resolve(ctx, uuid.New(), &outcome))
	_, err := store.List(ctx, &repoID, FeedbackPending, 20)
	expectErr(t, "List", err)
	_, err = store.ListPendingByRepo(ctx, repoID)
	expectErr(t, "ListPendingByRepo", err)

	counts, err := store.CountPendingByFactIDs(ctx, nil)
	if err != nil {
		t.Fatalf("CountPendingByFactIDs(empty): %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("CountPendingByFactIDs(empty) expected empty map, got %d", len(counts))
	}
	_, err = store.CountPendingByFactIDs(ctx, []uuid.UUID{factID})
	expectErr(t, "CountPendingByFactIDs(non-empty)", err)

	_, err = store.GetByID(ctx, uuid.New())
	expectErr(t, "GetByID", err)

	_, err = SubmitFactFeedback(ctx, pool, nil, "wrong", nil)
	if err == nil || !strings.Contains(err.Error(), "fact is required") {
		t.Fatalf("SubmitFactFeedback(nil fact) expected validation error, got %v", err)
	}

	f := &Fact{ID: factID, RepoID: repoID}
	_, err = SubmitFactFeedback(ctx, pool, f, "   ", nil)
	if err == nil || !strings.Contains(err.Error(), "reason is required") {
		t.Fatalf("SubmitFactFeedback(empty reason) expected validation error, got %v", err)
	}

	f.Provenance = []Provenance{{Ref: "internal/a.go"}}
	_, err = SubmitFactFeedback(ctx, pool, f, "bad fact", nil)
	expectErr(t, "SubmitFactFeedback(tx begin)", err)
}

func TestFlowStoreErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := &FlowStore{Pool: newUnreachableModelPool(t)}
	repoID := uuid.New()
	entryID := uuid.New()
	flow := &ExecutionFlow{
		RepoID:        repoID,
		EntryEntityID: entryID,
		Label:         "entry path",
		StepEntityIDs: []uuid.UUID{entryID},
		StepNames:     []string{"Entry"},
		Depth:         1,
	}

	expectErr(t, "Upsert", store.Upsert(ctx, flow))
	expectErr(t, "DeleteByRepo", store.DeleteByRepo(ctx, repoID))
	_, err := store.ListByRepo(ctx, repoID, 5)
	expectErr(t, "ListByRepo", err)
	_, err = store.FindByEntity(ctx, entryID, 5)
	expectErr(t, "FindByEntity", err)
}

func TestIndexingRunStoreErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := &IndexingRunStore{Pool: newUnreachableModelPool(t)}
	repoID := uuid.New()
	commit := "abc123"
	run := &IndexingRun{
		RepoID:    repoID,
		Mode:      "full",
		CommitSHA: &commit,
	}

	expectErr(t, "Create", store.Create(ctx, run))
	expectErr(t, "Complete", store.Complete(ctx, &IndexingRun{ID: uuid.New()}))
	_, err := store.GetLatest(ctx, repoID)
	expectErr(t, "GetLatest", err)
	_, err = store.ListByRepo(ctx, repoID)
	expectErr(t, "ListByRepo", err)
}

func TestJobStoreErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := &JobStore{Pool: newUnreachableModelPool(t)}
	repoID := uuid.New()
	jobID := uuid.New()

	job := &ExtractionJob{
		RepoID: repoID,
		Phase:  PhasePhase2,
		Target: "internal/a.go",
		Status: JobPending,
	}

	expectErr(t, "Create", store.Create(ctx, job))
	_, err := store.ClaimNext(ctx, repoID, PhasePhase2)
	expectErr(t, "ClaimNext", err)
	expectErr(t, "Complete", store.Complete(ctx, jobID, 10, 0.1))
	expectErr(t, "CompleteWithDetails", store.CompleteWithDetails(ctx, jobID, 20, 0.2, "model-a", 2))
	expectErr(t, "Fail", store.Fail(ctx, jobID, "boom"))
	_, err = store.ResetFailed(ctx, repoID, "")
	expectErr(t, "ResetFailed", err)
	_, err = store.CountByStatus(ctx, repoID, "")
	expectErr(t, "CountByStatus", err)
	_, err = store.ListFailed(ctx, repoID)
	expectErr(t, "ListFailed", err)
	_, err = store.GetByTarget(ctx, repoID, PhasePhase2, "internal/a.go")
	expectErr(t, "GetByTarget", err)
	expectErr(t, "DeleteByRepo", store.DeleteByRepo(ctx, repoID))
}

func TestRelationshipStoreErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	store := &RelationshipStore{Pool: newUnreachableModelPool(t)}
	repoID := uuid.New()
	fromID := uuid.New()
	toID := uuid.New()
	desc := "depends on"

	rel := &Relationship{
		RepoID:       repoID,
		FromEntityID: fromID,
		ToEntityID:   toID,
		Kind:         RelDependsOn,
		Description:  &desc,
		Strength:     StrengthStrong,
		Confidence:   0.9,
		Provenance:   []Provenance{{SourceType: "file", Repo: "r", Ref: "a.go", AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}

	expectErr(t, "Create", store.Create(ctx, rel))
	expectErr(t, "Upsert", store.Upsert(ctx, rel))
	_, err := store.ListByEntity(ctx, fromID)
	expectErr(t, "ListByEntity", err)
	_, err = store.ListByEntityLimited(ctx, fromID, 5)
	expectErr(t, "ListByEntityLimited", err)
	_, err = store.ListByRepo(ctx, repoID)
	expectErr(t, "ListByRepo", err)
	_, err = store.ListDependentsOf(ctx, toID, 5)
	expectErr(t, "ListDependentsOf", err)
	_, err = store.CountByRepo(ctx, repoID)
	expectErr(t, "CountByRepo", err)
	expectErr(t, "DeleteByRepo", store.DeleteByRepo(ctx, repoID))

	cross := &CrossRepoRelationship{
		FromEntityID: fromID,
		ToEntityID:   toID,
		FromRepoID:   repoID,
		ToRepoID:     uuid.New(),
		Kind:         RelDependsOn,
		Description:  &desc,
		Strength:     StrengthModerate,
		Confidence:   0.7,
		Provenance:   []Provenance{{SourceType: "file", Repo: "r", Ref: "a.go", AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}

	expectErr(t, "CreateCrossRepo", store.CreateCrossRepo(ctx, cross))
	expectErr(t, "UpsertCrossRepo", store.UpsertCrossRepo(ctx, cross))
	_, err = store.ListAllCrossRepo(ctx)
	expectErr(t, "ListAllCrossRepo", err)
	_, err = store.GetCrossRepoByID(ctx, uuid.New())
	expectErr(t, "GetCrossRepoByID", err)
	expectErr(t, "DeleteCrossRepo", store.DeleteCrossRepo(ctx, uuid.New()))
	_, err = store.ListCrossRepoByEntity(ctx, fromID)
	expectErr(t, "ListCrossRepoByEntity", err)
	_, err = store.ListCrossRepoByRepo(ctx, repoID)
	expectErr(t, "ListCrossRepoByRepo", err)

	_, err = store.TraverseFromEntity(ctx, fromID, TraversalOptions{
		MaxHops:        2,
		RelKinds:       []string{RelDependsOn},
		CrossRepo:      true,
		MaxEntities:    20,
		IncludeFacts:   true,
		FactsPerEntity: 3,
		MinConfidence:  0.5,
	})
	expectErr(t, "TraverseFromEntity", err)

	_, err = store.listRelationshipsBetween(ctx, []uuid.UUID{fromID, toID}, TraversalOptions{
		RelKinds:      []string{RelDependsOn},
		CrossRepo:     true,
		MinConfidence: 0.5,
	})
	expectErr(t, "listRelationshipsBetween", err)
}
