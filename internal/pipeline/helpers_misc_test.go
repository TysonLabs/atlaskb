package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func newUnreachablePipelinePool(t *testing.T) *pgxpool.Pool {
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

func TestCrossRepoHelpers(t *testing.T) {
	if got := normalizeDependencyName("github.com/TGeorge06/AtlasKB"); got != "atlaskb" {
		t.Fatalf("normalizeDependencyName(go module)=%q, want atlaskb", got)
	}
	if got := normalizeDependencyName("@scope/my-lib"); got != "my-lib" {
		t.Fatalf("normalizeDependencyName(npm scope)=%q, want my-lib", got)
	}

	targetRepo := models.Repo{Name: "vector-ivr-core"}
	idx := map[string]*repoMatch{
		"vector-ivr-core": {repo: targetRepo},
		"other-service":   {repo: models.Repo{Name: "other-service"}},
	}
	m := fuzzyMatchRepo("vector-ivr-core-lib", idx)
	if m == nil || m.repo.Name != "vector-ivr-core" {
		t.Fatalf("fuzzyMatchRepo did not choose best match")
	}
	if fuzzyMatchRepo("zzz", idx) != nil {
		t.Fatalf("fuzzyMatchRepo expected nil for no match")
	}

	if got := extractURLRepoName("git@github.com:tgeorge06/atlaskb.git"); got != "atlaskb" {
		t.Fatalf("extractURLRepoName(ssh)=%q, want atlaskb", got)
	}
	if got := extractURLRepoName("https://github.com/tgeorge06/atlaskb.git"); got != "atlaskb" {
		t.Fatalf("extractURLRepoName(https)=%q, want atlaskb", got)
	}

	created, skipped := DiscoverCrossRepoLinks(context.Background(), newUnreachablePipelinePool(t), uuid.New(), "repo-a", []Dependency{
		{Name: "repo-b", Source: "go.mod"},
	})
	if created != 0 || skipped != 0 {
		t.Fatalf("DiscoverCrossRepoLinks on DB error should return 0/0, got %d/%d", created, skipped)
	}
}

func TestClusterLabelingHelpers(t *testing.T) {
	members := []models.Entity{
		{Name: "UserService", QualifiedName: "auth::UserService", Kind: models.EntityService},
		{Name: "UserHandler", QualifiedName: "auth::UserHandler", Kind: models.EntityFunction},
		{Name: "UserStore", QualifiedName: "auth::UserStore", Kind: models.EntityType},
	}

	label := KeywordLabelCluster(members)
	if label == nil || label.Label == "" {
		t.Fatalf("KeywordLabelCluster returned empty label")
	}

	empty := KeywordLabelCluster([]models.Entity{{Name: "get"}, {Name: "set"}})
	if empty.Label != "misc" {
		t.Fatalf("KeywordLabelCluster(no keywords) label=%q, want misc", empty.Label)
	}

	tokens := tokenizeName("HTTPServer_User-Handler")
	if len(tokens) == 0 {
		t.Fatalf("tokenizeName returned no tokens")
	}
	if out := splitCamelCase("UserIDParser"); len(out) < 2 {
		t.Fatalf("splitCamelCase did not split as expected: %v", out)
	}
	if splitCamelCase("") != nil {
		t.Fatalf("splitCamelCase(empty) expected nil")
	}

	clientOK := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{Content: `{"label":"auth-core","description":"Auth cluster","domain":"auth"}`}, nil
		},
	}
	got, err := LabelCluster(context.Background(), clientOK, "model", members)
	if err != nil {
		t.Fatalf("LabelCluster(valid) error: %v", err)
	}
	if got.Label != "auth-core" {
		t.Fatalf("LabelCluster label=%q, want auth-core", got.Label)
	}

	clientBad := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{Content: `not-json`}, nil
		},
	}
	if _, err := LabelCluster(context.Background(), clientBad, "model", members); err == nil {
		t.Fatalf("LabelCluster(invalid json) expected error")
	}

	clientEmpty := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{Content: `{"label":"","description":"x","domain":"y"}`}, nil
		},
	}
	if _, err := LabelCluster(context.Background(), clientEmpty, "model", members); err == nil {
		t.Fatalf("LabelCluster(empty label) expected error")
	}

	clientErr := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return nil, errors.New("llm unavailable")
		},
	}
	if _, err := LabelCluster(context.Background(), clientErr, "model", members); err == nil {
		t.Fatalf("LabelCluster(llm error) expected error")
	}
}

