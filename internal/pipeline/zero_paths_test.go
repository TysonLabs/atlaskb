package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	sitter "github.com/smacker/go-tree-sitter"
	gitpkg "github.com/tgeorge06/atlaskb/internal/git"
	ghpkg "github.com/tgeorge06/atlaskb/internal/github"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func TestRetryAndOrchestratorHelpers(t *testing.T) {
	attempts := 0
	client := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			attempts++
			if attempts < 2 {
				return nil, errors.New("temporary failure")
			}
			return &llm.Response{Content: "{}"}, nil
		},
	}

	resp, used, err := callLLMWithRetry(context.Background(), client, "m", "sys", nil, 64, nil, LLMRetryConfig{
		MaxRetries: 3,
		BaseDelay:  time.Millisecond,
	})
	if err != nil || resp == nil || used != 2 {
		t.Fatalf("callLLMWithRetry expected success on retry, resp=%v used=%d err=%v", resp, used, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = callLLMWithRetry(ctx, &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return nil, errors.New("always fail")
		},
	}, "m", "sys", nil, 64, nil, LLMRetryConfig{MaxRetries: 2, BaseDelay: time.Millisecond})
	if err == nil {
		t.Fatalf("callLLMWithRetry with canceled context should error")
	}

	cbCalls := 0
	oc := OrchestratorConfig{ProgressFunc: func(msg string) { cbCalls++ }}
	oc.progress("hello")
	if cbCalls != 1 {
		t.Fatalf("OrchestratorConfig.progress did not invoke callback")
	}

	if !shouldRunPhase(nil, "phase2") {
		t.Fatalf("shouldRunPhase(nil) should be true")
	}
	if shouldRunPhase([]string{"phase4"}, "phase2") {
		t.Fatalf("shouldRunPhase should be false for missing phase")
	}
	if !shouldRunPhase([]string{"phase2", "phase4"}, "phase2") {
		t.Fatalf("shouldRunPhase should be true for included phase")
	}

	if got := computeMaxContentBytes(8192, 1024, 2048); got <= 0 {
		t.Fatalf("computeMaxContentBytes returned non-positive value: %d", got)
	}
	if got := computeOutputBudget(100, 8192); got <= 0 {
		t.Fatalf("computeOutputBudget returned non-positive value: %d", got)
	}
	if got := truncStr("abcdef", 3); got != "abc..." {
		t.Fatalf("truncStr=%q, want abc...", got)
	}

	if got := extractRepoName(&gitpkg.RepoInfo{RemoteURL: "git@github.com:tgeorge06/atlaskb.git"}); got != "atlaskb" {
		t.Fatalf("extractRepoName(remote)=%q, want atlaskb", got)
	}
	if got := extractRepoName(&gitpkg.RepoInfo{RootPath: "/tmp/atlaskb"}); got != "atlaskb" {
		t.Fatalf("extractRepoName(path)=%q, want atlaskb", got)
	}
	parts := splitPath("/a/b/c")
	if len(parts) != 3 || parts[2] != "c" {
		t.Fatalf("splitPath unexpected output: %v", parts)
	}

	phase3Calls := 0
	p3 := Phase3Config{ProgressFunc: func(msg string) { phase3Calls++ }}
	p3.progress("phase3")
	if phase3Calls != 1 {
		t.Fatalf("Phase3Config.progress did not invoke callback")
	}
}

