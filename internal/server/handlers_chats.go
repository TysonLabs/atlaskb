package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/query"
)

type ChatSession struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Messages  []ChatMessage `json:"messages"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
}

type ChatSessionSummary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	MessageCount int    `json:"message_count"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type ChatMessage struct {
	ID        string               `json:"id"`
	Role      string               `json:"role"`
	Content   string               `json:"content"`
	Evidence  []query.SearchResult `json:"evidence,omitempty"`
	Timestamp string               `json:"timestamp"`
}

func (s *Server) chatsDir() string {
	return s.cfg.Server.GetChatsDir()
}

func (s *Server) ensureChatsDir() error {
	return os.MkdirAll(s.chatsDir(), 0o700)
}

func (s *Server) loadSession(id string) (*ChatSession, error) {
	data, err := os.ReadFile(filepath.Join(s.chatsDir(), id+".json"))
	if err != nil {
		return nil, err
	}
	var session ChatSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Server) saveSession(session *ChatSession) error {
	if err := s.ensureChatsDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.chatsDir(), session.ID+".json"), data, 0o600)
}

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	if err := s.ensureChatsDir(); err != nil {
		writeError(w, NewInternal("failed to access chats directory"))
		return
	}

	entries, err := os.ReadDir(s.chatsDir())
	if err != nil {
		writeJSON(w, http.StatusOK, []ChatSessionSummary{})
		return
	}

	var summaries []ChatSessionSummary
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		session, err := s.loadSession(id)
		if err != nil {
			continue
		}
		summaries = append(summaries, ChatSessionSummary{
			ID:           session.ID,
			Title:        session.Title,
			MessageCount: len(session.Messages),
			CreatedAt:    session.CreatedAt,
			UpdatedAt:    session.UpdatedAt,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt > summaries[j].UpdatedAt
	})

	if summaries == nil {
		summaries = []ChatSessionSummary{}
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (s *Server) handleCreateChat(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339)
	session := &ChatSession{
		ID:        uuid.New().String(),
		Title:     "New Chat",
		Messages:  []ChatMessage{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.saveSession(session); err != nil {
		writeError(w, NewInternal("failed to create chat session"))
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleGetChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, err := s.loadSession(id)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, NewNotFound("chat session not found"))
			return
		}
		writeError(w, NewInternal("failed to load chat session"))
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleUpdateChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, err := s.loadSession(id)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, NewNotFound("chat session not found"))
			return
		}
		writeError(w, NewInternal("failed to load chat session"))
		return
	}

	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, NewBadRequest("invalid request body"))
		return
	}

	if req.Title != "" {
		session.Title = req.Title
	}
	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := s.saveSession(session); err != nil {
		writeError(w, NewInternal("failed to update chat session"))
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	path := filepath.Join(s.chatsDir(), id+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		writeError(w, NewNotFound("chat session not found"))
		return
	}
	if err := os.Remove(path); err != nil {
		writeError(w, NewInternal("failed to delete chat session"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type chatMessageRequest struct {
	Question string `json:"question"`
	RepoID   string `json:"repo_id,omitempty"`
	TopK     int    `json:"top_k,omitempty"`
}

func (s *Server) handleChatMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, err := s.loadSession(id)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, NewNotFound("chat session not found"))
			return
		}
		writeError(w, NewInternal("failed to load chat session"))
		return
	}

	var req chatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, NewBadRequest("invalid request body"))
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		writeError(w, NewBadRequest("question is required"))
		return
	}
	if req.TopK <= 0 {
		req.TopK = 40
	}

	// Append user message
	userMsg := ChatMessage{
		ID:        uuid.New().String(),
		Role:      "user",
		Content:   req.Question,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	session.Messages = append(session.Messages, userMsg)

	// Auto-title on first user message
	if session.Title == "New Chat" && len(session.Messages) == 1 {
		title := req.Question
		if len(title) > 50 {
			title = title[:50] + "..."
		}
		session.Title = title
	}

	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.saveSession(session); err != nil {
		writeError(w, NewInternal("failed to save chat session"))
		return
	}

	// Search
	var repoIDs []uuid.UUID
	if req.RepoID != "" {
		rid, err := uuid.Parse(req.RepoID)
		if err != nil {
			writeError(w, NewBadRequest("invalid repo_id"))
			return
		}
		repoIDs = []uuid.UUID{rid}
	}

	engine := query.NewEngine(s.pool, s.embedder)
	if s.llm != nil {
		engine.SetLLM(s.llm, s.cfg.Pipeline.ExtractionModel)
	}

	results, err := engine.Search(r.Context(), req.Question, repoIDs, req.TopK)
	if err != nil {
		writeError(w, NewInternal("search failed: "+err.Error()))
		return
	}

	// SSE setup
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, NewInternal("streaming not supported"))
		return
	}

	// Send facts
	factsJSON, _ := json.Marshal(results)
	fmt.Fprintf(w, "event: facts\ndata: %s\n\n", factsJSON)
	flusher.Flush()

	if len(results) == 0 {
		// Save empty assistant message
		assistantMsg := ChatMessage{
			ID:        uuid.New().String(),
			Role:      "assistant",
			Content:   "I couldn't find any relevant information in the knowledge base to answer your question.",
			Evidence:  []query.SearchResult{},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		session.Messages = append(session.Messages, assistantMsg)
		session.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = s.saveSession(session)

		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	synth := query.NewSynthesizer(s.llm, s.cfg.Pipeline.SynthesisModel)
	stream, err := synth.Synthesize(r.Context(), req.Question, results)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
		flusher.Flush()
		return
	}

	var fullContent strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			fmt.Fprintf(w, "event: error\ndata: %q\n\n", chunk.Error.Error())
			flusher.Flush()
			return
		}
		fullContent.WriteString(chunk.Text)
		escaped, _ := json.Marshal(chunk.Text)
		fmt.Fprintf(w, "event: chunk\ndata: %s\n\n", escaped)
		flusher.Flush()
	}

	// Save assistant message with full content and evidence
	assistantMsg := ChatMessage{
		ID:        uuid.New().String(),
		Role:      "assistant",
		Content:   fullContent.String(),
		Evidence:  results,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	session.Messages = append(session.Messages, assistantMsg)
	session.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_ = s.saveSession(session)

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}
