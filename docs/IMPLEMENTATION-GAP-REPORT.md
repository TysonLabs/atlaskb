# AtlasKB Implementation Gap Report

Date: 2026-03-05

This report reflects the current implementation status of backlog items by validating code, tests, workflows, and docs in this repository.

## Validation Method

Validation was done in parallel tracks across:

- CLI contracts and command surface
- MCP tool contracts and handlers
- API handlers and middleware
- Pipeline/orchestrator behavior
- Models + DB migrations
- CI/workflows and release packaging scripts

## Current Snapshot

Implemented:

- P0-1 contract alignment (CLI + MCP contract tests in CI)
- P0-3 feedback loop (DB table/store + API + MCP + revalidation hook)
- P0-4 observability foundation (health/readiness, metrics endpoint, counters/histograms, runbook)
- P1-3 exclusion controls (`index --exclude`, `.atlaskbignore`, effective excludes surfaced)
- P1-5 conditional caching (`ETag`/`If-None-Match`, 304 support, tests)

Partially implemented:

- P1-4 staleness and revalidation policy (baseline triggers and surfaces exist; policy depth is limited)
- P2-1 prompt/parser quality guardrails (parser fallback and unresolved counters exist; stronger CI quality gates remain)

Not implemented:

- P0-2 retrieval access control baseline
- P1-1 ingestion service (webhook/polling worker)
- P1-2 managed repository discovery/clone lifecycle
- P2-2 retrieval evaluation harness
- P2-3 security hardening track
- P2-4 packaging expansion beyond Homebrew

## Detailed Status by Backlog Item

### P0-1: Align Product Contracts with Real Runtime

Status: Implemented

Evidence:

- CLI contract tests verify required top-level commands/flags/help surface: `internal/cli/contract_test.go`
- MCP contract tests verify canonical tool list and ordering: `internal/mcp/contract_test.go`
- CI contract workflow runs these tests on push/PR: `.github/workflows/contracts.yml`

Notes:

- This now provides automated drift detection for core CLI/MCP contracts.

### P0-2: Retrieval Access Control Baseline

Status: Not Implemented

Evidence:

- API router and MCP HTTP are exposed without auth/authz middleware: `internal/server/server.go`
- Middleware stack currently includes recovery/logging/CORS only: `internal/server/middleware.go`

Missing for acceptance:

- Auth middleware abstraction for API and MCP HTTP
- Repo-level authorization checks
- Integration tests for allow/deny behavior (401/403)

### P0-3: Feedback Loop for Knowledge Corrections

Status: Implemented

Evidence:

- Feedback schema/migration exists: `internal/db/migrations/000018_fact_feedback.up.sql`
- Feedback model/store and transactional submit + queueing logic: `internal/models/fact_feedback.go`
- API endpoints for create/list/resolve: `internal/server/handlers_feedback.go`
- MCP tool contract + handler: `internal/mcp/server.go`, `internal/mcp/contract_test.go`
- Revalidation outcome processing during orchestration: `internal/pipeline/orchestrator.go`
- Retrieval includes pending feedback metadata: `internal/query/engine.go`

### P0-4: Observability Foundation

Status: Implemented

Evidence:

- Metrics registry with counters/histograms: `internal/telemetry/metrics.go`
- HTTP request metrics in middleware: `internal/server/middleware.go`
- Health/readiness endpoint: `internal/server/handlers_health.go`
- Metrics endpoint: `internal/server/handlers_metrics.go`
- Parse fallback/unresolved tracking in pipeline + persisted run fields: `internal/pipeline/parser.go`, `internal/pipeline/phase2.go`, `internal/models/indexing_run.go`, `internal/db/migrations/000017_observability_fields.up.sql`
- Operational thresholds/runbook: `docs/OPERATIONS-RUNBOOK.md`

### P1-1: Ingestion Service (Webhook + Polling)

Status: Not Implemented

Evidence:

