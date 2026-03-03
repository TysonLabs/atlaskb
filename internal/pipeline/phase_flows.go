package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgeorge06/atlaskb/internal/models"
)

const (
	minOutgoingCalls = 2
	maxIncomingCalls = 2
	maxBFSDepth      = 10
	minFlowDepth     = 2
	maxFlowsPerRepo  = 200
	maxEntryPoints   = 400
	maxLabelNames    = 8
)

// FlowsConfig configures the execution flow detection phase.
type FlowsConfig struct {
	RepoID   uuid.UUID
	RepoName string
	Pool     *pgxpool.Pool
}

// FlowsStats holds statistics from the flow detection phase.
type FlowsStats struct {
	EntryPoints  int
	FlowsCreated int
	FlowsSkipped int
}

// bfsStep represents a single node discovered during BFS traversal.
type bfsStep struct {
	ID            uuid.UUID
	Name          string
	QualifiedName string
	Kind          string
	Depth         int
}

// RunPhaseFlows detects entry points in the call graph and traces execution flows via BFS.
func RunPhaseFlows(ctx context.Context, cfg FlowsConfig) (*FlowsStats, error) {
	stats := &FlowsStats{}
	flowStore := &models.FlowStore{Pool: cfg.Pool}

	// 1. Delete existing flows for repo (rebuild each run)
	if err := flowStore.DeleteByRepo(ctx, cfg.RepoID); err != nil {
		return nil, fmt.Errorf("clearing existing flows: %w", err)
	}

	// 2. Find entry points: functions with high out-degree on 'calls' edges and low in-degree
	entryPoints, err := findEntryPoints(ctx, cfg.Pool, cfg.RepoID)
	if err != nil {
		return nil, fmt.Errorf("finding entry points: %w", err)
	}
	stats.EntryPoints = len(entryPoints)

	if len(entryPoints) == 0 {
		return stats, nil
	}

	// Track labels for deduplication
	seenLabels := make(map[string]bool)

	// 3. For each entry point, run BFS and create flows
	for _, ep := range entryPoints {
		if err := ctx.Err(); err != nil {
			return stats, nil
		}

		if stats.FlowsCreated >= maxFlowsPerRepo {
			break
		}

		steps, err := traceBFS(ctx, cfg.Pool, ep.ID, cfg.RepoID)
		if err != nil {
			logVerboseF("[flows] BFS failed for %s: %v", ep.Name, err)
			stats.FlowsSkipped++
			continue
		}

		// Skip flows that are too shallow
		maxDepth := 0
		for _, s := range steps {
			if s.Depth > maxDepth {
				maxDepth = s.Depth
			}
		}
		if maxDepth < minFlowDepth {
			stats.FlowsSkipped++
			continue
		}

		// Build ordered step lists (by depth, then name for stability)
		stepIDs := make([]uuid.UUID, 0, len(steps))
		stepNames := make([]string, 0, len(steps))
		for _, s := range steps {
			stepIDs = append(stepIDs, s.ID)
			stepNames = append(stepNames, s.Name)
		}

		// Build label
		label := buildFlowLabel(stepNames)

		// Deduplicate by label
		if seenLabels[label] {
			stats.FlowsSkipped++
			continue
		}
		seenLabels[label] = true

		// Upsert the flow
		flow := &models.ExecutionFlow{
			RepoID:        cfg.RepoID,
			EntryEntityID: ep.ID,
			Label:         label,
			StepEntityIDs: stepIDs,
			StepNames:     stepNames,
			Depth:         maxDepth,
		}
		if err := flowStore.Upsert(ctx, flow); err != nil {
			logVerboseF("[flows] upsert failed for %s: %v", ep.Name, err)
			stats.FlowsSkipped++
			continue
		}
		stats.FlowsCreated++
	}

	return stats, nil
}

// entryPoint represents a candidate entry point entity.
type entryPoint struct {
	ID            uuid.UUID
	Name          string
	QualifiedName string
	Kind          string
}

