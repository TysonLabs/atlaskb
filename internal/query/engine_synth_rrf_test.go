package query

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/embeddings"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func newUnreachableQueryPool(t *testing.T) *pgxpool.Pool {
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

func TestRRFDefaultsAndMerge(t *testing.T) {
	cfg := DefaultRRFConfig()
	if cfg.K == 0 || cfg.VectorLimit == 0 || cfg.FTSLimit == 0 {
		t.Fatalf("DefaultRRFConfig returned zero values: %+v", cfg)
	}

	id1 := uuid.New()
	id2 := uuid.New()
	vec := []models.ScoredFact{
		{Fact: models.Fact{ID: id1}, Score: 0.9},
		{Fact: models.Fact{ID: id2}, Score: 0.8},
	}
	fts := []models.ScoredFact{
		{Fact: models.Fact{ID: id2}, Score: 0.7},
	}
	out := mergeRRF(vec, fts, RRFConfig{})
	if len(out) != 2 {
		t.Fatalf("mergeRRF len=%d, want 2", len(out))
	}
	if !out[0].InVector {
		t.Fatalf("top result should include vector provenance")
	}
}

func TestEngineBasicsAndHelpers(t *testing.T) {
	e := NewEngine(nil, &embeddings.MockClient{})
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.RRF.K == 0 {
		t.Fatalf("NewEngine should set default RRF config")
	}

	llmClient := &llm.MockClient{}
	e.SetLLM(llmClient, "test-model")
	if e.LLM == nil || e.Model != "test-model" {
		t.Fatalf("SetLLM did not set fields")
	}

	trace := scoreTrace{
		factID:        uuid.New(),
		entityName:    "UserService",
		base:          0.123,
		source:        "hybrid",
		rrfVectorRank: 1,
		rrfFTSRank:    2,
		entityBoost:   1.2,
		entitySim:     0.8,
		confidence:    0.9,
		kindBias:      1.1,
		category:      1.0,
		repoAffinity:  1.3,
		overlap:       0.1,
		final:         0.4,
	}
	s := trace.String()
	if !strings.Contains(s, "UserService") || !strings.Contains(s, "final=0.4000") {
		t.Fatalf("scoreTrace.String missing expected fields: %s", s)
	}

	tokens := extractCandidateTokens("How does UserService handle user_profile updates?")
	if len(tokens) == 0 {
		t.Fatalf("extractCandidateTokens returned no candidates")
	}

	repoA := uuid.New()
	repoB := uuid.New()
	results := []SearchResult{
		{Fact: models.Fact{RepoID: repoA}, Score: 0.9},
		{Fact: models.Fact{RepoID: repoA}, Score: 0.8},
		{Fact: models.Fact{RepoID: repoA}, Score: 0.7},
		{Fact: models.Fact{RepoID: repoB}, Score: 0.6},
	}
	diverse := enforceRepoDiversity(results, 3)
	if len(diverse) != len(results) {
		t.Fatalf("enforceRepoDiversity should preserve list when no repo exceeds cap; got=%d want=%d", len(diverse), len(results))
	}
}

func TestDecomposeAndLookupRepoName(t *testing.T) {
	ctx := context.Background()
	pool := newUnreachableQueryPool(t)
	e := NewEngine(pool, &embeddings.MockClient{})

	e.SetLLM(&llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return nil, errors.New("llm down")
		},
	}, "m")
	_, err := e.decomposeQuery(ctx, "q")
	if err == nil {
		t.Fatalf("decomposeQuery expected LLM error")
	}

	e.SetLLM(&llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{Content: "not json"}, nil
		},
	}, "m")
	qs, err := e.decomposeQuery(ctx, "single question")
	if err != nil {
		t.Fatalf("decomposeQuery invalid-json fallback error: %v", err)
	}
	if len(qs) != 1 || qs[0] != "single question" {
		t.Fatalf("decomposeQuery invalid-json fallback=%v", qs)
	}

	e.SetLLM(&llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{Content: "```json\n[\"a\",\"b\",\"c\",\"d\",\"e\"]\n```"}, nil
		},
	}, "m")
	qs, err = e.decomposeQuery(ctx, "compound question")
	if err != nil {
		t.Fatalf("decomposeQuery fenced-json error: %v", err)
	}
	if len(qs) != 4 {
		t.Fatalf("decomposeQuery should cap at 4, got %d", len(qs))
	}

	cache := map[uuid.UUID]string{uuid.Nil: "cached"}
	if got := e.lookupRepoName(ctx, &models.RepoStore{Pool: pool}, cache, uuid.Nil); got != "cached" {
		t.Fatalf("lookupRepoName cache hit=%q, want cached", got)
	}
	if got := e.lookupRepoName(ctx, &models.RepoStore{Pool: pool}, cache, uuid.New()); got != "" {
		t.Fatalf("lookupRepoName miss=%q, want empty", got)
	}
}

func TestEntityMentionAndSearchErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool := newUnreachableQueryPool(t)
	entityStore := &models.EntityStore{Pool: pool}

	mentions := extractEntityMentions(ctx, "why?", entityStore, nil, false)
	if len(mentions) != 0 {
		t.Fatalf("extractEntityMentions(no candidates) len=%d, want 0", len(mentions))
	}
	mentions = extractEntityMentions(ctx, "How does UserService work?", entityStore, nil, true)
	if len(mentions) != 0 {
		t.Fatalf("extractEntityMentions(db error) len=%d, want 0", len(mentions))
	}

	e := NewEngine(pool, &embeddings.MockClient{
		EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
			return nil, errors.New("embed fail")
		},
	})
	_, err := e.Search(ctx, "q", nil, 5)
	if err == nil || !strings.Contains(err.Error(), "embedding question") {
		t.Fatalf("Search expected embedding error, got %v", err)
	}

	e.Embedder = &embeddings.MockClient{
		EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
			return [][]float32{{}}, nil
		},
	}
	_, err = e.Search(ctx, "q", nil, 5)
	if err == nil || !strings.Contains(err.Error(), "empty embedding") {
		t.Fatalf("Search expected empty embedding error, got %v", err)
	}

	e.Embedder = &embeddings.MockClient{
		EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
			return [][]float32{{0.1, 0.2, 0.3}}, nil
		},
	}
	_, err = e.Search(ctx, "q", nil, 5)
	if err == nil || !strings.Contains(err.Error(), "vector search") {
		t.Fatalf("Search expected vector search error, got %v", err)
	}

	e.SetLLM(&llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{Content: `["one","two"]`}, nil
		},
	}, "test")
	_, _ = e.Search(ctx, "compound", nil, 5) // still expected to fail at DB vector search, but exercises decomposition path
}

func TestSearchTripletsErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool := newUnreachableQueryPool(t)
	e := NewEngine(pool, &embeddings.MockClient{
		EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
			return nil, errors.New("embed fail")
		},
	})
	_, err := e.SearchTriplets(ctx, "q", nil, TripletSearchOptions{})
	if err == nil || !strings.Contains(err.Error(), "embedding question") {
		t.Fatalf("SearchTriplets expected embedding error, got %v", err)
	}

	e.Embedder = &embeddings.MockClient{
		EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
			return [][]float32{{}}, nil
		},
	}
	_, err = e.SearchTriplets(ctx, "q", nil, TripletSearchOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty embedding") {
		t.Fatalf("SearchTriplets expected empty embedding error, got %v", err)
	}

	e.Embedder = &embeddings.MockClient{
		EmbedFunc: func(ctx context.Context, texts []string, model string) ([][]float32, error) {
			return [][]float32{{0.1, 0.2, 0.3}}, nil
		},
	}
	_, err = e.SearchTriplets(ctx, "q", nil, TripletSearchOptions{})
	if err == nil || !strings.Contains(err.Error(), "vector search") {
		t.Fatalf("SearchTriplets expected vector search error, got %v", err)
	}
}

func TestSynthesizerPaths(t *testing.T) {
	path := "internal/server/handlers.go"
	summary := "handles API requests"
	results := []SearchResult{
		{
			Fact: models.Fact{
				Claim:      "Handles health route",
				Dimension:  models.DimensionWhat,
				Category:   models.CategoryBehavior,
				Confidence: models.ConfidenceHigh,
			},
			Entity: models.Entity{
				QualifiedName: "server::handleHealth",
				Kind:          models.EntityFunction,
				Path:          &path,
				Summary:       &summary,
			},
			RepoName: "atlaskb",
		},
	}

	var captured []llm.Message
	s := NewSynthesizer(&llm.MockClient{
		CompleteStreamFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int) (<-chan llm.StreamChunk, error) {
			captured = messages
			ch := make(chan llm.StreamChunk, 2)
			ch <- llm.StreamChunk{Text: "hello "}
			ch <- llm.StreamChunk{Text: "world", Done: true}
			close(ch)
			return ch, nil
		},
	}, "model-a")

	ch, err := s.Synthesize(context.Background(), "what does it do?", results)
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	var out strings.Builder
	for c := range ch {
		out.WriteString(c.Text)
	}
	if out.String() != "hello world" {
		t.Fatalf("Synthesize stream output=%q", out.String())
	}
	if len(captured) != 1 || !strings.Contains(captured[0].Content, "Retrieved Knowledge") {
		t.Fatalf("Synthesize prompt missing expected content")
	}

	_, err = s.SynthesizeWithHistory(context.Background(), "q2", results, []llm.Message{{Role: "assistant", Content: "prior"}})
	if err != nil {
		t.Fatalf("SynthesizeWithHistory error: %v", err)
	}

	sErr := NewSynthesizer(&llm.MockClient{
		CompleteStreamFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int) (<-chan llm.StreamChunk, error) {
			ch := make(chan llm.StreamChunk, 3)
			ch <- llm.StreamChunk{Text: "partial"}
			ch <- llm.StreamChunk{Error: errors.New("stream failed")}
			close(ch)
			return ch, nil
		},
	}, "model-b")
	got, err := sErr.SynthesizeSync(context.Background(), "q", results)
	if err == nil {
		t.Fatalf("SynthesizeSync expected stream error")
	}
	if got != "partial" {
		t.Fatalf("SynthesizeSync partial output=%q, want partial", got)
	}
}