func TestPhaseAndPromptHelperFunctions(t *testing.T) {
	if confidenceToStrength("high") != models.StrengthStrong {
		t.Fatalf("confidenceToStrength(high) mismatch")
	}
	if confidenceToStrength("moderate") != models.StrengthModerate {
		t.Fatalf("confidenceToStrength(moderate) mismatch")
	}
	if confidenceToStrength("low") != models.StrengthWeak {
		t.Fatalf("confidenceToStrength(low) mismatch")
	}

	if got := backfillPrompt("a.go", "go", "package a", []string{"a::Foo"}); !strings.Contains(got, "ENTITIES NEEDING FACTS") {
		t.Fatalf("backfillPrompt missing expected section")
	}
	var parsed map[string]any
	if err := parseJSON(`{"ok":true}`, &parsed); err != nil {
		t.Fatalf("parseJSON valid payload error: %v", err)
	}
	if err := parseJSON(`{`, &parsed); err == nil {
		t.Fatalf("parseJSON invalid payload should error")
	}

	now := time.Now()
	prText := formatPRBatch([]ghpkg.PR{{
		Number:   1,
		Title:    "Add feature",
		Author:   "dev",
		MergedAt: now,
		Body:     strings.Repeat("x", 2200),
		Labels:   []string{"feature"},
		ReviewComments: []ghpkg.ReviewComment{
			{Author: "reviewer", Body: strings.Repeat("y", 600), State: "COMMENTED"},
		},
		LinkedIssues: []ghpkg.Issue{
			{Number: 2, Title: "Issue", Body: strings.Repeat("z", 600)},
		},
	}})
	if !strings.Contains(prText, "PR #1") || !strings.Contains(prText, "Review comments") {
		t.Fatalf("formatPRBatch missing expected content")
	}
}

func TestImportsResolveAndFlowHelpers(t *testing.T) {
	tmp := t.TempDir()
	src := `package sample
import (
  "fmt"
  ioalias "io"
)
func Handle(){ fmt.Println(ioalias.EOF) }`
	if err := os.WriteFile(filepath.Join(tmp, "sample.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write sample.go: %v", err)
	}

	files := []FileInfo{{Path: "sample.go"}, {Path: "README.md"}}
	goFiles := filterGoFiles(files)
	if len(goFiles) != 1 || goFiles[0].Path != "sample.go" {
		t.Fatalf("filterGoFiles unexpected output: %+v", goFiles)
	}
	imports := ExtractGoImports(tmp, goFiles)
	if len(imports) < 2 {
		t.Fatalf("ExtractGoImports expected imports, got %d", len(imports))
	}

	// Early-return path should not touch DB.
	if created := StoreImportRelationships(context.Background(), nil, uuid.New(), "repo", nil, nil); created != 0 {
		t.Fatalf("StoreImportRelationships(empty)=%d, want 0", created)
	}

	alts := resolveNameAlternatives("src::Store::Create")
	if len(alts) == 0 {
		t.Fatalf("resolveNameAlternatives should produce alternatives")
	}

	entityMap := map[string]uuid.UUID{
		"pkg::Store.Create": uuid.New(),
		"pkg::Store":        uuid.New(),
	}
	if id, ok := resolveEntityWithMap(context.Background(), &models.EntityStore{Pool: newUnreachablePipelinePool(t)}, uuid.New(), "pkg::Store::Create", entityMap); !ok || id == uuid.Nil {
		t.Fatalf("resolveEntityWithMap should resolve from local map")
	}
	if _, ok := resolveEntity(context.Background(), &models.EntityStore{Pool: newUnreachablePipelinePool(t)}, uuid.New(), "UnknownEntity"); ok {
		t.Fatalf("resolveEntity should fail on unknown + DB error")
	}

	sorted := sortEntitiesByRichness(context.Background(),
		[]models.Entity{{ID: uuid.New(), Name: "a"}, {ID: uuid.New(), Name: "b"}},
		&models.FactStore{Pool: newUnreachablePipelinePool(t)},
		&models.RelationshipStore{Pool: newUnreachablePipelinePool(t)},
	)
	if len(sorted) != 2 {
		t.Fatalf("sortEntitiesByRichness len=%d, want 2", len(sorted))
	}

	if got := buildFlowLabel([]string{"A", "B", "C"}); !strings.Contains(got, "A") {
		t.Fatalf("buildFlowLabel unexpected output: %s", got)
	}
	if got := buildFlowLabel(nil); got != "" {
		t.Fatalf("buildFlowLabel(nil)=%q, want empty", got)
	}

	g := NewGraph(4)
	a := uuid.New()
	b := uuid.New()
	g.AddNode(a)
	g.AddNode(b)
	g.AddEdge(a, b, 1.0)
	cohesion := computeCohesion(g, []models.Entity{{ID: a}, {ID: b}})
	if cohesion <= 0 {
		t.Fatalf("computeCohesion should be positive, got %f", cohesion)
	}
}

