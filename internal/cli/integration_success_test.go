package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/db"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func newIntegrationCLIPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ATLASKB_TEST_DSN"))
	if dsn == "" {
		t.Skip("integration DB not configured; set ATLASKB_TEST_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(p.Close)
	if err := p.Ping(ctx); err != nil {
		t.Skipf("integration DB unavailable: %v", err)
	}
	lockConn, err := p.Acquire(ctx)
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

	if err := db.ResetSchema(ctx, p); err != nil {
		t.Fatalf("ResetSchema: %v", err)
	}
	if err := db.RunMigrations(dsn); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return p, ctx
}

func TestRunReposStatusRetryLinkAndIndexSuccess(t *testing.T) {
	p, ctx := newIntegrationCLIPool(t)

	oldPool, oldCfg, oldJSON := pool, cfg, jsonOut
	oldRetryPhase := retryPhase
	oldFromRepo, oldToRepo := linkFromRepo, linkToRepo
	oldFromEntity, oldToEntity := linkFromEntity, linkToEntity
	oldKind, oldStrength, oldDesc := linkKind, linkStrength, linkDesc
	oldIndexDryRun, oldIndexForce := indexDryRun, indexForce
	oldIndexConcurrency, oldIndexPhases := indexConcurrency, indexPhases
	oldIndexExcludes := indexExcludes
	t.Cleanup(func() {
		pool, cfg, jsonOut = oldPool, oldCfg, oldJSON
		retryPhase = oldRetryPhase
		linkFromRepo, linkToRepo = oldFromRepo, oldToRepo
		linkFromEntity, linkToEntity = oldFromEntity, oldToEntity
		linkKind, linkStrength, linkDesc = oldKind, oldStrength, oldDesc
		indexDryRun, indexForce = oldIndexDryRun, oldIndexForce
		indexConcurrency, indexPhases = oldIndexConcurrency, oldIndexPhases
		indexExcludes = oldIndexExcludes
	})

	pool = p
	cfg = config.DefaultConfig()
	cfg.Pipeline.Concurrency = 1

	repoStore := &models.RepoStore{Pool: p}
	entityStore := &models.EntityStore{Pool: p}
	jobStore := &models.JobStore{Pool: p}
	runStore := &models.IndexingRunStore{Pool: p}
	relStore := &models.RelationshipStore{Pool: p}

	repo1Path := t.TempDir()
	repo2Path := t.TempDir()
	repo1 := &models.Repo{Name: "cli-repo-a", LocalPath: repo1Path, DefaultBranch: "main"}
	repo2 := &models.Repo{Name: "cli-repo-b", LocalPath: repo2Path, DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repo1); err != nil {
		t.Fatalf("repo1 create: %v", err)
	}
	if err := repoStore.Create(ctx, repo2); err != nil {
		t.Fatalf("repo2 create: %v", err)
	}
	if err := repoStore.UpdateLastIndexed(ctx, repo1.ID, "abc123"); err != nil {
		t.Fatalf("repo1 UpdateLastIndexed: %v", err)
	}
	if err := repoStore.UpdateLastIndexed(ctx, repo2.ID, "def456"); err != nil {
		t.Fatalf("repo2 UpdateLastIndexed: %v", err)
	}

	fromEntity := &models.Entity{
		RepoID:        repo1.ID,
		Kind:          models.EntityFunction,
		Name:          "Service",
		QualifiedName: "svc::Service",
		Path:          models.Ptr("internal/svc/service.go"),
		Summary:       models.Ptr("Service"),
	}
	toEntity := &models.Entity{
		RepoID:        repo2.ID,
		Kind:          models.EntityFunction,
		Name:          "Notifier",
		QualifiedName: "notify::Notifier",
		Path:          models.Ptr("internal/notify/notifier.go"),
		Summary:       models.Ptr("Notifier"),
	}
	if err := entityStore.Create(ctx, fromEntity); err != nil {
		t.Fatalf("fromEntity create: %v", err)
	}
	if err := entityStore.Create(ctx, toEntity); err != nil {
		t.Fatalf("toEntity create: %v", err)
	}

	// Seed failed jobs for retry/status.
	job1 := &models.ExtractionJob{RepoID: repo1.ID, Phase: models.PhasePhase2, Target: "internal/svc/service.go", Status: models.JobPending}
	job2 := &models.ExtractionJob{RepoID: repo1.ID, Phase: models.PhasePhase4, Target: "synthesis", Status: models.JobPending}
	for _, j := range []*models.ExtractionJob{job1, job2} {
		if err := jobStore.Create(ctx, j); err != nil {
			t.Fatalf("job create: %v", err)
		}
		if _, err := jobStore.ClaimNext(ctx, repo1.ID, j.Phase); err != nil {
			t.Fatalf("job claim: %v", err)
		}
		if err := jobStore.Fail(ctx, j.ID, "integration failure"); err != nil {
			t.Fatalf("job fail: %v", err)
		}
	}

	// Seed indexing runs so status can show metrics and trend.
	runOld := &models.IndexingRun{RepoID: repo1.ID, Mode: "full", CommitSHA: models.Ptr("1111111"), Concurrency: models.Ptr(1)}
	runNew := &models.IndexingRun{RepoID: repo1.ID, Mode: "incremental", CommitSHA: models.Ptr("2222222"), Concurrency: models.Ptr(2)}
	if err := runStore.Create(ctx, runOld); err != nil {
		t.Fatalf("runOld create: %v", err)
	}
	runOld.QualityOverall = models.Ptr(70.0)
	runOld.FilesAnalyzed = models.Ptr(4)
	runOld.EntitiesCreated = models.Ptr(20)
	runOld.FactsCreated = models.Ptr(60)
	runOld.RelsCreated = models.Ptr(30)
	runOld.DurationMS = models.Ptr(int64(800))
	if err := runStore.Complete(ctx, runOld); err != nil {
		t.Fatalf("runOld complete: %v", err)
	}
	if err := runStore.Create(ctx, runNew); err != nil {
		t.Fatalf("runNew create: %v", err)
	}
	runNew.QualityOverall = models.Ptr(83.0)
	runNew.FilesAnalyzed = models.Ptr(6)
	runNew.EntitiesCreated = models.Ptr(24)
	runNew.FactsCreated = models.Ptr(72)
	runNew.RelsCreated = models.Ptr(40)
	runNew.DurationMS = models.Ptr(int64(1200))
	if err := runStore.Complete(ctx, runNew); err != nil {
		t.Fatalf("runNew complete: %v", err)
	}

	// repos command (text and json).
	jsonOut = false
	if out, err := captureStdout(func() error { return runRepos(testCmd(), nil) }); err != nil {
		t.Fatalf("runRepos text err: %v", err)
	} else if !strings.Contains(out, repo1.Name) {
		t.Fatalf("runRepos text output missing repo name: %s", out)
	}
	jsonOut = true
	if out, err := captureStdout(func() error { return runRepos(testCmd(), nil) }); err != nil {
		t.Fatalf("runRepos json err: %v", err)
	} else if !strings.Contains(out, `"name": "cli-repo-a"`) {
		t.Fatalf("runRepos json output missing repo name: %s", out)
	}

	// status command (text and json).
	jsonOut = false
	if out, err := captureStdout(func() error { return runStatus(testCmd(), []string{repo1.Name}) }); err != nil {
		t.Fatalf("runStatus text err: %v", err)
	} else if !strings.Contains(out, "Last indexing run") {
		t.Fatalf("runStatus text output missing run info: %s", out)
	}
	jsonOut = true
	if out, err := captureStdout(func() error { return runStatus(testCmd(), []string{repo1.Name}) }); err != nil {
		t.Fatalf("runStatus json err: %v", err)
	} else if !strings.Contains(out, `"name": "cli-repo-a"`) {
		t.Fatalf("runStatus json output missing repo: %s", out)
	}
	jsonOut = false

	// retry command.
	retryPhase = ""
	if out, err := captureStdout(func() error { return runRetry(testCmd(), []string{repo1.Name}) }); err != nil {
		t.Fatalf("runRetry err: %v", err)
	} else if !strings.Contains(out, "Reset") {
		t.Fatalf("runRetry output missing reset summary: %s", out)
	}

	// link command.
	linkFromRepo = repo1.Name
	linkFromEntity = "internal/svc/service.go"
	linkToRepo = repo2.Name
	linkToEntity = "internal/notify/notifier.go"
	linkKind = models.RelDependsOn
	linkStrength = models.StrengthModerate
	linkDesc = "CLI-created cross repo dependency"
	if err := runLink(testCmd(), nil); err != nil {
		t.Fatalf("runLink err: %v", err)
	}
	if rels, err := relStore.ListAllCrossRepo(ctx); err != nil || len(rels) == 0 {
		t.Fatalf("expected cross-repo rel after runLink, len=%d err=%v", len(rels), err)
	}

	// index command in dry-run mode.
	indexDryRun = true
	indexForce = false
	indexConcurrency = 1
	indexPhases = nil
	indexExcludes = nil
	repoForIndex := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoForIndex, "main.go"), []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := exec.CommandContext(ctx, "git", "-C", repoForIndex, "init").Run(); err != nil {
		t.Fatalf("git init repoForIndex: %v", err)
	}
	if out, err := captureStdout(func() error { return runIndex(testCmd(), []string{repoForIndex}) }); err != nil {
		t.Fatalf("runIndex dry-run err: %v", err)
	} else if !strings.Contains(out, "Dry run") {
		t.Fatalf("runIndex output missing dry-run marker: %s", out)
	}
}