// findEntryPoints finds functions with out-degree >= minOutgoingCalls on 'calls' edges
// and in-degree <= maxIncomingCalls, ordered by (out_degree - in_degree) DESC.
func findEntryPoints(ctx context.Context, pool *pgxpool.Pool, repoID uuid.UUID) ([]entryPoint, error) {
	query := `
WITH call_out AS (
    SELECT from_entity_id AS entity_id, COUNT(*) AS out_degree
    FROM relationships
    WHERE repo_id = $1 AND kind = 'calls'
    GROUP BY from_entity_id
),
call_in AS (
    SELECT to_entity_id AS entity_id, COUNT(*) AS in_degree
    FROM relationships
    WHERE repo_id = $1 AND kind = 'calls'
    GROUP BY to_entity_id
)
SELECT e.id, e.name, e.qualified_name, e.kind
FROM entities e
JOIN call_out co ON co.entity_id = e.id
LEFT JOIN call_in ci ON ci.entity_id = e.id
WHERE e.repo_id = $1
  AND co.out_degree >= $2
  AND COALESCE(ci.in_degree, 0) <= $3
ORDER BY (co.out_degree - COALESCE(ci.in_degree, 0)) DESC
LIMIT $4`

	rows, err := pool.Query(ctx, query, repoID, minOutgoingCalls, maxIncomingCalls, maxEntryPoints)
	if err != nil {
		return nil, fmt.Errorf("querying entry points: %w", err)
	}
	defer rows.Close()

	var results []entryPoint
	for rows.Next() {
		var ep entryPoint
		if err := rows.Scan(&ep.ID, &ep.Name, &ep.QualifiedName, &ep.Kind); err != nil {
			return nil, fmt.Errorf("scanning entry point: %w", err)
		}
		results = append(results, ep)
	}
	return results, nil
}

// traceBFS runs a recursive CTE to trace the call graph from a starting entity.
func traceBFS(ctx context.Context, pool *pgxpool.Pool, startID uuid.UUID, repoID uuid.UUID) ([]bfsStep, error) {
	query := `
WITH RECURSIVE flow AS (
    SELECT e.id, e.name, e.qualified_name, e.kind, 0 AS depth, ARRAY[e.id] AS visited_ids
    FROM entities e WHERE e.id = $1
    UNION ALL
    SELECT target.id, target.name, target.qualified_name, target.kind, flow.depth + 1, flow.visited_ids || target.id
    FROM flow
    JOIN relationships r ON r.from_entity_id = flow.id AND r.kind = 'calls' AND r.repo_id = $2
    JOIN entities target ON target.id = r.to_entity_id
    WHERE flow.depth < $3 AND NOT (target.id = ANY(flow.visited_ids))
)
SELECT DISTINCT ON (id) id, name, qualified_name, kind, depth FROM flow ORDER BY id, depth ASC`

	rows, err := pool.Query(ctx, query, startID, repoID, maxBFSDepth)
	if err != nil {
		return nil, fmt.Errorf("BFS traversal: %w", err)
	}
	defer rows.Close()

	var steps []bfsStep
	for rows.Next() {
		var s bfsStep
		if err := rows.Scan(&s.ID, &s.Name, &s.QualifiedName, &s.Kind, &s.Depth); err != nil {
			return nil, fmt.Errorf("scanning BFS step: %w", err)
		}
		steps = append(steps, s)
	}
	return steps, nil
}

// buildFlowLabel creates a human-readable label from step names.
// Shows up to maxLabelNames names joined with " -> ", appending "..." if truncated.
func buildFlowLabel(names []string) string {
	if len(names) == 0 {
		return ""
	}
	n := len(names)
	if n > maxLabelNames {
		n = maxLabelNames
	}
	label := strings.Join(names[:n], " \u2192 ")
	if len(names) > maxLabelNames {
		label += " \u2192 ..."
	}
	return label
}
