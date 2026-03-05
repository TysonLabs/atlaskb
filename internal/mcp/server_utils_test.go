package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func TestNewServer(t *testing.T) {
	s := NewServer(nil, nil)
	if s == nil {
		t.Fatal("NewServer() returned nil")
	}
	if s.pool != nil {
		t.Fatal("expected nil pool in test constructor")
	}
	if s.embedder != nil {
		t.Fatal("expected nil embedder in test constructor")
	}
}

func TestClampMaxResults(t *testing.T) {
	tests := []struct {
		n, def, max int
		want        int
	}{
		{n: 0, def: 20, max: 50, want: 20},
		{n: -1, def: 20, max: 50, want: 20},
		{n: 10, def: 20, max: 50, want: 10},
		{n: 500, def: 20, max: 50, want: 50},
	}
	for _, tc := range tests {
		if got := clampMaxResults(tc.n, tc.def, tc.max); got != tc.want {
			t.Fatalf("clampMaxResults(%d,%d,%d)=%d want %d", tc.n, tc.def, tc.max, got, tc.want)
		}
	}
}

func TestEntityPath(t *testing.T) {
	p := "internal/server/handlers.go"
	e := &models.Entity{Path: &p}
	if got := entityPath(e); got != p {
		t.Fatalf("entityPath() = %q, want %q", got, p)
	}
	if got := entityPath(&models.Entity{}); got != "" {
		t.Fatalf("entityPath(nil path) = %q, want empty", got)
	}
}

func TestToEntitySummary(t *testing.T) {
	path := "internal/mcp/server.go"
	summary := "server summary"
	sig := "func Handle(ctx)"
	ret := "error"
	start := 10
	end := 20

	e := &models.Entity{
		ID:            uuid.New(),
		Name:          "Handle",
		QualifiedName: "github.com/tgeorge06/atlaskb/internal/mcp.Handle",
		Kind:          models.EntityFunction,
		Path:          &path,
		Summary:       &summary,
		Signature:     &sig,
		TypeRef:       &ret,
		StartLine:     &start,
		EndLine:       &end,
		Capabilities:  []string{"parse", "validate"},
		Assumptions:   []string{"db connected"},
	}

	got := toEntitySummary(e)
	if got.Name != "Handle" || got.Kind != models.EntityFunction || got.Path != path {
		t.Fatalf("unexpected summary core fields: %+v", got)
	}
	if got.Summary != summary || got.Signature != sig || got.Returns != ret {
		t.Fatalf("unexpected summary text fields: %+v", got)
	}
	if got.StartLine == nil || *got.StartLine != start || got.EndLine == nil || *got.EndLine != end {
		t.Fatalf("unexpected line range: start=%v end=%v", got.StartLine, got.EndLine)
	}
}

func TestJSONResultAndErrorResult(t *testing.T) {
	ok := jsonResult(map[string]any{"status": "ok"})
	if ok == nil {
		t.Fatal("jsonResult() returned nil")
	}
	if ok.IsError {
		t.Fatalf("jsonResult() IsError = true, want false")
	}
	if len(ok.Content) != 1 {
		t.Fatalf("jsonResult() content len = %d, want 1", len(ok.Content))
	}
	text, castOK := ok.Content[0].(*gomcp.TextContent)
	if !castOK {
		t.Fatalf("jsonResult() content type = %T, want *mcp.TextContent", ok.Content[0])
	}
	if !strings.Contains(text.Text, `"status": "ok"`) {
		t.Fatalf("jsonResult() text = %q, missing status", text.Text)
	}

	errRes := errorResult("something failed")
	if errRes == nil || !errRes.IsError {
		t.Fatal("errorResult() should return IsError=true")
	}
	if len(errRes.Content) != 1 {
		t.Fatalf("errorResult content len = %d, want 1", len(errRes.Content))
	}
	errText, castOK := errRes.Content[0].(*gomcp.TextContent)
	if !castOK {
		t.Fatalf("errorResult content type = %T", errRes.Content[0])
	}
	if errText.Text != "something failed" {
		t.Fatalf("errorResult text = %q, want %q", errText.Text, "something failed")
	}

	// json marshalling failure should fall back to errorResult.
	bad := jsonResult(map[string]any{"bad": func() {}})
	if !bad.IsError {
		t.Fatal("jsonResult(bad) should return an error result")
	}
	if len(bad.Content) != 1 {
		t.Fatalf("jsonResult(bad) content len = %d, want 1", len(bad.Content))
	}
	badText, castOK := bad.Content[0].(*gomcp.TextContent)
	if !castOK {
		t.Fatalf("jsonResult(bad) content type = %T", bad.Content[0])
	}
	if !strings.Contains(badText.Text, "marshaling result") {
		t.Fatalf("jsonResult(bad) text = %q, want marshalling error", badText.Text)
	}
}

func newUnreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://atlaskb:atlaskb@127.0.0.1:1/atlaskb?sslmode=disable")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestRegisterTools(t *testing.T) {
	srv := gomcp.NewServer(&gomcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	RegisterTools(srv, nil, nil)
}

func TestRunCanceledContext(t *testing.T) {
	s := NewServer(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.Run(ctx)
	}()

	select {
	case <-time.After(2 * time.Second):
		t.Fatalf("Run should return promptly on canceled context")
	case <-done:
	}
}

func TestMCPStoreHelpersWithUnreachablePool(t *testing.T) {
	s := &Server{pool: newUnreachablePool(t)}

	entities, err := s.batchGetEntities(context.Background(), nil)
	if err != nil {
		t.Fatalf("batchGetEntities(empty) error: %v", err)
	}
	if len(entities) != 0 {
		t.Fatalf("batchGetEntities(empty) len=%d, want 0", len(entities))
	}

	_, err = s.batchGetEntities(context.Background(), []uuid.UUID{uuid.New()})
	if err == nil {
		t.Fatal("batchGetEntities(non-empty) expected error on unreachable pool")
	}

	_, err = s.resolveRepo(context.Background(), "repo")
	if err == nil {
		t.Fatal("resolveRepo expected error on unreachable pool")
	}

	_, err = s.resolveEntity(context.Background(), uuid.New(), "path")
	if err == nil {
		t.Fatal("resolveEntity expected error on unreachable pool")
	}
}

func TestLookupRepoNameCacheAndMiss(t *testing.T) {
	id := uuid.New()
	cache := map[uuid.UUID]string{id: "repo-a"}
	name := lookupRepoName(context.Background(), &models.RepoStore{Pool: nil}, cache, id)
	if name != "repo-a" {
		t.Fatalf("lookupRepoName cache hit = %q, want repo-a", name)
	}

	id2 := uuid.New()
	store := &models.RepoStore{Pool: newUnreachablePool(t)}
	name = lookupRepoName(context.Background(), store, cache, id2)
	if name != "" {
		t.Fatalf("lookupRepoName miss = %q, want empty", name)
	}
	if _, ok := cache[id2]; !ok {
		t.Fatal("lookupRepoName should cache miss as empty string")
	}
}

func TestMCPHandlersErrorWithUnreachablePool(t *testing.T) {
	s := &Server{pool: newUnreachablePool(t)}
	ctx := context.Background()

	res, _, _ := s.handleListRepos(ctx, nil, listReposInput{})
	if !res.IsError {
		t.Fatal("handleListRepos expected error result on unreachable pool")
	}

	res, _, _ = s.handleGetConventions(ctx, nil, getConventionsInput{})
	if !res.IsError {
		t.Fatal("handleGetConventions expected error result on unreachable pool")
	}

	res, _, _ = s.handleSearchEntities(ctx, nil, searchEntitiesInput{Query: "x"})
	if !res.IsError {
		t.Fatal("handleSearchEntities expected error result on unreachable pool")
	}
}

func TestBuildTransitivePaths(t *testing.T) {
	seed := uuid.New()
	mid := uuid.New()
	leaf := uuid.New()

	pathSeed := "seed.go"
	pathMid := "mid.go"
	pathLeaf := "leaf.go"

	sg := &models.Subgraph{
		Entities: map[uuid.UUID]models.Entity{
			seed: {ID: seed, Name: "Seed", Kind: models.EntityFunction, Path: &pathSeed},
			mid:  {ID: mid, Name: "Mid", Kind: models.EntityFunction, Path: &pathMid},
			leaf: {ID: leaf, Name: "Leaf", Kind: models.EntityFunction, Path: &pathLeaf},
		},
		Relationships: []models.Relationship{
			{FromEntityID: seed, ToEntityID: mid, Kind: models.RelCalls},
			{FromEntityID: mid, ToEntityID: leaf, Kind: models.RelDependsOn},
		},
		Depths: map[uuid.UUID]int{
			seed: 0,
			mid:  1,
			leaf: 2,
		},
	}

	paths := buildTransitivePaths(seed, sg)
	if len(paths) != 1 {
		t.Fatalf("len(paths)=%d, want 1", len(paths))
	}
	if len(paths[0].Path) != 3 {
		t.Fatalf("path length=%d, want 3", len(paths[0].Path))
	}
	if paths[0].Path[0].Name != "Seed" || paths[0].Path[2].Name != "Leaf" {
		t.Fatalf("unexpected path nodes: %+v", paths[0].Path)
	}
}
