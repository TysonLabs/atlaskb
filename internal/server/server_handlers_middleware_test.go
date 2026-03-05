package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/config"
)

func newUnreachableServerPool(t *testing.T) *pgxpool.Pool {
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

func withURLParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestServerSetupModeRoutes(t *testing.T) {
	webFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>index</html>")},
	}
	s := New(nil, nil, nil, config.DefaultConfig(), webFS, "/tmp/atlaskb-config.toml")

	healthReq := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	healthRec := httptest.NewRecorder()
	s.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("setup /api/health status=%d, want 200", healthRec.Code)
	}
	if !strings.Contains(healthRec.Body.String(), "setup_required") {
		t.Fatalf("setup health body missing readiness: %s", healthRec.Body.String())
	}

	setupReq := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	setupRec := httptest.NewRecorder()
	s.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusOK {
		t.Fatalf("setup /api/setup/status status=%d, want 200", setupRec.Code)
	}
	if !strings.Contains(setupRec.Body.String(), "/tmp/atlaskb-config.toml") {
		t.Fatalf("setup status should include config path, body=%s", setupRec.Body.String())
	}

	spaReq := httptest.NewRequest(http.MethodGet, "/anything", nil)
	spaRec := httptest.NewRecorder()
	s.ServeHTTP(spaRec, spaReq)
	if spaRec.Code != http.StatusOK {
		t.Fatalf("setup page status=%d, want 200", spaRec.Code)
	}
	if !strings.Contains(spaRec.Body.String(), "AtlasKB Setup") {
		t.Fatalf("setup page body missing title")
	}
}

func TestServerNormalModeSPAFallback(t *testing.T) {
	webFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>spa-index</html>")},
		"app.js":     &fstest.MapFile{Data: []byte("console.log('ok')")},
	}
	s := New(newUnreachableServerPool(t), nil, nil, config.DefaultConfig(), webFS, "")

	reqAsset := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	recAsset := httptest.NewRecorder()
	s.ServeHTTP(recAsset, reqAsset)
	if recAsset.Code != http.StatusOK {
		t.Fatalf("asset status=%d, want 200", recAsset.Code)
	}
	if !strings.Contains(recAsset.Body.String(), "console.log('ok')") {
		t.Fatalf("asset body mismatch: %s", recAsset.Body.String())
	}

	reqRoute := httptest.NewRequest(http.MethodGet, "/ui/dashboard", nil)
	recRoute := httptest.NewRecorder()
	s.ServeHTTP(recRoute, reqRoute)
	if recRoute.Code != http.StatusOK {
		t.Fatalf("SPA fallback status=%d, want 200", recRoute.Code)
	}
	if !strings.Contains(recRoute.Body.String(), "spa-index") {
		t.Fatalf("SPA fallback did not serve index.html")
	}
}

func TestMiddlewaresAndStatusWriter(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})
	handler := corsMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("cors middleware status=%d, want 201", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing CORS header")
	}

	optReq := httptest.NewRequest(http.MethodOptions, "/x", nil)
	optRec := httptest.NewRecorder()
	handler.ServeHTTP(optRec, optReq)
	if optRec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status=%d, want 204", optRec.Code)
	}

	recoverHandler := recoveryMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	recRecover := httptest.NewRecorder()
	recoverHandler.ServeHTTP(recRecover, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if recRecover.Code != http.StatusInternalServerError {
		t.Fatalf("recovery status=%d, want 500", recRecover.Code)
	}

	logHandler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	recLog := httptest.NewRecorder()
	logHandler.ServeHTTP(recLog, httptest.NewRequest(http.MethodGet, "/brew", nil))
	if recLog.Code != http.StatusTeapot {
		t.Fatalf("logging middleware status=%d, want 418", recLog.Code)
	}

	sw := &statusWriter{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	sw.WriteHeader(http.StatusAccepted)
	if sw.status != http.StatusAccepted {
		t.Fatalf("statusWriter status=%d, want 202", sw.status)
	}
	sw.Flush() // no panic even when underlying writer is not a Flusher
}

func TestResponseHelpersAndWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, NewBadRequest("bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("writeError(APIError) status=%d, want 400", rec.Code)
	}

	rec2 := httptest.NewRecorder()
	writeError(rec2, io.EOF)
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("writeError(generic) status=%d, want 500", rec2.Code)
	}

	if NewNotFound("x").Status != http.StatusNotFound {
		t.Fatalf("NewNotFound returned wrong status")
	}
	if NewInternal("x").Status != http.StatusInternalServerError {
		t.Fatalf("NewInternal returned wrong status")
	}
}

