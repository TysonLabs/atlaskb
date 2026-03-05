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
			"trailing commas",
			`{"items": ["a", "b",], "x": 1,}`,
			`{"items": ["a", "b"], "x": 1}`,
		},
		{
			"unquoted keys",
			`{name: "test", count: 5}`,
			`{"name": "test", "count": 5}`,
		},
		{
			"truncated JSON gets closed",
			`{"key": "value"`,
			`{"key": "value"}`,
		},
		{
			"missing comma between entries",
			"{\"a\": 1\n\"b\": 2}",
			"{\"a\": 1,\n\"b\": 2}",
		},
		{
			"single quotes to double quotes",
			`{'key': 'value'}`,
			`{"key": "value"}`,
		},
		{
			"preamble with brackets stripped",
			"[Note] Here is the JSON:\n{\"key\": \"value\"}",
			`{"key": "value"}`,
		},
		{
			"json array not stripped",
			`[{"key": "value"}]`,
			`[{"key": "value"}]`,
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

func TestParsePhase2_Sanitization(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		checkFunc func(t *testing.T, r *Phase2Result)
	}{
		{
			"invalid entity kind defaults to concept",
			`{"file_summary":"x","entities":[{"kind":"widget","name":"W","qualified_name":"W","summary":"w","capabilities":[],"assumptions":[]}],"facts":[],"relationships":[]}`,
			func(t *testing.T, r *Phase2Result) {
				if r.Entities[0].Kind != "concept" {
					t.Errorf("Kind = %q, want concept", r.Entities[0].Kind)
				}
			},
		},
		{
			"invalid dimension defaults to what",
			`{"file_summary":"x","entities":[],"facts":[{"entity_name":"e","claim":"c","dimension":"where","category":"behavior","confidence":"high"}],"relationships":[]}`,
			func(t *testing.T, r *Phase2Result) {
				if r.Facts[0].Dimension != "what" {
					t.Errorf("Dimension = %q, want what", r.Facts[0].Dimension)
				}
			},
		},
		{
			"invalid category defaults to behavior",
			`{"file_summary":"x","entities":[],"facts":[{"entity_name":"e","claim":"c","dimension":"what","category":"opinion","confidence":"high"}],"relationships":[]}`,
			func(t *testing.T, r *Phase2Result) {
				if r.Facts[0].Category != "behavior" {
					t.Errorf("Category = %q, want behavior", r.Facts[0].Category)
				}
			},
		},
		{
			"invalid confidence defaults to medium",
			`{"file_summary":"x","entities":[],"facts":[{"entity_name":"e","claim":"c","dimension":"what","category":"behavior","confidence":"very_high"}],"relationships":[]}`,
			func(t *testing.T, r *Phase2Result) {
				if r.Facts[0].Confidence != "medium" {
					t.Errorf("Confidence = %q, want medium", r.Facts[0].Confidence)
				}
			},
		},
		{
			"invalid rel kind defaults to depends_on",
			`{"file_summary":"x","entities":[],"facts":[],"relationships":[{"from":"a","to":"b","kind":"links_to","description":"d","strength":"strong"}]}`,
			func(t *testing.T, r *Phase2Result) {
				if r.Relationships[0].Kind != "depends_on" {
					t.Errorf("RelKind = %q, want depends_on", r.Relationships[0].Kind)
				}
			},
		},
		{
			"invalid strength defaults to moderate",
			`{"file_summary":"x","entities":[],"facts":[],"relationships":[{"from":"a","to":"b","kind":"calls","description":"d","strength":"very_strong"}]}`,
			func(t *testing.T, r *Phase2Result) {
				if r.Relationships[0].Strength != "moderate" {
					t.Errorf("Strength = %q, want moderate", r.Relationships[0].Strength)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParsePhase2(tt.input)
			if err != nil {
				t.Fatalf("ParsePhase2() error = %v", err)
			}
			tt.checkFunc(t, result)
		})
	}
}

func TestParsePhase2_MalformedJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"not json", "hello world"},
		{"just a number", "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePhase2(tt.input)
			if err == nil {
				t.Error("ParsePhase2() expected error for malformed JSON")
			}
		})
	}
}

