package query

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/db"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func newIntegrationQueryPool(t *testing.T) (*pgxpool.Pool, context.Context) {
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

func queryVec(seed float32) pgvector.Vector {
	return pgvector.NewVector(queryVecSlice(seed))
}

func queryVecSlice(seed float32) []float32 {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = seed + float32(i%5)*0.0001
	}
	return v
}

func TestIntegrationSearchAndTriplets(t *testing.T) {
	pool, ctx := newIntegrationQueryPool(t)

	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	feedbackStore := &models.FactFeedbackStore{Pool: pool}

	repo := &models.Repo{Name: "query-integration", LocalPath: "/tmp/query-integration", DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("Repo Create: %v", err)
	}

	service := &models.Entity{
		RepoID:        repo.ID,
		Kind:          models.EntityFunction,
		Name:          "Service",
		QualifiedName: "svc::Service",
		Path:          models.Ptr("internal/svc/service.go"),
		Summary:       models.Ptr("Orchestrates request and validation lifecycle"),
	}
	helper := &models.Entity{
		RepoID:        repo.ID,
		Kind:          models.EntityFunction,
		Name:          "Helper",
		QualifiedName: "svc::Helper",
		Path:          models.Ptr("internal/svc/service.go"),
		Summary:       models.Ptr("Transforms payload before validation"),
	}
	validator := &models.Entity{
		RepoID:        repo.ID,
		Kind:          models.EntityFunction,
		Name:          "Validator",
		QualifiedName: "svc::Validator",
		Path:          models.Ptr("internal/svc/validator.go"),
		Summary:       models.Ptr("Rejects invalid requests"),
	}
	for _, e := range []*models.Entity{service, helper, validator} {
		if err := entityStore.Create(ctx, e); err != nil {
			t.Fatalf("Entity Create(%s): %v", e.Name, err)
		}
	}

	for _, rel := range []*models.Relationship{
		{
			RepoID:       repo.ID,
			FromEntityID: service.ID,
			ToEntityID:   helper.ID,
			Kind:         models.RelCalls,
			Description:  models.Ptr("Service calls helper"),
			Strength:     models.StrengthStrong,
			Confidence:   0.95,
			Provenance:   []models.Provenance{{SourceType: "file", Repo: repo.Name, Ref: "internal/svc/service.go", AnalyzedAt: time.Now().Format(time.RFC3339)}},
		},
		{
			RepoID:       repo.ID,
			FromEntityID: helper.ID,
			ToEntityID:   validator.ID,
			Kind:         models.RelDependsOn,
			Description:  models.Ptr("Helper relies on validator"),
			Strength:     models.StrengthModerate,
			Confidence:   0.85,
			Provenance:   []models.Provenance{{SourceType: "file", Repo: repo.Name, Ref: "internal/svc/service.go", AnalyzedAt: time.Now().Format(time.RFC3339)}},
		},
	} {
		if err := relStore.Create(ctx, rel); err != nil {
			t.Fatalf("Relationship Create(%s): %v", rel.Kind, err)
		}
	}

	f1 := &models.Fact{
		EntityID:   service.ID,
		RepoID:     repo.ID,
		Claim:      "Service orchestrates helper and validator execution",
		Dimension:  models.DimensionWhat,
		Category:   models.CategoryBehavior,
		Confidence: models.ConfidenceHigh,
		Provenance: []models.Provenance{{SourceType: "file", Repo: repo.Name, Ref: "internal/svc/service.go", AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	f2 := &models.Fact{
		EntityID:   helper.ID,
		RepoID:     repo.ID,
		Claim:      "Helper normalizes payload before validation",
		Dimension:  models.DimensionHow,
		Category:   models.CategoryPattern,
		Confidence: models.ConfidenceMedium,
		Provenance: []models.Provenance{{SourceType: "file", Repo: repo.Name, Ref: "internal/svc/service.go", AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	f3 := &models.Fact{
		EntityID:   validator.ID,
		RepoID:     repo.ID,
		Claim:      "Validator rejects malformed payloads",
		Dimension:  models.DimensionWhy,
		Category:   models.CategoryConstraint,
		Confidence: models.ConfidenceHigh,
		Provenance: []models.Provenance{{SourceType: "file", Repo: repo.Name, Ref: "internal/svc/validator.go", AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	for _, f := range []*models.Fact{f1, f2, f3} {
		if err := factStore.Create(ctx, f); err != nil {
			t.Fatalf("Fact Create(%s): %v", f.Claim, err)
		}
	}
	if err := factStore.UpdateEmbedding(ctx, f1.ID, queryVec(0.11)); err != nil {
		t.Fatalf("UpdateEmbedding f1: %v", err)
	}
	if err := factStore.UpdateEmbedding(ctx, f2.ID, queryVec(0.12)); err != nil {
		t.Fatalf("UpdateEmbedding f2: %v", err)
	}
	if err := factStore.UpdateEmbedding(ctx, f3.ID, queryVec(0.13)); err != nil {
		t.Fatalf("UpdateEmbedding f3: %v", err)
	}

	// Mark one fact as pending feedback to exercise feedback metadata decoration.
	fb := &models.FactFeedback{
		FactID:    f1.ID,
		RepoID:    repo.ID,
		Reason:    "Fact is partially stale",
		Status:    models.FeedbackPending,
		CreatedAt: time.Now(),
	}
	if err := feedbackStore.Create(ctx, fb); err != nil {
		t.Fatalf("Feedback Create: %v", err)
	}

	engine := NewEngine(pool, &embeddings.MockClient{
		EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
			return [][]float32{queryVecSlice(0.11)}, nil
		},
	})
	engine.SetLLM(&llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{
				Model:       model,
				InputTokens: 80,
				OutputTokens: 40,
				Content:     `["service orchestration", "validation flow"]`,
			}, nil
		},
	}, "decompose-model")

	results, err := engine.Search(ctx, "How does service orchestration and validation work?", []uuid.UUID{repo.ID}, 20)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("Search should return results")
	}
	if results[0].RepoName != repo.Name {
		t.Fatalf("expected repo name %q, got %q", repo.Name, results[0].RepoName)
	}
	flagged := 0
	for _, r := range results {
		if r.PendingFeedback > 0 || r.Flagged {
			flagged++
		}
	}
	if flagged == 0 {
		t.Fatalf("expected at least one flagged/pending-feedback result")
	}

	triplets, err := engine.SearchTriplets(ctx, "show validation dependency flow", []uuid.UUID{repo.ID}, TripletSearchOptions{
		SeedLimit:      10,
		TraversalHops:  2,
		MaxTriplets:    10,
		IncludeFacts:   true,
		FactsPerEntity: 3,
	})
	if err != nil {
		t.Fatalf("SearchTriplets: %v", err)
	}
	if len(triplets) == 0 {
		t.Fatalf("SearchTriplets should return triplets")
	}
	if len(triplets[0].SourceFacts) == 0 && len(triplets[0].TargetFacts) == 0 {
		t.Fatalf("triplet facts were expected when IncludeFacts=true")
	}
}