func TestPhaseRunErrorPaths(t *testing.T) {
	ctx := context.Background()
	pool := newUnreachablePipelinePool(t)
	repoID := uuid.New()

	// Most phase runners should fail quickly on DB-unreachable pools.
	if err := RunPhase3(ctx, Phase3Config{RepoID: repoID, RepoName: "repo", RemoteURL: "", Pool: pool, LLM: &llm.MockClient{}, GitHub: nil}); err != nil {
		t.Fatalf("RunPhase3 with nil GitHub should skip without error: %v", err)
	}
	if _, err := RunPhase6(ctx, Phase6Config{RepoID: repoID, RepoName: "repo", Pool: pool, LLM: &llm.MockClient{}}); err == nil {
		t.Fatalf("RunPhase6 expected error on DB-unreachable pool")
	}
	if _, err := RunPhaseFlows(ctx, FlowsConfig{RepoID: repoID, RepoName: "repo", Pool: pool}); err == nil {
		t.Fatalf("RunPhaseFlows expected error on DB-unreachable pool")
	}
	if _, err := findEntryPoints(ctx, pool, repoID); err == nil {
		t.Fatalf("findEntryPoints expected query error")
	}
	if _, err := traceBFS(ctx, pool, uuid.New(), repoID); err == nil {
		t.Fatalf("traceBFS expected query error")
	}

	if err := RunPhase4(ctx, Phase4Config{RepoID: repoID, RepoName: "repo", Pool: pool, LLM: &llm.MockClient{}}); err == nil {
		t.Fatalf("RunPhase4 expected error on DB-unreachable pool")
	}
	if err := RunPhase5(ctx, Phase5Config{RepoID: repoID, RepoName: "repo", Pool: pool, LLM: &llm.MockClient{}}); err == nil {
		t.Fatalf("RunPhase5 expected error on DB-unreachable pool")
	}
	if err := RunGitLogAnalysis(ctx, GitLogConfig{RepoID: repoID, RepoName: "repo", RepoPath: "/does/not/exist", Pool: pool, LLM: &llm.MockClient{}}); err == nil {
		t.Fatalf("RunGitLogAnalysis expected error on DB-unreachable pool")
	}
	if _, err := RunBackfill(ctx, BackfillConfig{RepoID: repoID, RepoName: "repo", RepoPath: "/does/not/exist", Pool: pool, LLM: &llm.MockClient{}}); err == nil {
		t.Fatalf("RunBackfill expected error on DB-unreachable pool")
	}

	// Orchestrate should fail early for non-repo path.
	if _, err := Orchestrate(ctx, OrchestratorConfig{RepoPath: t.TempDir(), Pool: pool, LLM: &llm.MockClient{}, Embedder: nil}); err == nil {
		t.Fatalf("Orchestrate expected detect-repo error")
	}
	if err := generateEmbeddings(ctx, pool, nil, repoID); err == nil {
		t.Fatalf("generateEmbeddings expected error on nil embedder + DB error")
	}

	// RunPhase2 should error during job claiming on unreachable DB.
	m := &Manifest{
		RepoPath: t.TempDir(),
		Files:    []FileInfo{{Path: "a.go", Class: ClassSource, Language: "go"}},
	}
	if _, err := RunPhase2(ctx, Phase2Config{
		RepoID:      repoID,
		RepoName:    "repo",
		RepoPath:    m.RepoPath,
		Manifest:    m,
		Model:       "model",
		Concurrency: 1,
		Pool:        pool,
		LLM:         &llm.MockClient{},
	}); err == nil {
		t.Fatalf("RunPhase2 expected error on unreachable DB")
	}

	// processFile should execute and eventually fail when completing the job on unreachable DB.
	filePath := filepath.Join(m.RepoPath, "file.go")
	if err := os.WriteFile(filePath, []byte("package p\nfunc A(){}\n"), 0o644); err != nil {
		t.Fatalf("write file.go: %v", err)
	}
	job := &models.ExtractionJob{ID: uuid.New(), Target: "file.go"}
	_, err := processFile(ctx, Phase2Config{
		RepoID:      repoID,
		RepoName:    "repo",
		RepoPath:    m.RepoPath,
		Manifest:    &Manifest{Stack: StackInfo{Languages: []string{"go"}}},
		Model:       "model",
		Concurrency: 1,
		Pool:        pool,
		LLM: &llm.MockClient{
			CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
				return &llm.Response{
					Content:      `{"file_summary":"s","entities":[],"facts":[],"relationships":[]}`,
					Model:        model,
					InputTokens:  10,
					OutputTokens: 5,
					StopReason:   "end_turn",
				}, nil
			},
		},
	}, job, &models.EntityStore{Pool: pool}, &models.FactStore{Pool: pool}, &models.RelationshipStore{Pool: pool}, &Phase2Stats{})
	if err == nil {
		t.Fatalf("processFile expected completion/update error on unreachable DB")
	}
}

