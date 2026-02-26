# AtlasKB — Product Requirements Document

## 1. Overview

AtlasKB is a knowledge graph engine that uses deep LLM inference to analyze GitHub repositories and build a structured, queryable understanding of what systems do, how they work, why they were built that way, and when they evolved. It serves both human engineers and agentic systems through a CLI tool and MCP server.

### 1.1 Problem Statement

Engineering organizations generate knowledge every day — in code, commits, pull requests, issues, design docs, and code reviews. This knowledge is consumed briefly and then effectively lost, fragmented across tools and formats that make retrieval nearly impossible.

The consequences:
- Engineers spend days onboarding onto repos that teammates already understand
- The same questions get asked repeatedly across the organization
- Agents operating on codebases have no organizational context, producing code that works but violates conventions and ignores architectural decisions
- Knowledge walks out the door when people leave
- Decisions get re-litigated because nobody remembers the original rationale

### 1.2 Solution

AtlasKB extracts knowledge from repositories using multi-pass LLM analysis, normalizes it into a structured knowledge graph (entities, facts, decisions, relationships), indexes it for semantic retrieval, and serves it through interfaces designed for both humans and agents.

### 1.3 Target Users

- **Human engineers** on the team who need to understand, navigate, and contribute to repositories they didn't write
- **Agentic systems** (Claude Code, CI agents, custom agents) that need grounded organizational context to operate effectively on the team's codebases

---

## 2. Milestones

### Milestone 1: CLI Tool (MVP)

A usable CLI tool that indexes a local repo and allows a human to ask questions and get grounded answers.

**Delivers:**
- `atlaskb setup` — wizard for Postgres connection and API keys
- `atlaskb index <path>` — analyze a repo on disk (Phases 1, 2, 4, 5)
- `atlaskb ask "<question>"` — natural language Q&A with citations
- `atlaskb status` — show indexed repos and stats
- `atlaskb repos` — list indexed repositories
- `atlaskb retry <repo>` — retry failed extraction jobs
- Resumable extraction pipeline with progress tracking
- Cost estimation with confirmation before indexing

### Milestone 2: MCP Server

Expose the knowledge graph to agentic systems via structured tools.

**Delivers:**
- `atlaskb serve` — start the MCP server
- Agent tools: `get_conventions`, `get_module_context`, `get_service_contract`, `get_impact_analysis`, `get_decision_context`, `search_knowledge`

### Milestone 3: GitHub Integration

Add Phase 3 (history mining) to enrich the knowledge graph with PR/issue context.

**Delivers:**
- GitHub PAT configuration
- Phase 3 extraction: PR descriptions, review comments, issue threads
- Decision nodes with rationale linked to affected entities
- Re-indexing command to enrich previously indexed repos

### Milestone 4: Cross-Repo Intelligence

Add Phase 6 and organization-level features.

**Delivers:**
- Cross-repo relationship detection and linking
- Shared concept identification
- Organization-level architecture map
- Cross-repo impact analysis

---

## 3. Milestone 1 Requirements (MVP)

### 3.1 Setup Wizard

#### `atlaskb setup`

Interactive wizard that configures AtlasKB for first use.

**Flow:**

1. Prompt for PostgreSQL connection details:
   - Host (default: `localhost`)
   - Port (default: `5432`)
   - Database name (default: `atlaskb`)
   - Username (default: `atlaskb`)
   - Password (masked input)
2. Test the database connection
   - On failure: display error, allow retry
   - On success: run database migrations to create schema
3. Prompt for API keys:
   - Anthropic API key (required)
   - Voyage AI API key (required)
4. Validate API keys with a lightweight test call
5. Write configuration to `~/.atlaskb/config.toml`

**Config file format:**

```toml
[database]
host = "localhost"
port = 5432
database = "atlaskb"
user = "atlaskb"
password = "..."

[api]
anthropic_key = "sk-ant-..."
voyage_key = "vo-..."

[extraction]
max_concurrency = 10
```

**Environment variable overrides:**
- `ATLASKB_DATABASE_URL` — full connection string, overrides `[database]` section
- `ANTHROPIC_API_KEY` — overrides `api.anthropic_key`
- `VOYAGE_API_KEY` — overrides `api.voyage_key`

**Requirements:**
- Config file is created with `0600` permissions (user-only read/write)
- Running `atlaskb setup` when config already exists prompts to overwrite or update individual fields
- All other commands check for valid config on startup and direct user to `atlaskb setup` if missing

### 3.2 Repository Indexing

#### `atlaskb index <path>`

Analyzes a local repository and populates the knowledge graph.

