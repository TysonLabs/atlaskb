package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/tgeorge06/atlaskb/internal/llm"
)

type Synthesizer struct {
	LLM   llm.Client
	Model string
}

func NewSynthesizer(client llm.Client, model string) *Synthesizer {
	return &Synthesizer{LLM: client, Model: model}
}

const answerSystemPrompt = `You are AtlasKB, a knowledge base assistant that answers questions about codebases.

You have access to a knowledge graph of facts extracted from repositories. Answer the user's question using ONLY the provided context. If the context doesn't contain enough information, say so honestly.

Rules:
- Ground every claim in the provided facts
- Cite sources using [entity_name] notation
- Be specific and precise
- If you're uncertain, express the uncertainty
- Format your answer in clear, readable prose`

func (s *Synthesizer) Synthesize(ctx context.Context, question string, results []SearchResult) (<-chan llm.StreamChunk, error) {
	// Build context from search results
	var sb strings.Builder
	sb.WriteString("## Retrieved Knowledge\n\n")

	for i, r := range results {
		fmt.Fprintf(&sb, "### Fact %d\n", i+1)
		fmt.Fprintf(&sb, "Entity: %s (%s)\n", r.Entity.QualifiedName, r.Entity.Kind)
		if r.Entity.Path != nil && *r.Entity.Path != "" {
			fmt.Fprintf(&sb, "File: %s\n", *r.Entity.Path)
		}
		fmt.Fprintf(&sb, "Claim: %s\n", r.Fact.Claim)
		fmt.Fprintf(&sb, "Dimension: %s | Category: %s | Confidence: %s\n", r.Fact.Dimension, r.Fact.Category, r.Fact.Confidence)
		if r.Entity.Summary != nil && *r.Entity.Summary != "" {
			fmt.Fprintf(&sb, "Entity summary: %s\n", *r.Entity.Summary)
		}
		sb.WriteString("\n")
	}

	prompt := fmt.Sprintf("%s\n\n## Question\n%s", sb.String(), question)

	return s.LLM.CompleteStream(ctx, s.Model, answerSystemPrompt, []llm.Message{
		{Role: "user", Content: prompt},
	}, 4096)
}

func (s *Synthesizer) SynthesizeSync(ctx context.Context, question string, results []SearchResult) (string, error) {
	ch, err := s.Synthesize(ctx, question, results)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			return sb.String(), chunk.Error
		}
		sb.WriteString(chunk.Text)
	}
	return sb.String(), nil
}
