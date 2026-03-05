package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/config"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func initGitRepoForServerIntegration(t *testing.T, repoPath string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", repoPath, "init"},
		{"git", "-C", repoPath, "config", "user.email", "test@example.com"},
		{"git", "-C", repoPath, "config", "user.name", "atlas-server-test"},
		{"git", "-C", repoPath, "add", "."},
		{"git", "-C", repoPath, "commit", "-m", "initial"},
	}
	for _, args := range cmds {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			t.Fatalf("%s: %v", strings.Join(args, " "), err)
		}
	}
}

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func parsePostgresDSN(t *testing.T, dsn string) (host string, port int, user, pass, dbName, sslmode string) {
	t.Helper()
	u, err := url.Parse(strings.TrimSpace(dsn))
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	if u.User == nil {
		t.Fatalf("dsn missing user info")
	}
	user = u.User.Username()
	pass, _ = u.User.Password()
	dbName = strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		t.Fatalf("dsn missing dbname")
	}

	host, p, err := net.SplitHostPort(u.Host)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			host = u.Host
			port = 5432
		} else {
			t.Fatalf("split host/port: %v", err)
		}
	} else {
		port, err = strconv.Atoi(p)
		if err != nil {
			t.Fatalf("parse port: %v", err)
		}
	}

	sslmode = u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	return host, port, user, pass, dbName, sslmode
}

