# AtlasKB Operations Runbook

This runbook defines the minimum operational checks and alert thresholds for AtlasKB runtime health.

## Endpoints

- Health: `GET /api/health`
- Metrics: `GET /api/metrics`
- Stats: `GET /api/stats`

## Health / Readiness

`/api/health` returns:

- `status`: `ok` or `degraded`
- `readiness`: `ready` or `degraded`
- `db_connected`
- `llm_reachable`
- `embeddings_reachable`

Alert immediately when:

- `db_connected=false`
- `readiness=degraded` for > 5 minutes

## Core Metrics

Counters:

- `http_requests_total`
- `http_server_errors_total`
- `parser_sanitization_fallback_total`
- `pipeline_unresolved_entity_refs_total`
- `feedback_revalidated_total`

Histograms:

- `http_request_duration_ms`
- `pipeline_phase1_duration_ms`
- `pipeline_phase1_5_duration_ms`
- `pipeline_phase1_7_duration_ms`
- `pipeline_phase2_duration_ms`
- `pipeline_phase2_5_duration_ms`
- `pipeline_phase3_duration_ms`
- `pipeline_phase4_duration_ms`
- `pipeline_phase5_duration_ms`
- `pipeline_phase6_duration_ms`
- `pipeline_embedding_duration_ms`

## Suggested Alert Thresholds

- HTTP error rate: `http_server_errors_total / http_requests_total > 1%` over 10 min.
- p95 request latency: `http_request_duration_ms > 1000ms` sustained 10 min.
- Phase 2 latency regression: `pipeline_phase2_duration_ms` median > 2x weekly baseline.
- Parse fallback growth: `parser_sanitization_fallback_total` increases > 20% day-over-day.
- Unresolved refs growth: `pipeline_unresolved_entity_refs_total` increases > 20% day-over-day.
- Stale repo pressure: `/api/stats.stale_repos / /api/stats.repos > 30%`.
- Revalidation backlog: `/api/stats.revalidation_backlog > 100` for > 30 min.

## Triage Checklist

1. Confirm `/api/health` readiness and which dependency is degraded.
2. Check `/api/stats` for stale repo count and backlog growth.
3. Check `/api/metrics` for request error spikes and phase latency regressions.
4. If parser/unresolved counters spike, inspect recent indexing runs for:
   - `parse_fallbacks`
   - `unresolved_refs`
5. Run targeted reindex:
   - `atlaskb index <repo-path> --phase phase2 --phase backfill`
6. For known bad facts, submit feedback (`/api/feedback` or MCP `submit_fact_feedback`) and reindex the repo.
