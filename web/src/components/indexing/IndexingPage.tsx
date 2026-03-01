import { useEffect, useState, useRef } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type { BatchStatus, IndexingRun } from "../../types";
import {
  Loader2,
  CheckCircle2,
  Clock,
  XCircle,
  Ban,
  AlertTriangle,
} from "lucide-react";

export function IndexingPage() {
  const [batch, setBatch] = useState<BatchStatus | null>(null);
  const [history, setHistory] = useState<IndexingRun[]>([]);
  const [loading, setLoading] = useState(true);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const refresh = async () => {
    try {
      const [b, h] = await Promise.all([
        api.getBatchStatus(),
        api.getIndexingHistory(),
      ]);
      setBatch(b);
      setHistory(h);
    } catch (err) {
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    refresh();
  }, []);

  // Poll while batch is active
  useEffect(() => {
    if (batch?.active) {
      pollRef.current = setInterval(refresh, 2000);
    } else if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [batch?.active]);

  const handleCancel = async () => {
    try {
      await api.cancelBatch();
      refresh();
    } catch (err: any) {
      alert(err.message);
    }
  };

  if (loading) return <p className="text-foreground-secondary">Loading...</p>;

  const completed = batch?.completed ?? 0;
  const failed = batch?.failed ?? 0;
  const total = batch?.total ?? 0;
  const pct = total > 0 ? Math.round(((completed + failed) / total) * 100) : 0;

  // Parse Phase 2 progress from the current repo's logs
  const currentRepo =
    batch?.active && batch.repos
      ? batch.repos.find((r) => r.status === "running")
      : null;
  const phase2Progress = (() => {
    if (!currentRepo) return null;
    for (let i = currentRepo.logs.length - 1; i >= 0; i--) {
      const m = currentRepo.logs[i].match(/Phase 2: \[(\d+)\/(\d+)\]/);
      if (m) return { current: parseInt(m[1]), total: parseInt(m[2]) };
    }
    return null;
  })();

  const latestLog = currentRepo?.logs.length
    ? currentRepo.logs[currentRepo.logs.length - 1]
    : "";

  return (
    <div>
      <h1 className="text-2xl font-bold text-foreground mb-6">Indexing</h1>

      {/* Active Batch */}
      {batch && (batch.active || batch.repos?.length > 0) && (
        <div className="bg-surface-elevated rounded-lg border border-edge p-4 mb-6">
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-semibold text-foreground">
              {batch.active ? "Batch Reindex In Progress" : "Last Batch Reindex"}
            </h2>
            {batch.active && (
              <button
                onClick={handleCancel}
                className="flex items-center gap-1 text-xs text-syn-red hover:text-syn-red/80 transition-colors"
              >
                <Ban size={14} /> Cancel
              </button>
            )}
          </div>

          {/* Overall progress */}
          <div className="mb-3">
            <div className="flex items-center justify-between text-xs text-foreground-secondary mb-1">
              <span>
                {completed + failed} / {total} repos
              </span>
              <span>{pct}%</span>
            </div>
            <div className="w-full bg-surface-overlay rounded-full h-2">
              <div
                className="bg-accent h-2 rounded-full transition-all duration-500"
                style={{ width: `${pct}%` }}
              />
            </div>
          </div>

          {/* Current repo file progress */}
          {batch.active && currentRepo && phase2Progress && phase2Progress.total > 0 && (
            <div className="mb-3 pl-3 border-l-2 border-accent/30">
              <div className="flex items-center justify-between text-xs text-foreground-secondary mb-1">
                <span>{currentRepo.repo_name} — Phase 2: File analysis</span>
                <span>
                  {phase2Progress.current} / {phase2Progress.total} files
                </span>
              </div>
              <div className="w-full bg-surface-overlay rounded-full h-1.5">
                <div
                  className="bg-accent/60 h-1.5 rounded-full transition-all duration-500"
                  style={{
                    width: `${Math.round((phase2Progress.current / phase2Progress.total) * 100)}%`,
                  }}
                />
              </div>
            </div>
          )}

          {batch.active && latestLog && (
            <p className="text-xs text-foreground-secondary font-mono truncate mb-3">
              {latestLog}
            </p>
          )}

          {/* Per-repo status list */}
          <div className="space-y-1">
            {batch.repos?.map((rs) => (
              <div
                key={rs.repo_id}
                className="flex items-center gap-2 text-sm py-1"
              >
                <StatusIcon status={rs.status} />
                <Link
                  to={`/repos/${rs.repo_id}`}
                  className="text-foreground hover:text-accent transition-colors"
                >
                  {rs.repo_name}
                </Link>
                {rs.status === "running" && (
                  <span className="text-xs text-foreground-muted ml-auto truncate max-w-[50%]">
                    {rs.logs.length > 0 ? rs.logs[rs.logs.length - 1] : ""}
                  </span>
                )}
                {rs.status === "failed" && (
                  <span className="text-xs text-syn-red ml-auto truncate max-w-[50%]">
                    {rs.logs.length > 0 ? rs.logs[rs.logs.length - 1] : "Failed"}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* No batch, no history */}
      {(!batch || (!batch.active && (!batch.repos || batch.repos.length === 0))) &&
        history.length === 0 && (
          <div className="text-center py-12">
            <p className="text-foreground-secondary mb-2">No indexing activity yet.</p>
            <p className="text-sm text-foreground-muted">
              Go to{" "}
              <Link to="/repos" className="text-accent hover:underline">
                Repos
              </Link>{" "}
              and click "Re-index All" to get started.
            </p>
          </div>
        )}

      {/* History Table */}
      {history.length > 0 && (
        <div>
          <h2 className="text-sm font-semibold text-foreground mb-3">
            Indexing History
          </h2>
          <div className="bg-surface-elevated rounded-lg border border-edge overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-edge text-left text-xs text-foreground-secondary">
                  <th className="px-4 py-2 font-medium">Repo</th>
                  <th className="px-4 py-2 font-medium">Mode</th>
                  <th className="px-4 py-2 font-medium text-right">Files</th>
                  <th className="px-4 py-2 font-medium text-right">Entities</th>
                  <th className="px-4 py-2 font-medium text-right">Facts</th>
                  <th className="px-4 py-2 font-medium text-right">Duration</th>
                  <th className="px-4 py-2 font-medium text-right">Quality</th>
                  <th className="px-4 py-2 font-medium text-right">Date</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-edge">
                {history.map((run) => (
                  <tr key={run.id} className="hover:bg-surface-overlay/50">
                    <td className="px-4 py-2">
                      <Link
                        to={`/repos/${run.repo_id}`}
                        className="text-foreground hover:text-accent transition-colors"
                      >
                        {run.repo_name || run.repo_id}
                      </Link>
                    </td>
                    <td className="px-4 py-2 text-foreground-secondary">
                      {run.mode}
                    </td>
                    <td className="px-4 py-2 text-right text-foreground-secondary">
                      {run.files_analyzed ?? "—"}
                    </td>
                    <td className="px-4 py-2 text-right text-foreground-secondary">
                      {run.entities_created ?? "—"}
                    </td>
                    <td className="px-4 py-2 text-right text-foreground-secondary">
                      {run.facts_created ?? "—"}
                    </td>
                    <td className="px-4 py-2 text-right text-foreground-secondary">
                      {run.duration_ms != null
                        ? `${(run.duration_ms / 1000).toFixed(1)}s`
                        : "—"}
                    </td>
                    <td className="px-4 py-2 text-right">
                      {run.quality_overall != null ? (
                        <QualityBadge score={run.quality_overall} />
                      ) : (
                        <span className="text-foreground-muted">—</span>
                      )}
                    </td>
                    <td className="px-4 py-2 text-right text-foreground-muted text-xs">
                      {new Date(run.started_at).toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}

function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case "running":
      return <Loader2 size={16} className="text-accent animate-spin" />;
    case "completed":
      return <CheckCircle2 size={16} className="text-syn-green" />;
    case "failed":
      return <XCircle size={16} className="text-syn-red" />;
    case "pending":
      return <Clock size={16} className="text-foreground-muted" />;
    default:
      return <AlertTriangle size={16} className="text-foreground-muted" />;
  }
}

function QualityBadge({ score }: { score: number }) {
  const pct = Math.round(score);
  const color =
    pct >= 70
      ? "bg-syn-green/15 text-syn-green"
      : pct >= 40
        ? "bg-syn-yellow/15 text-syn-yellow"
        : "bg-syn-red/15 text-syn-red";
  return (
    <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${color}`}>
      {pct}%
    </span>
  );
}
