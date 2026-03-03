import { useEffect, useState, useRef, useCallback } from "react";
import { useParams, Link, useNavigate } from "react-router-dom";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api } from "../../api/client";
import type { RepoDetail as RepoDetailType, IndexingRun, Decision, FunctionalCluster, ExecutionFlow } from "../../types";
import { ArrowLeft, X, AlertTriangle, Trash2, RefreshCw, Loader2 } from "lucide-react";
import { EntityDrawer } from "./EntityDrawer";
import { ClustersTab } from "./ClustersTab";
import { FlowsTab } from "./FlowsTab";
import { RepoChatTab } from "./RepoChatTab";

export function RepoDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [repo, setRepo] = useState<RepoDetailType | null>(null);
  const [runs, setRuns] = useState<IndexingRun[]>([]);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [tab, setTab] = useState<"overview" | "clusters" | "flows" | "chat" | "quality" | "history" | "decisions" | "settings">("overview");
  const [indexing, setIndexing] = useState<{ status: string; logs: string[] }>({ status: "idle", logs: [] });
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const [drawerEntityId, setDrawerEntityId] = useState<string | null>(null);
  const [clusters, setClusters] = useState<FunctionalCluster[]>([]);
  const [flows, setFlows] = useState<ExecutionFlow[]>([]);
  const [clustersLoading, setClustersLoading] = useState(false);
  const [flowsLoading, setFlowsLoading] = useState(false);
  const [clustersLoaded, setClustersLoaded] = useState(false);
  const [flowsLoaded, setFlowsLoaded] = useState(false);

  const refreshData = useCallback(() => {
    if (!id) return;
    setClustersLoaded(false);
    setFlowsLoaded(false);
    api.getRepo(id).then(setRepo).catch(console.error);
    api.getRepoIndexingRuns(id).then(setRuns).catch(console.error);
    api.getRepoDecisions(id).then(setDecisions).catch(console.error);
  }, [id]);

  useEffect(() => {
    refreshData();
  }, [refreshData]);

  // Lazy-load clusters
  useEffect(() => {
    if (tab !== "clusters" || !id || clustersLoaded) return;
    setClustersLoading(true);
    api.getRepoClusters(id)
      .then((c) => { setClusters(c); setClustersLoaded(true); })
      .catch(console.error)
      .finally(() => setClustersLoading(false));
  }, [tab, id, clustersLoaded]);

  // Lazy-load flows
  useEffect(() => {
    if (tab !== "flows" || !id || flowsLoaded) return;
    setFlowsLoading(true);
    api.getRepoFlows(id)
      .then((f) => { setFlows(f); setFlowsLoaded(true); })
      .catch(console.error)
      .finally(() => setFlowsLoading(false));
  }, [tab, id, flowsLoaded]);

  // Check for in-flight indexing on mount
  useEffect(() => {
    if (!id) return;
    api.getReindexStatus(id).then(setIndexing).catch(console.error);
  }, [id]);

  // Poll while indexing is running
  useEffect(() => {
    if (indexing.status !== "running" || !id) {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
      return;
    }
    pollRef.current = setInterval(() => {
      api.getReindexStatus(id).then((s) => {
        setIndexing(s);
        if (s.status !== "running") {
          refreshData(); // reload repo data when done
        }
      }).catch(console.error);
    }, 2000);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [indexing.status, id, refreshData]);

  const handleReindex = async (force?: boolean) => {
    if (!id) return;
    try {
      await api.reindexRepo(id, force);
      setIndexing({ status: "running", logs: ["Starting indexing..."] });
    } catch (err: any) {
      if (err.message.includes("already in progress")) {
        setIndexing({ status: "running", logs: ["Indexing already in progress..."] });
      } else {
        alert(err.message);
      }
    }
  };

  if (!repo) return <p className="text-foreground-secondary">Loading...</p>;

  const qualityItems = [
    { label: "Overall", value: repo.quality_overall },
    { label: "Entity Coverage", value: repo.quality_entity_cov },
    { label: "Fact Density", value: repo.quality_fact_density },
    { label: "Relationship Connectivity", value: repo.quality_rel_connect },
    { label: "Dimension Coverage", value: repo.quality_dim_coverage },
    { label: "Parse Rate", value: repo.quality_parse_rate },
  ];

  const isRunning = indexing.status === "running";

  return (
    <div>
      <Link to="/repos" className="text-sm text-accent hover:underline flex items-center gap-1 mb-4">
        <ArrowLeft size={14} /> Back to Repos
      </Link>
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-foreground">{repo.name}</h1>
          <p className="text-sm text-foreground-secondary font-mono">{repo.local_path}</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => handleReindex(false)}
            disabled={isRunning}
            className="flex items-center gap-1.5 text-sm px-3 py-1.5 bg-syn-green/15 text-syn-green font-medium rounded-md hover:bg-syn-green/25 disabled:opacity-50 transition-colors"
          >
            {isRunning ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
            {isRunning ? "Indexing..." : "Re-index"}
          </button>
        </div>
      </div>

      {/* Indexing progress banner */}
      {indexing.status !== "idle" && (
        <IndexingBanner status={indexing.status} logs={indexing.logs} onForceReindex={() => handleReindex(true)} />
      )}

      <div className="grid grid-cols-4 gap-4 mb-6">
        <CountCard label="Entities" value={repo.entity_count} />
        <CountCard label="Facts" value={repo.fact_count} />
        <CountCard label="Relationships" value={repo.relationship_count} />
        <CountCard label="Decisions" value={repo.decision_count} />
      </div>

      {/* Entity by Kind */}
      {repo.entity_by_kind && Object.keys(repo.entity_by_kind).length > 0 && (
        <div className="bg-surface-elevated rounded-lg border border-edge p-4 mb-6">
          <h3 className="text-sm font-semibold text-foreground-secondary mb-2">Entities by Kind</h3>
          <div className="flex flex-wrap gap-2">
            {Object.entries(repo.entity_by_kind).sort((a, b) => b[1] - a[1]).map(([kind, count]) => (
              <Link
                key={kind}
                to={`/entities?repo_id=${id}&kind=${kind}`}
                className={`px-2.5 py-1 rounded-full text-xs font-medium ${kindColor(kind)}`}
              >
                {kind}: {count}
              </Link>
            ))}
          </div>
        </div>
      )}

      {/* Tabs */}
      <div className="border-b border-edge mb-4">
        <div className="flex gap-4 overflow-x-auto">
          {(["overview", "clusters", "flows", "chat", "quality", "history", "decisions", "settings"] as const).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`pb-2 text-sm font-medium border-b-2 transition-colors whitespace-nowrap ${
                tab === t ? "border-accent text-accent" : "border-transparent text-foreground-secondary hover:text-foreground"
              }`}
            >
              {t === "overview" ? "Overview" : t === "clusters" ? "Clusters" : t === "flows" ? "Flows" : t === "chat" ? "Chat" : t === "quality" ? "Quality" : t === "history" ? "History" : t === "decisions" ? `Decisions (${decisions.length})` : "Settings"}
            </button>
          ))}
        </div>
      </div>

      {tab === "overview" && (
        <div className="bg-surface-elevated rounded-lg border border-edge p-6">
          {repo.overview ? (
            <div className="prose prose-sm max-w-none">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{repo.overview}</ReactMarkdown>
            </div>
          ) : (
            <p className="text-sm text-foreground-secondary">No overview yet — run indexing to generate.</p>
          )}
        </div>
      )}

      {tab === "quality" && (
        <div className="bg-surface-elevated rounded-lg border border-edge p-4">
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
            {qualityItems.map((item) => (
              <div key={item.label}>
                <p className="text-xs text-foreground-secondary">{item.label}</p>
                <p className="text-lg font-semibold text-foreground">
                  {item.value != null ? `${Math.round(item.value)}%` : "—"}
                </p>
              </div>
            ))}
          </div>
        </div>
      )}

      {tab === "history" && (
        <div className="bg-surface-elevated rounded-lg border border-edge divide-y divide-edge">
          {runs.length === 0 ? (
            <p className="p-4 text-sm text-foreground-secondary">No indexing runs.</p>
          ) : (
            runs.map((run) => (
              <div key={run.id} className="p-4 text-sm">
                <div className="flex justify-between mb-1">
                  <span className="font-medium text-foreground">{run.mode}</span>
                  <span className="text-foreground-muted">{new Date(run.started_at).toLocaleString()}</span>
                </div>
                <div className="flex gap-3 text-xs text-foreground-secondary">
                  {run.files_analyzed != null && <span>{run.files_analyzed} files</span>}
                  {run.entities_created != null && <span>{run.entities_created} entities</span>}
                  {run.facts_created != null && <span>{run.facts_created} facts</span>}
                  {run.duration_ms != null && <span>{(run.duration_ms / 1000).toFixed(1)}s</span>}
                  {run.quality_overall != null && <span>Quality: {Math.round(run.quality_overall)}%</span>}
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {tab === "decisions" && (
        <div className="space-y-3">
          {decisions.length === 0 ? (
            <p className="text-sm text-foreground-secondary">No decisions recorded.</p>
          ) : (
            decisions.map((d) => (
              <div key={d.id} className="bg-surface-elevated rounded-lg border border-edge p-4">
                <h3 className="font-medium text-foreground">{d.summary}</h3>
                <p className="text-sm text-foreground-secondary mt-1">{d.description}</p>
                <p className="text-sm text-foreground-muted mt-2"><strong className="text-foreground-secondary">Rationale:</strong> {d.rationale}</p>
                {d.alternatives && d.alternatives.length > 0 && (
                  <div className="mt-2">
                    <p className="text-xs text-foreground-secondary font-medium">Alternatives considered:</p>
                    <ul className="list-disc list-inside text-sm text-foreground-secondary">
                      {d.alternatives.map((a, i) => (
                        <li key={i}>{a.description} — <span className="text-foreground-muted italic">{a.rejected_because}</span></li>
                      ))}
                    </ul>
                  </div>
                )}
                {d.tradeoffs && d.tradeoffs.length > 0 && (
                  <div className="mt-2">
                    <p className="text-xs text-foreground-secondary font-medium">Tradeoffs:</p>
                    <ul className="list-disc list-inside text-sm text-foreground-secondary">
                      {d.tradeoffs.map((t, i) => <li key={i}>{t}</li>)}
                    </ul>
                  </div>
                )}
                {d.provenance && d.provenance.length > 0 && (
                  <div className="mt-2 flex flex-wrap gap-2">
                    {d.provenance.map((p, i) => (
                      p.source_type === "pr" && p.url ? (
                        <a
                          key={i}
                          href={p.url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="inline-flex items-center gap-1 text-xs text-accent hover:underline"
                        >
                          {p.ref}{p.excerpt ? ` — ${p.excerpt}` : ""}
                        </a>
                      ) : (
                        <span key={i} className="text-xs text-foreground-muted">
                          {p.source_type}: {p.ref}
                        </span>
                      )
                    ))}
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      )}

      {tab === "clusters" && (
        <ClustersTab clusters={clusters} loading={clustersLoading} onEntityClick={setDrawerEntityId} />
      )}

      {tab === "flows" && (
        <FlowsTab flows={flows} loading={flowsLoading} onEntityClick={setDrawerEntityId} />
      )}

      {tab === "chat" && (
        <RepoChatTab repoId={id!} repoName={repo.name} />
      )}

      {tab === "settings" && (
        <SettingsTab
          repo={repo}
          onUpdated={(updated) => setRepo({ ...repo, ...updated })}
          onDeleted={() => navigate("/repos")}
          onReindex={() => handleReindex(true)}
          isIndexing={isRunning}
        />
      )}

      <EntityDrawer
        entityId={drawerEntityId}
        onClose={() => setDrawerEntityId(null)}
        onEntityClick={setDrawerEntityId}
      />
    </div>
  );
}

function IndexingBanner({ status, logs, onForceReindex }: { status: string; logs: string[]; onForceReindex: () => void }) {
  const isRunning = status === "running";
  const isFailed = status === "failed";

  const borderColor = isRunning ? "border-accent/30" : isFailed ? "border-syn-red/30" : "border-syn-green/30";
  const bgColor = isRunning ? "bg-accent/10" : isFailed ? "bg-syn-red/10" : "bg-syn-green/10";
  const iconColor = isRunning ? "text-accent" : isFailed ? "text-syn-red" : "text-syn-green";

  // Parse the latest Phase 2 progress line for the progress bar
  const phase2Progress = (() => {
    for (let i = logs.length - 1; i >= 0; i--) {
      const m = logs[i].match(/Phase 2: \[(\d+)\/(\d+)\]/);
      if (m) return { current: parseInt(m[1]), total: parseInt(m[2]) };
    }
    return null;
  })();

  // Extract ETA from latest log
  const eta = (() => {
    for (let i = logs.length - 1; i >= 0; i--) {
      const m = logs[i].match(/ETA (.+)/);
      if (m) return m[1];
    }
    return null;
  })();

  // Get the most recent log line as "current activity"
  const latestLog = logs.length > 0 ? logs[logs.length - 1] : "";

  return (
    <div className={`${bgColor} ${borderColor} border rounded-lg p-3 mb-6`}>
      <div className="flex items-center gap-2 mb-1">
        {isRunning ? (
          <Loader2 size={16} className={`${iconColor} animate-spin`} />
        ) : isFailed ? (
          <AlertTriangle size={16} className={iconColor} />
        ) : (
          <RefreshCw size={16} className={iconColor} />
        )}
        <span className={`text-sm font-medium ${iconColor}`}>
          {isRunning ? "Indexing in progress..." : isFailed ? "Indexing failed" : "Indexing complete"}
        </span>
        {eta && isRunning && (
          <span className="text-xs text-foreground-muted ml-auto">ETA {eta}</span>
        )}
        {isFailed && (
          <button
            onClick={onForceReindex}
            className="ml-auto text-xs text-accent hover:underline"
          >
            Retry
          </button>
        )}
      </div>

      {/* Progress bar for Phase 2 */}
      {phase2Progress && phase2Progress.total > 0 && isRunning && (
        <div className="mt-2 mb-1">
          <div className="flex items-center justify-between text-xs text-foreground-secondary mb-1">
            <span>Phase 2: File analysis</span>
            <span>{phase2Progress.current} / {phase2Progress.total} files</span>
          </div>
          <div className="w-full bg-surface-overlay rounded-full h-2">
            <div
              className="bg-accent h-2 rounded-full transition-all duration-500"
              style={{ width: `${Math.round((phase2Progress.current / phase2Progress.total) * 100)}%` }}
            />
          </div>
        </div>
      )}

      {/* Current activity */}
      {latestLog && (
        <p className="text-xs text-foreground-secondary font-mono mt-2 truncate">{latestLog}</p>
      )}

      {/* Expandable log history */}
      {logs.length > 1 && (
        <details className="mt-2">
          <summary className="text-xs text-foreground-muted cursor-pointer hover:text-foreground-secondary">
            Show all logs ({logs.length})
          </summary>
          <div className="mt-1 space-y-0.5 max-h-48 overflow-y-auto">
            {logs.map((log, i) => (
              <p key={i} className="text-xs text-foreground-muted font-mono">{log}</p>
            ))}
          </div>
        </details>
      )}
    </div>
  );
}

function SettingsTab({
  repo,
  onUpdated,
  onDeleted,
  onReindex,
  isIndexing,
}: {
  repo: RepoDetailType;
  onUpdated: (r: Partial<RepoDetailType>) => void;
  onDeleted: () => void;
  onReindex: () => void;
  isIndexing: boolean;
}) {
  const [name, setName] = useState(repo.name);
  const [dirs, setDirs] = useState<string[]>(repo.exclude_dirs || []);
  const [newDir, setNewDir] = useState("");
  const [saving, setSaving] = useState(false);
  const [reindexWarning, setReindexWarning] = useState(false);
  const [error, setError] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const addDir = () => {
    const d = newDir.trim();
    if (d && !dirs.includes(d)) {
      setDirs([...dirs, d]);
      setNewDir("");
    }
  };

  const removeDir = (dir: string) => {
    setDirs(dirs.filter((d) => d !== dir));
  };

  const handleSave = async () => {
    setError("");
    setSaving(true);
    setReindexWarning(false);
    try {
      const result = await api.updateRepo(repo.id, {
        name,
        exclude_dirs: dirs,
      });
      if (result.reindex_required) {
        setReindexWarning(true);
      }
      onUpdated({ name, exclude_dirs: dirs });
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await api.deleteRepo(repo.id);
      onDeleted();
    } catch (err: any) {
      setError(err.message);
      setDeleting(false);
    }
  };

  return (
    <div className="space-y-4">
      {error && <p className="text-sm text-syn-red bg-syn-red/10 rounded p-2">{error}</p>}

      {reindexWarning && (
        <div className="flex items-start gap-2 bg-syn-yellow/10 border border-syn-yellow/30 rounded-lg p-3">
          <AlertTriangle size={18} className="text-syn-yellow mt-0.5 shrink-0" />
          <div className="flex-1">
            <p className="text-sm font-medium text-syn-yellow">Re-index required</p>
            <p className="text-xs text-foreground-secondary mt-0.5">
              Exclude directories changed. Re-index with force to apply the new exclusions.
            </p>
          </div>
          <button
            onClick={onReindex}
            disabled={isIndexing}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-syn-yellow/20 text-syn-yellow font-medium rounded-md hover:bg-syn-yellow/30 disabled:opacity-50 transition-colors shrink-0"
          >
            {isIndexing ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
            {isIndexing ? "Indexing..." : "Re-index Now"}
          </button>
        </div>
      )}

      <div className="bg-surface-elevated rounded-lg border border-edge p-4 space-y-4">
        <div>
          <label className="block text-xs text-foreground-secondary mb-1">Repository Name</label>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full max-w-md border border-edge rounded px-2 py-1.5 text-sm bg-surface text-foreground focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>

        <div>
          <label className="block text-xs text-foreground-secondary mb-1">Exclude Directories</label>
          <div className="flex flex-wrap gap-2 mb-2">
            {dirs.map((dir) => (
              <span
                key={dir}
                className="inline-flex items-center gap-1 bg-surface-overlay text-foreground px-2 py-1 rounded text-xs font-mono"
              >
                {dir}
                <button onClick={() => removeDir(dir)} className="text-foreground-muted hover:text-syn-red">
                  <X size={12} />
                </button>
              </span>
            ))}
          </div>
          <div className="flex gap-2">
            <input
              value={newDir}
              onChange={(e) => setNewDir(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addDir(); } }}
              className="border border-edge rounded px-2 py-1.5 text-sm font-mono bg-surface text-foreground placeholder-foreground-muted focus:outline-none focus:ring-1 focus:ring-accent"
              placeholder="vendor"
            />
            <button
              type="button"
              onClick={addDir}
              className="px-3 py-1.5 text-sm bg-surface-overlay text-foreground rounded hover:bg-edge transition-colors"
            >
              Add
            </button>
          </div>
        </div>

        <div className="flex justify-end">
          <button
            onClick={handleSave}
            disabled={saving}
            className="px-4 py-1.5 bg-accent text-surface text-sm font-medium rounded-md hover:bg-accent-hover disabled:opacity-50 transition-colors"
          >
            {saving ? "Saving..." : "Save Changes"}
          </button>
        </div>
      </div>

      {/* Danger zone */}
      <div className="bg-surface-elevated rounded-lg border border-syn-red/30 p-4">
        <h3 className="text-sm font-semibold text-syn-red mb-2">Danger Zone</h3>
        <p className="text-xs text-foreground-secondary mb-3">
          Deleting this repository will permanently remove all entities, facts, relationships, decisions, and indexing history.
        </p>
        {!confirmDelete ? (
          <button
            onClick={() => setConfirmDelete(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-syn-red border border-syn-red/30 rounded hover:bg-syn-red/10 transition-colors"
          >
            <Trash2 size={14} /> Delete Repository
          </button>
        ) : (
          <div className="flex items-center gap-2">
            <button
              onClick={handleDelete}
              disabled={deleting}
              className="px-3 py-1.5 text-sm bg-syn-red text-white rounded hover:bg-syn-red/80 disabled:opacity-50 transition-colors"
            >
              {deleting ? "Deleting..." : "Confirm Delete"}
            </button>
            <button
              onClick={() => setConfirmDelete(false)}
              className="px-3 py-1.5 text-sm text-foreground-secondary hover:text-foreground"
            >
              Cancel
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function CountCard({ label, value }: { label: string; value: number }) {
  return (
    <div className="bg-surface-elevated rounded-lg border border-edge p-3 text-center">
      <p className="text-2xl font-bold text-foreground">{value.toLocaleString()}</p>
      <p className="text-xs text-foreground-secondary">{label}</p>
    </div>
  );
}

function kindColor(kind: string): string {
  const map: Record<string, string> = {
    module: "bg-syn-blue/15 text-syn-blue",
    service: "bg-syn-green/15 text-syn-green",
    function: "bg-syn-magenta/15 text-syn-magenta",
    type: "bg-syn-orange/15 text-syn-orange",
    endpoint: "bg-syn-red/15 text-syn-red",
    concept: "bg-surface-overlay text-foreground-secondary",
    config: "bg-syn-yellow/15 text-syn-yellow",
  };
  return map[kind] || "bg-surface-overlay text-foreground-secondary";
}
