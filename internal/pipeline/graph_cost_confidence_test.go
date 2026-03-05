package pipeline

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func TestEstimateCostAndFormatCost(t *testing.T) {
	m := &Manifest{
		Files: []FileInfo{
			{Path: "main.go", Class: ClassSource, Size: 400},
			{Path: "go.mod", Class: ClassBuild, Size: 40},
			{Path: "main_test.go", Class: ClassTest, Size: 99999},
		},
	}

	est := EstimateCost(m)
	if est.Phase2Tokens != 3110 {
		t.Fatalf("Phase2Tokens = %d, want 3110", est.Phase2Tokens)
	}
	if est.Phase4Tokens != 311 {
		t.Fatalf("Phase4Tokens = %d, want 311", est.Phase4Tokens)
	}
	if est.Phase5Tokens != 5000 {
		t.Fatalf("Phase5Tokens = %d, want 5000", est.Phase5Tokens)
	}
	if est.TotalInputTokens != 8421 {
		t.Fatalf("TotalInputTokens = %d, want 8421", est.TotalInputTokens)
	}

	out := FormatCost(est)
	for _, want := range []string{"Estimated cost:", "Phase 2", "Phase 4", "Phase 5", "Total:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatCost() missing %q in output: %q", want, out)
		}
	}
}

func TestRelConfidence(t *testing.T) {
	if got := RelConfidence(models.ConfRelDeterministicAST, models.StrengthStrong); got != 1.0 {
		t.Fatalf("RelConfidence(strong) = %v, want 1.0", got)
	}
	if got := RelConfidence(0.01, models.StrengthWeak); got != 0.0 {
		t.Fatalf("RelConfidence(weak) = %v, want 0.0", got)
	}
	if got := RelConfidence(0.6, models.StrengthModerate); got != 0.6 {
		t.Fatalf("RelConfidence(moderate) = %v, want 0.6", got)
	}
}

func TestGraphAddNodeAddEdge(t *testing.T) {
	g := NewGraph(2)
	a := uuid.New()
	b := uuid.New()
	c := uuid.New()

	aIdx := g.AddNode(a)
	bIdx := g.AddNode(b)
	if aIdx == bIdx {
		t.Fatalf("expected distinct node indexes, got %d and %d", aIdx, bIdx)
	}
	if again := g.AddNode(a); again != aIdx {
		t.Fatalf("duplicate node index = %d, want %d", again, aIdx)
	}

	g.AddEdge(a, b, 2.0)
	if g.totalW != 2.0 {
		t.Fatalf("totalW = %v, want 2.0", g.totalW)
	}
	if g.adj[aIdx][bIdx] != 2.0 || g.adj[bIdx][aIdx] != 2.0 {
		t.Fatalf("edge weight mismatch: %v %v", g.adj[aIdx][bIdx], g.adj[bIdx][aIdx])
	}

	g.AddEdge(a, b, 1.5)
	if g.totalW != 3.5 {
		t.Fatalf("totalW after accumulate = %v, want 3.5", g.totalW)
	}

	// No-op cases.
	g.AddEdge(a, a, 1.0)
	g.AddEdge(a, c, 10.0)
	if g.totalW != 3.5 {
		t.Fatalf("totalW changed by no-op edge to %v, want 3.5", g.totalW)
	}
}

func TestLouvainDetect(t *testing.T) {
	g := NewGraph(4)
	a := uuid.New()
	b := uuid.New()
	c := uuid.New()
	d := uuid.New()
	aIdx := g.AddNode(a)
	bIdx := g.AddNode(b)
	cIdx := g.AddNode(c)
	dIdx := g.AddNode(d)

	// Two clear communities with a weak bridge.
	g.AddEdge(a, b, 8.0)
	g.AddEdge(c, d, 8.0)
	g.AddEdge(b, c, 0.1)

	res := g.louvainDetect(20, 1e-6)
	if res == nil {
		t.Fatal("louvainDetect() returned nil")
	}
	if res.NumCommunities < 2 {
		t.Fatalf("NumCommunities = %d, want at least 2", res.NumCommunities)
	}
	if res.Communities[aIdx] != res.Communities[bIdx] {
		t.Fatalf("a and b expected same community, got %d and %d", res.Communities[aIdx], res.Communities[bIdx])
	}
	if res.Communities[cIdx] != res.Communities[dIdx] {
		t.Fatalf("c and d expected same community, got %d and %d", res.Communities[cIdx], res.Communities[dIdx])
	}
}

func TestLouvainDetectEmptyAndNoEdges(t *testing.T) {
	empty := NewGraph(0)
	res := empty.louvainDetect(5, 1e-6)
	if res.NumCommunities != 0 || res.Modularity != 0 {
		t.Fatalf("empty graph result = %+v, want zeroed result", res)
	}

	noEdges := NewGraph(2)
	noEdges.AddNode(uuid.New())
	noEdges.AddNode(uuid.New())
	res = noEdges.louvainDetect(5, 1e-6)
	if res.NumCommunities != 0 || res.Modularity != 0 {
		t.Fatalf("no-edge graph result = %+v, want zeroed result", res)
	}
}

func TestComputeModularityFromAdjZero(t *testing.T) {
	adj := []map[int]float64{{}, {}}
	comm := []int{0, 1}
	if got := computeModularityFromAdj(adj, comm, 2, 0); got != 0 {
		t.Fatalf("computeModularityFromAdj(..., m2=0) = %v, want 0", got)
	}
}
