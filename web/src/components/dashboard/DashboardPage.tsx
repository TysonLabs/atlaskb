import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../../api/client";
import type { Stats, IndexingRun } from "../../types";
import { Database, Boxes, FileText, GitBranch, Lightbulb, Search } from "lucide-react";

export function DashboardPage() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [runs, setRuns] = useState<IndexingRun[]>([]);
  const [query, setQuery] = useState("");
  const navigate = useNavigate();

  useEffect(() => {
    api.getStats().then(setStats).catch(console.error);
    api.getRecentRuns().then(setRuns).catch(console.error);
  }, []);

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    if (query.trim()) {
      navigate(`/ask?q=${encodeURIComponent(query.trim())}`);
    }
  };

  return (
    <div>
      <h1 className="text-2xl font-bold text-foreground mb-6">Dashboard</h1>

      {/* Quick Search */}
      <form onSubmit={handleSearch} className="mb-6">
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-foreground-muted" size={18} />
          <input
            type="text"
            placeholder="Ask a question about your codebase..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="w-full pl-10 pr-4 py-3 border border-edge rounded-lg bg-surface-elevated text-foreground placeholder-foreground-muted focus:outline-none focus:ring-2 focus:ring-accent focus:border-transparent text-sm"
          />
        </div>
      </form>

      {/* Stat Cards */}
      {stats && (
        <div className="grid grid-cols-2 md:grid-cols-5 gap-4 mb-8">
          <StatCard icon={Database} label="Repos" value={stats.repos} color="blue" />
          <StatCard icon={Boxes} label="Entities" value={stats.entities} color="green" />
          <StatCard icon={FileText} label="Facts" value={stats.facts} color="magenta" />
          <StatCard icon={GitBranch} label="Relationships" value={stats.relationships} color="orange" />
          <StatCard icon={Lightbulb} label="Decisions" value={stats.decisions} color="cyan" />
        </div>
      )}

      {/* Recent Indexing Runs */}
      <div className="bg-surface-elevated rounded-lg border border-edge">
        <div className="px-4 py-3 border-b border-edge">
          <h2 className="font-semibold text-foreground">Recent Indexing Runs</h2>
        </div>
        {runs.length === 0 ? (
          <p className="p-4 text-sm text-foreground-secondary">No indexing runs yet. Run <code className="bg-surface-overlay px-1 rounded text-syn-yellow">atlaskb index</code> to get started.</p>
        ) : (
          <div className="divide-y divide-edge">
            {runs.map((run) => (
              <div key={run.id} className="px-4 py-3 flex items-center justify-between text-sm">
                <div>
                  <Link to={`/repos/${run.repo_id}`} className="font-medium text-accent hover:underline">
                    {run.repo_name || run.repo_id.slice(0, 8)}
                  </Link>
                  <span className="text-foreground-secondary ml-2">{run.mode}</span>
                </div>
                <div className="flex items-center gap-4">
                  {run.quality_overall != null && (
                    <QualityBadge score={run.quality_overall} />
                  )}
                  <span className="text-foreground-muted text-xs">
                    {new Date(run.started_at).toLocaleDateString()}
                  </span>
                  {run.duration_ms != null && (
                    <span className="text-foreground-muted text-xs">
                      {(run.duration_ms / 1000).toFixed(1)}s
                    </span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

const colorMap: Record<string, string> = {
  blue: "bg-syn-blue/15 text-syn-blue",
  green: "bg-syn-green/15 text-syn-green",
  magenta: "bg-syn-magenta/15 text-syn-magenta",
  orange: "bg-syn-orange/15 text-syn-orange",
  cyan: "bg-syn-cyan/15 text-syn-cyan",
};

function StatCard({ icon: Icon, label, value, color }: { icon: React.ElementType; label: string; value: number; color: string }) {
  return (
    <div className={`rounded-lg p-4 border border-edge ${colorMap[color] || "bg-surface-elevated"}`}>
      <div className="flex items-center gap-2 mb-1">
        <Icon size={16} />
        <span className="text-xs font-medium opacity-75">{label}</span>
      </div>
      <p className="text-2xl font-bold">{value.toLocaleString()}</p>
    </div>
  );
}

function QualityBadge({ score }: { score: number }) {
  const pct = Math.round(score);
  const color = pct >= 70 ? "bg-syn-green/15 text-syn-green" : pct >= 40 ? "bg-syn-yellow/15 text-syn-yellow" : "bg-syn-red/15 text-syn-red";
  return <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${color}`}>{pct}%</span>;
}
