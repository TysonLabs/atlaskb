package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/db"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

type serverSeed struct {
	repo1       *models.Repo
	repo2       *models.Repo
	service     *models.Entity
	helper      *models.Entity
	validator   *models.Entity
	cluster     *models.Entity
	notifier    *models.Entity
	fact1       *models.Fact
	crossRepoID uuid.UUID
	fileRelPath string
}

func newIntegrationServerPool(t *testing.T) (*pgxpool.Pool, context.Context) {
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

func serverVec(seed float32) pgvector.Vector {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = seed + float32(i%3)*0.0001
	}
	return pgvector.NewVector(v)
}

func seedServerData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *serverSeed {
	t.Helper()

	repoStore := &models.RepoStore{Pool: pool}
	entityStore := &models.EntityStore{Pool: pool}
	factStore := &models.FactStore{Pool: pool}
	relStore := &models.RelationshipStore{Pool: pool}
	decisionStore := &models.DecisionStore{Pool: pool}
	flowStore := &models.FlowStore{Pool: pool}
	runStore := &models.IndexingRunStore{Pool: pool}
	feedbackStore := &models.FactFeedbackStore{Pool: pool}

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

	repo1 := &models.Repo{Name: "repo-server-a", LocalPath: repo1Path, DefaultBranch: "main", ExcludeDirs: []string{"vendor"}}
	repo2 := &models.Repo{Name: "repo-server-b", LocalPath: repo2Path, DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repo1); err != nil {
		t.Fatalf("repo1 create: %v", err)
	}
	if err := repoStore.Create(ctx, repo2); err != nil {
		t.Fatalf("repo2 create: %v", err)
	}
	if err := repoStore.UpdateOverview(ctx, repo1.ID, "Primary API orchestration service."); err != nil {
		t.Fatalf("repo overview: %v", err)
	}

	service := &models.Entity{RepoID: repo1.ID, Kind: models.EntityFunction, Name: "Service", QualifiedName: "svc::Service", Path: models.Ptr(fileRelPath), Summary: models.Ptr("Entry orchestration")}
	helper := &models.Entity{RepoID: repo1.ID, Kind: models.EntityFunction, Name: "Helper", QualifiedName: "svc::Helper", Path: models.Ptr(fileRelPath), Summary: models.Ptr("Helper logic")}
	validator := &models.Entity{RepoID: repo1.ID, Kind: models.EntityFunction, Name: "Validator", QualifiedName: "svc::Validator", Path: models.Ptr("internal/svc/validator.go"), Summary: models.Ptr("Validation logic")}
	cluster := &models.Entity{RepoID: repo1.ID, Kind: models.EntityCluster, Name: "svc-core", QualifiedName: "cluster::svc-core", Summary: models.Ptr("Service core cluster")}
	notifier := &models.Entity{RepoID: repo2.ID, Kind: models.EntityFunction, Name: "Notifier", QualifiedName: "notify::Notifier", Path: models.Ptr("internal/notify/notifier.go"), Summary: models.Ptr("Notification sender")}
	for _, e := range []*models.Entity{service, helper, validator, cluster, notifier} {
		if err := entityStore.Create(ctx, e); err != nil {
			t.Fatalf("entity create %s: %v", e.Name, err)
		}
	}

	f1 := &models.Fact{
		EntityID:   service.ID,
		RepoID:     repo1.ID,
		Claim:      "Service orchestrates helper and validator operations",
		Dimension:  models.DimensionWhat,
		Category:   models.CategoryBehavior,
		Confidence: models.ConfidenceHigh,
		Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: fileRelPath, AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	f2 := &models.Fact{
		EntityID:   helper.ID,
		RepoID:     repo1.ID,
		Claim:      "Helper follows convention: validate before transform",
		Dimension:  models.DimensionHow,
		Category:   models.CategoryConvention,
		Confidence: models.ConfidenceMedium,
		Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: fileRelPath, AnalyzedAt: time.Now().Format(time.RFC3339)}},
	}
	f3 := &models.Fact{
		EntityID:   cluster.ID,
		RepoID:     repo1.ID,
		Claim:      "Architecture: layered service with validation and helper modules",
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
	if err := factStore.UpdateEmbedding(ctx, f1.ID, serverVec(0.11)); err != nil {
		t.Fatalf("fact embedding f1: %v", err)
	}
	if err := factStore.UpdateEmbedding(ctx, f2.ID, serverVec(0.12)); err != nil {
		t.Fatalf("fact embedding f2: %v", err)
	}
	if err := factStore.UpdateEmbedding(ctx, f3.ID, serverVec(0.13)); err != nil {
		t.Fatalf("fact embedding f3: %v", err)
	}

	for _, rel := range []*models.Relationship{
		{RepoID: repo1.ID, FromEntityID: service.ID, ToEntityID: helper.ID, Kind: models.RelCalls, Strength: models.StrengthStrong, Confidence: 0.95, Description: models.Ptr("service -> helper"), Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: fileRelPath}}},
		{RepoID: repo1.ID, FromEntityID: helper.ID, ToEntityID: validator.ID, Kind: models.RelDependsOn, Strength: models.StrengthModerate, Confidence: 0.80, Description: models.Ptr("helper -> validator"), Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: fileRelPath}}},
		{RepoID: repo1.ID, FromEntityID: helper.ID, ToEntityID: cluster.ID, Kind: models.RelMemberOf, Strength: models.StrengthStrong, Confidence: 0.90, Description: models.Ptr("helper in cluster"), Provenance: []models.Provenance{{SourceType: "file", Repo: repo1.Name, Ref: "phase6-clustering"}}},
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
		Confidence:   0.82,
		Description:  models.Ptr("service depends on notifier"),
		Provenance:   []models.Provenance{{SourceType: "manual", Repo: repo1.Name, Ref: "integration-test"}},
	}
	if err := relStore.CreateCrossRepo(ctx, cross); err != nil {
		t.Fatalf("cross repo create: %v", err)
	}

	decision := &models.Decision{
		RepoID:      repo1.ID,
		Summary:     "Use helper + validator split",
		Description: "Separate helper and validator responsibilities",
		Rationale:   "Improves testability",
		StillValid:  true,
		Provenance:  []models.Provenance{{SourceType: "doc", Repo: repo1.Name, Ref: "docs/decision.md", AnalyzedAt: time.Now().Format(time.RFC3339)}},
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

	run := &models.IndexingRun{
		RepoID:      repo1.ID,
		Mode:        "full",
		CommitSHA:   models.Ptr("deadbeef"),
		Concurrency: models.Ptr(2),
	}
	if err := runStore.Create(ctx, run); err != nil {
		t.Fatalf("run create: %v", err)
	}
	run.FilesTotal = models.Ptr(10)
	run.FilesAnalyzed = models.Ptr(8)
	run.QualityOverall = models.Ptr(88.5)
	run.DurationMS = models.Ptr(int64(1200))
	if err := runStore.Complete(ctx, run); err != nil {
		t.Fatalf("run complete: %v", err)
	}

	fb := &models.FactFeedback{
		FactID:  f1.ID,
		RepoID:  repo1.ID,
		Reason:  "needs update",
		Status:  models.FeedbackPending,
		Outcome: nil,
	}
	if err := feedbackStore.Create(ctx, fb); err != nil {
		t.Fatalf("feedback create: %v", err)
	}

	return &serverSeed{
		repo1:       repo1,
		repo2:       repo2,
		service:     service,
		helper:      helper,
		validator:   validator,
		cluster:     cluster,
		notifier:    notifier,
		fact1:       f1,
		crossRepoID: cross.ID,
		fileRelPath: fileRelPath,
	}
}

