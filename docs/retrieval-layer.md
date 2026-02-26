# Retrieval Layer

This document describes how knowledge flows out of AtlasKB to humans and agents.

## Two audiences, different needs

AtlasKB serves two fundamentally different consumers. Designing for both from the start — not bolting one onto the other — is a core principle.

### Humans need narrative

A human asking "how does auth work?" wants a story. They want to read a few paragraphs, follow some links, and build a mental model. They tolerate ambiguity and can fill in gaps with intuition.

### Agents need structure

An agent in the middle of a task needs precise, scoped facts it can act on immediately. It doesn't want a narrative — it wants to know "this endpoint requires a Bearer token, returns 401 on invalid tokens, and the token format is JWT signed with RS256." No more, no less.

---

## Agent Retrieval Interface

The primary agent interface is an **MCP (Model Context Protocol) server** exposing purpose-built tools. Each tool is designed for a specific kind of question agents ask mid-task.

### `get_conventions`

**When an agent needs to:** Write new code that fits the repo's style.

```
Input:
  repo: string
  category?: "error_handling" | "testing" | "naming" | "logging" | "all"

Output:
  conventions: [
    {
      category: string,
      description: string,
      examples: string[],      # code snippets showing the pattern
      provenance: Provenance[]
    }
  ]
```

### `get_module_context`

**When an agent needs to:** Understand a file before modifying it.

```
Input:
  file_path: string
  repo: string

Output:
  module: {
    summary: string,
    responsibilities: string[],
    invariants: string[],         # things that must remain true
    assumptions: string[],        # what this module assumes
    dependents: Entity[],         # what would break if this changes
    dependencies: Entity[],       # what this relies on
    conventions: Convention[],    # patterns specific to this module
    recent_decisions: Decision[], # recent design decisions affecting this
    provenance: Provenance[]
  }
```

### `get_service_contract`

**When an agent needs to:** Integrate with another service.

```
Input:
  service: string
  repo?: string

Output:
  contract: {
    summary: string,
    endpoints: [
      {
        method: string,
        path: string,
        description: string,
        auth: string,
        input_schema: object,
        output_schema: object,
        error_codes: { code: string, meaning: string }[],
        rate_limits?: string
      }
    ],
    events_produced: Event[],
    events_consumed: Event[],
    data_models: Entity[],
    retry_policy?: string,
    provenance: Provenance[]
  }
```

### `get_impact_analysis`

**When an agent needs to:** Understand the blast radius before making a change.

```
Input:
  entity: string            # name or path of the thing being changed
  repo: string
  change_type?: "modify" | "delete" | "rename"

Output:
  impact: {
    directly_affected: [
      {
        entity: Entity,
        relationship: string,   # how it's connected
        severity: "breaking" | "may_break" | "cosmetic"
      }
    ],
    transitively_affected: Entity[],
    tests_to_run: string[],      # test files that cover this entity
    warnings: string[],          # things to be careful about
    provenance: Provenance[]
  }
```

### `get_decision_context`

**When an agent needs to:** Understand why something is the way it is before proposing changes.

```
Input:
  topic: string             # natural language query about a decision
  repo?: string

Output:
  decisions: [
    {
      summary: string,
      rationale: string,
      alternatives_rejected: [
        { description: string, reason: string }
      ],
      tradeoffs: string[],
      still_valid: boolean,
      made_at: timestamp,
      provenance: Provenance[]
    }
  ]
```

### `search_knowledge`

**Fallback for open-ended queries** that don't fit the structured tools.

```
Input:
  query: string
  scope?: {
    repos?: string[],
    entity_types?: string[],
    dimensions?: ("what" | "how" | "why" | "when")[],
    confidence_min?: "low" | "medium" | "high"
  }

Output:
  results: [
    {
      fact: Fact,
      entity: Entity,
      relevance_score: float,
      provenance: Provenance[]
    }
  ]
```

---

## Human Retrieval Interface

### Natural Language Q&A

Humans interact through a conversational interface (CLI, web, or integrated into existing tools). The system:

1. Takes a natural language question
2. Retrieves relevant facts, entities, and decisions from the knowledge graph
3. Uses an LLM to synthesize a narrative answer grounded in the retrieved knowledge
4. Cites sources throughout the answer

**Example:**

> **Q:** How does authentication work across our services?
>
> **A:** Authentication is handled by the `auth-service` repo, which issues JWT tokens signed with RS256. Other services validate tokens using a shared middleware defined in `common-lib/auth`. The token contains user ID, roles, and tenant ID.
>
> The decision to use JWT over session-based auth was made in [PR #142](link) because the team needed stateless auth for the Kubernetes migration. The tradeoff was accepting the inability to revoke individual tokens — instead, tokens have a 15-minute expiry with refresh tokens stored in Redis (see [ADR-007](link)).
>
> Services that call other services use service-to-service tokens issued by `auth-service`'s `/internal/token` endpoint, which are scoped to specific permissions ([service-gateway/auth.go:45](link)).

### Exploration Mode

Beyond Q&A, humans should be able to browse:

- Entity graph visualization (what connects to what)
- Repo capability inventories
- Decision timelines
- Convention guides per repo

---

## Feedback Loop

Both humans and agents can flag knowledge as incorrect or outdated:

```
flag_fact(fact_id, reason: "incorrect" | "outdated" | "incomplete", correction?: string)
```

Flagged facts get:
1. Marked with reduced confidence immediately
2. Queued for re-analysis against current source material
3. If a correction is provided, stored as a candidate fact for human validation

Over time, this creates a quality signal: facts that are frequently retrieved and never flagged are high-trust. Facts that get flagged repeatedly need re-extraction or human curation.

---

## Authentication & Access Control

The retrieval layer should respect repository access boundaries:

- Users/agents only see knowledge from repos they have access to
- Cross-repo facts are only visible if the user has access to both repos
- Admin interface for managing which repos are indexed and who can query what

Integration with existing GitHub org permissions is the natural approach — if you can see the repo on GitHub, you can query its knowledge in AtlasKB.