**Input:** Absolute or relative path to a directory containing a git repository.

**Pre-flight:**
1. Validate the path exists and is a git repository
2. Detect repository identity from git remote (for dedup if re-indexed)
3. If this repo has been previously indexed:
   - Check for interrupted run → offer to resume
   - Check for completed run → compare file hashes, offer incremental re-index of changed files or full re-index
4. Run Phase 1 (structural inventory) to classify files and detect the stack
5. Estimate token cost based on file count, average file size, and analysis plan
6. Display summary and estimated cost, prompt for confirmation

```
Analyzing repository: my-service
  Path: /home/user/src/my-service
  Detected: Go 1.21, PostgreSQL, gRPC, Docker
  Files to analyze: 347 (skipping 89 generated/vendored)
  Git history: 1,247 commits, 43 contributors
  Estimated cost: ~$18.50 (2.1M tokens)

  Proceed? [y/N]
```

**Extraction phases (MVP):**

**Phase 1: Structural Inventory**
- Walk the file tree
- Classify each file: source code (by language), test, config, documentation, build/dependency, generated/vendored
- Detect technology stack from dependencies and framework patterns
- Identify entry points, module boundaries, and test structure
- Produce a manifest for subsequent phases
- No LLM calls — heuristic-based

**Phase 2: File-Level Analysis**
- For each non-skipped source file, send to LLM with structured prompts
- Extract per file:
  - Summary and responsibility
  - Entities defined (functions, types, endpoints, handlers)
  - Business rules and domain logic
  - Invariants and assumptions
  - Dependencies (imports, calls, data access)
  - Patterns and conventions followed
  - Anything notable or risky
- Parse LLM responses into Entity, Fact, and Relationship nodes
- Track each file as an individual job for resumability
- Parallelizable: bounded worker pool (configurable, default 10 concurrent)

**Git Log Analysis (lightweight Phase 3 substitute):**
- Parse git log for commit messages, authorship, timestamps, file change frequency
- Extract "when" dimension: when modules were introduced, last modified, change velocity
- Extract lightweight "why" signals from commit messages
- No GitHub API calls — purely from local git history

**Phase 4: Cross-Module Synthesis**
- Group entities by directory/package structure
- For each module group, send summaries + cross-boundary relationships to LLM
- Extract:
  - Inter-module interactions and data flows
  - Implicit and explicit contracts
  - Architectural patterns and boundaries
  - Coupling analysis
- Produce refined Relationship edges and architecture-level Facts

**Phase 5: Repository Summary**
- Send all entity summaries, architectural facts, and key relationships to LLM
- Extract:
  - One-paragraph repo description
  - Capability inventory (structured list of what this repo can do)
  - High-level architecture description
  - Integration points (what it depends on, what likely depends on it)
  - Convention guide (how to write code that fits this repo)
  - Known risks or tech debt signals
- Produce repo-level Entity node with comprehensive metadata

**Progress display:**

```
Phase 1: Structural inventory ........... done (2s)
Phase 2: File analysis .................. 198/347 [████████░░░░░░] 57%
  Current: internal/auth/middleware.go
  Tokens used: 1.2M | Est. remaining: ~$8.40
```

**Post-indexing:**
- Generate embeddings for all Fact nodes via Voyage API
- Store embeddings in pgvector index
- Display final summary:

```
Indexing complete.
  Entities: 1,247 | Facts: 4,891 | Decisions: 142 | Relationships: 3,456
  Tokens used: 2.1M | Cost: ~$18.50
  Duration: 18m 46s
```

**Resumability requirements:**
- Each file/module is an individual job with state: `pending`, `in_progress`, `completed`, `failed`
- Job state is persisted to Postgres before and after each LLM call
- If interrupted (crash, Ctrl+C, rate limit exhaustion), re-running `atlaskb index <path>` detects the incomplete run and resumes from the last incomplete job
- Completed jobs are never re-run unless the source file has changed (detected via content hash)
- Phase transitions are recorded — if Phase 2 completed but Phase 4 didn't, resume starts at Phase 4

**Error handling:**
- Failed LLM calls retry 3 times with exponential backoff
- Respect Anthropic rate limit headers (retry-after)
- After 3 retries, mark the job as `failed` and continue with remaining jobs
- A partially-indexed repo is still queryable
- `atlaskb retry <repo>` re-runs only failed jobs

### 3.3 Natural Language Q&A

#### `atlaskb ask "<question>" [--repo <name>]`

Ask a question and get a grounded answer with citations.

**Input:** Natural language question, optional repo filter.

