# Decisions

Key decisions made before PRD and scaffolding. These inform scope, priorities, and implementation choices.

## MVP Scope

The first usable version is a **CLI tool** that:

1. Takes a path to an already-cloned repo on disk
2. Runs the extraction pipeline (all phases applicable to a single repo)
3. Stores the knowledge graph in a local PostgreSQL database
4. Allows a human to ask natural language questions and get grounded answers

No web UI. No webhook-based incremental updates. No cross-repo linking (Phase 6). No GitHub API calls (Phase 3 history mining). Those come later. The MVP analyzes only what's on disk: source code, docs, and git log history. The goal is: index one repo, ask it questions, validate the knowledge quality.

The second milestone adds the **MCP server** so agents can query the same knowledge graph via structured tools. Phase 3 (GitHub PR/issue mining) is added in a later milestone.

### MVP phases

- Phase 1: Structural inventory (on disk)
- Phase 2: File-level analysis (on disk)
- Phase 3: **Deferred** — requires GitHub API, not MVP
- Phase 4: Cross-module synthesis (from Phase 1-2 outputs)
- Phase 5: Repository summary (from Phase 1-2-4 outputs)
- Phase 6: **Deferred** — requires multiple repos indexed

Git log analysis (commit messages, authorship, file change frequency) is included in MVP as a lightweight substitute for full Phase 3 — it lives on disk and provides the "when" dimension and some "why" signal.

### MVP user flow

```
$ atlaskb setup

  AtlasKB Setup Wizard
  ────────────────────
  Database connection
    Host [localhost]:
    Port [5432]:
    Database [atlaskb]:
    User [atlaskb]:
    Password: ********

  Testing connection... ✓
  Running migrations... ✓

  API Keys
    Anthropic API key: sk-ant-...
    Voyage API key: vo-...

  Configuration saved to ~/.atlaskb/config.toml

$ atlaskb index /path/to/repo

Analyzing repository: my-service
  Detected: Go, PostgreSQL, gRPC, Docker
  Files to analyze: 347 (skipping 89 generated/vendored)
  Estimated cost: ~$18.50 (2.1M tokens)

  Proceed? [y/N] y

  Phase 1: Structural inventory ........... done (2s)
  Phase 2: File analysis .................. done (12m 34s) [347/347 files]
  Phase 4: Cross-module synthesis ......... done (1m 45s) [12 modules]
  Phase 5: Repository summary ............. done (15s)

  Indexing complete.
  Entities: 1,247 | Facts: 4,891 | Decisions: 142 | Relationships: 3,456

$ atlaskb ask "how does authentication work in this service?"

  Authentication is handled by the auth middleware in internal/auth/middleware.go.
  Incoming requests must include a Bearer token in the Authorization header...
  [grounded answer with citations]

$ atlaskb ask "what databases are used across all repos?"

  Searching across 5 indexed repositories...
  [answer spanning multiple repos]

$ atlaskb ask "how does auth work?" --repo my-service

  [answer scoped to my-service only]
```

## User Interaction

### CLI tool (`atlaskb`)

Primary interface for both indexing and querying.

Commands:
- `atlaskb index <path>` — analyze a local repo and populate the knowledge graph
- `atlaskb ask "<question>"` — ask a natural language question
- `atlaskb status` — show indexed repos, fact counts, last updated
- `atlaskb repos` — list all indexed repositories

### MCP server (milestone 2)

Exposes the structured agent tools (`get_conventions`, `get_module_context`, etc.) over the MCP protocol. Started via:
- `atlaskb serve` — starts the MCP server

## Setup & Configuration

First-run experience is a **setup wizard** via `atlaskb setup` that walks the user through:

1. **Database connection** — host, port, database, user, password for a user-managed PostgreSQL instance. AtlasKB does not manage Postgres — the user provisions it. The wizard tests the connection and runs migrations.
2. **API keys** — Anthropic API key (required), Voyage API key (required).
3. **Writes config** to `~/.atlaskb/config.toml`.

Environment variables (`ANTHROPIC_API_KEY`, `VOYAGE_API_KEY`, `ATLASKB_DATABASE_URL`) override config file values, for CI/scripting use cases.

OAuth integration with enterprise Anthropic/OpenAI accounts is a future enhancement.

## Repo Management

Repos are added manually via `atlaskb index <path>`. No auto-discovery, no config file listing repos. The CLI points at a directory on disk — AtlasKB doesn't clone anything itself.

This keeps the MVP simple. The user is responsible for having the repo cloned and up to date. Auto-discovery and managed cloning are future enhancements.

## Query Scoping

`atlaskb ask` searches across **all indexed repos** by default. A `--repo` flag scopes to a single repo.

## GitHub Integration (post-MVP)

Phase 3 (PR/issue/comment mining) requires GitHub API access. When added:
- Authentication via GitHub PAT (`GITHUB_TOKEN` env var or in config)
- AtlasKB resolves the GitHub remote from the repo's git config to know which repo to query
- GraphQL API (v4) for efficient batch fetching

## Cost Management

**Philosophy: burn the tokens.** The value of deep understanding far exceeds the API cost.

**But:** before starting an indexing run, AtlasKB estimates the token cost based on file count, average file size, and number of PRs/issues. The user sees the estimate and confirms before any LLM calls are made.

```
Estimated cost: ~$18.50 (2.1M tokens)
Proceed? [y/N]
```

No hard budget limits. No approval gates beyond the confirmation prompt. Just transparency.

Cost tracking is logged per run so the team can see spend over time.

## Testing Strategy

Keep it simple. The base testing approach:

- **Unit tests** for pure logic (file classification, graph construction, query planning)
- **Interface-based LLM client** so tests can swap in a mock that returns canned responses
- **Golden file tests** for the extraction pipeline: record real LLM responses for a small test repo, replay them in CI to verify parsing and graph construction remain correct
- **Integration tests** that run against a real PostgreSQL instance (via Docker in CI)

No property-based testing, no fuzzing, no elaborate test infrastructure. Just enough to catch regressions and validate the plumbing.

## Resilience & Progress

### Rate limiting

AtlasKB rate-limits its own LLM calls to stay within API limits:
- Configurable concurrency (e.g., max 10 parallel LLM calls)
- Respect Anthropic's rate limit headers (retry-after, etc.)
- Exponential backoff on 429s and 5xx errors

### Resumable extraction

The extraction pipeline tracks progress at a granular level so it can resume from where it left off:

- Each file/PR/issue is tracked as an individual job in the database
- Jobs have states: `pending`, `in_progress`, `completed`, `failed`
- If the process is interrupted (crash, Ctrl+C, rate limit exhaustion), re-running `atlaskb index <path>` picks up from the last incomplete job
- Completed extractions are never re-run unless the source has changed

```
$ atlaskb index /path/to/repo

  Resuming previous run (interrupted at Phase 2, file 198/347)
  Phase 2: File analysis .................. done (6m 12s) [149/347 remaining]
  ...
```

### Failure handling

- Failed LLM calls retry 3 times with exponential backoff
- Permanently failed jobs are marked `failed` and skipped — the rest of the pipeline continues
- Failed jobs can be retried later via `atlaskb retry <repo>`
- A partially-indexed repo is still queryable — answers are grounded in whatever knowledge exists, with a note that indexing is incomplete
