package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/config"
)

func TestAdditionalHandlerGuards(t *testing.T) {
	s := &Server{pool: newUnreachableServerPool(t), cfg: config.DefaultConfig()}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/repos", nil)
	s.handleListRepos(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("handleListRepos status=%d, want 500", rec.Code)
	}

	// Entities
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/entities?repo_id=bad", nil)
	s.handleListEntities(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleListEntities invalid repo_id status=%d, want 400", rec.Code)
	}
	for _, tc := range []struct {
		name string
		fn   func(http.ResponseWriter, *http.Request)
	}{
		{"handleGetEntity", s.handleGetEntity},
		{"handleEntityFacts", s.handleEntityFacts},
		{"handleEntityRelationships", s.handleEntityRelationships},
		{"handleEntityDecisions", s.handleEntityDecisions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := withURLParam(httptest.NewRequest(http.MethodGet, "/x", nil), "id", "bad")
			tc.fn(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s invalid id status=%d, want 400", tc.name, rec.Code)
			}
		})
	}

	// Graph
	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodGet, "/x", nil), "id", "bad")
	s.handleRepoGraph(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleRepoGraph invalid id status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodGet, "/x", nil), "id", "bad")
	s.handleEntityGraph(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleEntityGraph invalid id status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	s.handleMultiRepoGraph(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleMultiRepoGraph missing repo_ids status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x?repo_ids=bad", nil)
	s.handleMultiRepoGraph(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleMultiRepoGraph bad repo_ids status=%d, want 400", rec.Code)
	}

	// Cross-repo links
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x?repo_id=bad", nil)
	s.handleListCrossRepoLinks(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleListCrossRepoLinks invalid repo_id status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodGet, "/x", nil), "id", "bad")
	s.handleGetCrossRepoLink(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleGetCrossRepoLink invalid id status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{"))
	s.handleCreateCrossRepoLink(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleCreateCrossRepoLink invalid json status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"from_entity_id":"bad","to_entity_id":"bad","kind":"depends_on"}`))
	s.handleCreateCrossRepoLink(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleCreateCrossRepoLink invalid ids status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodDelete, "/x", nil), "id", "bad")
	s.handleDeleteCrossRepoLink(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleDeleteCrossRepoLink invalid id status=%d, want 400", rec.Code)
	}

	// Ask/search
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{"))
	s.handleAsk(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleAsk invalid json status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"question":"  "}`))
	s.handleAsk(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleAsk blank question status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	s.handleSearch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleSearch missing q status=%d, want 400", rec.Code)
	}
	if _, err := s.resolveRepoIDs(req.Context(), "bad", nil, ""); err == nil {
		t.Fatalf("resolveRepoIDs invalid repo_id should error")
	}

	// Feedback
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{"))
	s.handleCreateFeedback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleCreateFeedback invalid json status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"fact_id":"","reason":""}`))
	s.handleCreateFeedback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleCreateFeedback missing fields status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x?repo_id=bad", nil)
	s.handleListFeedback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleListFeedback invalid repo_id status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodPost, "/x", nil), "id", "bad")
	s.handleResolveFeedback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleResolveFeedback invalid id status=%d, want 400", rec.Code)
	}

	// File read
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x?repo_id=bad", nil)
	s.handleReadFile(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleReadFile invalid repo_id status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x?repo_id="+uuid.New().String(), nil)
	s.handleReadFile(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleReadFile missing path status=%d, want 400", rec.Code)
	}

	// Explore
	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodGet, "/x", nil), "id", "bad")
	s.handleRepoClusters(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleRepoClusters invalid id status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodGet, "/x", nil), "id", "bad")
	s.handleRepoFlows(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleRepoFlows invalid id status=%d, want 400", rec.Code)
	}

	// Reindex
	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodPost, "/x", nil), "id", "bad")
	s.handleReindex(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleReindex invalid id status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodGet, "/x", nil), "id", "bad")
	s.handleReindexStatus(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleReindexStatus invalid id status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{"))
	s.handleBatchReindex(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleBatchReindex invalid json status=%d, want 400", rec.Code)
	}

	// Metrics and helpers
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	s.handleMetrics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleMetrics status=%d, want 200", rec.Code)
	}
	if got := parseCommaSeparated(" a, b ,,c "); len(got) != 3 {
		t.Fatalf("parseCommaSeparated len=%d, want 3", len(got))
	}
	if !contains([]string{"a", "b"}, "b") {
		t.Fatalf("contains expected true")
	}
}

func TestChatHandlersAndBatchNoActive(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Server.ChatsDir = tmp

	s := &Server{pool: newUnreachableServerPool(t), cfg: cfg}

	if err := s.ensureChatsDir(); err != nil {
		t.Fatalf("ensureChatsDir: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	session := &ChatSession{
		ID:        uuid.New().String(),
		Title:     "chat",
		Messages:  []ChatMessage{{ID: uuid.New().String(), Role: "user", Content: "hi", Timestamp: now}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.saveSession(session); err != nil {
		t.Fatalf("saveSession: %v", err)
	}
	loaded, err := s.loadSession(session.ID)
	if err != nil {
		t.Fatalf("loadSession: %v", err)
	}
	if loaded.ID != session.ID {
		t.Fatalf("loadSession ID=%q want %q", loaded.ID, session.ID)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	s.handleListChats(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleListChats status=%d, want 200", rec.Code)
	}
	var summaries []ChatSessionSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil {
		t.Fatalf("unmarshal summaries: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatalf("expected at least one chat summary")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	s.handleCreateChat(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("handleCreateChat status=%d, want 201", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodGet, "/x", nil), "id", "missing")
	s.handleGetChat(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("handleGetChat missing status=%d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodPut, "/x", strings.NewReader("{")), "id", session.ID)
	s.handleUpdateChat(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleUpdateChat invalid body status=%d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodDelete, "/x", nil), "id", "missing")
	s.handleDeleteChat(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("handleDeleteChat missing status=%d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{")), "id", session.ID)
	s.handleChatMessage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleChatMessage invalid body status=%d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"question":" "}`)), "id", session.ID)
	s.handleChatMessage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleChatMessage blank question status=%d, want 400", rec.Code)
	}

	batchMu.Lock()
	activeBatch = nil
	batchMu.Unlock()

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	s.handleBatchStatus(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"active":false`) {
		t.Fatalf("handleBatchStatus(no active) response=%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	s.handleBatchCancel(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "no active batch") {
		t.Fatalf("handleBatchCancel(no active) response=%s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	s.handleListIndexingJobs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleListIndexingJobs status=%d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	s.handleIndexingHistory(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("handleIndexingHistory status=%d, want 500", rec.Code)
	}

	// exercise helper methods used by chat handlers
	if s.chatsDir() == "" {
		t.Fatalf("chatsDir should not be empty")
	}
	if _, err := os.Stat(filepath.Join(tmp, session.ID+".json")); err != nil {
		t.Fatalf("expected chat file to exist: %v", err)
	}
}

func TestReindexHelpers(t *testing.T) {
	job := &indexJob{}
	job.appendLog("line-1")
	job.appendLog("line-2")
	if len(job.Logs) != 2 {
		t.Fatalf("appendLog expected 2 entries, got %d", len(job.Logs))
	}

	s := &Server{cfg: config.DefaultConfig()}
	batch := &batchState{
		ID:    "b1",
		Repos: map[uuid.UUID]*batchRepoStatus{},
		Order: nil,
	}
	s.runBatch(context.Background(), batch, nil)
	if !batch.Done {
		t.Fatalf("runBatch with no repos should mark batch as done")
	}
}

func TestChatHandlerAdditionalErrorPaths(t *testing.T) {
	// chats dir points at a file so ensureChatsDir/saveSession fail.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Server.ChatsDir = blocker
	s := &Server{pool: newUnreachableServerPool(t), cfg: cfg}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	s.handleListChats(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("handleListChats ensureChatsDir failure status=%d, want 500", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	s.handleCreateChat(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("handleCreateChat save failure status=%d, want 500", rec.Code)
	}

	// Corrupt existing chat session file to force load failures.
	okDir := filepath.Join(tmp, "chats")
	if err := os.MkdirAll(okDir, 0o755); err != nil {
		t.Fatalf("mkdir chats: %v", err)
	}
	cfg.Server.ChatsDir = okDir
	s = &Server{pool: newUnreachableServerPool(t), cfg: cfg}
	if err := os.WriteFile(filepath.Join(okDir, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write bad session: %v", err)
	}

	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodGet, "/x", nil), "id", "bad")
	s.handleGetChat(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("handleGetChat malformed session status=%d, want 500", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"title":"new"}`)), "id", "bad")
	s.handleUpdateChat(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("handleUpdateChat malformed session status=%d, want 500", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"question":"q"}`)), "id", "bad")
	s.handleChatMessage(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("handleChatMessage malformed session status=%d, want 500", rec.Code)
	}

	// Valid chat file then invalid repo_id path in handleChatMessage.
	now := time.Now().UTC().Format(time.RFC3339)
	session := &ChatSession{ID: "ok", Title: "New Chat", Messages: []ChatMessage{}, CreatedAt: now, UpdatedAt: now}
	data, _ := json.Marshal(session)
	if err := os.WriteFile(filepath.Join(okDir, "ok.json"), data, 0o600); err != nil {
		t.Fatalf("write ok session: %v", err)
	}

	rec = httptest.NewRecorder()
	req = withURLParam(httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"question":"q","repo_id":"bad"}`)), "id", "ok")
	s.handleChatMessage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("handleChatMessage invalid repo_id status=%d, want 400", rec.Code)
	}
}
