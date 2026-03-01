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

You have access to a knowledge graph of facts extracted from repositories. Answer the user's question using ONLY the provided context. Answer confidently using the provided context. If certain topics aren't covered in the context, simply omit them rather than noting their absence. Focus on what IS in the context.

Formatting rules:
- Use ## headers to organize your answer into logical sections
- Use bullet points or numbered lists for enumerations (e.g. listing components, steps, dependencies)
- Use backtick code formatting for entity names, service names, file paths, functions, and technical terms (e.g. ` + "`UserService`" + `, ` + "`main.go`" + `)
- Do NOT use [Fact N] citations — instead, naturally reference entities in code formatting
- Keep paragraphs to 2-3 sentences max
- Bold **key terms** on first mention
- Be specific and precise`

func (s *Synthesizer) Synthesize(ctx context.Context, question string, results []SearchResult) (<-chan llm.StreamChunk, error) {
	// Build context from search results
	var sb strings.Builder
	sb.WriteString("## Retrieved Knowledge\n\n")

	for i, r := range results {
		fmt.Fprintf(&sb, "### Fact %d\n", i+1)
		fmt.Fprintf(&sb, "Entity: %s (%s)\n", r.Entity.QualifiedName, r.Entity.Kind)
		if r.RepoName != "" {
			fmt.Fprintf(&sb, "Repo: %s\n", r.RepoName)
		}
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

func (s *Synthesizer) SynthesizeWithHistory(ctx context.Context, question string, results []SearchResult, history []llm.Message) (<-chan llm.StreamChunk, error) {
	// Build context from search results
	var sb strings.Builder
	sb.WriteString("## Retrieved Knowledge\n\n")

	for i, r := range results {
		fmt.Fprintf(&sb, "### Fact %d\n", i+1)
		fmt.Fprintf(&sb, "Entity: %s (%s)\n", r.Entity.QualifiedName, r.Entity.Kind)
		if r.RepoName != "" {
			fmt.Fprintf(&sb, "Repo: %s\n", r.RepoName)
		}
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

	// Prepend conversation history before the current user prompt
	messages := make([]llm.Message, 0, len(history)+1)
	messages = append(messages, history...)
	messages = append(messages, llm.Message{Role: "user", Content: prompt})

	return s.LLM.CompleteStream(ctx, s.Model, answerSystemPrompt, messages, 4096)
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