func TestTreeSitterExtractionHelpers(t *testing.T) {
	engine, err := NewTreeSitterEngine()
	if err != nil {
		t.Fatalf("NewTreeSitterEngine: %v", err)
	}
	defer engine.Close()

	source := []byte(`package sample
type Base struct{}
type Child struct{ Base }
func util() {}
func Handle() { util() }`)

	root, err := engine.ParseGo(context.Background(), source)
	if err != nil || root == nil {
		t.Fatalf("ParseGo error=%v root=%v", err, root)
	}

	tmp := t.TempDir()
	relPath := "sample.go"
	if err := os.WriteFile(filepath.Join(tmp, relPath), source, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	roster := []EntityEntry{
		{Name: "util", QualifiedName: "sample::util"},
		{Name: "Handle", QualifiedName: "sample::Handle"},
		{Name: "Base", QualifiedName: "sample::Base"},
		{Name: "Child", QualifiedName: "sample::Child"},
	}
	idx := BuildSuffixIndex(roster)

	imports := buildGoImportMap(tmp, relPath)
	if imports == nil {
		t.Fatalf("buildGoImportMap should return map")
	}

	calls := ExtractGoCalls(root, source, relPath, tmp, idx)
	if len(calls) == 0 {
		t.Fatalf("ExtractGoCalls expected at least one call")
	}

	embs := ExtractGoEmbeddings(root, source, relPath, tmp, idx)
	if len(embs) == 0 {
		t.Fatalf("ExtractGoEmbeddings expected at least one embedding")
	}

	// Cover helper branch by locating a method receiver node and extracting its type.
	methodSrc := []byte(`package p
type S struct{}
func (s *S) M() {}
`)
	methodRoot, err := engine.ParseGo(context.Background(), methodSrc)
	if err != nil {
		t.Fatalf("ParseGo(method source): %v", err)
	}
	var receiverNode *sitter.Node
	walkTree(methodRoot, func(node *sitter.Node) {
		if receiverNode != nil {
			return
		}
		if node.Type() == "method_declaration" {
			receiverNode = nodeChildByFieldName(node, "receiver")
		}
	})
	if receiverNode == nil {
		t.Fatalf("failed to locate receiver node")
	}
	if got := extractReceiverType(receiverNode, methodSrc); got != "S" {
		t.Fatalf("extractReceiverType=%q, want S", got)
	}
}

func TestCtagsOverviewAndPhase17ZeroPaths(t *testing.T) {
	// RunCtags: force missing ctags binary via empty PATH to hit graceful path.
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	defer os.Setenv("PATH", oldPath)
	syms, err := RunCtags(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("RunCtags expected nil error when ctags missing: %v", err)
	}
	if syms != nil {
		t.Fatalf("RunCtags expected nil symbols when ctags missing")
	}

	// GenerateOverview should return no-data error when stores return nothing.
	_, err = GenerateOverview(context.Background(), newUnreachablePipelinePool(t), uuid.New(), "repo")
	if err == nil {
		t.Fatalf("GenerateOverview expected no-data error")
	}

	// RunPhase17 should succeed with empty manifest/roster (no DB operations).
	stats, err := RunPhase17(context.Background(), Phase17Config{
		RepoID:   uuid.New(),
		RepoName: "repo",
		RepoPath: t.TempDir(),
		Manifest: &Manifest{Files: nil},
		Roster:   nil,
		Pool:     nil,
	})
	if err != nil {
		t.Fatalf("RunPhase17 empty-manifest error: %v", err)
	}
	if stats == nil {
		t.Fatalf("RunPhase17 expected stats object")
	}
}