**Process:**
1. Generate embedding for the question via Voyage API
2. Query the knowledge graph:
   - Vector similarity search against Fact embeddings
   - Filter by repo if `--repo` specified
   - Retrieve top-N relevant Facts with their associated Entities, Decisions, and Provenance
3. Assemble context from retrieved knowledge
4. Send to LLM (Sonnet) with instructions to:
   - Answer the question using only the provided knowledge
   - Cite sources throughout (file:line, commit SHA)
   - Indicate confidence level
   - Note if the answer may be incomplete (partial indexing)
5. Stream the response to the terminal

**Output format:**

```
Authentication is handled by the auth middleware in internal/auth/middleware.go:23.
Incoming requests must include a Bearer token in the Authorization header.
The middleware validates tokens using the shared secret defined in config/auth.go:8.

Invalid tokens receive a 401 response with a JSON error body (internal/auth/errors.go:15).
Token expiry is set to 15 minutes, configured via the AUTH_TOKEN_TTL environment
variable (config/env.go:42).

Sources:
  - internal/auth/middleware.go:23-58
  - config/auth.go:8-12
  - internal/auth/errors.go:15-30
  - config/env.go:42
```

**Requirements:**
- Response is streamed to the terminal as it's generated (not buffered)
- If no relevant knowledge is found, say so rather than hallucinating
- If repo filter is specified and the repo isn't indexed, display an error with `atlaskb repos` suggestion
- Query latency target: first token within 2 seconds (excluding LLM generation time)

### 3.4 Status & Management

#### `atlaskb status`

Display overview of all indexed repositories and system health.

```
AtlasKB Status
──────────────
Database: connected (localhost:5432/atlaskb)
Repos indexed: 3

  my-service      1,247 entities  4,891 facts  18m ago   ✓ complete
  auth-lib          312 entities  1,023 facts   2h ago   ✓ complete
  data-pipeline     891 entities  3,201 facts   5m ago   ⧖ in progress (Phase 2: 401/523)
```

#### `atlaskb repos`

List all indexed repositories with basic metadata.

```
my-service       Go     /home/user/src/my-service        complete
auth-lib         Rust   /home/user/src/auth-lib           complete
data-pipeline    Python /home/user/src/data-pipeline      indexing
```

#### `atlaskb retry <repo>`

Re-run failed extraction jobs for a given repo.

---

## 4. Knowledge Graph Schema

Full schema defined in [docs/knowledge-model.md](knowledge-model.md). Summary:

### Tables

**entities**
| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| kind | ENUM | module, service, function, type, endpoint, concept, config |
| name | TEXT | Short name |
| qualified_name | TEXT | Fully qualified name |
| repo_id | UUID | FK to repos |
| file_path | TEXT | Source file path (nullable) |
| summary | TEXT | LLM-generated description |
| capabilities | JSONB | What this entity can do |
| assumptions | JSONB | What this entity assumes |
| created_at | TIMESTAMPTZ | When first indexed |
| updated_at | TIMESTAMPTZ | When last analyzed |

**facts**
| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| entity_id | UUID | FK to entities |
| claim | TEXT | Natural language statement |
| dimension | ENUM | what, how, why, when |
| category | ENUM | behavior, constraint, pattern, convention, debt, risk |
| confidence | ENUM | high, medium, low |
| provenance | JSONB | Array of source references |
| embedding | VECTOR(1024) | Voyage embedding |
| created_at | TIMESTAMPTZ | |
| updated_at | TIMESTAMPTZ | |
| superseded_by | UUID | FK to facts (nullable) |

**decisions**
| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| repo_id | UUID | FK to repos |
| summary | TEXT | One-line description |
| description | TEXT | Full context |
| rationale | TEXT | Why this choice was made |
| alternatives | JSONB | What else was considered |
| tradeoffs | JSONB | What was accepted/sacrificed |
| provenance | JSONB | Source references |
| made_at | TIMESTAMPTZ | When the decision was made |
| created_at | TIMESTAMPTZ | |
| still_valid | BOOLEAN | Has this been superseded? |

**relationships**
| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| from_entity_id | UUID | FK to entities |
| to_entity_id | UUID | FK to entities |
| kind | ENUM | depends_on, calls, implements, extends, produces, consumes, replaced_by, tested_by, configured_by, owns |
| description | TEXT | Optional natural language description |
| strength | ENUM | strong, moderate, weak |
| provenance | JSONB | Source references |
| created_at | TIMESTAMPTZ | |

