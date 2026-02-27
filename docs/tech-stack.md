# Tech Stack

## Philosophy

One language, one database, one binary. Add complexity only when proven necessary.

## Decisions

### Language: Go

The extraction pipeline is fundamentally a concurrent I/O orchestrator — fan out hundreds of LLM API calls, collect results, write to the database. Go's goroutines and channels are purpose-built for this pattern. The MCP server and query engine live in the same binary, keeping deployment simple.

### Database: PostgreSQL + pgvector

One database for everything:

- **Relational tables** for entities, facts, decisions, relationships (the graph structure)
- **pgvector** extension for embedding-based semantic search
- **JSONB columns** for flexible metadata that doesn't need its own schema yet
- **Native full-text search** for keyword/BM25-style queries alongside vector search
- **Recursive CTEs** for graph traversal (impact analysis, dependency chains)
- **Job queue** via a simple status-tracked table for extraction pipeline orchestration

No separate graph database. No separate vector database. No separate message broker. Postgres handles all of it until scale proves otherwise.

### LLM: Local OpenAI-Compatible Server (LM Studio)

LLM inference runs locally via an OpenAI-compatible chat completions endpoint (e.g. LM Studio), same server as embeddings. No cloud API key required.

- **`qwen/qwen3.5-35b-a3b`** for both extraction and synthesis phases
- Any OpenAI-compatible `/v1/chat/completions` endpoint can be used by configuring the base URL

### Embeddings: Local OpenAI-Compatible Server (LM Studio)

Embeddings run locally via an OpenAI-compatible server (e.g. LM Studio), eliminating the need for a cloud API key.

- **`mxbai-embed-large-v1`** (1024 dimensions) for all facts — code and natural language
- Any OpenAI-compatible embeddings endpoint can be used by configuring the base URL

### GitHub Integration: GraphQL API (v4)

GraphQL enables batch-fetching PRs with their comments, reviews, and linked issues in a single call instead of dozens of REST round-trips. Critical for efficient history mining across large repos.

### MCP Server: Go (built-in)

The MCP server is a thin layer over the query engine. Keeping it in Go means one build, one binary, one deploy. The MCP protocol is simple enough that a dedicated SDK isn't necessary.

### Orchestration: Go concurrency + Postgres job queue

The extraction pipeline uses:
- Goroutines with bounded worker pools for parallel LLM calls
- A Postgres-backed `extraction_jobs` table for tracking state, retries, and progress
- No external orchestrator (Temporal, Dagster, etc.) unless complexity warrants it later

### Observability: OpenTelemetry + structured logging

Non-negotiable for a system that burns tokens. Key metrics:

- Token spend per repo, per phase, per file
- Extraction latency and throughput
- LLM error rates, retries, and rate limit hits
- Query latency and cache hit rates on the retrieval side
- Fact count, staleness rate, and feedback signals

## What we intentionally avoid

| Technology | Why not |
|---|---|
| Neo4j / graph database | Postgres recursive CTEs handle graph traversal until millions of nodes. One less system to operate. |
| Pinecone / Qdrant / Weaviate | Separate vector store means syncing data between systems. pgvector keeps vectors co-located with the relational data they describe. |
| Kafka / RabbitMQ | No throughput to justify a message broker. Postgres LISTEN/NOTIFY or a job table covers our needs. |
| Kubernetes | Single binary + Postgres. Deploy to a VM or single container. Graduate to k8s when horizontal scaling is actually needed. |
| Microservices | One repo, one binary, multiple internal packages. The pipeline, query engine, and MCP server run in one process. Split later if independent scaling is needed. |
| External orchestrator | Go's concurrency model + Postgres job tracking is sufficient. Adding Temporal or Dagster is overhead we don't need yet. |

## Deployment

```
┌─────────────────────────────────┐
│         Single Binary           │
│                                 │
│  ┌───────────┐ ┌─────────────┐ │
│  │ Extraction │ │ MCP Server  │ │
│  │ Pipeline   │ │ + HTTP API  │ │
│  └─────┬─────┘ └──────┬──────┘ │
│        │               │        │
│        └───────┬───────┘        │
│                │                │
│         ┌──────▼──────┐        │
│         │ Query Engine │        │
│         └──────┬──────┘        │
└────────────────┼────────────────┘
                 │
          ┌──────▼──────┐
          │  PostgreSQL  │
          │  + pgvector  │
          └─────────────┘
```

Initial deployment: single VM or container running the Go binary alongside a managed PostgreSQL instance. No container orchestration, no service mesh, no multi-region. Ship and validate before scaling.
