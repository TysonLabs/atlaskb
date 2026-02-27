# Architecture Overview

High-level system architecture for AtlasKB.

## System Diagram

```
                    ┌──────────────────────────────────────────────────────┐
                    │                   GitHub                             │
                    │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐  │
                    │  │ Repo A  │ │ Repo B  │ │ Repo C  │ │ Repo N  │  │
                    │  └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘  │
                    └───────┼───────────┼───────────┼───────────┼────────┘
                            │           │           │           │
                            ▼           ▼           ▼           ▼
                    ┌──────────────────────────────────────────────────────┐
                    │              Ingestion Layer                         │
                    │                                                      │
                    │  ┌─────────────┐  ┌──────────────┐  ┌───────────┐  │
                    │  │ Git Cloner  │  │ GitHub API   │  │ Webhook   │  │
                    │  │             │  │ (PRs, Issues)│  │ Listener  │  │
                    │  └──────┬──────┘  └──────┬───────┘  └─────┬─────┘  │
                    └─────────┼────────────────┼────────────────┼─────────┘
                              │                │                │
                              ▼                ▼                ▼
                    ┌──────────────────────────────────────────────────────┐
                    │              Extraction Pipeline                     │
                    │                                                      │
                    │  ┌───────────┐ ┌───────────┐ ┌────────────────────┐ │
                    │  │ Phase 1   │ │ Phase 2   │ │ Phase 3            │ │
                    │  │ Inventory │→│ File      │→│ History Mining     │ │
                    │  │           │ │ Analysis  │ │                    │ │
                    │  └───────────┘ └───────────┘ └────────────────────┘ │
                    │  ┌───────────┐ ┌───────────┐ ┌────────────────────┐ │
                    │  │ Phase 4   │ │ Phase 5   │ │ Phase 6            │ │
                    │  │ Synthesis │→│ Repo      │→│ Cross-Repo         │ │
                    │  │           │ │ Summary   │ │ Linking            │ │
                    │  └───────────┘ └───────────┘ └────────────────────┘ │
                    │                                                      │
                    │  ┌──────────────────────────────────────────────┐   │
                    │  │     LLM Provider (Local OpenAI-Compatible)    │   │
                    │  └──────────────────────────────────────────────┘   │
                    └─────────────────────────┬────────────────────────────┘
                                              │
                                              ▼
                    ┌──────────────────────────────────────────────────────┐
                    │              Knowledge Graph Store                   │
                    │                                                      │
                    │  ┌──────────┐ ┌──────────┐ ┌──────────────────────┐ │
                    │  │ Entities │ │ Facts    │ │ Decisions            │ │
                    │  └──────────┘ └──────────┘ └──────────────────────┘ │
                    │  ┌──────────┐ ┌──────────────────────────────────┐  │
                    │  │Relations │ │ Vector Index (embeddings)        │  │
                    │  └──────────┘ └──────────────────────────────────┘  │
                    │                                                      │
                    │  ┌──────────────────────────────────────────────┐   │
                    │  │      PostgreSQL + pgvector                    │   │
                    │  └──────────────────────────────────────────────┘   │
                    └─────────────────────────┬────────────────────────────┘
                                              │
                                              ▼
                    ┌──────────────────────────────────────────────────────┐
                    │              Retrieval Layer                         │
                    │                                                      │
                    │  ┌──────────────────┐  ┌───────────────────────┐    │
                    │  │  MCP Server      │  │  HTTP API             │    │
                    │  │  (Agent Tools)   │  │  (Human Q&A, Web UI)  │    │
                    │  └────────┬─────────┘  └───────────┬───────────┘    │
                    └───────────┼─────────────────────────┼────────────────┘
                                │                         │
                    ┌───────────▼──────┐     ┌────────────▼────────────┐
                    │  Agentic Systems │     │  Engineers              │
                    │  (Claude Code,   │     │  (CLI, Web, IDE)       │
                    │   CI agents,     │     │                         │
                    │   custom agents) │     │                         │
                    └──────────────────┘     └─────────────────────────┘
```

## Component Responsibilities

### Ingestion Layer
- Clones repositories and keeps local copies in sync
- Fetches PRs, issues, comments, and review threads via GitHub API
- Listens for webhooks to trigger incremental re-analysis
- Manages the queue of repos and artifacts to be processed

### Extraction Pipeline
- Orchestrates the six-phase analysis process
- Manages LLM calls: batching, parallelism, rate limiting, retries
- Transforms raw LLM responses into structured graph nodes and edges
- Tracks what has been analyzed and what needs re-analysis
- Handles cost tracking and budget management

### Knowledge Graph Store
- Persists all entities, facts, decisions, and relationships
- Maintains the vector index for semantic search
- Handles fact versioning (superseded_by chains)
- Provides graph traversal queries (impact analysis, dependency chains)
- Manages staleness detection and re-analysis triggers

### Retrieval Layer
- MCP server for agent tool access
- HTTP API for human-facing applications
- Query planning: decides whether to use vector search, graph traversal, or both
- Response synthesis: assembles facts into coherent answers (for human mode)
- Access control: respects repository-level permissions

## Technology Choices (Preliminary)

| Component | Technology | Rationale |
|---|---|---|
| Language | Go or Rust | Pipeline needs concurrency and performance for parallel LLM calls |
| Database | PostgreSQL + pgvector | Single store for relational data and vectors, proven at scale |
| LLM | Local OpenAI-compatible (qwen3.5-35b-a3b) | Local inference via LM Studio, no cloud API key needed |
| Embeddings | Local OpenAI-compatible (mxbai-embed-large-v1) | High-quality embeddings via local server, no cloud API key needed |
| MCP Server | TypeScript or Python | MCP SDK maturity, fast iteration on tool definitions |
| Queue | PostgreSQL (LISTEN/NOTIFY) or Redis | Keep infra simple early, graduate to dedicated queue if needed |
| GitHub | GitHub API v4 (GraphQL) | Efficient batch fetching of PRs, issues, comments |

## Deployment

Initially: single-node deployment (API server + worker processes + PostgreSQL).

The extraction pipeline is inherently parallelizable (file analysis, PR analysis are independent) but the system doesn't need distributed infrastructure early on. A single machine with good concurrency handling can process repos in the background while serving queries.

Scale concerns to address later:
- Extraction workers as separate processes/containers for horizontal scaling
- Read replicas for the query path if retrieval load grows
- Object storage for raw cloned repos if disk becomes a constraint

## Data Flow: Initial Indexing

```
1. User adds a repo via CLI or API
2. Ingestion layer clones the repo, fetches GitHub metadata
3. Phase 1 runs: produces a manifest and analysis plan
4. Phases 2-3 run in parallel: file analysis + history mining
5. Phase 4 runs: cross-module synthesis using Phase 2-3 outputs
6. Phase 5 runs: repo-level summary
7. All nodes/edges written to the knowledge graph
8. Embeddings generated and indexed for all facts
9. Repo marked as "indexed" — available for queries
```

## Data Flow: Incremental Update

```
1. Webhook fires on push to main (or polling detects new commits)
2. Ingestion layer pulls changes, identifies affected files and new PRs
3. Phase 2 re-runs on changed files only
4. Phase 3 runs on new PRs/issues
5. Phase 4 selectively re-runs for affected modules
6. Updated nodes/edges replace stale ones in the graph
7. Affected embeddings re-generated
8. Stale facts on adjacent entities flagged for review
```
