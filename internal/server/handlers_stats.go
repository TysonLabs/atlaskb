package server

import (
	"net/http"

	"github.com/tgeorge06/atlaskb/internal/models"
)

type statsResponse struct {
	Repos         int `json:"repos"`
	Entities      int `json:"entities"`
	Facts         int `json:"facts"`
	Relationships int `json:"relationships"`
	Decisions     int `json:"decisions"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	var stats statsResponse
	err := s.pool.QueryRow(r.Context(),
		`SELECT
			(SELECT COUNT(*) FROM repos),
			(SELECT COUNT(*) FROM entities),
			(SELECT COUNT(*) FROM facts),
			(SELECT COUNT(*) FROM relationships),
			(SELECT COUNT(*) FROM decisions)`,
	).Scan(&stats.Repos, &stats.Entities, &stats.Facts, &stats.Relationships, &stats.Decisions)
	if err != nil {
		writeError(w, NewInternal("querying stats: "+err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

type recentRunResponse struct {
	models.IndexingRun
	RepoName string `json:"repo_name"`
}

func (s *Server) handleRecentRuns(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(),
		`SELECT ir.id, ir.repo_id, ir.commit_sha, ir.mode, ir.model_extraction, ir.model_synthesis, ir.concurrency,
			ir.files_total, ir.files_analyzed, ir.files_skipped,
			ir.entities_created, ir.facts_created, ir.rels_created, ir.decisions_created,
			ir.orphan_entities, ir.backfill_facts, ir.backfill_rels,
			ir.total_tokens, ir.total_cost_usd,
			ir.quality_overall, ir.quality_entity_cov, ir.quality_fact_density,
			ir.quality_rel_connect, ir.quality_dim_coverage, ir.quality_parse_rate,
			ir.duration_ms, ir.started_at, ir.completed_at, ir.created_at,
			r.name
		 FROM indexing_runs ir
		 JOIN repos r ON r.id = ir.repo_id
		 ORDER BY ir.created_at DESC
		 LIMIT 10`)
	if err != nil {
		writeError(w, NewInternal("querying recent runs: "+err.Error()))
		return
	}
	defer rows.Close()

	var runs []recentRunResponse
	for rows.Next() {
		var rr recentRunResponse
		if err := rows.Scan(
			&rr.ID, &rr.RepoID, &rr.CommitSHA, &rr.Mode, &rr.ModelExtraction, &rr.ModelSynthesis, &rr.Concurrency,
			&rr.FilesTotal, &rr.FilesAnalyzed, &rr.FilesSkipped,
			&rr.EntitiesCreated, &rr.FactsCreated, &rr.RelsCreated, &rr.DecisionsCreated,
			&rr.OrphanEntities, &rr.BackfillFacts, &rr.BackfillRels,
			&rr.TotalTokens, &rr.TotalCostUSD,
			&rr.QualityOverall, &rr.QualityEntityCov, &rr.QualityFactDensity,
			&rr.QualityRelConnect, &rr.QualityDimCoverage, &rr.QualityParseRate,
			&rr.DurationMS, &rr.StartedAt, &rr.CompletedAt, &rr.CreatedAt,
			&rr.RepoName,
		); err != nil {
			writeError(w, NewInternal("scanning run: "+err.Error()))
			return
		}
		runs = append(runs, rr)
	}
	if runs == nil {
		runs = []recentRunResponse{}
	}
	writeJSON(w, http.StatusOK, runs)
}
