package mcp

import "testing"

func TestContractToolNames(t *testing.T) {
	got := ToolNames()
	want := []string{
		"search_knowledge_base",
		"list_repos",
		"get_conventions",
		"get_module_context",
		"get_service_contract",
		"get_impact_analysis",
		"get_decision_context",
		"get_task_context",
		"get_execution_flows",
		"get_functional_clusters",
		"get_repo_overview",
		"search_entities",
		"get_entity_source",
		"submit_fact_feedback",
	}

	if len(got) != len(want) {
		t.Fatalf("tool count mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool name mismatch at index %d: got %q, want %q", i, got[i], want[i])
		}
	}

	seen := map[string]bool{}
	for _, name := range got {
		if name == "" {
			t.Fatal("tool name must not be empty")
		}
		if seen[name] {
			t.Fatalf("duplicate tool name: %q", name)
		}
		seen[name] = true
	}
}
