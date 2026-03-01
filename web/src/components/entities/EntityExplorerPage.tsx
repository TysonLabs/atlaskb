import { useEffect, useState, useCallback } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api } from "../../api/client";
import type { Entity, RepoListItem } from "../../types";
import { useDebounce } from "../../hooks/useDebounce";
import { Search, ChevronLeft, ChevronRight } from "lucide-react";

const ENTITY_KINDS = ["module", "service", "function", "type", "endpoint", "concept", "config"];
const PAGE_SIZE = 50;

export function EntityExplorerPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [entities, setEntities] = useState<Entity[]>([]);
  const [total, setTotal] = useState(0);
  const [repos, setRepos] = useState<RepoListItem[]>([]);
  const [loading, setLoading] = useState(true);

  const query = searchParams.get("q") || "";
  const kind = searchParams.get("kind") || "";
  const repoId = searchParams.get("repo_id") || "";
  const page = parseInt(searchParams.get("page") || "1", 10);

  const debouncedQuery = useDebounce(query, 300);

  const updateParam = useCallback((key: string, value: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (value) next.set(key, value);
      else next.delete(key);
      if (key !== "page") next.delete("page");
      return next;
    });
  }, [setSearchParams]);

  useEffect(() => {
    api.listRepos().then(setRepos).catch(console.error);
  }, []);

  useEffect(() => {
    setLoading(true);
    api
      .listEntities({
        q: debouncedQuery || undefined,
        kind: kind || undefined,
        repo_id: repoId || undefined,
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
      })
      .then((res) => {
        setEntities(res.items);
        setTotal(res.total);
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  }, [debouncedQuery, kind, repoId, page]);

  const totalPages = Math.ceil(total / PAGE_SIZE);

  return (
    <div>
      <h1 className="text-2xl font-bold text-foreground mb-6">Entity Explorer</h1>

      {/* Filters */}
      <div className="flex flex-wrap gap-3 mb-4">
        <div className="relative flex-1 min-w-[200px]">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-foreground-muted" size={16} />
          <input
            type="text"
            placeholder="Search entities..."
            value={query}
            onChange={(e) => updateParam("q", e.target.value)}
            className="w-full pl-9 pr-4 py-2 border border-edge rounded-md text-sm bg-surface-elevated text-foreground placeholder-foreground-muted focus:outline-none focus:ring-2 focus:ring-accent"
          />
        </div>
        <select
          value={kind}
          onChange={(e) => updateParam("kind", e.target.value)}
          className="border border-edge rounded-md px-3 py-2 text-sm bg-surface-elevated text-foreground"
        >
          <option value="">All kinds</option>
          {ENTITY_KINDS.map((k) => (
            <option key={k} value={k}>{k}</option>
          ))}
        </select>
        <select
          value={repoId}
          onChange={(e) => updateParam("repo_id", e.target.value)}
          className="border border-edge rounded-md px-3 py-2 text-sm bg-surface-elevated text-foreground"
        >
          <option value="">All repos</option>
          {repos.map((r) => (
            <option key={r.id} value={r.id}>{r.name}</option>
          ))}
        </select>
      </div>

      {/* Results */}
      <div className="bg-surface-elevated rounded-lg border border-edge">
        <div className="px-4 py-2 border-b border-edge flex items-center justify-between">
          <span className="text-sm text-foreground-secondary">{total.toLocaleString()} entities</span>
        </div>

        {loading ? (
          <p className="p-4 text-sm text-foreground-secondary">Loading...</p>
        ) : entities.length === 0 ? (
          <p className="p-4 text-sm text-foreground-secondary">No entities found.</p>
        ) : (
          <div className="divide-y divide-edge">
            {entities.map((entity) => (
              <Link
                key={entity.id}
                to={`/entities/${entity.id}`}
                className="block px-4 py-3 hover:bg-surface-overlay/50 transition-colors"
              >
                <div className="flex items-center gap-2">
                  <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${kindColor(entity.kind)}`}>
                    {entity.kind}
                  </span>
                  <span className="font-medium text-sm text-foreground">{entity.name}</span>
                  <span className="text-xs text-foreground-muted">{entity.qualified_name}</span>
                </div>
                {entity.summary && (
                  <p className="text-xs text-foreground-secondary mt-1 line-clamp-1">{entity.summary}</p>
                )}
                {entity.path && (
                  <p className="text-xs text-foreground-muted mt-0.5 font-mono">{entity.path}</p>
                )}
              </Link>
            ))}
          </div>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="px-4 py-3 border-t border-edge flex items-center justify-between">
            <button
              onClick={() => updateParam("page", String(page - 1))}
              disabled={page <= 1}
              className="flex items-center gap-1 text-sm text-foreground-secondary disabled:opacity-30"
            >
              <ChevronLeft size={16} /> Previous
            </button>
            <span className="text-sm text-foreground-secondary">Page {page} of {totalPages}</span>
            <button
              onClick={() => updateParam("page", String(page + 1))}
              disabled={page >= totalPages}
              className="flex items-center gap-1 text-sm text-foreground-secondary disabled:opacity-30"
            >
              Next <ChevronRight size={16} />
            </button>
          </div>
        )}
      </div>
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