**repos**
| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| name | TEXT | Repository name |
| path | TEXT | Local filesystem path |
| remote_url | TEXT | Git remote URL (nullable) |
| languages | JSONB | Detected languages |
| stack | JSONB | Detected technology stack |
| summary | TEXT | LLM-generated repo summary |
| capabilities | JSONB | Repo-level capability inventory |
| conventions | JSONB | Repo-level convention guide |
| status | ENUM | indexing, complete, failed, stale |
| indexed_at | TIMESTAMPTZ | Last completed indexing |
| created_at | TIMESTAMPTZ | |

**extraction_jobs**
| Column | Type | Description |
|---|---|---|
| id | UUID | Primary key |
| repo_id | UUID | FK to repos |
| phase | INTEGER | Which extraction phase |
| target | TEXT | File path, module name, etc. |
| target_hash | TEXT | Content hash for change detection |
| status | ENUM | pending, in_progress, completed, failed |
| attempts | INTEGER | Number of attempts |
| tokens_used | INTEGER | Tokens consumed |
| error | TEXT | Last error message (nullable) |
| started_at | TIMESTAMPTZ | |
| completed_at | TIMESTAMPTZ | |
| created_at | TIMESTAMPTZ | |

### Indexes

- `facts.embedding` — pgvector HNSW index for semantic search
- `facts.entity_id` — FK lookup
- `facts.dimension`, `facts.category`, `facts.confidence` — filtering
- `entities.repo_id`, `entities.kind` — scoped queries
- `entities.qualified_name` — lookup by name
- `relationships.from_entity_id`, `relationships.to_entity_id` — graph traversal
- `extraction_jobs.repo_id, extraction_jobs.status` — resume queries

---

## 5. Extraction Prompts

The quality of the knowledge graph depends entirely on the quality of the LLM prompts. These are the core prompts for each phase.

### 5.1 Phase 2: File Analysis Prompt

```
You are analyzing a source code file to extract structured knowledge for a knowledge graph.

Repository: {repo_name}
Technology stack: {detected_stack}
File path: {file_path}
File classification: {classification}

File contents:
---
{file_contents}
---

Analyze this file and return a JSON response with the following structure:

{
  "summary": "One paragraph describing this file's purpose and responsibility",
  "entities": [
    {
      "kind": "function|type|endpoint|handler|config",
      "name": "entity name",
      "summary": "what this entity does",
      "capabilities": ["what it can do"],
      "assumptions": ["what it assumes to be true"],
      "invariants": ["conditions that must hold"]
    }
  ],
  "facts": [
    {
      "entity_name": "which entity this is about (or null for file-level)",
      "claim": "a specific, grounded claim",
      "dimension": "what|how|why|when",
      "category": "behavior|constraint|pattern|convention|debt|risk",
      "confidence": "high|medium|low",
      "line_range": "start-end line numbers"
    }
  ],
  "dependencies": [
    {
      "from": "entity in this file",
      "to": "entity or module being depended on",
      "kind": "calls|depends_on|implements|extends|produces|consumes",
      "description": "nature of the dependency"
    }
  ],
  "conventions": ["patterns or conventions this file follows"],
  "risks": ["anything notable, risky, or potentially problematic"]
}

Guidelines:
- Be specific and grounded. Every claim should be traceable to specific code.
- Distinguish between what you can directly observe (high confidence) and what you infer (medium/low).
- Focus on business logic and domain rules, not boilerplate.
- Note assumptions that callers must satisfy.
- Note invariants that maintainers must preserve.
- Identify any tech debt, risks, or code smells.
```

### 5.2 Phase 4: Cross-Module Synthesis Prompt

```
You are analyzing how multiple modules interact within a repository to extract architectural knowledge.

Repository: {repo_name}
Module group: {module_path}

Module summaries:
---
{module_summaries}
---

Cross-boundary relationships detected:
---
{cross_boundary_relationships}
---

Analyze the interactions between these modules and return a JSON response:

{
  "architecture_pattern": "description of the architectural pattern this group follows",
  "data_flows": [
    {
      "description": "how data flows through these modules",
      "path": ["module_a", "module_b", "module_c"],
      "data_description": "what data is flowing"
    }
  ],
  "contracts": [
    {
      "between": ["module_a", "module_b"],
      "description": "the implicit or explicit contract",
      "type": "explicit|implicit",
      "confidence": "high|medium|low"
    }
  ],
  "boundaries": [
    {
      "description": "an architectural boundary",
      "enforced_by": "how it's enforced (if at all)"
    }
  ],
  "coupling_analysis": [
    {
      "modules": ["module_a", "module_b"],
      "coupling": "tight|moderate|loose",
      "description": "nature of the coupling"
    }
  ],
  "facts": [
    {
      "claim": "architectural-level claim",
      "dimension": "what|how|why",
      "category": "pattern|constraint|risk|debt",
      "confidence": "high|medium|low"
    }
  ]
}
```

