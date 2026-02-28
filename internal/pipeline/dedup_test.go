package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

func TestParseDedupDecision_ValidActions(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantAction string
	}{
		{"skip", `{"action":"skip","reason":"same"}`, "skip"},
		{"update", `{"action":"update","reason":"better"}`, "update"},
		{"insert", `{"action":"insert","reason":"new"}`, "insert"},
		{"supersede", `{"action":"supersede","reason":"improved"}`, "supersede"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := parseDedupDecision(tt.input)
			if err != nil {
				t.Fatalf("parseDedupDecision() error = %v", err)
			}
			if d.Action != tt.wantAction {
				t.Errorf("Action = %q, want %q", d.Action, tt.wantAction)
			}
		})
	}
}

func TestParseDedupDecision_UppercaseNormalization(t *testing.T) {
	d, err := parseDedupDecision(`{"action":"SKIP","reason":"same"}`)
	if err != nil {
		t.Fatalf("parseDedupDecision() error = %v", err)
	}
	if d.Action != "skip" {
		t.Errorf("Action = %q, want %q", d.Action, "skip")
	}
}

func TestParseDedupDecision_WhitespaceTrimming(t *testing.T) {
	d, err := parseDedupDecision(`{"action":"  insert  ","reason":"new"}`)
	if err != nil {
		t.Fatalf("parseDedupDecision() error = %v", err)
	}
	if d.Action != "insert" {
		t.Errorf("Action = %q, want %q", d.Action, "insert")
	}
}

func TestParseDedupDecision_UnrecognizedAction(t *testing.T) {
	d, err := parseDedupDecision(`{"action":"merge","reason":"dunno"}`)
	if err != nil {
		t.Fatalf("parseDedupDecision() error = %v", err)
	}
	if d.Action != "insert" {
		t.Errorf("Action = %q, want %q (default)", d.Action, "insert")
	}
}

func TestParseDedupDecision_SupersedeWithUUID(t *testing.T) {
	id := uuid.New()
	raw := fmt.Sprintf(`{"action":"supersede","supersedes_id":"%s","reason":"better"}`, id)
	d, err := parseDedupDecision(raw)
	if err != nil {
		t.Fatalf("parseDedupDecision() error = %v", err)
	}
	if d.Action != "supersede" {
		t.Errorf("Action = %q, want supersede", d.Action)
	}
	if d.SupersedesID == nil || *d.SupersedesID != id {
		t.Errorf("SupersedesID = %v, want %s", d.SupersedesID, id)
	}
}

func TestParseDedupDecision_InvalidJSON(t *testing.T) {
	_, err := parseDedupDecision("not json at all")
	if err == nil {
		t.Error("parseDedupDecision() expected error for invalid JSON")
	}
}

func TestParseDedupDecision_MarkdownWrapped(t *testing.T) {
	raw := "```json\n{\"action\":\"skip\",\"reason\":\"same\"}\n```"
	d, err := parseDedupDecision(raw)
	if err != nil {
		t.Fatalf("parseDedupDecision() error = %v", err)
	}
	if d.Action != "skip" {
		t.Errorf("Action = %q, want skip", d.Action)
	}
}

func TestDedupEntity_MockSkip(t *testing.T) {
	mock := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{
				Content: `{"action":"skip","reason":"same entity"}`,
			}, nil
		},
	}

	existing := &models.Entity{
		QualifiedName: "pkg::Foo",
		Kind:          "type",
		Name:          "Foo",
		Summary:       models.Ptr("A foo"),
		Capabilities:  []string{"do foo"},
	}
	candidate := ExtractedEntity{
		QualifiedName: "pkg::Foo",
		Kind:          "type",
		Name:          "Foo",
		Summary:       "A foo thing",
		Capabilities:  []string{"do foo"},
	}

	d, err := DedupEntity(context.Background(), mock, "test-model", existing, candidate)
	if err != nil {
		t.Fatalf("DedupEntity() error = %v", err)
	}
	if d.Action != "skip" {
		t.Errorf("Action = %q, want skip", d.Action)
	}
}

func TestDedupEntity_MockInsert(t *testing.T) {
	mock := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{
				Content: `{"action":"insert","reason":"different entity"}`,
			}, nil
		},
	}

	existing := &models.Entity{
		QualifiedName: "pkg::Foo",
		Kind:          "type",
		Name:          "Foo",
	}
	candidate := ExtractedEntity{
		QualifiedName: "pkg::Bar",
		Kind:          "type",
		Name:          "Bar",
		Summary:       "A bar",
	}

	d, err := DedupEntity(context.Background(), mock, "test-model", existing, candidate)
	if err != nil {
		t.Fatalf("DedupEntity() error = %v", err)
	}
	if d.Action != "insert" {
		t.Errorf("Action = %q, want insert", d.Action)
	}
}

func TestDedupFact_MockSkip(t *testing.T) {
	mock := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{
				Content: `{"action":"skip","reason":"duplicate fact"}`,
			}, nil
		},
	}

	existing := []models.Fact{{
		ID:       uuid.New(),
		Claim:    "Uses mutex for thread safety",
		Category: "behavior",
	}}
	candidate := ExtractedFact{
		Claim:     "Uses sync.Mutex for concurrency safety",
		Dimension: "how",
		Category:  "behavior",
	}

	d, err := DedupFact(context.Background(), mock, "test-model", existing, candidate)
	if err != nil {
		t.Fatalf("DedupFact() error = %v", err)
	}
	if d.Action != "skip" {
		t.Errorf("Action = %q, want skip", d.Action)
	}
}

func TestDedupFact_MockSupersede(t *testing.T) {
	factID := uuid.New()
	mock := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{
				Content: fmt.Sprintf(`{"action":"supersede","supersedes_id":"%s","reason":"improved"}`, factID),
			}, nil
		},
	}

	existing := []models.Fact{{
		ID:       factID,
		Claim:    "Old fact",
		Category: "behavior",
	}}
	candidate := ExtractedFact{
		Claim:     "Better fact",
		Dimension: "what",
		Category:  "behavior",
	}

	d, err := DedupFact(context.Background(), mock, "test-model", existing, candidate)
	if err != nil {
		t.Fatalf("DedupFact() error = %v", err)
	}
	if d.Action != "supersede" {
		t.Errorf("Action = %q, want supersede", d.Action)
	}
	if d.SupersedesID == nil || *d.SupersedesID != factID {
		t.Errorf("SupersedesID = %v, want %s", d.SupersedesID, factID)
	}
}

func TestDedupFact_MockInsert(t *testing.T) {
	mock := &llm.MockClient{
		CompleteFunc: func(ctx context.Context, model string, system string, messages []llm.Message, maxTokens int, schema *llm.JSONSchema) (*llm.Response, error) {
			return &llm.Response{
				Content: `{"action":"insert","reason":"new info"}`,
			}, nil
		},
	}

	d, err := DedupFact(context.Background(), mock, "test-model", nil, ExtractedFact{
		Claim:     "Brand new fact",
		Dimension: "what",
		Category:  "behavior",
	})
	if err != nil {
		t.Fatalf("DedupFact() error = %v", err)
	}
	if d.Action != "insert" {
		t.Errorf("Action = %q, want insert", d.Action)
	}
}

// Verify the JSON schema is valid
func TestSchemaDedupIsValidJSON(t *testing.T) {
	var parsed map[string]interface{}
	if err := json.Unmarshal(SchemaDedup.Schema, &parsed); err != nil {
		t.Errorf("SchemaDedup is not valid JSON: %v", err)
	}
}
