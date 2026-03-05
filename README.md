# AtlasKB

AtlasKB is a code knowledge graph runtime for engineering teams and coding agents.

It indexes repositories into a structured graph (entities, facts, decisions, relationships), then serves that context through:

- CLI workflows (`index`, `ask`, `status`, `repos`, `retry`, `link`)
- A web dashboard + REST API
- MCP tools over HTTP (`/mcp`) and legacy stdio transport

## Table of Contents

- [What AtlasKB Does](#what-atlaskb-does)
- [Architecture](#architecture)
- [Implementation Status](#implementation-status)
- [Current Limitations](#current-limitations)
- [Requirements](#requirements)
- [Install](#install)
- [Quick Start](#quick-start)
- [Docker Quick Start](#docker-quick-start)
- [Using Runtime and CLI Together](#using-runtime-and-cli-together)
- [Configuration](#configuration)
- [CLI Reference](#cli-reference)
- [Indexing Pipeline (Detailed)](#indexing-pipeline-detailed)
- [Retrieval and Querying](#retrieval-and-querying)
- [MCP Interface](#mcp-interface)
- [REST API Surface](#rest-api-surface)
- [Homebrew Tap and Release Automation](#homebrew-tap-and-release-automation)
- [Development](#development)
- [Troubleshooting](#troubleshooting)
- [Repository Layout](#repository-layout)
- [Additional Docs](#additional-docs)

## What AtlasKB Does

AtlasKB turns source code and engineering history into queryable organizational memory.

It extracts and links:

- `entities`: modules, services, functions, types, endpoints, configs, concepts, clusters
- `facts`: grounded claims with dimension/category/confidence/provenance
- `decisions`: rationale and tradeoffs mined from git history and GitHub PRs/issues
- `relationships`: calls, depends_on, implements, extends, configured_by, member_of, etc.
- `execution_flows`: detected call-chain flows from entry points

Primary outcomes:

- Faster onboarding to large codebases
- Better cross-repo impact analysis
- Agent context grounded in real project conventions and architecture

## Architecture

```text
Local Repos + Git + GitHub
          |
          v
  Multi-phase Indexing Pipeline
  (static + LLM + backfill + clustering)
          |
          v
PostgreSQL + pgvector Knowledge Graph
(entities, facts, decisions, relationships, flows)
          |
          +--------------------------+
          |                          |
          v                          v
      CLI / Ask                   Runtime Server
                                  - Web Dashboard
                                  - REST API (/api/*)
                                  - MCP over HTTP (/mcp)
```

Runtime modes:

- `atlaskb` (default): starts combined runtime (dashboard + MCP HTTP)
- `atlaskb serve`: same runtime explicitly
- `atlaskb mcp`: stdio MCP server for clients that require stdio transport

## Implementation Status

As of `2026-03-05`, backlog status is:

- Implemented: contract checks, feedback lifecycle, observability baseline, exclusion controls, ETag caching
- Partial: staleness/revalidation policy depth, parser quality guardrails
- Not implemented yet: API/MCP authz baseline, webhook/polling ingestion worker, managed clone/discovery lifecycle, retrieval eval harness, security hardening, packaging beyond Homebrew

Detailed evidence and sequencing: [`docs/IMPLEMENTATION-GAP-REPORT.md`](docs/IMPLEMENTATION-GAP-REPORT.md)

## Current Limitations

- API and MCP HTTP currently do not enforce built-in authentication/authorization boundaries.
- Treat runtime endpoints as trusted local/dev infrastructure unless protected by network or reverse-proxy auth.
- Repository onboarding requires an existing local git checkout path.
- Incremental ingestion is currently manual (`atlaskb index` or reindex endpoints), not webhook-driven.

## Requirements

Required:

- Go `1.26` (for source builds)
- Node.js + npm (for web build during source/Homebrew builds)
- PostgreSQL with extensions:
  - `vector` (pgvector)
  - `uuid-ossp`
- OpenAI-compatible LLM endpoint for extraction/synthesis
- OpenAI-compatible embeddings endpoint

Recommended:

- Universal Ctags (`brew install universal-ctags`) for better entity grounding

## Install

### Option A: Private Homebrew Tap (macOS)

```bash
brew tap <owner>/atlaskb https://github.com/<owner>/homebrew-atlaskb.git
brew install <owner>/atlaskb/atlaskb
```

Why `brew install <owner>/atlaskb/atlaskb` and not `brew install <owner>/atlaskb`?

- Homebrew install syntax is `owner/tap/formula`.
- Here, tap name is `atlaskb` and formula name is also `atlaskb`, so both segments appear.

Run as a background service:

```bash
atlaskb setup
brew services start atlaskb
brew services list | grep atlaskb
```

Stop service:

```bash
brew services stop atlaskb
```

### Option B: Build from Source

```bash
git clone https://github.com/tgeorge06/atlaskb.git
cd atlaskb
make build-full
./bin/atlaskb version
```

`make build-full` builds web assets first, then the Go binary.

### Option C: Install Script (curl / PowerShell)

Linux/macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/tgeorge06/atlaskb/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/tgeorge06/atlaskb/main/install.ps1 | iex
```

Scoop (Windows):

```powershell
scoop bucket add atlaskb https://github.com/tgeorge06/scoop-atlaskb
scoop install atlaskb
```

### Option D: Docker Compose (Fast Local Bring-Up)

```bash
docker compose up --build
```

This starts:

- AtlasKB runtime on `http://localhost:3000`
- PostgreSQL + pgvector as `db` service

On first run (no config), AtlasKB serves setup mode at `http://localhost:3000`.
Complete setup in the browser, then restart runtime:

```bash
docker compose restart atlaskb
```

## Quick Start

1. Run setup wizard:

```bash
atlaskb setup
```

Aliases: `atlaskb configure`, `atlaskb init`

2. Start runtime:

```bash
atlaskb
```

Default endpoints:

- Dashboard: `http://localhost:3000`
- MCP HTTP: `http://localhost:3000/mcp`

3. Index a repository:

```bash
atlaskb index /path/to/repo
```

4. Ask questions:

```bash
atlaskb ask "How is authentication handled?"
atlaskb ask "Where are retries implemented?" --repo my-service
```

5. Inspect status:

```bash
atlaskb status
atlaskb repos
```

## Docker Quick Start

Start stack:

```bash
docker compose up --build
```

Open setup UI:

```bash
open http://localhost:3000
```

After setup is saved, restart runtime:

```bash
docker compose restart atlaskb
```

Run one-off commands in the app container:

```bash
docker compose exec atlaskb atlaskb version
docker compose exec atlaskb atlaskb status
docker compose exec -it atlaskb atlaskb setup
```

Stop stack:

```bash
docker compose down
```

Reset DB volume:

```bash
docker compose down -v
```

## Using Runtime and CLI Together

You can run AtlasKB as a long-running runtime and still use CLI commands.

- Runtime process (`atlaskb` or `atlaskb serve`) hosts dashboard/MCP HTTP.
- CLI subcommands (`index`, `ask`, `status`, etc.) are independent invocations that connect to the same DB/config.
- Homebrew service mode (`brew services start atlaskb`) is ideal if you do not want a tmux session.

Typical local pattern:

```bash
brew services start atlaskb     # keep runtime in background
atlaskb index ~/src/my-repo     # run indexing jobs on demand
atlaskb ask "what changed in retry flow?"
```

## Configuration

Default config path:

- `~/.atlaskb/config.toml`

Override path:

```bash
atlaskb --config /path/to/config.toml <command>
```

### Config shape

```toml
[database]
host = "localhost"
port = 5432
user = "atlaskb"
password = "atlaskb"
dbname = "atlaskb"
sslmode = "disable"

[llm]
base_url = "http://localhost:1234"
api_key = ""

[embeddings]
base_url = "http://localhost:1234"
model = "mxbai-embed-large-v1"
api_key = ""

[pipeline]
concurrency = 2
extraction_model = "qwen/qwen3.5-35b-a3b"
synthesis_model = "qwen/qwen3.5-35b-a3b"
context_window = 32768
git_log_limit = 500
global_exclude_dirs = ["tests", "test", "__tests__", "spec", "testing", "testdata", "fixtures", "e2e", "cypress", "playwright", "migrations"]

[server]
port = 3000
chats_dir = ""

[github]
token = ""
api_url = "https://api.github.com/graphql"
max_prs = 200
pr_batch_size = 10
enterprise_host = ""
```

### Environment variable overrides

Supported variables:

- `ATLASKB_DB_HOST`
- `ATLASKB_DB_USER`
- `ATLASKB_DB_PASSWORD`
- `ATLASKB_DB_NAME`
- `ATLASKB_LLM_URL`
- `ATLASKB_LLM_API_KEY`
- `ATLASKB_EMBEDDINGS_URL`
- `ATLASKB_EMBEDDINGS_MODEL`
- `ATLASKB_EMBEDDINGS_API_KEY`
- `ATLASKB_GITHUB_TOKEN` (takes priority)
- `GITHUB_TOKEN` (fallback)
- `ATLASKB_GITHUB_API_URL`

Show active config:

```bash
atlaskb config show
```

## CLI Reference

Top-level help:

```bash
atlaskb --help
```

### `atlaskb` / `atlaskb serve`

Start combined runtime.

Flags:

- `--port` (serve only, default `3000`)
- global: `--config`, `--verbose`, `--json`

### `atlaskb setup` (`configure`, `init`)

Interactive wizard covering:

1. Database connection + migration + optional schema reset
2. LLM endpoint + extraction model + synthesis model + API key
3. Embeddings endpoint/model/key
4. Runtime port + chats directory
5. Pipeline concurrency
6. GitHub integration (token/API URL/enterprise host/max PRs/batch size)
7. Optional Ctags install prompt

### `atlaskb index [path]`

Analyze repository and update graph.

Flags:

- `--dry-run`: cost estimate only, stop before LLM calls
- `--force`: full rebuild for target repo
- `-y, --yes`: skip prompts
- `--concurrency <n>`: override configured concurrency
- `--phase <name>`: run partial phases
- `--exclude <pattern>`: exclude path/pattern (repeatable, highest precedence)

AtlasKB also reads `.atlaskbignore` from the repo root using gitignore-style patterns.

Examples:

```bash
atlaskb index ~/src/repo
atlaskb index ~/src/repo --force
atlaskb index ~/src/repo --dry-run
atlaskb index ~/src/repo --phase phase2 --phase backfill
atlaskb index ~/src/repo --exclude generated --exclude '**/*.snap'
```

### `atlaskb ask [question]`

Retrieve facts and synthesize answer.

Flags:

- `--repo <name>`: scope to repo
- `--top-k <n>`: retrieval depth (default `40`)

### `atlaskb status [repo-name]`

Show indexing health, staleness/revalidation backlog, and recent run metrics.

Supports `--json` global flag.

### `atlaskb repos`

List indexed repos and quality snapshot.

Supports `--json` global flag.

### `atlaskb retry [repo-name]`

Reset failed jobs to pending for retry.

Flag:

- `--phase <phase>`: only retry failed jobs in a phase

### `atlaskb link`

Create manual cross-repo relationship.

Required flags:

- `--from-repo`, `--from-entity`, `--to-repo`, `--to-entity`

Optional:

- `--kind` (default `depends_on`)
- `--strength` (`strong|moderate|weak`)
- `--description`

### `atlaskb config`

Subcommands:

- `atlaskb config show`
- `atlaskb config set-github-token <token>`
- `atlaskb config set-github-api-url <url>`

### `atlaskb mcp`

Runs MCP server over stdio (legacy mode).

Use this for clients that cannot connect over HTTP.

### `atlaskb version`

Print build version.

With global `--json`, prints version metadata JSON.

## Indexing Pipeline (Detailed)

AtlasKB orchestrator supports three run modes:

- `incremental` (default)
- `full` (`--force`)
- `partial` (`--phase ...`)

### Pipeline stages

1. `phase1` Structural inventory
- Enumerates files/languages/stack
- Computes cost estimate
- Cleans stale entities for deleted files
- Parses dependencies and discovers cross-repo links

2. `phase1.5` Ctags symbol extraction
- Creates canonical entity roster for name grounding

3. `phase1.6` Import parsing
- Deterministic relationship extraction from Go imports

4. `phase1.7` Tree-sitter extraction
- Structural call and embedding relationships (Go AST)

5. `phase2` File-level LLM extraction
- Extracts entities/facts/relationships per file
- Uses context-window-aware budgeting
- Dedup and stale cleanup logic

6. `phase2.5` Backfill
- Repairs orphan entities with no facts

7. `phase2.7` Flow detection
- Detects execution flows and entry-point chains

8. `gitlog`
- Mines local commit history for facts/decisions

9. `phase3`
- Mines GitHub PR/issue/comment signals (if GitHub token configured)

10. `phase4`
- Cross-module synthesis (patterns, contracts, data-flow links)

11. `phase5`
- Repository summary synthesis

12. `phase6`
- Functional clustering + `member_of` relationships

13. `embedding`
- Generates embeddings for facts and entity summaries

14. Quality + overview
- Computes quality score dimensions
- Stores repo overview text

### Incremental smart-skip behavior

If an incremental full run finds no changed files in Phase 2, AtlasKB short-circuits later phases and reports up-to-date state.

### Phase filtering

CLI help lists common filters:

- `phase1`, `phase2`, `backfill`, `gitlog`, `phase3`, `phase4`, `phase5`, `embedding`

Internally, additional recognized phases include `phase1.5`, `phase1.7`, `flows`, and `phase6`.

## Retrieval and Querying

`ask` and MCP search rely on hybrid retrieval:

- query decomposition (when LLM is configured)
- vector search (pgvector)
- full-text search (tsvector)
- reciprocal rank fusion
- entity mention and relationship-neighborhood expansion
- score adjustments by confidence/category/kind/repo affinity

This enables both direct fact retrieval and graph-traversal oriented answers.

## MCP Interface

### Transports

- HTTP streamable MCP endpoint: `http://localhost:3000/mcp`
- Legacy stdio: `atlaskb mcp`

### Registered tools

- `search_knowledge_base`
- `list_repos`
- `get_conventions`
- `get_module_context`
- `get_service_contract`
- `get_impact_analysis`
- `get_decision_context`
- `get_task_context`
- `get_execution_flows`
- `get_functional_clusters`
- `get_repo_overview`
- `search_entities`
- `get_entity_source`
- `submit_fact_feedback`

`get_task_context` is the best default tool for coding-agent task bootstrap because it bundles conventions, context, contracts, and decision history.

## REST API Surface

Base path: `/api`

Security note:

- No auth middleware is enabled by default for API/MCP HTTP routes.
- For shared environments, place AtlasKB behind a trusted network boundary or proxy auth.

### Health and stats

- `GET /api/health`
- `GET /api/metrics`
- `GET /api/stats`
- `GET /api/stats/recent-runs`

### Repos

- `GET /api/repos`
- `POST /api/repos`
- `GET /api/repos/{id}`
- `PUT /api/repos/{id}`
- `DELETE /api/repos/{id}`
- `POST /api/repos/{id}/reindex`
- `GET /api/repos/{id}/reindex/status`
- `GET /api/repos/{id}/indexing-runs`
- `GET /api/repos/{id}/decisions`
- `GET /api/repos/{id}/clusters`
- `GET /api/repos/{id}/flows`

### Entities

- `GET /api/entities`
- `GET /api/entities/{id}`
- `GET /api/entities/{id}/facts`
- `GET /api/entities/{id}/relationships`
- `GET /api/entities/{id}/decisions`

### Graph

- `GET /api/graph/repo/{id}`
- `GET /api/graph/entity/{id}`
- `GET /api/graph/multi`

### Cross-repo links

- `GET /api/cross-repo/links`
- `GET /api/cross-repo/links/{id}`
- `POST /api/cross-repo/links`
- `DELETE /api/cross-repo/links/{id}`

### Indexing control and history

- `POST /api/indexing/batch`
- `GET /api/indexing/batch/status`
- `POST /api/indexing/batch/cancel`
- `GET /api/indexing/jobs`
- `GET /api/indexing/history`

### Query and chat

- `POST /api/ask`
- `GET /api/search`
- `GET /api/feedback`
- `POST /api/feedback`
- `POST /api/feedback/{id}/resolve`
- `GET /api/chats`
- `POST /api/chats`
- `GET /api/chats/{id}`
- `PUT /api/chats/{id}`
- `DELETE /api/chats/{id}`
- `POST /api/chats/{id}/messages`

### File access

- `GET /api/file`

Heavy GET endpoints (`/api/search`, `/api/repos`, `/api/repos/{id}`, `/api/stats`, `/api/stats/recent-runs`) support conditional caching with `ETag`/`If-None-Match`.

## Homebrew Tap and Release Automation

Detailed guide:

- [`docs/homebrew-private-tap.md`](docs/homebrew-private-tap.md)

Formula generation script:

- [`scripts/generate-homebrew-formula.sh`](scripts/generate-homebrew-formula.sh)

Release automation workflow:

- [`.github/workflows/release-homebrew-tap.yml`](.github/workflows/release-homebrew-tap.yml)
- [`.github/workflows/release.yml`](.github/workflows/release.yml) (cross-platform release tarballs + checksums + scoop update)

Install/packaging scripts:

- [`install.sh`](install.sh)
- [`install.ps1`](install.ps1)
- [`scripts/generate-scoop-manifest.sh`](scripts/generate-scoop-manifest.sh)

How release automation works:

1. Push tag `v*` in source repo.
2. Workflow generates `Formula/atlaskb.rb` pinned to tag + revision.
3. Workflow commits formula update to private tap repo `main`.

Required repo secret:

- `HOMEBREW_TAP_TOKEN` (must push to tap repository)
- `SCOOP_BUCKET_TOKEN` (must push to scoop bucket repository)

Optional repo variables:

- `HOMEBREW_TAP_REPO` (defaults to `<owner>/homebrew-atlaskb`)
- `HOMEBREW_SOURCE_REPO` (defaults to `https://github.com/<owner>/<repo>.git`)
- `SCOOP_BUCKET_REPO` (defaults to `<owner>/scoop-atlaskb`)
- `SCOOP_SOURCE_REPO` (defaults to current source repository)

## Development

### Make targets

```bash
make build         # Go binary only
make web           # web npm ci + build
make build-full    # web build + go build
make run           # build + run
make test          # go test ./... -v
make lint          # golangci-lint
make clean
```

Contract drift checks run in CI via [`.github/workflows/contracts.yml`](.github/workflows/contracts.yml) and validate CLI command/flag surface plus MCP tool names.

### Frontend dev

```bash
make dev-web       # vite dev server
make dev-server    # go runtime at :8080
```

### Local runtime

```bash
go run ./cmd/atlaskb serve --port 3000
```

## Troubleshooting

### `pattern all:dist: no matching files found`

Cause: `web/dist` was missing when building.

Fix:

```bash
cd web
npm ci
npm run build
cd ..
go build ./cmd/atlaskb
```

For Homebrew formula builds, ensure formula runs both web build and Go build (current generated formula already does this).

### Homebrew tries `git clone ssh://...` but you need HTTPS

Use HTTPS for both tap URL and formula source URL:

```bash
brew tap <owner>/atlaskb https://github.com/<owner>/homebrew-atlaskb.git
```

Generate formula with:

```bash
scripts/generate-homebrew-formula.sh --tag vX.Y.Z --source-repo https://github.com/<owner>/atlaskb.git
```

### `atlaskb` command not found after `brew install`

Check:

```bash
brew list atlaskb
brew info atlaskb
echo $PATH
```

Then relink/reinstall if needed:

```bash
brew reinstall <owner>/atlaskb/atlaskb
```

### Service starts but CLI commands fail

Validate DB + config:

```bash
atlaskb config show
atlaskb status
```

If DB auth/host changed, rerun:

```bash
atlaskb setup
brew services restart atlaskb
```

### Index preflight fails

`atlaskb index` checks:

- `GET <llm_base_url>/v1/models`
- `POST <embeddings_base_url>/v1/embeddings`

Ensure both endpoints are reachable and model IDs are valid.

### Docker cannot reach host LLM endpoint

When AtlasKB runs in Docker, host model servers are typically reachable at:
`http://host.docker.internal:<port>`.

Set LLM and embeddings URL to that value in setup UI (or `atlaskb setup` in container).

If you need to reconfigure:

```bash
docker compose exec -it atlaskb atlaskb setup
docker compose restart atlaskb
```

## Repository Layout

```text
cmd/atlaskb/              main entrypoint
internal/cli/             cobra commands and setup wizard
internal/server/          web runtime, API handlers, MCP HTTP wiring
internal/mcp/             MCP tool registry + handlers
internal/pipeline/        indexing orchestration and phase implementations
internal/models/          stores and graph/domain models
internal/db/              DB connection + embedded SQL migrations
web/                      React dashboard (Vite), embedded into Go binary
docs/                     architecture, model, pipeline, tap/release docs
scripts/                  release and utility scripts
```

## Additional Docs

- [Implementation Gap Report](docs/IMPLEMENTATION-GAP-REPORT.md)
- [Operations Runbook](docs/OPERATIONS-RUNBOOK.md)

## License

No license file is currently present in this repository.