func doHTTP(t *testing.T, s *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body == nil {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestIntegrationHTTPHandlers(t *testing.T) {
	pool, ctx := newIntegrationServerPool(t)
	seed := seedServerData(t, ctx, pool)

	cfg := config.DefaultConfig()
	cfg.Server.ChatsDir = t.TempDir()
	s := New(
		pool,
		&embeddings.MockClient{EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
			v := make([]float32, 1024)
			for i := range v {
				v[i] = 0.11 + float32(i%3)*0.0001
			}
			return [][]float32{v}, nil
		}},
		&llm.MockClient{},
		cfg,
		fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>ok</html>")}},
		"",
	)

	if rec := doHTTP(t, s, http.MethodGet, "/api/health", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/health status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/stats", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/stats status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/stats/recent-runs", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/stats/recent-runs status=%d body=%s", rec.Code, rec.Body.String())
	}

	if rec := doHTTP(t, s, http.MethodGet, "/api/repos", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/repos status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/repos/"+seed.repo1.ID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/repos/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/repos/"+seed.repo1.ID.String()+"/indexing-runs", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/repos/{id}/indexing-runs status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/repos/"+seed.repo1.ID.String()+"/decisions", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/repos/{id}/decisions status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/repos/"+seed.repo1.ID.String()+"/clusters", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/repos/{id}/clusters status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/repos/"+seed.repo1.ID.String()+"/flows", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/repos/{id}/flows status=%d body=%s", rec.Code, rec.Body.String())
	}

	if rec := doHTTP(t, s, http.MethodGet, "/api/entities?repo_id="+seed.repo1.ID.String()+"&q=Service", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/entities status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/entities/"+seed.service.ID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/entities/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/entities/"+seed.service.ID.String()+"/facts", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/entities/{id}/facts status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/entities/"+seed.service.ID.String()+"/relationships", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/entities/{id}/relationships status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/entities/"+seed.service.ID.String()+"/decisions", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/entities/{id}/decisions status=%d body=%s", rec.Code, rec.Body.String())
	}

	if rec := doHTTP(t, s, http.MethodGet, "/api/graph/repo/"+seed.repo1.ID.String()+"?include_cross_repo=true", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/graph/repo/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/graph/entity/"+seed.service.ID.String()+"?depth=2", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/graph/entity/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}
	multiURL := "/api/graph/multi?repo_ids=" + seed.repo1.ID.String() + "," + seed.repo2.ID.String()
	if rec := doHTTP(t, s, http.MethodGet, multiURL, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/graph/multi status=%d body=%s", rec.Code, rec.Body.String())
	}

	if rec := doHTTP(t, s, http.MethodGet, "/api/search?q=service&repo_name="+seed.repo1.Name, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/search status=%d body=%s", rec.Code, rec.Body.String())
	}

	askBody := []byte(`{"question":"How does service orchestration work?","repo_name":"` + seed.repo1.Name + `"}`)
	if rec := doHTTP(t, s, http.MethodPost, "/api/ask", askBody); rec.Code != http.StatusOK {
		t.Fatalf("POST /api/ask status=%d body=%s", rec.Code, rec.Body.String())
	} else if !strings.Contains(rec.Body.String(), "event: facts") {
		t.Fatalf("/api/ask missing facts event: %s", rec.Body.String())
	}

	fileURL := "/api/file?repo_id=" + seed.repo1.ID.String() + "&path=" + seed.fileRelPath
	if rec := doHTTP(t, s, http.MethodGet, fileURL, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/file status=%d body=%s", rec.Code, rec.Body.String())
	}

	if rec := doHTTP(t, s, http.MethodGet, "/api/cross-repo/links?repo_id="+seed.repo1.ID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/cross-repo/links status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/cross-repo/links/"+seed.crossRepoID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/cross-repo/links/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}
	createCross := []byte(`{"from_entity_id":"` + seed.helper.ID.String() + `","to_entity_id":"` + seed.notifier.ID.String() + `","kind":"depends_on","strength":"moderate"}`)
	recCross := doHTTP(t, s, http.MethodPost, "/api/cross-repo/links", createCross)
	if recCross.Code != http.StatusCreated {
		t.Fatalf("POST /api/cross-repo/links status=%d body=%s", recCross.Code, recCross.Body.String())
	}
	var createdCross models.CrossRepoRelationship
	if err := json.Unmarshal(recCross.Body.Bytes(), &createdCross); err != nil {
		t.Fatalf("unmarshal created cross link: %v", err)
	}
	if rec := doHTTP(t, s, http.MethodDelete, "/api/cross-repo/links/"+createdCross.ID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/cross-repo/links/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}

	createFeedback := []byte(`{"fact_id":"` + seed.fact1.ID.String() + `","reason":"needs refresh"}`)
	recFeedback := doHTTP(t, s, http.MethodPost, "/api/feedback", createFeedback)
	if recFeedback.Code != http.StatusCreated {
		t.Fatalf("POST /api/feedback status=%d body=%s", recFeedback.Code, recFeedback.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/feedback?repo_id="+seed.repo1.ID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/feedback status=%d body=%s", rec.Code, rec.Body.String())
	}
	var feedbackResp map[string]any
	if err := json.Unmarshal(recFeedback.Body.Bytes(), &feedbackResp); err != nil {
		t.Fatalf("unmarshal feedback response: %v", err)
	}
	feedbackID, _ := feedbackResp["id"].(string)
	if feedbackID != "" {
		resolveBody := []byte(`{"outcome":"accepted"}`)
		if rec := doHTTP(t, s, http.MethodPost, "/api/feedback/"+feedbackID+"/resolve", resolveBody); rec.Code != http.StatusOK {
			t.Fatalf("POST /api/feedback/{id}/resolve status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	recCreateChat := doHTTP(t, s, http.MethodPost, "/api/chats", nil)
	if recCreateChat.Code != http.StatusCreated {
		t.Fatalf("POST /api/chats status=%d body=%s", recCreateChat.Code, recCreateChat.Body.String())
	}
	var chat ChatSession
	if err := json.Unmarshal(recCreateChat.Body.Bytes(), &chat); err != nil {
		t.Fatalf("unmarshal chat create: %v", err)
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/chats", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/chats status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodGet, "/api/chats/"+chat.ID, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /api/chats/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodPut, "/api/chats/"+chat.ID, []byte(`{"title":"Integration Chat"}`)); rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/chats/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}
	chatMsg := []byte(`{"question":"Explain service flow","repo_id":"` + seed.repo1.ID.String() + `","top_k":10}`)
	if rec := doHTTP(t, s, http.MethodPost, "/api/chats/"+chat.ID+"/messages", chatMsg); rec.Code != http.StatusOK {
		t.Fatalf("POST /api/chats/{id}/messages status=%d body=%s", rec.Code, rec.Body.String())
	} else if !strings.Contains(rec.Body.String(), "event: facts") {
		t.Fatalf("chat message missing facts event: %s", rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodDelete, "/api/chats/"+chat.ID, nil); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/chats/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Exercise repo create/update/delete success paths with a real git path.
	repoCreatePath := t.TempDir()
	if err := exec.CommandContext(ctx, "git", "-C", repoCreatePath, "init").Run(); err != nil {
		t.Fatalf("git init repoCreatePath: %v", err)
	}
	createRepoBody := []byte(`{"name":"http-created","local_path":"` + repoCreatePath + `","exclude_dirs":["tmp"]}`)
	recRepoCreate := doHTTP(t, s, http.MethodPost, "/api/repos", createRepoBody)
	if recRepoCreate.Code != http.StatusCreated {
		t.Fatalf("POST /api/repos status=%d body=%s", recRepoCreate.Code, recRepoCreate.Body.String())
	}
	var createdRepo models.Repo
	if err := json.Unmarshal(recRepoCreate.Body.Bytes(), &createdRepo); err != nil {
		t.Fatalf("unmarshal created repo: %v", err)
	}
	updateBody := []byte(`{"name":"http-created-updated","exclude_dirs":["tmp","build"]}`)
	if rec := doHTTP(t, s, http.MethodPut, "/api/repos/"+createdRepo.ID.String(), updateBody); rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/repos/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doHTTP(t, s, http.MethodDelete, "/api/repos/"+createdRepo.ID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/repos/{id} status=%d body=%s", rec.Code, rec.Body.String())
	}
}
