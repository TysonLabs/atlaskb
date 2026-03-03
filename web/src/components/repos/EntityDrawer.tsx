import { useEffect, useState } from "react";
import { api } from "../../api/client";
import type { EntityDetail, Fact, Relationship } from "../../types";
import { X, Loader2, ChevronRight, ChevronLeft, Target } from "lucide-react";

const kindColor: Record<string, string> = {
  module: "bg-syn-blue/15 text-syn-blue",
  service: "bg-syn-green/15 text-syn-green",
  function: "bg-syn-magenta/15 text-syn-magenta",
  type: "bg-syn-orange/15 text-syn-orange",
  endpoint: "bg-syn-red/15 text-syn-red",
  concept: "bg-surface-overlay text-foreground-secondary",
  config: "bg-syn-yellow/15 text-syn-yellow",
  cluster: "bg-syn-cyan/15 text-syn-cyan",
};

interface Props {
  entityId: string | null;
  onClose: () => void;
  onEntityClick?: (id: string) => void;
  onFocusInGraph?: (id: string) => void;
}

export function EntityDrawer({ entityId, onClose, onEntityClick, onFocusInGraph }: Props) {
  const [entity, setEntity] = useState<EntityDetail | null>(null);
  const [facts, setFacts] = useState<Fact[]>([]);
  const [relationships, setRelationships] = useState<Relationship[]>([]);
  const [loading, setLoading] = useState(false);
  const [collapsed, setCollapsed] = useState<boolean>(false);

  useEffect(() => {
    if (!entityId) {
      setEntity(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    Promise.all([
      api.getEntity(entityId),
      api.getEntityFacts(entityId),
      api.getEntityRelationships(entityId),
    ])
      .then(([e, f, r]) => {
        if (cancelled) return;
        setEntity(e);
        setFacts(f);
        setRelationships(r);
      })
      .catch(console.error)
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [entityId]);

  if (!entityId) return null;

  const factsByCategory = facts.reduce<Record<string, Fact[]>>((acc, f) => {
    const cat = f.category || "general";
    (acc[cat] ||= []).push(f);
    return acc;
  }, {});

  if (collapsed) {
    return (
      <div className="fixed top-0 right-0 h-full w-12 bg-surface-elevated border-l border-edge shadow-2xl z-50 flex flex-col items-center animate-in slide-in-from-right">
        <button
          onClick={() => setCollapsed(false)}
          className="p-3 text-foreground-muted hover:text-foreground"
          title="Expand drawer"
        >
          <ChevronLeft size={16} />
        </button>
        <div className="flex-1 flex flex-col items-center justify-center overflow-hidden">
          <span
            className={`inline-block px-1.5 py-0.5 rounded text-xs font-medium ${kindColor[entity?.kind || ""] || kindColor.concept}`}
            style={{ writingMode: "vertical-rl", textOrientation: "mixed" }}
          >
            {entity?.kind || ""}
          </span>
          <span
            className="text-xs text-foreground font-medium mt-2 max-h-48 overflow-hidden"
            style={{ writingMode: "vertical-rl", textOrientation: "mixed" }}
          >
            {entity?.name || "Entity"}
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="fixed top-0 right-0 h-full w-96 bg-surface-elevated border-l border-edge shadow-2xl z-50 flex flex-col animate-in slide-in-from-right">
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-edge">
        <div className="flex items-center gap-2 min-w-0">
          <button
            onClick={() => setCollapsed(true)}
            className="text-foreground-muted hover:text-foreground shrink-0"
            title="Collapse drawer"
          >
            <ChevronRight size={16} />
          </button>
          <h3 className="text-sm font-semibold text-foreground truncate">
            {loading ? "Loading..." : entity?.name || "Entity"}
          </h3>
        </div>
        <div className="flex items-center gap-1 shrink-0">
          {onFocusInGraph && entityId && (
            <button
              onClick={() => onFocusInGraph(entityId)}
              className="text-foreground-muted hover:text-foreground"
              title="Focus in graph"
            >
              <Target size={16} />
            </button>
          )}
          <button onClick={onClose} className="text-foreground-muted hover:text-foreground">
            <X size={16} />
          </button>
        </div>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 size={24} className="animate-spin text-foreground-muted" />
          </div>
        ) : entity ? (
          <>
            {/* Kind badge + path */}
            <div className="space-y-2">
              <span className={`inline-block px-2 py-0.5 rounded-full text-xs font-medium ${kindColor[entity.kind] || kindColor.concept}`}>
                {entity.kind}
              </span>
              {entity.path && (
                <p className="text-xs text-foreground-muted font-mono">{entity.path}</p>
              )}
              <p className="text-xs text-foreground-secondary font-mono">{entity.qualified_name}</p>
            </div>

            {/* Summary */}
            {entity.summary && (
              <div>
                <h4 className="text-xs font-semibold text-foreground-secondary mb-1">Summary</h4>
                <p className="text-sm text-foreground">{entity.summary}</p>
              </div>
            )}

            {/* Capabilities */}
            {entity.capabilities && entity.capabilities.length > 0 && (
              <div>
                <h4 className="text-xs font-semibold text-foreground-secondary mb-1">Capabilities</h4>
                <ul className="space-y-0.5">
                  {entity.capabilities.map((c, i) => (
                    <li key={i} className="text-sm text-foreground-secondary flex items-start gap-1.5">
                      <span className="text-accent mt-0.5">-</span> {c}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {/* Facts by category */}
            {Object.keys(factsByCategory).length > 0 && (
              <div>
                <h4 className="text-xs font-semibold text-foreground-secondary mb-2">Facts</h4>
                {Object.entries(factsByCategory).map(([cat, catFacts]) => (
                  <div key={cat} className="mb-3">
                    <p className="text-xs font-medium text-foreground-muted uppercase tracking-wide mb-1">{cat}</p>
                    <div className="space-y-1">
                      {catFacts.map((f) => (
                        <div key={f.id} className="text-sm text-foreground-secondary bg-surface rounded px-2 py-1.5">
                          {f.claim}
                          <span className="text-xs text-foreground-muted ml-1">({f.confidence})</span>
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}

            {/* Relationships */}
            {relationships.length > 0 && (
              <div>
                <h4 className="text-xs font-semibold text-foreground-secondary mb-2">Relationships ({relationships.length})</h4>
                <div className="space-y-1">
                  {relationships.map((r) => {
                    const isOutgoing = r.from_entity_id === entity.id;
                    const otherName = isOutgoing ? r.to_entity_name : r.from_entity_name;
                    const otherId = isOutgoing ? r.to_entity_id : r.from_entity_id;
                    return (
                      <div key={r.id} className="flex items-center gap-1.5 text-sm">
                        <span className="text-foreground-muted text-xs w-20 text-right shrink-0">
                          {isOutgoing ? r.kind : `${r.kind} by`}
                        </span>
                        <span className="text-foreground-muted">{isOutgoing ? "\u2192" : "\u2190"}</span>
                        <button
                          onClick={() => onEntityClick?.(otherId)}
                          className="text-accent hover:underline truncate text-left"
                        >
                          {otherName || otherId.slice(0, 8)}
                        </button>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
          </>
        ) : (
          <p className="text-sm text-foreground-secondary">Entity not found.</p>
        )}
      </div>
    </div>
  );
}
