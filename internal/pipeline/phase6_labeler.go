package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/tgeorge06/atlaskb/internal/llm"
	"github.com/tgeorge06/atlaskb/internal/models"
)

// ClusterLabel is the result of labeling a community of entities.
type ClusterLabel struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Domain      string `json:"domain"`
}

const systemPromptPhase6 = `You are a software architect. You label clusters of related code entities with concise, descriptive names.

CRITICAL RULES:
- You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.
- Your entire response must start with { and end with }.
- Do NOT output "..." or ellipsis as values. Use real content.`

// LabelCluster uses an LLM to generate a label, description, and domain for a cluster of entities.
func LabelCluster(ctx context.Context, client llm.Client, model string, members []models.Entity) (*ClusterLabel, error) {
	var sb strings.Builder
	sb.WriteString("Label this cluster of related code entities. Respond with JSON: {\"label\": \"short name (2-4 words)\", \"description\": \"one sentence\", \"domain\": \"functional area\"}\n\nMembers:\n")

	for _, m := range members {
		summary := ""
		if m.Summary != nil {
			summary = *m.Summary
		}
		fmt.Fprintf(&sb, "- %s (%s): %s\n", m.QualifiedName, m.Kind, summary)
	}

	resp, err := client.Complete(ctx, model, systemPromptPhase6, []llm.Message{
		{Role: "user", Content: sb.String()},
	}, 256, SchemaClusterLabel)
	if err != nil {
		return nil, fmt.Errorf("LLM cluster labeling: %w", err)
	}

	var label ClusterLabel
	if err := json.Unmarshal([]byte(resp.Content), &label); err != nil {
		return nil, fmt.Errorf("parsing cluster label: %w", err)
	}

	if label.Label == "" {
		return nil, fmt.Errorf("LLM returned empty label")
	}

	return &label, nil
}

// noiseWords are common words stripped during keyword-based labeling.
var noiseWords = map[string]bool{
	"handler": true, "store": true, "manager": true, "service": true,
	"new": true, "get": true, "set": true, "run": true,
	"create": true, "update": true, "delete": true, "list": true,
	"find": true, "init": true, "config": true, "helper": true,
	"utils": true, "error": true, "response": true, "request": true,
	"base": true, "default": true,
}

// KeywordLabelCluster generates a label by tokenizing member names and taking
// the top-3 most frequent tokens, excluding noise words.
func KeywordLabelCluster(members []models.Entity) *ClusterLabel {
	freq := make(map[string]int)

	for _, m := range members {
		tokens := tokenizeName(m.Name)
		for _, t := range tokens {
			lower := strings.ToLower(t)
			if !noiseWords[lower] && len(lower) > 1 {
				freq[lower]++
			}
		}
	}

	// Sort by frequency descending
	type kv struct {
		word  string
		count int
	}
	var sorted []kv
	for w, c := range freq {
		sorted = append(sorted, kv{w, c})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count == sorted[j].count {
			return sorted[i].word < sorted[j].word
		}
		return sorted[i].count > sorted[j].count
	})

	// Take top 3
	var top []string
	for i := 0; i < len(sorted) && i < 3; i++ {
		top = append(top, sorted[i].word)
	}

	if len(top) == 0 {
		return &ClusterLabel{
			Label:       "misc",
			Description: "Miscellaneous cluster",
			Domain:      "other",
		}
	}

	label := strings.Join(top, "-")
	return &ClusterLabel{
		Label:       label,
		Description: fmt.Sprintf("Cluster related to %s", strings.Join(top, ", ")),
		Domain:      top[0],
	}
}

// tokenizeName splits a name by camelCase and snake_case boundaries.
func tokenizeName(name string) []string {
	// First split by underscores, hyphens, dots, colons
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == ':' || r == '/'
	})

	var tokens []string
	for _, part := range parts {
		tokens = append(tokens, splitCamelCase(part)...)
	}
	return tokens
}

// splitCamelCase splits a camelCase or PascalCase string into tokens.
func splitCamelCase(s string) []string {
	if s == "" {
		return nil
	}

	var tokens []string
	var current []rune

	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			// Check if this starts a new word
			prevLower := unicode.IsLower(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || nextLower {
				if len(current) > 0 {
					tokens = append(tokens, string(current))
					current = nil
				}
			}
		}
		current = append(current, r)
	}
	if len(current) > 0 {
		tokens = append(tokens, string(current))
	}

	return tokens
}
