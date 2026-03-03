import { useState } from "react";
import type { ExecutionFlow } from "../../types";
import { Eye, GitBranch, Lightbulb, Search } from "lucide-react";
import { FlowModal } from "./FlowModal";

interface Props {
  flows: ExecutionFlow[];
  loading: boolean;
  onEntityClick: (id: string) => void;
  onHighlightEntities?: (ids: string[], source?: { type: "cluster" | "flow"; id: string }) => void;
  highlightSource?: { type: "cluster" | "flow"; id: string } | null;
}

export function FlowsTab({ flows, loading, onEntityClick, onHighlightEntities, highlightSource }: Props) {
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedFlow, setSelectedFlow] = useState<ExecutionFlow | null>(null);

  if (loading) {
    return <p className="text-sm text-foreground-secondary">Loading flows...</p>;
  }

  if (flows.length === 0) {
    return (
      <div className="bg-surface-elevated rounded-lg border border-edge p-6 text-center">
        <GitBranch size={32} className="mx-auto text-foreground-muted mb-2" />
        <p className="text-sm text-foreground-secondary">No execution flows detected — re-index to generate.</p>
      </div>
    );
  }

  const query = searchQuery.toLowerCase();
  const filtered = query
    ? flows.filter(
        (f) =>
          f.label.toLowerCase().includes(query) ||
          f.step_names.some((s) => s.toLowerCase().includes(query)),
      )
    : flows;

  return (
    <div className="space-y-4">
      {flows.length >= 3 && (
        <div className="space-y-1">
          <div className="relative">
            <Search size={14} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-foreground-muted" />
            <input
              type="text"
              placeholder="Filter flows by name or step..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full border border-edge rounded px-2 py-1.5 pl-8 text-sm bg-surface text-foreground placeholder-foreground-muted focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
          {searchQuery && (
            <p className="text-xs text-foreground-muted">
              Showing {filtered.length} of {flows.length} flows
            </p>
          )}
        </div>
      )}

      {filtered.map((flow) => {
        const isFocused = highlightSource?.type === "flow" && highlightSource?.id === flow.id;
        return (
        <div key={flow.id} className="bg-surface-elevated rounded-lg border border-edge p-4">
          <div className="flex items-center gap-2 mb-3">
            <GitBranch size={14} className="text-syn-cyan shrink-0" />
            <h3 className="font-medium text-foreground text-sm">{flow.label}</h3>
            <span className="text-xs text-foreground-muted ml-auto">
              {flow.step_names.length} steps &middot; depth {flow.depth}
            </span>
            <button
              onClick={() => setSelectedFlow(flow)}
              className="p-1 rounded-md transition-colors text-foreground-muted hover:text-foreground"
              title="View flow diagram"
            >
              <Eye size={16} />
            </button>
            {onHighlightEntities && (
              <button
                onClick={() => {
                  if (isFocused) {
                    onHighlightEntities([], undefined);
                  } else {
                    const highlightIds = [flow.entry_entity_id, ...flow.step_entity_ids];
                    onHighlightEntities(highlightIds, { type: "flow", id: flow.id });
                  }
                }}
                className={`p-1 rounded-md transition-colors ${isFocused ? "text-syn-yellow animate-pulse" : "text-foreground-muted hover:text-foreground"}`}
                title="Highlight in graph"
              >
                <Lightbulb size={16} />
              </button>
            )}
          </div>

          {/* Entry point */}
          {flow.entry_entity && (
            <div className="mb-3">
              <span className="text-xs text-foreground-muted">Entry: </span>
              <button
                onClick={() => onEntityClick(flow.entry_entity!.id)}
                className="text-sm text-syn-green hover:underline font-medium"
              >
                {flow.entry_entity.name}
              </button>
            </div>
          )}

          {/* Step chain */}
          <div className="flex flex-wrap items-center gap-1">
            {flow.step_names.map((name, i) => {
              const stepId = i < flow.step_entity_ids.length ? flow.step_entity_ids[i] : undefined;
              return (
                <div key={i} className="flex items-center gap-1">
                  {i > 0 && (
                    <span className="text-foreground-muted text-xs">{"\u2192"}</span>
                  )}
                  <button
                    onClick={() => stepId && onEntityClick(stepId)}
                    className={`text-xs px-2 py-1 rounded font-mono transition-colors ${
                      i === 0
                        ? "bg-syn-green/15 text-syn-green hover:bg-syn-green/25"
                        : "bg-surface-overlay text-foreground-secondary hover:bg-edge hover:text-foreground"
                    }`}
                  >
                    {name}
                  </button>
                </div>
              );
            })}
          </div>
        </div>
        );
      })}

      {selectedFlow && (
        <FlowModal
          flow={selectedFlow}
          onClose={() => setSelectedFlow(null)}
          onEntityClick={onEntityClick}
        />
      )}
    </div>
  );
}
