package pipeline

import "testing"

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"plain json",
			`{"key": "value"}`,
			`{"key": "value"}`,
		},
		{
			"with markdown fences",
			"```json\n{\"key\": \"value\"}\n```",
			`{"key": "value"}`,
		},
		{
			"with fences no lang",
			"```\n{\"key\": \"value\"}\n```",
			`{"key": "value"}`,
		},
		{
			"with leading text",
			"Here is the result:\n{\"key\": \"value\"}",
			`{"key": "value"}`,
		},
		{
			"with trailing text",
			"{\"key\": \"value\"}\n\nLet me know if you need more.",
			`{"key": "value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanJSON(tt.input)
			if got != tt.want {
				t.Errorf("CleanJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsePhase2(t *testing.T) {
	raw := `{
		"file_summary": "Handles user authentication",
		"entities": [
			{
				"kind": "function",
				"name": "Login",
				"qualified_name": "myapp::auth::Login",
				"summary": "Authenticates a user",
				"capabilities": ["authenticate users"],
				"assumptions": ["database is available"]
			}
		],
		"facts": [
			{
				"entity_name": "myapp::auth::Login",
				"claim": "Returns 401 on invalid credentials",
				"dimension": "what",
				"category": "behavior",
				"confidence": "high"
			}
		],
		"relationships": []
	}`

	result, err := ParsePhase2(raw)
	if err != nil {
		t.Fatalf("ParsePhase2: %v", err)
	}
	if result.FileSummary != "Handles user authentication" {
		t.Errorf("summary = %q", result.FileSummary)
	}
	if len(result.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(result.Entities))
	}
	if result.Entities[0].Name != "Login" {
		t.Errorf("entity name = %q", result.Entities[0].Name)
	}
	if len(result.Facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(result.Facts))
	}
}
