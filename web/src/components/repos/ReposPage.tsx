import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type { RepoListItem } from "../../types";
import { FolderGit2, Plus, X } from "lucide-react";

export function ReposPage() {
  const [repos, setRepos] = useState<RepoListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);

  const loadRepos = () => {
    setLoading(true);
    api.listRepos().then(setRepos).catch(console.error).finally(() => setLoading(false));
  };

  useEffect(() => { loadRepos(); }, []);

  if (loading) return <p className="text-foreground-secondary">Loading repos...</p>;

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold text-foreground">Repositories</h1>
        <button
          onClick={() => setShowForm(true)}
          className="flex items-center gap-1.5 px-3 py-1.5 bg-accent text-surface text-sm font-medium rounded-md hover:bg-accent-hover transition-colors"
        >
          <Plus size={16} /> Register Repo
        </button>
      </div>

      {showForm && (
        <RegisterRepoForm
          onCreated={() => { setShowForm(false); loadRepos(); }}
          onCancel={() => setShowForm(false)}
        />
      )}

      {repos.length === 0 ? (
        <p className="text-foreground-secondary">No repositories indexed yet.</p>
      ) : (
        <div className="grid gap-4">
          {repos.map((repo) => (
            <Link
              key={repo.id}
              to={`/repos/${repo.id}`}
              className="bg-surface-elevated rounded-lg border border-edge p-4 hover:border-accent/50 transition-colors"
            >
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-2">
                  <FolderGit2 size={18} className="text-foreground-muted" />
                  <h2 className="font-semibold text-foreground">{repo.name}</h2>
                </div>
                {repo.quality_overall != null && (
                  <QualityBadge score={repo.quality_overall} />
                )}
              </div>
              <p className="text-xs text-foreground-muted mt-1 font-mono">{repo.local_path}</p>
              <div className="flex gap-4 mt-3 text-xs text-foreground-secondary">
                <span>{repo.entity_count} entities</span>
                <span>{repo.fact_count} facts</span>
                <span>{repo.relationship_count} relationships</span>
                <span>{repo.decision_count} decisions</span>
              </div>
              {repo.last_indexed_at && (
                <p className="text-xs text-foreground-muted mt-2">
                  Last indexed: {new Date(repo.last_indexed_at).toLocaleString()}
                </p>
              )}
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

function RegisterRepoForm({ onCreated, onCancel }: { onCreated: () => void; onCancel: () => void }) {
  const [name, setName] = useState("");
  const [localPath, setLocalPath] = useState("");
  const [branch, setBranch] = useState("main");
  const [excludeDirs, setExcludeDirs] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setSaving(true);
    try {
      const dirs = excludeDirs
        .split(",")
        .map((d) => d.trim())
        .filter(Boolean);
      await api.createRepo({
        name,
        local_path: localPath,
        default_branch: branch || "main",
        exclude_dirs: dirs,
      });
      onCreated();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="bg-surface-elevated rounded-lg border border-edge p-4 mb-6 space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="font-semibold text-foreground">Register Repository</h3>
        <button type="button" onClick={onCancel} className="text-foreground-muted hover:text-foreground">
          <X size={18} />
        </button>
      </div>
      {error && <p className="text-sm text-syn-red bg-syn-red/10 rounded p-2">{error}</p>}
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs text-foreground-secondary mb-1">Name</label>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            className="w-full border border-edge rounded px-2 py-1.5 text-sm bg-surface text-foreground placeholder-foreground-muted focus:outline-none focus:ring-1 focus:ring-accent"
            placeholder="my-service"
          />
        </div>
        <div>
          <label className="block text-xs text-foreground-secondary mb-1">Default Branch</label>
          <input
            value={branch}
            onChange={(e) => setBranch(e.target.value)}
            className="w-full border border-edge rounded px-2 py-1.5 text-sm bg-surface text-foreground placeholder-foreground-muted focus:outline-none focus:ring-1 focus:ring-accent"
            placeholder="main"
          />
        </div>
      </div>
      <div>
        <label className="block text-xs text-foreground-secondary mb-1">Local Path</label>
        <input
          value={localPath}
          onChange={(e) => setLocalPath(e.target.value)}
          required
          className="w-full border border-edge rounded px-2 py-1.5 text-sm font-mono bg-surface text-foreground placeholder-foreground-muted focus:outline-none focus:ring-1 focus:ring-accent"
          placeholder="/path/to/repo"
        />
      </div>
      <div>
        <label className="block text-xs text-foreground-secondary mb-1">Exclude Directories (comma-separated)</label>
        <input
          value={excludeDirs}
          onChange={(e) => setExcludeDirs(e.target.value)}
          className="w-full border border-edge rounded px-2 py-1.5 text-sm font-mono bg-surface text-foreground placeholder-foreground-muted focus:outline-none focus:ring-1 focus:ring-accent"
          placeholder="vendor, node_modules, dist"
        />
      </div>
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="px-3 py-1.5 text-sm text-foreground-secondary hover:text-foreground">
          Cancel
        </button>
        <button
          type="submit"
          disabled={saving}
          className="px-3 py-1.5 bg-accent text-surface text-sm font-medium rounded-md hover:bg-accent-hover disabled:opacity-50 transition-colors"
        >
          {saving ? "Registering..." : "Register"}
        </button>
      </div>
    </form>
  );
}

function QualityBadge({ score }: { score: number }) {
  const pct = Math.round(score);
  const color = pct >= 70 ? "bg-syn-green/15 text-syn-green" : pct >= 40 ? "bg-syn-yellow/15 text-syn-yellow" : "bg-syn-red/15 text-syn-red";
  return <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${color}`}>{pct}%</span>;
}