func TestParsePhase4(t *testing.T) {
	raw := `{
		"architectural_patterns": [
			{"pattern": "MVC", "description": "Model-View-Controller", "confidence": "high"}
		],
		"data_flows": [
			{"description": "HTTP to service", "from_module": "handler", "to_module": "service", "mechanism": "function call"}
		],
		"contracts": [
			{"between": ["handler", "service"], "description": "REST API contract", "explicit": true}
		],
		"facts": [
			{"entity_name": "handler::Handler", "claim": "Validates input", "dimension": "what", "category": "behavior", "confidence": "high"}
		],
		"relationships": [
			{"from": "handler::Handler", "to": "service::Service", "kind": "calls", "description": "Handler calls service", "strength": "strong"}
		]
	}`

	result, err := ParsePhase4(raw)
	if err != nil {
		t.Fatalf("ParsePhase4() error = %v", err)
	}
	if len(result.ArchitecturalPatterns) != 1 {
		t.Errorf("patterns = %d, want 1", len(result.ArchitecturalPatterns))
	}
	if result.ArchitecturalPatterns[0].Pattern != "MVC" {
		t.Errorf("pattern = %q, want MVC", result.ArchitecturalPatterns[0].Pattern)
	}
	if len(result.DataFlows) != 1 {
		t.Errorf("data_flows = %d, want 1", len(result.DataFlows))
	}
	if len(result.Facts) != 1 {
		t.Errorf("facts = %d, want 1", len(result.Facts))
	}
	if len(result.Relationships) != 1 {
		t.Errorf("relationships = %d, want 1", len(result.Relationships))
	}
}

func TestParsePhase4_Sanitization(t *testing.T) {
	raw := `{
		"architectural_patterns": [],
		"data_flows": [],
		"contracts": [],
		"facts": [
			{"entity_name": "x", "claim": "c", "dimension": "where", "category": "opinion", "confidence": "super"}
		],
		"relationships": [
			{"from": "a", "to": "b", "kind": "links_to", "description": "d", "strength": "mega"}
		]
	}`

	result, err := ParsePhase4(raw)
	if err != nil {
		t.Fatalf("ParsePhase4() error = %v", err)
	}
	if result.Facts[0].Dimension != "what" {
		t.Errorf("Dimension = %q, want what", result.Facts[0].Dimension)
	}
	if result.Facts[0].Category != "behavior" {
		t.Errorf("Category = %q, want behavior", result.Facts[0].Category)
	}
	if result.Relationships[0].Kind != "depends_on" {
		t.Errorf("Kind = %q, want depends_on", result.Relationships[0].Kind)
	}
}

func TestParsePhase5(t *testing.T) {
	raw := `{
		"summary": "A web application",
		"capabilities": ["handle HTTP requests", "store data"],
		"architecture": "Layered architecture with handlers, services, and stores",
		"conventions": [
			{"category": "naming", "description": "CamelCase for types", "examples": ["TaskStore", "UserHandler"]}
		],
		"risks_and_debt": ["No input validation"],
		"key_integration_points": ["PostgreSQL database"]
	}`

	result, err := ParsePhase5(raw)
	if err != nil {
		t.Fatalf("ParsePhase5() error = %v", err)
	}
	if result.Summary != "A web application" {
		t.Errorf("Summary = %q", result.Summary)
	}
	if len(result.Capabilities) != 2 {
		t.Errorf("Capabilities = %d, want 2", len(result.Capabilities))
	}
	if len(result.Conventions) != 1 {
		t.Errorf("Conventions = %d, want 1", len(result.Conventions))
	}
	if len(result.RisksAndDebt) != 1 {
		t.Errorf("RisksAndDebt = %d, want 1", len(result.RisksAndDebt))
	}
}

func TestParseGitLog(t *testing.T) {
	raw := `{
		"facts": [
			{"entity_name": "main::main", "claim": "Added in initial commit", "dimension": "when", "category": "behavior", "confidence": "high"}
		],
		"decisions": [
			{"summary": "Use PostgreSQL", "description": "Chose PostgreSQL for storage", "rationale": "ACID compliance needed", "made_at": "2024-01-15"}
		]
	}`

	result, err := ParseGitLog(raw)
	if err != nil {
		t.Fatalf("ParseGitLog() error = %v", err)
	}
	if len(result.Facts) != 1 {
		t.Errorf("Facts = %d, want 1", len(result.Facts))
	}
	if result.Facts[0].Dimension != "when" {
		t.Errorf("Dimension = %q, want when", result.Facts[0].Dimension)
	}
	if len(result.Decisions) != 1 {
		t.Errorf("Decisions = %d, want 1", len(result.Decisions))
	}
	if result.Decisions[0].Summary != "Use PostgreSQL" {
		t.Errorf("Decision summary = %q", result.Decisions[0].Summary)
	}
}

func TestParsePhase3(t *testing.T) {
	raw := `{
		"facts": [
			{"entity_name":"repo::A","claim":"Decision rationale captured","dimension":"why","category":"pattern","confidence":"high"}
		],
		"decisions": [
			{"summary":"Use X","description":"desc","rationale":"because","alternatives":[{"description":"Y","rejected_because":"too slow"}],"tradeoffs":["complexity"],"pr_number":12,"made_at":"2026-01-01T00:00:00Z"}
		]
	}`
	result, err := ParsePhase3(raw)
	if err != nil {
		t.Fatalf("ParsePhase3() error = %v", err)
	}
	if len(result.Facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(result.Facts))
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(result.Decisions))
	}
}
