package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// DedupDecision represents the LLM's decision about whether an entity or fact is a duplicate.
type DedupDecision struct {
	Action       string     `json:"action"` // "skip", "update", "insert", "supersede"
	SupersedesID *uuid.UUID `json:"supersedes_id,omitempty"`
	Reason       string     `json:"reason"`
}

// DedupStats tracks dedup decisions for logging.
type DedupStats struct {
	EntityChecks   int
	EntityInserts  int
	EntitySkips    int
	EntityUpdates  int
	FactChecks     int
	FactInserts    int
	FactSkips      int
	FactSupersedes int
}

const systemPromptDedup = `You are a deduplication expert. You compare extracted knowledge to existing records and decide whether to insert, update, or skip.

CRITICAL RULES:
- You MUST respond with ONLY a JSON object — no thinking, no explanation, no markdown.
- Your entire response must be valid JSON starting with { and ending with }.
- Do NOT include any text before or after the JSON object.`

// DedupEntity checks whether a candidate entity is a duplicate of an existing one.
func DedupEntity(ctx context.Context, llmClient llm.Client, model string,
	existing *models.Entity, candidate ExtractedEntity) (*DedupDecision, error) {

	existingSummary := ""
	if existing.Summary != nil {
		existingSummary = *existing.Summary
	}

	prompt := fmt.Sprintf(`Compare these two entities and decide if they represent the same thing.

EXISTING ENTITY:
  qualified_name: %s
  kind: %s
  name: %s
  summary: %s
  capabilities: %s

CANDIDATE ENTITY (newly extracted):
  qualified_name: %s
  kind: %s
  name: %s
  summary: %s
  capabilities: %s

Respond with JSON:
{"action": "skip|update|insert", "reason": "brief explanation"}

- "skip": candidate is a duplicate, discard it
- "update": same entity but candidate has better/newer info, merge into existing
- "insert": genuinely different entity, create new record`,
		existing.QualifiedName, existing.Kind, existing.Name, existingSummary, strings.Join(existing.Capabilities, ", "),
		candidate.QualifiedName, candidate.Kind, candidate.Name, candidate.Summary, strings.Join(candidate.Capabilities, ", "))

	resp, err := llmClient.Complete(ctx, model, systemPromptDedup, []llm.Message{
		{Role: "user", Content: prompt},
	}, 256, SchemaDedup)
	if err != nil {
		return nil, fmt.Errorf("dedup entity LLM call: %w", err)
	}

	return parseDedupDecision(resp.Content)
}

// DedupFact checks whether a candidate fact is a duplicate of existing facts on the same entity.
func DedupFact(ctx context.Context, llmClient llm.Client, model string,
	existing []models.Fact, candidate ExtractedFact) (*DedupDecision, error) {

	var sb strings.Builder
	for _, f := range existing {
		fmt.Fprintf(&sb, "  - [%s] (id=%s) %s\n", f.Category, f.ID, f.Claim)
	}

	prompt := fmt.Sprintf(`Compare this candidate fact against existing facts for the same entity.

EXISTING FACTS:
%s
CANDIDATE FACT:
  claim: %s
  dimension: %s
  category: %s

Respond with JSON:
{"action": "skip|supersede|insert", "reason": "brief explanation"}
If action is "supersede", also include: "supersedes_id": "uuid-of-old-fact"

- "skip": candidate is a duplicate of an existing fact
- "supersede": candidate replaces/improves an existing fact (set supersedes_id)
- "insert": candidate is genuinely new information`,
		sb.String(), candidate.Claim, candidate.Dimension, candidate.Category)

	resp, err := llmClient.Complete(ctx, model, systemPromptDedup, []llm.Message{
		{Role: "user", Content: prompt},
	}, 256, SchemaDedup)
	if err != nil {
		return nil, fmt.Errorf("dedup fact LLM call: %w", err)
	}

	return parseDedupDecision(resp.Content)
}

func parseDedupDecision(raw string) (*DedupDecision, error) {
	cleaned := CleanJSON(raw)
	var decision DedupDecision
	if err := json.Unmarshal([]byte(cleaned), &decision); err != nil {
		return nil, fmt.Errorf("parsing dedup decision: %w (raw: %.200s)", err, cleaned)
	}

	// Normalize action
	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	switch decision.Action {
	case "skip", "update", "insert", "supersede":
		// valid
	default:
		// Default to insert if unrecognized
		decision.Action = "insert"
		decision.Reason = fmt.Sprintf("unrecognized action %q, defaulting to insert", decision.Action)
	}

	return &decision, nil
}

// FastFuzzyMatch checks if a candidate entity can be deduplicated against existing
// entities using normalized name matching, without requiring an LLM call.
// Returns the matched entity ID and true if a match is found.
func FastFuzzyMatch(ctx context.Context, entityStore *models.EntityStore, repoID uuid.UUID, candidate ExtractedEntity) (uuid.UUID, bool) {
	candidateNorm := models.NormalizeName(candidate.Name)

	// Find entities with high similarity normalized name match + same kind
	results, err := entityStore.SearchFuzzy(ctx, candidate.Name, &repoID, 0.95, 5)
	if err != nil || len(results) == 0 {
		return uuid.Nil, false
	}

	candidateOwner := qualifiedNameOwner(candidate.QualifiedName)

	for _, r := range results {
		// Must be the same kind
		if r.Kind != candidate.Kind {
			continue
		}

		// Must have exact normalized name match (not just high similarity)
		if models.NormalizeName(r.Name) != candidateNorm {
			continue
		}

		// Must have the same owner (type prefix / package scope)
		if qualifiedNameOwner(r.QualifiedName) != candidateOwner {
			continue
		}

		return r.ID, true
	}

	return uuid.Nil, false
}