func TestIntegrationReindexBatchAndHistoryHandlers(t *testing.T) {
	pool, ctx := newIntegrationServerPool(t)
	seed := seedServerData(t, ctx, pool)
	initGitRepoForServerIntegration(t, seed.repo1.LocalPath)

	indexingMu.Lock()
	indexingJobs = map[uuid.UUID]*indexJob{}
	indexingMu.Unlock()
	batchMu.Lock()
	activeBatch = nil
	batchMu.Unlock()
	t.Cleanup(func() {
		indexingMu.Lock()
		indexingJobs = map[uuid.UUID]*indexJob{}
		indexingMu.Unlock()
		batchMu.Lock()
		activeBatch = nil
		batchMu.Unlock()
	})

	cfg := config.DefaultConfig()
	cfg.Pipeline.Concurrency = 1
	s := New(
		pool,
		&embeddings.MockClient{},
		&llm.MockClient{},
		cfg,
		fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>ok</html>")}},
		"",
	)

	// Conflict path: repo already in active batch.
	batchMu.Lock()
	activeBatch = &batchState{Done: false, Repos: map[uuid.UUID]*batchRepoStatus{seed.repo1.ID: {RepoID: seed.repo1.ID, RepoName: seed.repo1.Name, Status: "running"}}}
	batchMu.Unlock()
	rec := doHTTP(t, s, http.MethodPost, "/api/repos/"+seed.repo1.ID.String()+"/reindex", []byte(`{"force":false}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("reindex conflict for active batch status=%d body=%s", rec.Code, rec.Body.String())
	}
	batchMu.Lock()
	activeBatch = nil
	batchMu.Unlock()

	// Start a focused phase1 reindex run.
	reindexBody := []byte(`{"force":false,"phases":["phase1"],"concurrency":1}`)
	rec = doHTTP(t, s, http.MethodPost, "/api/repos/"+seed.repo1.ID.String()+"/reindex", reindexBody)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start reindex status=%d body=%s", rec.Code, rec.Body.String())
	}

	waitUntil(t, 20*time.Second, func() bool {
		statusRec := doHTTP(t, s, http.MethodGet, "/api/repos/"+seed.repo1.ID.String()+"/reindex/status", nil)
		if statusRec.Code != http.StatusOK {
			return false
		}
		var payload map[string]any
		if err := json.Unmarshal(statusRec.Body.Bytes(), &payload); err != nil {
			return false
		}
		status, _ := payload["status"].(string)
		return status == "completed" || status == "failed"
	})

	statusRec := doHTTP(t, s, http.MethodGet, "/api/repos/"+seed.repo1.ID.String()+"/reindex/status", nil)
	if statusRec.Code != http.StatusOK || !strings.Contains(statusRec.Body.String(), "completed") {
		t.Fatalf("reindex status response=%d body=%s", statusRec.Code, statusRec.Body.String())
	}

	// Batch start for non-git repo should fail per-repo and complete batch state.
	batchReq := []byte(fmt.Sprintf(`{"repo_ids":["%s"],"force":true}`, seed.repo2.ID.String()))
	batchStart := doHTTP(t, s, http.MethodPost, "/api/indexing/batch", batchReq)
	if batchStart.Code != http.StatusAccepted {
		t.Fatalf("batch start status=%d body=%s", batchStart.Code, batchStart.Body.String())
	}
	waitUntil(t, 20*time.Second, func() bool {
		st := doHTTP(t, s, http.MethodGet, "/api/indexing/batch/status", nil)
		if st.Code != http.StatusOK {
			return false
		}
		return strings.Contains(st.Body.String(), `"active":false`)
	})

	st := doHTTP(t, s, http.MethodGet, "/api/indexing/batch/status", nil)
	if st.Code != http.StatusOK || !strings.Contains(st.Body.String(), "failed") {
		t.Fatalf("batch status=%d body=%s", st.Code, st.Body.String())
	}

	jobsRec := doHTTP(t, s, http.MethodGet, "/api/indexing/jobs", nil)
	if jobsRec.Code != http.StatusOK {
		t.Fatalf("indexing jobs status=%d body=%s", jobsRec.Code, jobsRec.Body.String())
	}
	historyRec := doHTTP(t, s, http.MethodGet, "/api/indexing/history", nil)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("indexing history status=%d body=%s", historyRec.Code, historyRec.Body.String())
	}

	// Active cancel path.
	ctxCancel, cancel := context.WithCancel(context.Background())
	batchMu.Lock()
	activeBatch = &batchState{Done: false, cancel: cancel, Repos: map[uuid.UUID]*batchRepoStatus{seed.repo1.ID: {RepoID: seed.repo1.ID, RepoName: seed.repo1.Name, Status: "running"}}, Order: []uuid.UUID{seed.repo1.ID}}
	batchMu.Unlock()
	cancelRec := doHTTP(t, s, http.MethodPost, "/api/indexing/batch/cancel", nil)
	if cancelRec.Code != http.StatusOK || !strings.Contains(cancelRec.Body.String(), "cancelling") {
		t.Fatalf("batch cancel status=%d body=%s", cancelRec.Code, cancelRec.Body.String())
	}
	_ = ctxCancel
}

func TestIntegrationSetupApplyHandlers(t *testing.T) {
	// Acquire integration DB lock/schema setup for deterministic setup-apply DB checks.
	pool, _ := newIntegrationServerPool(t)
	_ = pool

	dsn := strings.TrimSpace(os.Getenv("ATLASKB_TEST_DSN"))
	if dsn == "" {
		t.Skip("integration DB not configured; set ATLASKB_TEST_DSN")
	}
	host, port, user, pass, dbName, sslmode := parsePostgresDSN(t, dsn)

	depOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"mock-model"}]}`))
		case "/v1/embeddings":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer depOK.Close()

	cfgPath := filepath.Join(t.TempDir(), "atlaskb.toml")
	s := &Server{cfg: config.DefaultConfig(), configPath: cfgPath}

	goodPayload := map[string]any{
		"config": map[string]any{
			"database":   map[string]any{"host": host, "port": port, "user": user, "password": pass, "dbname": dbName, "sslmode": sslmode},
			"llm":        map[string]any{"base_url": depOK.URL, "api_key": ""},
			"embeddings": map[string]any{"base_url": depOK.URL, "model": "mock-embed", "api_key": ""},
			"pipeline":   map[string]any{"concurrency": 1, "extraction_model": "mock-model", "synthesis_model": "mock-model", "context_window": 8192, "git_log_limit": 100},
			"server":     map[string]any{"port": 3000, "chats_dir": ""},
			"github":     map[string]any{"token": "", "api_url": "https://api.github.com/graphql", "max_prs": 10, "pr_batch_size": 5, "enterprise_host": ""},
		},
	}
	body, _ := json.Marshal(goodPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/setup/apply", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleSetupApply(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup apply success status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config file to be saved at %s: %v", cfgPath, err)
	}
	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if loaded.LLM.BaseURL != depOK.URL || loaded.Embeddings.BaseURL != depOK.URL {
		t.Fatalf("saved setup config mismatch: llm=%s emb=%s", loaded.LLM.BaseURL, loaded.Embeddings.BaseURL)
	}

	depFailEmb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"mock-model"}]}`))
		case "/v1/embeddings":
			http.Error(w, "down", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer depFailEmb.Close()

	badPayload := map[string]any{
		"config": map[string]any{
			"database":   map[string]any{"host": host, "port": port, "user": user, "password": pass, "dbname": dbName, "sslmode": sslmode},
			"llm":        map[string]any{"base_url": depFailEmb.URL},
			"embeddings": map[string]any{"base_url": depFailEmb.URL, "model": "mock-embed"},
			"pipeline":   map[string]any{"concurrency": 1, "extraction_model": "mock-model", "synthesis_model": "mock-model"},
			"server":     map[string]any{"port": 3000},
			"github":     map[string]any{"api_url": "https://api.github.com/graphql", "max_prs": 1, "pr_batch_size": 1},
		},
	}
	badBody, _ := json.Marshal(badPayload)
	badReq := httptest.NewRequest(http.MethodPost, "/api/setup/apply", bytes.NewReader(badBody))
	badRec := httptest.NewRecorder()
	s.handleSetupApply(badRec, badReq)
	if badRec.Code != http.StatusBadRequest || !strings.Contains(badRec.Body.String(), "embeddings endpoint check failed") {
		t.Fatalf("setup apply expected embeddings failure, status=%d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestIntegrationSetupApplyAdditionalFailurePaths(t *testing.T) {
	pool, _ := newIntegrationServerPool(t)
	_ = pool

	dsn := strings.TrimSpace(os.Getenv("ATLASKB_TEST_DSN"))
	if dsn == "" {
		t.Skip("integration DB not configured; set ATLASKB_TEST_DSN")
	}
	host, port, user, pass, dbName, sslmode := parsePostgresDSN(t, dsn)

	depOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"mock-model"}]}`))
		case "/v1/embeddings":
			_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer depOK.Close()

	// LLM endpoint failure branch.
	llmFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			http.Error(w, "llm down", http.StatusBadGateway)
		case "/v1/embeddings":
			_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer llmFail.Close()

	s := &Server{cfg: config.DefaultConfig(), configPath: filepath.Join(t.TempDir(), "cfg.toml")}

	llmFailPayload := map[string]any{
		"config": map[string]any{
			"database":   map[string]any{"host": host, "port": port, "user": user, "password": pass, "dbname": dbName, "sslmode": sslmode},
			"llm":        map[string]any{"base_url": llmFail.URL},
			"embeddings": map[string]any{"base_url": llmFail.URL, "model": "mock-embed"},
			"pipeline":   map[string]any{"concurrency": 1, "extraction_model": "mock", "synthesis_model": "mock"},
			"server":     map[string]any{"port": 3000},
		},
	}
	b1, _ := json.Marshal(llmFailPayload)
	rec1 := httptest.NewRecorder()
	s.handleSetupApply(rec1, httptest.NewRequest(http.MethodPost, "/api/setup/apply", bytes.NewReader(b1)))
	if rec1.Code != http.StatusBadRequest || !strings.Contains(rec1.Body.String(), "LLM endpoint check failed") {
		t.Fatalf("expected llm endpoint failure, status=%d body=%s", rec1.Code, rec1.Body.String())
	}

	// Database connection failure branch.
	dbFailPayload := map[string]any{
		"config": map[string]any{
			"database":   map[string]any{"host": "127.0.0.1", "port": 1, "user": user, "password": pass, "dbname": dbName, "sslmode": "disable"},
			"llm":        map[string]any{"base_url": depOK.URL},
			"embeddings": map[string]any{"base_url": depOK.URL, "model": "mock-embed"},
			"pipeline":   map[string]any{"concurrency": 1, "extraction_model": "mock", "synthesis_model": "mock"},
			"server":     map[string]any{"port": 3000},
		},
	}
	b2, _ := json.Marshal(dbFailPayload)
	rec2 := httptest.NewRecorder()
	s.handleSetupApply(rec2, httptest.NewRequest(http.MethodPost, "/api/setup/apply", bytes.NewReader(b2)))
	if rec2.Code != http.StatusBadRequest || !strings.Contains(rec2.Body.String(), "database connection failed") {
		t.Fatalf("expected db connection failure, status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// Config validation failure branch.
	validateFailPayload := map[string]any{
		"config": map[string]any{
			"database":   map[string]any{"host": "", "port": port, "user": user, "password": pass, "dbname": dbName, "sslmode": sslmode},
			"llm":        map[string]any{"base_url": depOK.URL},
			"embeddings": map[string]any{"base_url": depOK.URL, "model": "mock-embed"},
			"pipeline":   map[string]any{"concurrency": 1, "extraction_model": "mock", "synthesis_model": "mock"},
			"server":     map[string]any{"port": 3000},
		},
	}
	b3, _ := json.Marshal(validateFailPayload)
	rec3 := httptest.NewRecorder()
	s.handleSetupApply(rec3, httptest.NewRequest(http.MethodPost, "/api/setup/apply", bytes.NewReader(b3)))
	if rec3.Code != http.StatusBadRequest || !strings.Contains(rec3.Body.String(), "invalid configuration") {
		t.Fatalf("expected config validation failure, status=%d body=%s", rec3.Code, rec3.Body.String())
	}
}

func TestIntegrationReindexBatchAdditionalBranches(t *testing.T) {
	pool, ctx := newIntegrationServerPool(t)
	seed := seedServerData(t, ctx, pool)

	indexingMu.Lock()
	indexingJobs = map[uuid.UUID]*indexJob{}
	indexingMu.Unlock()
	batchMu.Lock()
	activeBatch = nil
	batchMu.Unlock()
	t.Cleanup(func() {
		indexingMu.Lock()
		indexingJobs = map[uuid.UUID]*indexJob{}
		indexingMu.Unlock()
		batchMu.Lock()
		activeBatch = nil
		batchMu.Unlock()
	})

	s := New(
		pool,
		&embeddings.MockClient{},
		&llm.MockClient{},
		config.DefaultConfig(),
		fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>ok</html>")}},
		"",
	)

	// Existing in-progress reindex conflict path.
	indexingMu.Lock()
	indexingJobs[seed.repo1.ID] = &indexJob{RepoID: seed.repo1.ID, Status: "running"}
	indexingMu.Unlock()
	rec := doHTTP(t, s, http.MethodPost, "/api/repos/"+seed.repo1.ID.String()+"/reindex", []byte(`{"force":false}`))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already in progress") {
		t.Fatalf("expected reindex conflict for running job, status=%d body=%s", rec.Code, rec.Body.String())
	}
	indexingMu.Lock()
	indexingJobs = map[uuid.UUID]*indexJob{}
	indexingMu.Unlock()

	// Reindex status idle path.
	statusRec := doHTTP(t, s, http.MethodGet, "/api/repos/"+seed.repo1.ID.String()+"/reindex/status", nil)
	if statusRec.Code != http.StatusOK || !strings.Contains(statusRec.Body.String(), `"status":"idle"`) {
		t.Fatalf("expected idle reindex status, status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}

	// Batch reindex no-targets path.
	noTargetBody := []byte(`{"repo_ids":["` + uuid.New().String() + `"],"force":false}`)
	noTargetRec := doHTTP(t, s, http.MethodPost, "/api/indexing/batch", noTargetBody)
	if noTargetRec.Code != http.StatusBadRequest {
		t.Fatalf("expected batch no-targets bad request, status=%d body=%s", noTargetRec.Code, noTargetRec.Body.String())
	}

	// Batch reindex conflict path.
	batchMu.Lock()
	activeBatch = &batchState{Done: false, Repos: map[uuid.UUID]*batchRepoStatus{}, Order: []uuid.UUID{}}
	batchMu.Unlock()
	conflictRec := doHTTP(t, s, http.MethodPost, "/api/indexing/batch", []byte(`{"all":true}`))
	if conflictRec.Code != http.StatusConflict {
		t.Fatalf("expected batch conflict, status=%d body=%s", conflictRec.Code, conflictRec.Body.String())
	}
	batchMu.Lock()
	activeBatch = nil
	batchMu.Unlock()

	// Batch status should include live logs from indexingJobs for current repo.
	batchMu.Lock()
	activeBatch = &batchState{
		ID:      "active-1",
		Done:    false,
		Current: 0,
		Force:   false,
		Order:   []uuid.UUID{seed.repo1.ID},
		Repos: map[uuid.UUID]*batchRepoStatus{
			seed.repo1.ID: {RepoID: seed.repo1.ID, RepoName: seed.repo1.Name, Status: "running", Logs: []string{"stale"}},
		},
	}
	batchMu.Unlock()
	indexingMu.Lock()
	indexingJobs[seed.repo1.ID] = &indexJob{RepoID: seed.repo1.ID, Status: "running", Logs: []string{"live-progress-log"}}
	indexingMu.Unlock()
	batchStatus := doHTTP(t, s, http.MethodGet, "/api/indexing/batch/status", nil)
	if batchStatus.Code != http.StatusOK || !strings.Contains(batchStatus.Body.String(), "live-progress-log") {
		t.Fatalf("expected batch status to include live log, status=%d body=%s", batchStatus.Code, batchStatus.Body.String())
	}

	// Direct runBatch cancellation path: pre-cancel context marks pending repos as failed.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelBatch := &batchState{
		ID:    "cancel-me",
		Repos: map[uuid.UUID]*batchRepoStatus{},
		Order: []uuid.UUID{seed.repo1.ID, seed.repo2.ID},
	}
	cancelBatch.Repos[seed.repo1.ID] = &batchRepoStatus{RepoID: seed.repo1.ID, RepoName: seed.repo1.Name, Status: "pending"}
	cancelBatch.Repos[seed.repo2.ID] = &batchRepoStatus{RepoID: seed.repo2.ID, RepoName: seed.repo2.Name, Status: "pending"}
	s.runBatch(cancelCtx, cancelBatch, []models.Repo{
		{ID: seed.repo1.ID, Name: seed.repo1.Name, LocalPath: seed.repo1.LocalPath},
		{ID: seed.repo2.ID, Name: seed.repo2.Name, LocalPath: seed.repo2.LocalPath},
	})
	if cancelBatch.Repos[seed.repo1.ID].Status != "failed" || cancelBatch.Repos[seed.repo2.ID].Status != "failed" {
		t.Fatalf("expected cancelled batch repos to be marked failed, got %q and %q", cancelBatch.Repos[seed.repo1.ID].Status, cancelBatch.Repos[seed.repo2.ID].Status)
	}
	if !cancelBatch.Done {
		t.Fatalf("expected cancelled batch to be marked done")
	}
}

func TestIntegrationResolveRepoIDs(t *testing.T) {
	pool, ctx := newIntegrationServerPool(t)
	repoStore := &models.RepoStore{Pool: pool}

	repoA := &models.Repo{Name: "resolve-a", LocalPath: t.TempDir(), DefaultBranch: "main"}
	repoB := &models.Repo{Name: "resolve-b", LocalPath: t.TempDir(), DefaultBranch: "main"}
	if err := repoStore.Create(ctx, repoA); err != nil {
		t.Fatalf("create repoA: %v", err)
	}
	if err := repoStore.Create(ctx, repoB); err != nil {
		t.Fatalf("create repoB: %v", err)
	}

	s := &Server{pool: pool, cfg: config.DefaultConfig()}

	ids, err := s.resolveRepoIDs(
		ctx,
		repoA.ID.String(),
		[]string{"  ", repoA.ID.String(), repoB.ID.String(), repoA.ID.String()},
		repoB.Name,
	)
	if err != nil {
		t.Fatalf("resolveRepoIDs merged inputs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("resolveRepoIDs expected deduped 2 ids, got %d (%v)", len(ids), ids)
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen[repoA.ID] || !seen[repoB.ID] {
		t.Fatalf("resolved ids missing expected repos: %v", ids)
	}

	if _, err := s.resolveRepoIDs(ctx, "", []string{"bad-id"}, ""); err == nil {
		t.Fatalf("resolveRepoIDs should fail on invalid repo_ids entry")
	}
	if _, err := s.resolveRepoIDs(ctx, "", nil, "missing-repo-name"); err == nil {
		t.Fatalf("resolveRepoIDs should fail on unknown repo_name")
	}
}