- No webhook ingestion worker, scheduled poller, or ingestion event store currently present.
- Reindex flows are manually triggered (`index` command / reindex endpoints), not event-driven.

### P1-2: Managed Repository Discovery and Clone Lifecycle

Status: Not Implemented

Evidence:

- Repo create path requires an existing local git directory: `internal/server/handlers_repos.go`
- No managed clone root, remote URL onboarding flow, or pull/sync lifecycle worker.

### P1-3: Exclusion Controls (`--exclude` + `.atlaskbignore`)

Status: Implemented

Evidence:

- CLI repeated `--exclude` flag: `internal/cli/index.go`
- Exclusion composition + `.atlaskbignore` loading: `internal/pipeline/excludes.go`
- Exclusion tests, including anchored pattern behavior: `internal/pipeline/excludes_test.go`
- Effective excludes surfaced in repo list/detail responses: `internal/server/handlers_repos.go`
- Orchestrator applies exclusion set and logs effective excludes in verbose mode: `internal/pipeline/orchestrator.go`

### P1-4: Staleness Policy and Revalidation

Status: Partial

Implemented:

- Baseline staleness triggers: never indexed, index age, commit drift: `internal/models/staleness.go`
- Staleness surfaced in retrieval/MCP/API/CLI status views: `internal/query/engine.go`, `internal/mcp/server.go`, `internal/server/handlers_repos.go`, `internal/cli/status.go`
- Revalidation backlog surfaced in stats/task context: `internal/server/handlers_stats.go`, `internal/mcp/server.go`
- Feedback-driven revalidation queueing exists via P0-3.

Missing for full acceptance:

- Broader stale triggers (dependency drift and richer policy controls)
- Explicit targeted revalidation modes beyond current retry/index operational flows
- More formal stale policy definition and docs-level contract

### P1-5: Caching and Conditional Responses

Status: Implemented

Evidence:

- ETag + `If-None-Match` handling (including wildcard/list matching): `internal/server/response.go`
- Coverage tests for 304 behavior: `internal/server/response_test.go`
- Applied to heavy read endpoints (ask/repos/stats): `internal/server/handlers_ask.go`, `internal/server/handlers_repos.go`, `internal/server/handlers_stats.go`
- Cache guidance documented in README endpoint section.

### P2-1: Prompt/Parser Quality Guardrails

Status: Partial

Implemented:

- Parser sanitization fallback counters: `internal/pipeline/parser.go`
- Unresolved reference counters in phase2: `internal/pipeline/phase2.go`
- Metrics and run surfaces include these signals.

Missing for full acceptance:

- Stronger fact-grounding lint/quality gates enforced in CI
- Explicit threshold-based regression checks in automated test pipeline

### P2-2: Query/Retrieval Evaluation Harness

Status: Not Implemented

Evidence:

- No curated benchmark suite, automated scoring harness, or required benchmark delta checks found.

### P2-3: Security Hardening

Status: Not Implemented

Evidence:

- No authz audit logging or enterprise-grade security checklist implementation found.
- Secret management remains basic config/env-based.

### P2-4: Packaging Expansion

Status: Not Implemented

Evidence:

- Homebrew private tap automation is implemented: `.github/workflows/release-homebrew-tap.yml`, `scripts/generate-homebrew-formula.sh`
- No container image/install-script release path, signing, or checksums currently implemented.

## Updated Recommended Sequence

1. P0-2 Retrieval access control baseline
2. P1-1 Ingestion service (webhook/polling)
3. P1-2 Managed clone/discovery lifecycle
4. P1-4 Staleness policy completion
5. P2-2 Retrieval evaluation harness
6. P2-3 Security hardening
7. P2-4 Packaging expansion
8. P2-1 CI quality guardrails completion

## Completion Gate (Per Item)

An item should be considered complete only when:

- Code is merged
- Acceptance behavior is covered by tests (including failure paths)
- CLI/API/MCP behavior is documented in README/help/docs
- Operational visibility exists for production impact areas
- Contract/behavior drift is caught in CI where applicable
