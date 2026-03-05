# AtlasKB Implementation Gap Report

Date: 2026-03-05

This report replaces prior design/TODO docs with an implementation-first backlog based on the current repository state.

## Scope

Included in this assessment:

- CLI/runtime behavior
- Indexing pipeline phases and retrieval
- MCP and HTTP API capabilities
- Homebrew install/release path
- Configuration, operations, and quality tooling

Excluded:

- External repos referenced by historical QA artifacts
- Non-repo infrastructure not present in this codebase

## Current State Summary

Implemented and working:

- Combined runtime (`atlaskb` / `atlaskb serve`) with dashboard + MCP HTTP endpoint
- Legacy stdio MCP mode (`atlaskb mcp`)
- Setup wizard (`atlaskb setup`) for DB, LLM, embeddings, runtime, GitHub, pipeline settings
- Indexing pipeline with phases 1, 1.5, 1.6, 1.7, 2, 2.5 (backfill), 2.7 (flows), gitlog, 3, 4, 5, 6, embeddings
- Query stack (`ask` + search engine + synthesis)
- MCP tool suite (`search_knowledge_base`, `get_task_context`, `get_impact_analysis`, etc.)
- REST API for repos/entities/graph/cross-repo/indexing/chats/search
- Homebrew formula generation + release workflow + service stanza

Partially implemented / inconsistent:

- Docs/spec alignment with actual behavior
- Auth/access-control model for API/MCP
- Feedback loop APIs for fact correction lifecycle
- Staleness governance beyond current indicators
- Observability stack beyond logs/indexing metrics

Not implemented:

- Webhook-driven incremental ingestion service
- Repo auto-discovery and managed clone lifecycle in core runtime
- `.atlaskbignore` support and `index --exclude` overrides
- ETag/conditional caching on context APIs
- OAuth/SSO flows

## Prioritized Backlog

## P0 (Do Next)

### P0-1: Align Product Contracts with Real Runtime

Problem:
- Historical docs and contracts diverge from live behavior (command semantics, models, phases, transport details).

Deliverables:
- Treat this file as source-of-truth backlog.
- Add machine-readable API/MCP contract tests for critical commands/tools.
- Ensure `--help` and README remain authoritative.

Acceptance criteria:
- CI test suite validates top-level CLI commands, key flags, and MCP tool names.
- Any contract drift fails CI.

### P0-2: Retrieval Access Control Baseline

Problem:
- API/MCP currently do not enforce per-user/per-repo access boundaries.

Deliverables:
- Add auth middleware abstraction for API and MCP HTTP.
- Introduce repository-level authorization checks in handlers/tool resolvers.
- Support local dev bypass mode via config.

Acceptance criteria:
- Unauthorized requests receive 401/403.
- Cross-repo responses include only allowed repos.
- Integration tests cover allow/deny paths for API and MCP.

### P0-3: Feedback Loop for Knowledge Corrections

Problem:
- No first-class way to flag incorrect/outdated facts and route revalidation.

Deliverables:
- Add `fact_feedback` table + model/store.
- Add API endpoints: create/list/resolve feedback.
- Add MCP tool for feedback submission.
- Add pipeline hook to lower confidence / queue re-analysis when feedback is added.

Acceptance criteria:
- Feedback is persisted with reason and optional correction.
- Flagged facts are visible in retrieval metadata.
- Re-index run processes pending feedback and records outcomes.

### P0-4: Observability Foundation

Problem:
- Current logs and run metrics exist, but no consistent telemetry baseline.

Deliverables:
- Add request/phase latency metrics.
- Add counters for parse/sanitization fallbacks and unresolved entities.
- Add health/readiness details for dependency checks.

Acceptance criteria:
- Metrics endpoint exposes key counters/histograms.
- Status surface shows parse fallback counts and unresolved-reference trends.
- Operational runbook documents alert thresholds.

## P1 (High Impact, After P0)

### P1-1: Ingestion Service (Webhook + Polling)

Problem:
- Incremental updates are manually initiated; no event-driven ingestion loop.