func TestSuffixIndexHelpers(t *testing.T) {
	roster := []EntityEntry{
		{Name: "Foo", QualifiedName: "pkg::Foo"},
		{Name: "Bar", QualifiedName: "pkg::Bar"},
		{Name: "Foo", QualifiedName: "other::Foo"},
		{Name: "Method", QualifiedName: "pkg::Store.Method"},
		{Name: "Store", QualifiedName: "pkg::Store"},
	}
	idx := BuildSuffixIndex(roster)
	if idx == nil {
		t.Fatal("BuildSuffixIndex returned nil")
	}

	if qn, conf, ok := idx.Resolve("pkg::Foo", ""); !ok || qn != "pkg::Foo" || conf != "high" {
		t.Fatalf("Resolve exact failed: %q %q %v", qn, conf, ok)
	}
	if qn, conf, ok := idx.Resolve("Method", "pkg"); !ok || qn == "" || conf == "" {
		t.Fatalf("Resolve short-name failed: %q %q %v", qn, conf, ok)
	}
	if qn, conf, ok := idx.Resolve("Store.Method", "pkg"); !ok || qn != "pkg::Store.Method" || conf != "high" {
		t.Fatalf("Resolve receiver-method failed: %q %q %v", qn, conf, ok)
	}
	if _, _, ok := idx.Resolve("Unknown", "pkg"); ok {
		t.Fatalf("Resolve unknown should fail")
	}

	if extractPackage("pkg::Foo") != "pkg" {
		t.Fatalf("extractPackage failed for qualified name")
	}
	if extractPackage("Foo") != "" {
		t.Fatalf("extractPackage without separator should be empty")
	}
}

func TestCtagsRosterHelpers(t *testing.T) {
	symTop := CtagsSymbol{Name: "Handler", Path: "internal/server/handlers.go", Line: 10, Kind: "type"}
	symMethod := CtagsSymbol{Name: "Handle", Scope: "Handler", ScopeKind: "type", Path: "internal/server/handlers.go", Line: 20, Kind: "function"}
	if got := buildQualifiedName(symTop); got != "server::Handler" {
		t.Fatalf("buildQualifiedName(top)=%q, want server::Handler", got)
	}
	if got := buildQualifiedName(symMethod); got != "server::Handler.Handle" {
		t.Fatalf("buildQualifiedName(method)=%q, want server::Handler.Handle", got)
	}

	if got := deriveModuleName("src/channels/registry.ts"); got != "channels" {
		t.Fatalf("deriveModuleName(src/channels)= %q, want channels", got)
	}
	if got := deriveModuleName("internal/storage/memory.go"); got != "storage" {
		t.Fatalf("deriveModuleName(internal/storage)= %q, want storage", got)
	}
	if got := deriveModuleName("cmd/api/server/main.go"); got != "api.server" {
		t.Fatalf("deriveModuleName(cmd/api/server)= %q, want api.server", got)
	}

	roster := BuildEntityRoster(map[string][]CtagsSymbol{
		"internal/server/handlers.go": {
			{Name: "B", Path: "internal/server/handlers.go", Line: 30, Kind: "function"},
			{Name: "A", Path: "internal/server/handlers.go", Line: 10, Kind: "type"},
		},
	})
	if len(roster) != 2 {
		t.Fatalf("BuildEntityRoster len=%d, want 2", len(roster))
	}
	if roster[0].Line > roster[1].Line {
		t.Fatalf("BuildEntityRoster should sort by path/line")
	}

	ends := ComputeEndLines(roster)
	if len(ends) == 0 {
		t.Fatalf("ComputeEndLines expected at least one computed end line")
	}

	prompt := FormatRosterForPrompt(roster, "internal/server/handlers.go")
	if !strings.Contains(prompt, "Known Entities") || !strings.Contains(prompt, "Entities in THIS File") {
		t.Fatalf("FormatRosterForPrompt missing expected sections")
	}
}

