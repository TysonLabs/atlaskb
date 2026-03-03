import { useState } from "react";
import type { FunctionalCluster } from "../../types";
import { ChevronDown, ChevronRight, Search, Users } from "lucide-react";

const kindColor: Record<string, string> = {
  module: "bg-syn-blue/15 text-syn-blue",
  service: "bg-syn-green/15 text-syn-green",
  function: "bg-syn-magenta/15 text-syn-magenta",
  type: "bg-syn-orange/15 text-syn-orange",
  endpoint: "bg-syn-red/15 text-syn-red",
  concept: "bg-surface-overlay text-foreground-secondary",
  config: "bg-syn-yellow/15 text-syn-yellow",
};

interface Props {
  clusters: FunctionalCluster[];
  loading: boolean;
  onEntityClick: (id: string) => void;
}

export function ClustersTab({ clusters, loading, onEntityClick }: Props) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [searchQuery, setSearchQuery] = useState("");

  if (loading) {
    return <p className="text-sm text-foreground-secondary">Loading clusters...</p>;
  }

  if (clusters.length === 0) {
    return (
      <div className="bg-surface-elevated rounded-lg border border-edge p-6 text-center">
        <Users size={32} className="mx-auto text-foreground-muted mb-2" />
        <p className="text-sm text-foreground-secondary">No clusters detected — re-index to generate.</p>
      </div>
    );
  }

  const toggle = (id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const query = searchQuery.toLowerCase();
  const filtered = query
    ? clusters.filter(
        (c) =>
          c.name.toLowerCase().includes(query) ||
          (c.summary && c.summary.toLowerCase().includes(query)) ||
          c.members.some((m) => m.name.toLowerCase().includes(query)),
      )
    : clusters;

  return (
    <div className="space-y-3">
      {clusters.length >= 3 && (
        <div className="space-y-1">
          <div className="relative">
            <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-foreground-muted" />
            <input
              type="text"
              placeholder="Filter clusters by name, summary, or member..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full border border-edge rounded px-2 py-1.5 pl-8 text-sm bg-surface text-foreground placeholder-foreground-muted focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
          {searchQuery && (
            <p className="text-xs text-foreground-muted">
              Showing {filtered.length} of {clusters.length} clusters
            </p>
          )}
        </div>
      )}

      {filtered.map((cluster) => {
        const isExpanded = expanded.has(cluster.id);
        return (
          <div key={cluster.id} className="bg-surface-elevated rounded-lg border border-edge">
            <button
              onClick={() => toggle(cluster.id)}
              className="w-full flex items-center gap-3 p-4 text-left hover:bg-surface-overlay/50 transition-colors"
            >
              {isExpanded ? <ChevronDown size={16} className="text-foreground-muted shrink-0" /> : <ChevronRight size={16} className="text-foreground-muted shrink-0" />}
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-foreground truncate">{cluster.name}</span>
                  <span className="text-xs bg-accent/15 text-accent px-1.5 py-0.5 rounded-full shrink-0">
                    {cluster.members.length} members
                  </span>
                </div>
                {cluster.summary && (
                  <p className="text-sm text-foreground-secondary mt-0.5 truncate">{cluster.summary}</p>
                )}
              </div>
            </button>

            {isExpanded && (
              <div className="border-t border-edge px-4 py-3">
                {cluster.capabilities && cluster.capabilities.length > 0 && (
                  <div className="mb-3">
                    <p className="text-xs font-semibold text-foreground-secondary mb-1">Capabilities</p>
                    <div className="flex flex-wrap gap-1.5">
                      {cluster.capabilities.map((c, i) => (
                        <span key={i} className="text-xs bg-surface-overlay text-foreground-secondary px-2 py-0.5 rounded">
                          {c}
                        </span>
                      ))}
                    </div>
                  </div>
                )}
                <p className="text-xs font-semibold text-foreground-secondary mb-2">Members</p>
                <div className="space-y-1.5">
                  {cluster.members.map((member) => (
                    <button
                      key={member.id}
                      onClick={() => onEntityClick(member.id)}
                      className="group w-full flex items-center gap-2 text-left px-2 py-1.5 rounded hover:bg-surface-overlay transition-colors"
                    >
                      <span className={`text-xs px-1.5 py-0.5 rounded-full font-medium shrink-0 ${kindColor[member.kind] || kindColor.concept}`}>
                        {member.kind}
                      </span>
                      <span className="text-sm text-accent hover:underline truncate">{member.name}</span>
                      {member.summary && (
                        <span className="text-xs text-foreground-muted truncate ml-auto">{member.summary}</span>
                      )}
                      <ChevronRight size={14} className="opacity-0 group-hover:opacity-100 text-foreground-muted transition-opacity shrink-0" />
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
