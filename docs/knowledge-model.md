# Knowledge Model

This document defines what AtlasKB considers "knowledge" and how it's structured in the graph.

## The Four Dimensions

Every repository is analyzed through four fundamental lenses. Together, they form a complete understanding of a system.

### What — Capabilities & Structure

What does this system do? What can it be asked to do? What does it operate on?

**Sources:**
- Code: exported functions, API endpoints, CLI commands, event handlers
- Types: data models, schemas, interfaces, database tables
- Config: environment variables, feature flags, configuration files
- Infrastructure: Dockerfiles, CI/CD pipelines, deployment manifests
- Docs: READMEs, API docs, user guides

**Extracted knowledge:**
- Capability inventory (this service can do X, Y, Z)
- Data model descriptions (this entity has these fields, these constraints, these relationships)
- API surface (these endpoints exist, accept this input, return this output)
- Configuration surface (these knobs exist, with these defaults and these effects)

### How — Implementation & Patterns

How does it accomplish what it does? What patterns and conventions does it follow?

**Sources:**
- Code: function bodies, module structure, import graphs
- Tests: test patterns, fixtures, mocking strategies
- Config: linter rules, formatter configs, CI checks

**Extracted knowledge:**
- Architectural patterns (layered, hexagonal, event-driven, etc.)
- Coding conventions (error handling, logging, naming, file organization)
- Integration patterns (how it talks to databases, queues, other services)
- Data flow traces (request comes in here, gets validated here, persisted here, response goes out here)
- Invariants and constraints (this function assumes X, this module requires Y to be true)

### Why — Decisions & Intent

Why is it built this way? What alternatives were considered? What tradeoffs were made?

**Sources:**
- Pull requests: descriptions, review comments, discussion threads
- Issues: problem descriptions, proposed solutions, debate
- Commits: messages explaining reasoning (not just "fix bug")
- ADRs: architecture decision records
- Code comments: the "why" comments (not the "what" comments)
- Docs: design documents, RFCs, proposals

**Extracted knowledge:**
- Decision records (we chose X over Y because Z)
- Rejected alternatives (we considered A but rejected it because B)
- Tradeoff documentation (we accepted C in exchange for D)
- Intent statements (this module exists to solve E)
- Constraint rationale (we can't do F because of G)

### When — Lifecycle & Evolution

When did things change? How has the system evolved? What's the trajectory?

**Sources:**
- Git history: commit timestamps, branch/merge patterns
- Releases: changelogs, release notes, tags
- Issues: creation dates, resolution times
- PRs: merge dates, review turnaround

**Extracted knowledge:**
- Introduction timeline (this capability was added in v2.3, this module was created 6 months ago)
- Evolution narrative (this service started as X, was refactored to Y, is moving toward Z)
- Deprecation status (this is deprecated, replaced by W)
- Stability indicators (this module hasn't changed in a year vs. this one changes weekly)

---

## Graph Schema

### Entity

The fundamental unit. Something that exists in the codebase and can be talked about.

```
Entity {
  id:          UUID
  kind:        module | service | function | type | endpoint | concept | config
  name:        string
  qualified_name: string          # e.g., "atlaskb::pipeline::extract"
  repo:        string
  path:        string?            # file path, if applicable
  summary:     string             # LLM-generated natural language summary
  capabilities: string[]          # what this entity can do
  assumptions:  string[]          # what this entity assumes to be true
  created_at:  timestamp          # when first indexed
  updated_at:  timestamp          # when last re-analyzed
}
```

### Fact

A grounded claim about an entity. The atomic unit of knowledge.

```
Fact {
  id:          UUID
  subject:     Entity ID
  claim:       string             # natural language statement
  dimension:   what | how | why | when
  category:    behavior | constraint | pattern | convention | debt | risk
  confidence:  high | medium | low
  provenance:  Provenance[]       # where this fact came from
  embedding:   vector             # for semantic retrieval
  created_at:  timestamp
  updated_at:  timestamp
  superseded_by: Fact ID?         # if this fact has been replaced
}
```

### Decision

A captured design decision with full context. The "why" layer.

```
Decision {
  id:            UUID
  summary:       string           # one-line description
  description:   string           # full context
  rationale:     string           # why this choice was made
  alternatives:  Alternative[]    # what else was considered
  tradeoffs:     string[]         # what was accepted/sacrificed
  affected:      Entity ID[]      # what entities this decision impacts
  provenance:    Provenance[]
  made_at:       timestamp        # when the decision was made
  created_at:    timestamp
  still_valid:   boolean          # has this been superseded?
}

Alternative {
  description:   string
  rejected_because: string
}
```

### Relationship

A directed edge between two entities.

```
Relationship {
  id:          UUID
  from:        Entity ID
  to:          Entity ID
  kind:        depends_on | calls | implements | extends | produces | consumes
               | replaced_by | tested_by | configured_by | owns
  description: string?           # optional natural language description
  strength:    strong | moderate | weak
  provenance:  Provenance[]
  created_at:  timestamp
}
```

### Provenance

Where a piece of knowledge came from. Attached to Facts, Decisions, and Relationships.

```
Provenance {
  source_type: file | commit | pr | issue | comment | adr | doc
  repo:        string
  ref:         string            # file:line, commit SHA, PR #, issue #
  url:         string?           # link to GitHub
  excerpt:     string?           # relevant snippet
  analyzed_at: timestamp         # when the LLM processed this source
}
```

---

## Confidence Levels

Not all knowledge is created equal. AtlasKB tracks confidence to help consumers (especially agents) calibrate trust.

| Level | Meaning | Example |
|---|---|---|
| **High** | Directly observable in code or explicitly stated in docs | "This function returns a 404 if the user is not found" |
| **Medium** | Inferred from patterns, comments, or PR context | "This service follows a hexagonal architecture pattern" |
| **Low** | Synthesized from indirect signals, may need human validation | "This module appears to be deprecated based on declining commit activity" |

## Staleness

Every fact has a temporal dimension. AtlasKB tracks when facts were last validated against the source material and flags facts that may be stale based on:

- The underlying source file has changed since the fact was extracted
- The entity the fact describes has been significantly modified
- A related decision has been superseded
- Time-based decay (facts about rapidly-changing modules get stale faster)