func TestSetupHandlersAndMergeConfig(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig(), configPath: "/tmp/cfg.toml"}

	reqBad := httptest.NewRequest(http.MethodPost, "/api/setup/apply", strings.NewReader("{"))
	recBad := httptest.NewRecorder()
	s.handleSetupApply(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("handleSetupApply invalid JSON status=%d, want 400", recBad.Code)
	}

	cfg := mergeSetupConfig(setupInputConfig{
		Database: setupInputDatabase{
			Host: " db ",
			Port: 0,
			User: " user ",
		},
		LLM: setupInputLLM{BaseURL: " http://llm "},
		Embeddings: setupInputEmbeddings{
			BaseURL: " http://emb ",
			Model:   " emb-model ",
		},
		Pipeline: setupInputPipeline{
			Concurrency:     4,
			ExtractionModel: " ex-model ",
			SynthesisModel:  " sy-model ",
			ContextWindow:   8192,
		},
		Server: setupInputServer{
			Port:     9090,
			ChatsDir: " /tmp/chats ",
		},
		GitHub: setupInputGitHub{
			Token:       " token ",
			APIURL:      " https://api.example.com ",
			MaxPRs:      50,
			PRBatchSize: 10,
		},
	})
	if cfg.Database.Host != "db" || cfg.Database.User != "user" {
		t.Fatalf("mergeSetupConfig did not trim database fields: %+v", cfg.Database)
	}
	if cfg.Database.Port != 5432 {
		t.Fatalf("mergeSetupConfig default db port=%d, want 5432", cfg.Database.Port)
	}
	if cfg.Database.SSLMode != "disable" {
		t.Fatalf("mergeSetupConfig default sslmode=%q, want disable", cfg.Database.SSLMode)
	}
	if cfg.LLM.BaseURL != "http://llm" || cfg.Embeddings.BaseURL != "http://emb" || cfg.Embeddings.Model != "emb-model" {
		t.Fatalf("mergeSetupConfig did not set/trim llm/embedding fields")
	}
	if cfg.Pipeline.Concurrency != 4 || cfg.Pipeline.ExtractionModel != "ex-model" || cfg.Pipeline.SynthesisModel != "sy-model" {
		t.Fatalf("mergeSetupConfig pipeline mismatch: %+v", cfg.Pipeline)
	}
	if cfg.Server.Port != 9090 || cfg.Server.ChatsDir != "/tmp/chats" {
		t.Fatalf("mergeSetupConfig server mismatch: %+v", cfg.Server)
	}
	if cfg.GitHub.Token != "token" || cfg.GitHub.APIURL != "https://api.example.com" {
		t.Fatalf("mergeSetupConfig github mismatch: %+v", cfg.GitHub)
	}
}

func TestHealthStatsAndRepoHandlerErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool := newUnreachableServerPool(t)
	s := &Server{
		pool: pool,
		cfg:  config.DefaultConfig(),
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/api/health", nil).WithContext(ctx)
	healthRec := httptest.NewRecorder()
	s.handleHealth(healthRec, healthReq)
	if healthRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("handleHealth degraded status=%d, want 503", healthRec.Code)
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/api/stats", nil).WithContext(ctx)
	statsRec := httptest.NewRecorder()
	s.handleStats(statsRec, statsReq)
	if statsRec.Code != http.StatusInternalServerError {
		t.Fatalf("handleStats status=%d, want 500", statsRec.Code)
	}

	runsReq := httptest.NewRequest(http.MethodGet, "/api/stats/recent-runs", nil).WithContext(ctx)
	runsRec := httptest.NewRecorder()
	s.handleRecentRuns(runsRec, runsReq)
	if runsRec.Code != http.StatusInternalServerError {
		t.Fatalf("handleRecentRuns status=%d, want 500", runsRec.Code)
	}

	badIDReq := withURLParam(httptest.NewRequest(http.MethodGet, "/api/repos/x", nil), "id", "not-a-uuid").WithContext(ctx)
	badIDRec := httptest.NewRecorder()
	s.handleGetRepo(badIDRec, badIDReq)
	if badIDRec.Code != http.StatusBadRequest {
		t.Fatalf("handleGetRepo invalid id status=%d, want 400", badIDRec.Code)
	}

	badRunsReq := withURLParam(httptest.NewRequest(http.MethodGet, "/api/repos/x/indexing-runs", nil), "id", "bad").WithContext(ctx)
	badRunsRec := httptest.NewRecorder()
	s.handleRepoIndexingRuns(badRunsRec, badRunsReq)
	if badRunsRec.Code != http.StatusBadRequest {
		t.Fatalf("handleRepoIndexingRuns invalid id status=%d, want 400", badRunsRec.Code)
	}

	badDecReq := withURLParam(httptest.NewRequest(http.MethodGet, "/api/repos/x/decisions", nil), "id", "bad").WithContext(ctx)
	badDecRec := httptest.NewRecorder()
	s.handleRepoDecisions(badDecRec, badDecReq)
	if badDecRec.Code != http.StatusBadRequest {
		t.Fatalf("handleRepoDecisions invalid id status=%d, want 400", badDecRec.Code)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/repos", strings.NewReader("{"))
	createRec := httptest.NewRecorder()
	s.handleCreateRepo(createRec, createReq.WithContext(ctx))
	if createRec.Code != http.StatusBadRequest {
		t.Fatalf("handleCreateRepo invalid JSON status=%d, want 400", createRec.Code)
	}

	updateReq := withURLParam(
		httptest.NewRequest(http.MethodPut, "/api/repos/x", strings.NewReader(`{"name":"x"}`)),
		"id", "bad",
	).WithContext(ctx)
	updateRec := httptest.NewRecorder()
	s.handleUpdateRepo(updateRec, updateReq)
	if updateRec.Code != http.StatusBadRequest {
		t.Fatalf("handleUpdateRepo invalid id status=%d, want 400", updateRec.Code)
	}

	deleteReq := withURLParam(httptest.NewRequest(http.MethodDelete, "/api/repos/x", nil), "id", "bad").WithContext(ctx)
	deleteRec := httptest.NewRecorder()
	s.handleDeleteRepo(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusBadRequest {
		t.Fatalf("handleDeleteRepo invalid id status=%d, want 400", deleteRec.Code)
	}

	if !stringSliceEqual([]string{"a", "b"}, []string{"a", "b"}) {
		t.Fatalf("stringSliceEqual expected true for equal slices")
	}
	if stringSliceEqual([]string{"a"}, []string{"b"}) {
		t.Fatalf("stringSliceEqual expected false for different slices")
	}
}

func TestCheckHTTPDependency(t *testing.T) {
	tsOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("expected Content-Type for POST")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer tsOK.Close()

	if !checkHTTPDependency(context.Background(), tsOK.URL, http.MethodGet, nil) {
		t.Fatalf("checkHTTPDependency(GET) expected true")
	}
	if !checkHTTPDependency(context.Background(), tsOK.URL, http.MethodPost, []byte(`{"a":1}`)) {
		t.Fatalf("checkHTTPDependency(POST) expected true")
	}

	tsErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer tsErr.Close()
	if checkHTTPDependency(context.Background(), tsErr.URL, http.MethodGet, nil) {
		t.Fatalf("checkHTTPDependency(503) expected false")
	}

	if checkHTTPDependency(context.Background(), "://bad-url", http.MethodGet, nil) {
		t.Fatalf("checkHTTPDependency(invalid URL) expected false")
	}
	if checkHTTPDependency(context.Background(), "   ", http.MethodGet, nil) {
		t.Fatalf("checkHTTPDependency(blank URL) expected false")
	}
}
