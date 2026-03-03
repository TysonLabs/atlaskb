# Myrmex Integration Guide

AtlasKB serves as the organizational code intelligence layer for Myrmex soldier agents. This document covers how soldiers should consume AtlasKB context and the recommended patterns for task execution.

## Architecture

```
Myrmex Gateway
  └── Soldier (Claude Code agent)
        └── AtlasKB MCP Server (or HTTP API)
              └── PostgreSQL + pgvector
```

Soldiers connect to AtlasKB via MCP tools (stdio for local, HTTP/SSE for remote workers). Before writing any code, soldiers query AtlasKB for relevant context about the target repo, modules, conventions, and dependencies.

## MCP Tools

### Existing

| Tool | Purpose |
|------|---------|
| `search_knowledge_base` | Free-text semantic search across all indexed repos |
| `list_repos` | List all indexed repositories |

### P0: Purpose-Built Tools

These tools eliminate the need for soldiers to craft search queries — they return structured, task-relevant context directly.

| Tool | Purpose | Implementation |
|------|---------|----------------|
| `get_conventions` | Coding conventions for a repo (style, patterns, naming) | Filter facts by `category=convention` for repo |
| `get_module_context` | Summary, responsibilities, invariants, dependents for a file/module | Find entity by path, return facts + relationships |
| `get_service_contract` | What other code depends on a module's public API | Find entity, return reverse relationships (who depends on it) |
| `get_impact_analysis` | Given a file, what else might need updating | Traverse relationship graph outward from entity |
| `get_decision_context` | Design decisions relevant to a module | Query decisions linked via `decision_entities` table |

### P1: Context Bundle + Transport

| Feature | Purpose |
|---------|---------|
| `get_task_context` | Single tool returning pre-assembled bundle (conventions + module contexts + decisions + invariants) for a repo + file list. Reduces 3-5 round-trips to 1. |
| Response size controls | `max_tokens`, `relevance_threshold`, `depth` (shallow/deep) params |
| HTTP/SSE transport | For N workers → 1 AtlasKB instance. stdio works for single-agent use. |

### P2: Staleness + Health

- **Staleness indicator** — Each response includes `last_indexed`, `head_commit_at_index`, `current_head`, `stale` bool
- **Health endpoint** — `GET /health` with status, repos_indexed, total_facts, database connectivity

### P3: Caching

- Content-based ETags on responses
- Cache-Control hints for convention stability (conventions change rarely)

## Soldier Pre-Prompt Patterns

### Context-First Execution

The most important pattern: soldiers must load relevant context from AtlasKB **before** writing any code. This replaces verbose inline instructions with actual organizational knowledge.

**Why this matters:** A soldier without AtlasKB context will write code that compiles but violates team conventions, misses dependencies, or duplicates existing patterns. AtlasKB turns "write a handler" into "write a handler using chi router, following the existing middleware chain, with table-driven tests."

### Task-Type Templates

Structure soldier pre-prompts by task type. Each template defines which AtlasKB queries to run before execution.

#### Feature Implementation

```
1. get_conventions(repo)        → coding standards, patterns, naming
2. get_module_context(files)    → architecture of modules being modified
3. get_impact_analysis(modules) → what might break from these changes
4. get_decision_context(module) → why things are the way they are
5. Plan implementation
6. Implement
7. Validate against conventions
```

#### Bug Fix

```
1. get_module_context(file)     → understand the module's responsibilities
2. get_service_contract(module) → who depends on this, what invariants exist
3. get_decision_context(module) → was this behavior intentional?
4. Diagnose root cause
5. Fix with minimal blast radius
6. Validate fix doesn't break dependents
```

#### Refactor

```
1. get_conventions(repo)        → target patterns to align with
2. get_module_context(files)    → current architecture
3. get_impact_analysis(modules) → full dependency graph
4. get_service_contract(module) → public API that must be preserved
5. Plan refactor preserving contracts
6. Implement
7. Validate no contract violations
```

#### Code Review

```
1. get_conventions(repo)        → standards to review against
2. get_module_context(files)    → expected patterns for these modules
3. get_decision_context(module) → context for why things are done this way
4. Review changes against conventions + architecture
5. Flag violations, suggest improvements
```

### Blueprint Integration

For Myrmex blueprints (multi-task plans), the blueprint prompt should include an AtlasKB context-loading step. Example blueprint task:

```yaml
tasks:
  - prompt: |
      Before implementing, query AtlasKB:
      - get_conventions("vector-ivr-core") for coding standards
      - get_module_context("internal/handlers/webhook.go") for current architecture

      Then implement: Add rate limiting middleware to the webhook handler.
      Follow existing middleware patterns from the conventions.
    files:
      - internal/handlers/webhook.go
      - internal/middleware/
```

### What NOT To Do

- **Don't use "act as X role" prompts** — Soldiers are already specialized agents. Role-playing adds nothing.
- **Don't inline conventions in every task** — That's what AtlasKB is for. Query it.
- **Don't skip context loading to save time** — The tokens spent querying AtlasKB are far cheaper than the tokens wasted on wrong approaches, rework, and convention violations.
- **Don't ask soldiers clarifying questions** — They're autonomous. Give them the right context upfront via AtlasKB and clear task descriptions.

## Effective Soldier Task Descriptions

A good Myrmex task for a soldier working with AtlasKB:

```
Query AtlasKB for conventions and module context for internal/auth/.
Then add JWT token refresh logic to the auth middleware.
The refresh should happen transparently when a token is within 5 minutes
of expiry. Follow existing error handling patterns.
```

A bad task description:

```
Add JWT refresh to auth. Make it good. Follow best practices.
```

The difference: the first tells the soldier exactly what context to load and what "done" looks like. The second forces the soldier to guess.

## Implementation Status

| Priority | Feature | Status |
|----------|---------|--------|
| P0 | `get_conventions` | Not started |
| P0 | `get_module_context` | Not started |
| P0 | `get_service_contract` | Not started |
| P0 | `get_impact_analysis` | Not started |
| P0 | `get_decision_context` | Not started |
| P1 | `get_task_context` (bundle) | Not started |
| P1 | Response size controls | Not started |
| P1 | HTTP/SSE transport | Not started |
| P2 | Staleness indicator | Not started |
| P2 | Health endpoint | Not started |
| P3 | ETag caching | Not started |
