package mcp

import (
	"context"
	"strings"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func assertErrorResultContains(t *testing.T, res *gomcp.CallToolResult, want string) {
	t.Helper()
	if res == nil {
		t.Fatalf("expected non-nil result containing %q", want)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for %q", want)
	}
	if len(res.Content) == 0 {
		t.Fatalf("expected error content for %q", want)
	}
	text, ok := res.Content[0].(*gomcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", res.Content[0])
	}
	if !strings.Contains(text.Text, want) {
		t.Fatalf("error text = %q, want %q", text.Text, want)
	}
}

func TestMCPHandleSearchGuards(t *testing.T) {
	s := &Server{}
	res, _, err := s.handleSearch(context.Background(), nil, searchInput{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	assertErrorResultContains(t, res, "query parameter is required")

	res, _, err = s.handleSearch(context.Background(), nil, searchInput{Query: "q", Mode: "bad"})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	assertErrorResultContains(t, res, `invalid mode "bad"`)
}

func TestMCPHandleGetModuleContextGuards(t *testing.T) {
	s := &Server{}

	res, _, _ := s.handleGetModuleContext(context.Background(), nil, getModuleContextInput{})
	assertErrorResultContains(t, res, "repo parameter is required")

	res, _, _ = s.handleGetModuleContext(context.Background(), nil, getModuleContextInput{Repo: "r"})
	assertErrorResultContains(t, res, "path parameter is required")

	res, _, _ = s.handleGetModuleContext(context.Background(), nil, getModuleContextInput{
		Repo:  "r",
		Path:  "p",
		Depth: "invalid",
	})
	assertErrorResultContains(t, res, `invalid depth "invalid"`)
}

func TestMCPHandleGetServiceContractGuards(t *testing.T) {
	s := &Server{}

	res, _, _ := s.handleGetServiceContract(context.Background(), nil, getServiceContractInput{})
	assertErrorResultContains(t, res, "repo parameter is required")

	res, _, _ = s.handleGetServiceContract(context.Background(), nil, getServiceContractInput{Repo: "r"})
	assertErrorResultContains(t, res, "path parameter is required")
}

func TestMCPHandleGetImpactAnalysisGuards(t *testing.T) {
	s := &Server{}

	res, _, _ := s.handleGetImpactAnalysis(context.Background(), nil, getImpactAnalysisInput{})
	assertErrorResultContains(t, res, "repo parameter is required")

	res, _, _ = s.handleGetImpactAnalysis(context.Background(), nil, getImpactAnalysisInput{Repo: "r"})
	assertErrorResultContains(t, res, "path parameter is required")
}

func TestMCPHandleGetDecisionContextGuards(t *testing.T) {
	s := &Server{}

	res, _, _ := s.handleGetDecisionContext(context.Background(), nil, getDecisionContextInput{})
	assertErrorResultContains(t, res, "repo parameter is required")

	res, _, _ = s.handleGetDecisionContext(context.Background(), nil, getDecisionContextInput{Repo: "r"})
	assertErrorResultContains(t, res, "path parameter is required")
}

func TestMCPHandleGetTaskContextGuards(t *testing.T) {
	s := &Server{}

	res, _, _ := s.handleGetTaskContext(context.Background(), nil, getTaskContextInput{})
	assertErrorResultContains(t, res, "repo parameter is required")

	res, _, _ = s.handleGetTaskContext(context.Background(), nil, getTaskContextInput{Repo: "r"})
	assertErrorResultContains(t, res, "files parameter is required")

	tooMany := make([]string, 21)
	res, _, _ = s.handleGetTaskContext(context.Background(), nil, getTaskContextInput{Repo: "r", Files: tooMany})
	assertErrorResultContains(t, res, "too many files")

	res, _, _ = s.handleGetTaskContext(context.Background(), nil, getTaskContextInput{
		Repo:  "r",
		Files: []string{"a.go"},
		Depth: "invalid",
	})
	assertErrorResultContains(t, res, `invalid depth "invalid"`)
}

func TestMCPHandleExecutionFlowsGuards(t *testing.T) {
	s := &Server{}
	res, _, _ := s.handleGetExecutionFlows(context.Background(), nil, getExecutionFlowsInput{})
	assertErrorResultContains(t, res, "repo is required")
}

func TestMCPHandleFunctionalClustersGuards(t *testing.T) {
	s := &Server{}
	res, _, _ := s.handleGetFunctionalClusters(context.Background(), nil, getFunctionalClustersInput{})
	assertErrorResultContains(t, res, "repo parameter is required")
}

func TestMCPHandleRepoOverviewGuards(t *testing.T) {
	s := &Server{}
	res, _, _ := s.handleGetRepoOverview(context.Background(), nil, getRepoOverviewInput{})
	assertErrorResultContains(t, res, "repo parameter is required")
}

func TestMCPHandleGetEntitySourceGuards(t *testing.T) {
	s := &Server{}

	res, _, _ := s.handleGetEntitySource(context.Background(), nil, getEntitySourceInput{})
	assertErrorResultContains(t, res, "repo is required")

	res, _, _ = s.handleGetEntitySource(context.Background(), nil, getEntitySourceInput{Repo: "r"})
	assertErrorResultContains(t, res, "path is required")
}

func TestMCPHandleSubmitFactFeedbackGuards(t *testing.T) {
	s := &Server{}

	res, _, _ := s.handleSubmitFactFeedback(context.Background(), nil, submitFactFeedbackInput{})
	assertErrorResultContains(t, res, "fact_id is required")

	res, _, _ = s.handleSubmitFactFeedback(context.Background(), nil, submitFactFeedbackInput{FactID: "not-a-uuid", Reason: "r"})
	assertErrorResultContains(t, res, "fact_id must be a valid UUID")

	res, _, _ = s.handleSubmitFactFeedback(context.Background(), nil, submitFactFeedbackInput{FactID: "550e8400-e29b-41d4-a716-446655440000"})
	assertErrorResultContains(t, res, "reason is required")
}
