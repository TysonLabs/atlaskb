package pipeline

import "fmt"

const systemPromptPhase2 = `You are a code analysis expert. You analyze source files and extract structured knowledge about the codebase.

CRITICAL RULES:
- You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.
- You MUST fill in real values — NEVER use "..." or ellipsis as placeholder values.
- Every string value must contain actual content extracted from the code.
- If a file has no entities or facts to extract, use empty arrays [].`

func Phase2Prompt(filePath, language, repoName string, stackInfo StackInfo, content string) string {
	return fmt.Sprintf(`Analyze this file and extract structured knowledge.

Repository: %s
File: %s
Language: %s
Stack: %v

<file_content>
%s
</file_content>

Respond with JSON in this exact schema:
{
  "file_summary": "one paragraph describing this file's responsibility",
  "entities": [
    {
      "kind": "module|service|function|type|endpoint|concept|config",
      "name": "short name",
      "qualified_name": "repo::package::name",
      "summary": "what this entity does",
      "capabilities": ["list of things this entity can do"],
      "assumptions": ["things this entity assumes to be true"]
    }
  ],
  "facts": [
    {
      "entity_name": "which entity this fact is about (qualified_name)",
      "claim": "a specific, grounded claim",
      "dimension": "what|how|why|when",
      "category": "behavior|constraint|pattern|convention|debt|risk",
      "confidence": "high|medium|low"
    }
  ],
  "relationships": [
    {
      "from": "qualified_name of source entity",
      "to": "qualified_name of target entity",
      "kind": "depends_on|calls|implements|extends|produces|consumes|tested_by|configured_by|owns",
      "description": "brief description",
      "strength": "strong|moderate|weak"
    }
  ]
}`, repoName, filePath, language, stackInfo.Languages, content)
}

const systemPromptPhase4 = `You are a software architect. You analyze entities and facts extracted from a codebase and synthesize cross-module insights about architecture, data flows, and contracts.

You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.`

func Phase4Prompt(repoName string, moduleSummaries string) string {
	return fmt.Sprintf(`Analyze the following extracted entities and facts from the "%s" repository and synthesize cross-module insights.

%s

Respond with JSON in this exact schema:
{
  "architectural_patterns": [
    {
      "pattern": "name of the pattern",
      "description": "how this pattern is applied",
      "confidence": "high|medium|low"
    }
  ],
  "data_flows": [
    {
      "description": "data flow description",
      "from_module": "source module qualified_name",
      "to_module": "target module qualified_name",
      "mechanism": "how data flows (function call, queue, HTTP, etc.)"
    }
  ],
  "contracts": [
    {
      "between": ["module_a", "module_b"],
      "description": "the contract between these modules",
      "explicit": true
    }
  ],
  "facts": [
    {
      "entity_name": "qualified_name (or repo-level if cross-cutting)",
      "claim": "architectural insight",
      "dimension": "how",
      "category": "pattern|convention|constraint",
      "confidence": "high|medium|low"
    }
  ],
  "relationships": [
    {
      "from": "qualified_name",
      "to": "qualified_name",
      "kind": "depends_on|calls|produces|consumes",
      "description": "brief description",
      "strength": "strong|moderate|weak"
    }
  ]
}`, repoName, moduleSummaries)
}

const systemPromptPhase5 = `You are a technical writer and software architect. You synthesize comprehensive repository summaries from extracted knowledge.

You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.`

func Phase5Prompt(repoName string, entitySummaries, architecturalFacts, decisions string) string {
	return fmt.Sprintf(`Create a comprehensive summary of the "%s" repository based on the following extracted knowledge.

## Entities
%s

## Architectural Facts
%s

## Decisions
%s

Respond with JSON in this exact schema:
{
  "summary": "2-3 paragraph overview of the repository",
  "capabilities": ["list of what this repository can do"],
  "architecture": "description of the high-level architecture",
  "conventions": [
    {
      "category": "error_handling|testing|naming|logging|other",
      "description": "description of the convention",
      "examples": ["example snippets or references"]
    }
  ],
  "risks_and_debt": ["list of identified risks or tech debt"],
  "key_integration_points": ["list of external dependencies and how they're used"]
}`, repoName, entitySummaries, architecturalFacts, decisions)
}

const systemPromptGitLog = `You are a code historian. You analyze git commit history to extract the "when" and "why" dimensions of a codebase's evolution.

You MUST respond with valid JSON only — no markdown fences, no commentary outside the JSON.`

func GitLogPrompt(repoName string, commits string) string {
	return fmt.Sprintf(`Analyze the following git history from the "%s" repository. Extract facts about the evolution, significant changes, and decision rationale visible in the commit history.

%s

Respond with JSON in this exact schema:
{
  "facts": [
    {
      "entity_name": "qualified_name of the affected entity (or repo name if repo-level)",
      "claim": "what happened and when",
      "dimension": "when",
      "category": "behavior|pattern|convention",
      "confidence": "high|medium|low"
    }
  ],
  "decisions": [
    {
      "summary": "one-line decision description",
      "description": "fuller context",
      "rationale": "why this was done",
      "made_at": "ISO timestamp if determinable"
    }
  ]
}`, repoName, commits)
}