Deliverables:
- Add ingestion worker capable of receiving webhook events and/or scheduled polling.
- Resolve changed repos/commits and trigger scoped reindex jobs.
- Persist ingestion events and outcomes.

Acceptance criteria:
- New push event triggers indexed update without manual CLI call.
- Duplicate events are de-duplicated.
- Failed ingestion retries with backoff and dead-letter visibility.

### P1-2: Managed Repository Discovery and Clone Lifecycle

Problem:
- Core runtime assumes repos already exist locally.

Deliverables:
- Add optional managed clone root and repo registration flow.
- Support repo onboarding by remote URL/org discovery.
- Add update/pull logic with safety checks.

Acceptance criteria:
- Repo can be added by remote URL and indexed without manual clone.
- Sync status and failures are visible in API/UI.

### P1-3: Exclusion Controls (`--exclude` + `.atlaskbignore`)

Problem:
- Exclusions exist in config/per-repo fields but not via index command overrides or ignore file.

Deliverables:
- Add `atlaskb index --exclude` repeated flag.
- Add `.atlaskbignore` parsing with gitignore semantics.
- Merge precedence: CLI > repo settings > global settings.

Acceptance criteria:
- Excluded paths never enter manifest/ctags/phase2 jobs.
- Effective exclusion set is shown in verbose logs and API.

### P1-4: Staleness Policy and Revalidation

Problem:
- Staleness info exists but policy-based stale marking/revalidation is limited.

Deliverables:
- Define stale triggers (age, commit drift, changed dependencies, feedback signals).
- Add stale flags at fact/entity/repo levels.
- Add targeted revalidation modes.

Acceptance criteria:
- Retrieval responses expose stale markers.
- `status` and API show stale counts and revalidation backlog.

### P1-5: Caching and Conditional Responses

Problem:
- No ETag/If-None-Match support for heavy context endpoints.

Deliverables:
- Add content-hash ETag on stable context endpoints.
- Honor `If-None-Match` with 304 responses.
- Add cache-control guidance per endpoint class.

Acceptance criteria:
- Repeat identical requests return 304 where applicable.
- Bandwidth/latency improvement measurable in benchmarks.

## P2 (Optimization and Hardening)

### P2-1: Prompt/Parser Quality Guardrails

Deliverables:
- Emit explicit sanitization counters and threshold warnings.
- Add stronger fact-grounding lint checks.
- Add stricter relationship validation reports.

Acceptance criteria:
- Quality regressions detectable in CI and run summaries.

### P2-2: Query/Retrieval Evaluation Harness

Deliverables:
- Build repeatable retrieval benchmark suite from curated QA sets.
- Score relevance, grounding, and citation accuracy per model/version.

Acceptance criteria:
- Model/prompt changes require benchmark delta report.

### P2-3: Security Hardening

Deliverables:
- Token scoping guidance and secret storage improvements.
- Optional encrypted config secret fields.
- Audit logging for authz decisions and administrative actions.

Acceptance criteria:
- Security checklist passes for self-hosted enterprise deployment.

### P2-4: Packaging Expansion

Deliverables:
- Extend beyond Homebrew (container images/install script).
- Add signed release artifacts and checksums.

Acceptance criteria:
- Reproducible install path for non-macOS environments.

## Work Sequencing

Recommended order:

1. P0-1 contract alignment and CI guards
2. P0-2 access control baseline
3. P0-3 feedback loop APIs and revalidation hook
4. P0-4 observability foundation
5. P1-3 exclusion controls (fast UX win)
6. P1-4 staleness policy
7. P1-5 caching
8. P1-1/P1-2 ingestion and managed clone lifecycle
9. P2 hardening tracks

## Delivery Milestones

- Milestone A (2-3 weeks): P0 complete
- Milestone B (2-4 weeks): P1-3/P1-4/P1-5 complete
- Milestone C (4-8 weeks): P1-1/P1-2 + P2 foundation

## Definition of Done (Global)

A backlog item is done when:

- Code implemented and merged
- Tests cover success/failure paths
- CLI/API/MCP behavior documented in README or inline help
- Relevant observability is in place
- No contract drift against CI checks