### 5.3 Phase 5: Repository Summary Prompt

```
You are creating a comprehensive summary of an entire repository based on deep analysis.

Repository: {repo_name}
Technology stack: {detected_stack}

Entity summaries (by module):
---
{entity_summaries}
---

Architectural facts:
---
{architecture_facts}
---

Key relationships:
---
{key_relationships}
---

Git history summary:
---
{git_summary}
---

Create a comprehensive repository summary as JSON:

{
  "description": "One paragraph describing what this repository is and does",
  "capabilities": [
    {
      "name": "capability name",
      "description": "what it does and how",
      "key_entities": ["entities that implement this"]
    }
  ],
  "architecture": {
    "pattern": "high-level architecture pattern",
    "description": "how the system is structured",
    "layers": ["layer descriptions"],
    "key_boundaries": ["important architectural boundaries"]
  },
  "integration_points": {
    "depends_on": ["external systems this repo depends on"],
    "depended_on_by": ["likely consumers of this repo (inferred)"]
  },
  "conventions": {
    "error_handling": "how errors are handled",
    "testing": "testing patterns and conventions",
    "naming": "naming conventions",
    "file_organization": "how code is organized",
    "other": ["other notable conventions"]
  },
  "risks_and_debt": [
    {
      "description": "risk or tech debt item",
      "severity": "high|medium|low",
      "affected_entities": ["entities affected"]
    }
  ]
}
```

---

## 6. Non-Functional Requirements

### 6.1 Performance

- **Extraction:** Throughput limited by LLM API rate limits, not by AtlasKB. Target: process files as fast as the API allows within configured concurrency.
- **Query (ask):** First token streamed within 2 seconds of query submission (excluding LLM generation time). Vector search + context assembly should complete in <500ms.
- **Embedding generation:** Batched to minimize API round-trips during indexing.

### 6.2 Reliability

- **Resumable extraction:** Any interruption (crash, Ctrl+C, rate limit, network failure) loses at most one in-flight LLM call. All completed work is persisted.
- **Graceful shutdown:** SIGINT/SIGTERM handler completes in-flight LLM calls (with timeout) before exiting and persisting state.
- **Partial availability:** A partially-indexed repo is queryable. Answers note when knowledge may be incomplete.

### 6.3 Observability

- **Structured logging** to stderr (JSON format in production, human-readable in development)
- **Token tracking** per phase, per file, per repo — stored in extraction_jobs and summarized in CLI output
- **Cost reporting** in `atlaskb status` (total tokens and estimated cost per repo)

### 6.4 Security

- Config file written with `0600` permissions
- API keys never logged or displayed after setup (masked in status output)
- No secrets in database — API keys live only in the config file or environment
- Database connection supports SSL

### 6.5 Compatibility

- **Operating systems:** macOS, Linux
- **Go version:** 1.22+
- **PostgreSQL:** 15+ with pgvector extension
- **Git:** repos must be valid git repositories

---

## 7. CLI Interface Summary

```
atlaskb — organizational knowledge graph for your repositories

Usage:
  atlaskb <command> [flags]

Commands:
  setup                 Configure database connection and API keys
  index <path>          Analyze a repository and build the knowledge graph
  ask "<question>"      Ask a question about your indexed repositories
  status                Show system status and indexed repository overview
  repos                 List all indexed repositories
  retry <repo>          Retry failed extraction jobs for a repository

Flags (global):
  --config <path>       Path to config file (default: ~/.atlaskb/config.toml)
  --verbose             Enable verbose logging
  --json                Output in JSON format (for scripting)

Flags (ask):
  --repo <name>         Scope question to a specific repository

Flags (index):
  --concurrency <n>     Max parallel LLM calls (default: 10)
  --force               Re-index all files even if unchanged
  --dry-run             Show analysis plan and cost estimate without executing
  --yes                 Skip confirmation prompt
```

---

## 8. Future Considerations

Captured for context but explicitly **not in scope** for Milestone 1:

- **MCP server** for agentic retrieval (Milestone 2)
- **GitHub API integration** for PR/issue mining (Milestone 3)
- **Cross-repo linking** and org-level architecture maps (Milestone 4)
- **Web UI** for browsing the knowledge graph
- **Webhook listener** for automatic re-indexing on push
- **OAuth login** with enterprise Anthropic/OpenAI accounts
- **Feedback API** for humans/agents to flag incorrect facts
- **Auto-discovery** of repos from GitHub org
- **Multi-tenancy** and access control