func TestRunAskNoResultsSuccessPath(t *testing.T) {
	p, ctx := newIntegrationCLIPool(t)

	oldPool, oldCfg := pool, cfg
	oldAskRepo, oldAskTopK := askRepo, askTopK
	t.Cleanup(func() {
		pool, cfg = oldPool, oldCfg
		askRepo, askTopK = oldAskRepo, oldAskTopK
	})

	pool = p
	cfg = config.DefaultConfig()

	repoStore := &models.RepoStore{Pool: p}
	repo := &models.Repo{Name: "cli-ask-repo", LocalPath: t.TempDir(), DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	askRepo = repo.Name
	askTopK = 10

	emb := make([]string, 1024)
	for i := range emb {
		emb[i] = "0.11"
	}
	embJSON := "[" + strings.Join(emb, ",") + "]"

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/embeddings":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"data":[{"embedding":%s,"index":0}]}`, embJSON)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"content":"[\"service behavior\"]"},"finish_reason":"stop"}],"model":"mock","usage":{"prompt_tokens":8,"completion_tokens":5}}`)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"mock-model"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.Close)

	cfg.LLM.BaseURL = api.URL
	cfg.Embeddings.BaseURL = api.URL
	cfg.Pipeline.ExtractionModel = "mock-model"
	cfg.Pipeline.SynthesisModel = "mock-model"
	cfg.Embeddings.Model = "mock-embed"

	out, err := captureStdout(func() error { return runAsk(testCmd(), []string{"How", "does", "this", "work?"}) })
	if err != nil {
		t.Fatalf("runAsk err: %v", err)
	}
	if !strings.Contains(out, "No relevant knowledge found") {
		t.Fatalf("runAsk output missing no-results text: %s", out)
	}
}