func TestQualityAndPromptHelpers(t *testing.T) {
	qs := &QualityScore{
		Overall:            88.3,
		EntityCoverage:     90,
		FactDensity:        80,
		RelConnectivity:    70,
		DimensionCoverage:  100,
		ParseSuccessRate:   95,
		EntityCount:        10,
		FactCount:          20,
		RelationshipCount:  30,
		DecisionCount:      2,
		ExternalDepCount:   1,
		NonDepEntities:     9,
		EntitiesWithFacts:  8,
		EntitiesWithRels:   7,
		DimensionBreakdown: map[string]int{"what": 1, "how": 2, "why": 3, "when": 4},
		EntityKindBreakdown: map[string]int{
			"function": 6,
			"type":     4,
		},
		TotalJobs:      10,
		SuccessfulJobs: 9,
	}
	if got := FormatQualityScore(qs); !strings.Contains(got, "Quality score:") {
		t.Fatalf("FormatQualityScore missing prefix: %s", got)
	}
	if got := FormatQualityDetails(qs); !strings.Contains(got, "Quality Score Breakdown") {
		t.Fatalf("FormatQualityDetails missing header")
	}

	_, err := ComputeQuality(context.Background(), newUnreachablePipelinePool(t), uuid.New())
	if err == nil {
		t.Fatalf("ComputeQuality expected DB error on unreachable pool")
	}

	stack := StackInfo{Languages: []string{"go"}}
	roster := []EntityEntry{{QualifiedName: "server::Handler", Kind: "type", Path: "internal/server/handlers.go", Line: 10}}
	if s := Phase2Prompt("internal/server/handlers.go", "go", "atlaskb", stack, "package server", roster); !strings.Contains(s, "Analyze this file") {
		t.Fatalf("Phase2Prompt missing expected body")
	}
	if s := Phase3Prompt("atlaskb", "PR #1", "server::Handler"); !strings.Contains(s, "Pull Requests") {
		t.Fatalf("Phase3Prompt missing expected body")
	}
	if s := Phase4Prompt("atlaskb", "module summaries"); !strings.Contains(s, "architectural_patterns") {
		t.Fatalf("Phase4Prompt missing expected schema")
	}
	if s := Phase5Prompt("atlaskb", "entities", "facts", "decisions"); !strings.Contains(s, "comprehensive summary") {
		t.Fatalf("Phase5Prompt missing expected text")
	}
	if s := GitLogPrompt("atlaskb", "commit log"); !strings.Contains(s, "git history") {
		t.Fatalf("GitLogPrompt missing expected text")
	}
}

func TestDirectoryExcludesAndFastFuzzyMatch(t *testing.T) {
	es := &ExclusionSet{directoryPrefixes: []string{"vendor", "dist"}}
	dirs := es.DirectoryExcludes()
	if len(dirs) != 2 {
		t.Fatalf("DirectoryExcludes len=%d, want 2", len(dirs))
	}
	dirs[0] = "mutated"
	if es.directoryPrefixes[0] == "mutated" {
		t.Fatalf("DirectoryExcludes should return a copy")
	}
	if ((*ExclusionSet)(nil)).DirectoryExcludes() != nil {
		t.Fatalf("nil ExclusionSet DirectoryExcludes should return nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	repoID := uuid.New()
	entityStore := &models.EntityStore{Pool: newUnreachablePipelinePool(t)}
	id, ok := FastFuzzyMatch(ctx, entityStore, repoID, ExtractedEntity{
		Name:          "UserService",
		Kind:          models.EntityService,
		QualifiedName: "auth::UserService",
	})
	if ok || id != uuid.Nil {
		t.Fatalf("FastFuzzyMatch on DB error should return no match")
	}

	if got := qualifiedNamePackage("storage::TaskStore.Create"); got != "storage" {
		t.Fatalf("qualifiedNamePackage=%q, want storage", got)
	}
	if got := qualifiedNameOwner("storage::TaskStore.Create"); got != "storage::TaskStore" {
		t.Fatalf("qualifiedNameOwner=%q, want storage::TaskStore", got)
	}
}
